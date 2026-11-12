package gorun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/ssh"

	"github.com/jenmud/gorun/internal/envvars"
)

// Exit codes for tasks that didn't exit by themselves.
const (
	exitError       = 2   // the task couldn't be run, or failed without an exit code
	exitTimeout     = 124 // the task's timeout expired, as timeout(1) reports it
	exitInterrupted = 130 // the run was cancelled, as a shell reports SIGINT
)

// TaskExec runs one task on one server, and holds the result of its latest attempt.
type TaskExec struct {
	task      Task
	server    *ssh.Client // nil runs the task on this machine
	target    *Server     // the server templates are rendered for
	logger    *slog.Logger
	progress  CopyProgressFunc // when set, is told how far each copy has got
	onPhase   func(Phase)      // when set, is told as each phase of an attempt starts
	force     bool             // skip the check and always run the command
	checkOnly bool             // run only the check: never the copies, command or verify

	command   string // the command as written, with templates rendered
	startedAt time.Time
	stdout    []byte
	stderr    []byte
	exitCode  int
	phase     Phase       // the phase running, or the one that decided the outcome
	check     *StepResult // the check's result, when it ran
	verify    *StepResult // the verify's result, when it ran
	unchanged bool        // the check found nothing to do
	wouldRun  bool        // a check-only run found the task has something to do
	// stopped is set when the task stopped because its context was canceled or
	// its timeout expired. Such attempts are not retried.
	stopped bool
}

// NewLocalTaskExec creates TaskExecs that run on the local machine (no SSH).
func NewLocalTaskExec(task ...Task) ([]*TaskExec, error) {
	return NewTaskExec(nil, task...)
}

// NewTaskExec validates the tasks and prepares each to run over server, or on this machine
// when server is nil.
func NewTaskExec(server *ssh.Client, task ...Task) ([]*TaskExec, error) {
	user, session := envvars.Get("USER"), "localhost"
	if server != nil {
		user, session = server.User(), server.RemoteAddr().String()
	}

	tasks := make([]*TaskExec, len(task))
	for i, t := range task {
		// Config.Run renders templated settings for each server while planning; a task
		// handed straight here is rendered for the default target instead.
		target := &Server{Name: "localhost"}
		t, err := t.ForServer(target)
		if err != nil {
			return nil, err
		}
		if err := t.validate(); err != nil {
			return nil, err
		}
		tasks[i] = &TaskExec{
			task:   t,
			server: server,
			target: target,
			logger: NewLogger(t.LogLevel, logOutput, false).With(
				slog.String("name", t.Name),
				slog.String("shell", t.Shell),
				slog.Bool("sudo", t.Sudo),
				slog.String("cmd_file", t.CmdFile),
				slog.Any("depends_on", t.DependsOn),
				slog.Any("envvars", envNames(t.EnvVars)), // names only, the values can be secrets
				slog.Group("server", slog.String("user", user), slog.String("session", session)),
			),
		}
	}
	return tasks, nil
}

// Execute runs the task within its timeout: its check, then its copies and command, then its
// verify.
func (t *TaskExec) Execute(ctx context.Context) error {
	return t.execute(ctx, t.server == nil)
}

// LocalExecute runs the task on this machine.
func (t *TaskExec) LocalExecute(ctx context.Context) error {
	return t.execute(ctx, true)
}

// execute runs one attempt, here or over SSH, and records its result and metrics.
func (t *TaskExec) execute(ctx context.Context, local bool) error {
	ctx, span := tracer.Start(ctx, "TaskExec.Execute")
	defer span.End()
	span.SetAttributes(
		attribute.String("task.name", t.task.Name),
		attribute.String("server.name", t.target.Name),
		attribute.Bool("task.sudo", t.task.Sudo),
	)

	ctx, cancel := context.WithTimeout(ctx, t.task.TimeoutDuration())
	defer cancel()

	t.startedAt = time.Now()
	if local {
		t.logger.InfoContext(ctx, "running local task")
	} else {
		t.logger.InfoContext(ctx, "running task")
	}
	err := t.finish(ctx, t.phases(ctx, local))

	status := "ok"
	if t.exitCode != 0 {
		status = "error"
	}
	attrs := metric.WithAttributes(
		attribute.String("task.name", t.task.Name),
		attribute.String("server.name", t.target.Name),
		attribute.String("task.status", status),
	)
	taskExecutions.Add(ctx, 1, attrs)
	taskDuration.Record(ctx, time.Since(t.startedAt).Seconds(), attrs)
	return err
}

// phases runs the task's check, its copies and command, and its verify, stopping at the first
// that fails. A check that exits 0 ends the attempt early, with the task unchanged.
func (t *TaskExec) phases(ctx context.Context, local bool) error {
	run := t.runRemote
	if local {
		run = t.runLocal
	}

	if t.task.hasCheck() && !t.force {
		t.enter(PhaseCheck)
		changed, err := t.runCheck(ctx, run)
		if err != nil {
			return fmt.Errorf("check failed: %w", err)
		}
		if !changed {
			t.unchanged = true
			return nil
		}
	}
	if t.checkOnly {
		t.wouldRun = true
		return nil
	}

	t.enter(PhaseRun)
	command, err := "", error(nil)
	if t.task.hasCommand() {
		if command, err = t.task.CommandFor(t.target); err != nil {
			return err
		}
	}
	// record the command as written (templates rendered), not the shell, sudo and envvars
	// wrapping, so history and events show what the user wrote
	t.command = command
	t.stdout, t.stderr, err = run(ctx, command, true)
	if err != nil {
		return err
	}

	if t.task.Verify != "" {
		t.enter(PhaseVerify)
		if t.verify, err = t.step(ctx, run, t.task.Verify); err != nil {
			return fmt.Errorf("verify failed: %w", err)
		}
	}
	return nil
}

// runCheck runs the task's copy comparison and check command, and reports whether there is
// anything to do. Any exit code but 0 means yes; a check that couldn't run is an error. Both
// results go into one check result.
func (t *TaskExec) runCheck(ctx context.Context, run runFunc) (bool, error) {
	if t.task.CheckCopies {
		res, err := t.compareCopies(ctx, run)
		if err != nil {
			return false, err
		}
		t.check = res
		if res.ExitCode != 0 || t.task.Check == "" {
			return res.ExitCode != 0, nil
		}
	}

	res, err := t.step(ctx, run, t.task.Check)
	if copies := t.check; copies != nil {
		res.Command = copies.Command + "; " + res.Command
		res.Stdout = copies.Stdout + res.Stdout
		res.Stderr = copies.Stderr + res.Stderr
		res.Duration += copies.Duration
	}
	t.check = res
	switch {
	case err == nil:
		return false, nil
	case hasExitStatus(err) && ctx.Err() == nil:
		return true, nil
	}
	return false, err
}

// enter starts a phase, and tells onPhase.
func (t *TaskExec) enter(phase Phase) {
	t.phase = phase
	if t.onPhase != nil {
		t.onPhase(phase)
	}
}

// step runs a check or verify command and returns its result, and the error a failing exit
// code gives.
func (t *TaskExec) step(ctx context.Context, run runFunc, command string) (*StepResult, error) {
	started := time.Now()
	stdout, stderr, err := run(ctx, command, false)
	res := &StepResult{Command: command, ExitCode: exitCode(err), Stdout: string(stdout), Stderr: string(stderr), Duration: time.Since(started)}
	return res, err
}

// exitCode is the exit status err reports, 0 for nil, or exitError when it reports none.
func exitCode(err error) int {
	var sshErr *ssh.ExitError
	var execErr *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &sshErr):
		return sshErr.ExitStatus()
	case errors.As(err, &execErr):
		return execErr.ExitCode()
	}
	return exitError
}

// hasExitStatus reports whether err is a command exiting with a status, as opposed to one
// that couldn't be run at all.
func hasExitStatus(err error) bool {
	var sshErr *ssh.ExitError
	var execErr *exec.ExitError
	return errors.As(err, &sshErr) || errors.As(err, &execErr)
}

// runFunc runs command where the task runs, after the task's copies when copies is set, and
// returns its output. An empty command only copies.
type runFunc func(ctx context.Context, command string, copies bool) (stdout, stderr []byte, err error)

// runLocal runs a command on this machine.
func (t *TaskExec) runLocal(ctx context.Context, command string, copies bool) ([]byte, []byte, error) {
	if copies {
		if err := CopyFilesContext(ctx, t.server, t.target, &t.task, t.progress); err != nil {
			return nil, nil, err
		}
	}
	if command == "" {
		return nil, nil, nil
	}
	stdin, err := envStdin(t.task.EnvVars)
	if err != nil {
		return nil, nil, err
	}

	name, args := t.task.localArgs(command, stdin != nil)
	cmd := exec.CommandContext(ctx, name, args...)
	configureCommand(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, &stdout, &stderr
	err = cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// runRemote runs a command over SSH. When ctx ends first, the command is told to stop and the
// connection is closed, so a stuck task can't hold up the run.
func (t *TaskExec) runRemote(ctx context.Context, command string, copies bool) ([]byte, []byte, error) {
	const failed = "error running the task on the remote server: %w"

	var stdin io.Reader
	if command != "" {
		var err error
		if stdin, err = envStdin(t.task.EnvVars); err != nil {
			return nil, nil, err
		}
	}
	switch {
	case command == "":
	case t.task.Sudo && stdin != nil:
		command = sudoWrap(envPrelude + command)
	case t.task.Sudo:
		command = sudoWrap(command)
	case stdin != nil:
		// run the prelude in sh, whatever the remote user's login shell is
		command = shWrap(envPrelude + command)
	}

	type result struct {
		stdout, stderr []byte
		err            error
	}
	done := make(chan result, 1)

	// The command's session is only opened once the files are copied, so a task
	// holds at most one of the connection's session slots at a time and tasks
	// can't deadlock waiting on each other's slots.
	var (
		sessionMu sync.Mutex
		session   *ssh.Session
	)
	go func() {
		if copies {
			if err := CopyFilesContext(ctx, t.server, t.target, &t.task, t.progress); err != nil {
				done <- result{err: fmt.Errorf(failed, err)}
				return
			}
		}
		if command == "" { // a copy-only task
			done <- result{}
			return
		}
		s, closeSession, err := newSession(ctx, t.server)
		if err != nil {
			done <- result{err: fmt.Errorf("error creating SSH session: %w", err)}
			return
		}
		defer closeSession()

		var stdout, stderr bytes.Buffer
		s.Stdin, s.Stdout, s.Stderr = stdin, &stdout, &stderr
		sessionMu.Lock()
		session = s
		sessionMu.Unlock()

		if err = s.Run(command); err != nil {
			err = fmt.Errorf(failed, err)
		}
		done <- result{stdout.Bytes(), stderr.Bytes(), err}
	}()

	select {
	case res := <-done:
		return res.stdout, res.stderr, res.err
	case <-ctx.Done():
	}

	// The command may have finished at the same moment the deadline hit.
	// Prefer its result over reporting a timeout.
	select {
	case res := <-done:
		return res.stdout, res.stderr, res.err
	default:
	}

	// Ask the remote command to stop. Closing the client also unblocks a
	// stuck copy or command, so this returns promptly.
	sessionMu.Lock()
	if session != nil {
		_ = session.Signal(ssh.SIGTERM)
	}
	sessionMu.Unlock()
	_ = t.server.Close()

	select {
	case res := <-done:
		return res.stdout, res.stderr, ctx.Err()
	case <-time.After(taskKillGrace):
		// The connection didn't close in time; don't hold up the whole run.
	}
	return nil, nil, ctx.Err()
}

// finish records how an attempt ended: its exit code, and for a failure the reason, which it
// returns. A task stopped by its timeout or a cancelled run gets the reason in its stderr too,
// so it shows wherever the output does.
func (t *TaskExec) finish(ctx context.Context, err error) error {
	span := trace.SpanFromContext(ctx)
	output := []any{slog.String("stderr", string(t.stderr)), slog.String("stdout", string(t.stdout))}
	if err == nil {
		t.exitCode = 0
		span.SetAttributes(attribute.Int("task.exit_code", 0))
		span.SetStatus(codes.Ok, "")
		t.logger.DebugContext(ctx, "task completed", append(output, slog.Int("exitcode", 0))...)
		return nil
	}

	ctxErr := ctx.Err()
	t.stopped = ctxErr != nil
	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		t.exitCode = exitTimeout
		err = fmt.Errorf("task %q timed out after %s", t.task.Name, t.task.TimeoutDuration())
	case ctxErr != nil:
		t.exitCode = exitInterrupted
		err = fmt.Errorf("task %q was interrupted", t.task.Name)
	case errors.Is(err, exec.ErrWaitDelay):
		t.exitCode = exitError
		err = fmt.Errorf("task %q finished but left a background process holding its output open", t.task.Name)
	default:
		t.exitCode = exitCode(err)
	}

	level, msg := slog.LevelError, "task failed"
	switch {
	case t.stopped:
		t.appendStderr(err.Error())
		level, msg = slog.LevelWarn, "task terminated"
	case t.phase == PhaseCheck || t.phase == PhaseVerify:
		// the command's output stays as it was; the step's reason is added so it shows too
		t.appendStderr(stepFailure(t.phase, err, t.check, t.verify))
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	span.SetAttributes(attribute.Int("task.exit_code", t.exitCode))
	t.logger.Log(ctx, level, msg, append(output, slog.Int("exitcode", t.exitCode), slog.String("reason", err.Error()))...)
	return err
}

// stepFailure explains a failed check or verify, with the end of its output.
func stepFailure(phase Phase, err error, check, verify *StepResult) string {
	res := check
	if phase == PhaseVerify {
		res = verify
	}
	msg := err.Error()
	if res != nil {
		if out := strings.TrimSpace(res.Stderr + "\n" + res.Stdout); out != "" {
			lines := strings.Split(out, "\n")
			msg += ": " + strings.TrimSpace(lines[len(lines)-1])
		}
	}
	return msg
}

// resetForRetry clears the result of the previous attempt so the task can run
// again with a clean exit code, output and command.
func (t *TaskExec) resetForRetry() {
	t.stdout, t.stderr, t.exitCode, t.command, t.stopped = nil, nil, 0, "", false
	t.phase, t.check, t.verify, t.unchanged, t.wouldRun = "", nil, nil, false, false
	t.startedAt = time.Time{}
}

// waitForRetry pauses before the next attempt. It returns false when the run is
// canceled while waiting, so the task is not retried.
func (t *TaskExec) waitForRetry(ctx context.Context) bool {
	select {
	case <-time.After(t.task.RetryDelayDuration()):
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

// appendStderr adds a reason to the captured stderr without losing any output
// the command already produced. This keeps the reason visible to consumers
// that only see stdout/stderr (the web UI and history database).
func (t *TaskExec) appendStderr(msg string) {
	if len(t.stderr) > 0 && !bytes.HasSuffix(t.stderr, []byte("\n")) {
		t.stderr = append(t.stderr, '\n')
	}
	t.stderr = append(t.stderr, msg...)
}

// localArgs returns the program and arguments that run command locally: through
// the task's shell (sh -c by default, so multi-word commands work), under sudo if set.
// With env, a plain sh first runs envPrelude and then execs the task's shell, so that
// shell never sees the values.
func (t *Task) localArgs(command string, env bool) (string, []string) {
	name, args := "sh", []string{"-c", command}
	if t.Shell != "" {
		shell := strings.Split(t.Shell, " ")
		name, args = shell[0], append(shell[1:len(shell):len(shell)], command)
	}
	if env {
		name, args = "sh", append([]string{"-c", envPrelude + `exec "$@"`, "sh", name}, args...)
	}
	if t.Sudo {
		// -n: fail instead of hanging on a password prompt
		name, args = "sudo", append([]string{"-n", name}, args...)
	}
	return name, args
}

// sudoWrap runs command as root through sh, single-quoting it.
func sudoWrap(command string) string {
	return "sudo -n " + shWrap(command)
}

// shWrap runs command through sh, single-quoting it.
func shWrap(command string) string {
	return "sh -c '" + strings.ReplaceAll(command, "'", `'\''`) + "'"
}
