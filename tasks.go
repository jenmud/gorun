package gorun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/crypto/ssh"

	"gorun/internal/envvars"
)

const (
	// DefaultTaskTimeout is how long a task may run when it doesn't set its own
	// timeout. It stops a hung command from holding up a run forever.
	DefaultTaskTimeout = 30 * time.Minute

	// taskKillGrace bounds how long gorun waits for output pipes to close after
	// a canceled task has been killed.
	taskKillGrace = 5 * time.Second
)

// Copy represents a file copy operation.
type Copy struct {
	Src      string `toml:"src"`
	SrcFile  string `toml:"src_file,omitempty"`
	SrcTpl   string `toml:"src_template,omitempty"`
	SrcTmpl  string `toml:"src_tmpl,omitempty"`
	Template string `toml:"template,omitempty"`
	Content  string `toml:"content,omitempty"`
	Dst      string `toml:"dst"`
	DstPath  string `toml:"dst_path,omitempty"`
	Dir      string `toml:"dir,omitempty"`
	Dest     string `toml:"dest,omitempty"`
	Mode     string `toml:"mode,omitempty"`
	Perm     string `toml:"perm,omitempty"`
}

// Task is a task to execute on one or more servers.
type Task struct {
	LogLevel        string            `toml:"log_level"`
	Name            string            `toml:"-"` // the <name> in [tasks.<name>]
	Sudo            bool              `toml:"sudo"`
	Cmd             string            `toml:"cmd,omitempty"`
	CmdFile         string            `toml:"cmd_file,omitempty"`
	CmdTemplate     string            `toml:"cmd_template,omitempty"`
	CmdTemplateFile string            `toml:"cmd_template_file,omitempty"`
	Copy            []Copy            `toml:"copy,omitempty"`
	Copies          []Copy            `toml:"copies,omitempty"`
	Upload          []Copy            `toml:"upload,omitempty"`
	Files           []Copy            `toml:"files,omitempty"`
	EnvVars         map[string]string `toml:"envvars"`
	DependsOn       []string          `toml:"depends_on"`
	Shell           string            `toml:"shell"`
	Timeout         time.Duration     `toml:"timeout,omitempty"`
}

// TimeoutDuration returns the timeout to apply to the task. A zero (or
// negative) Timeout uses DefaultTaskTimeout.
func (t Task) TimeoutDuration() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}

	return DefaultTaskTimeout
}

// Command reads the command from either inline cmd or from a file
func (t *Task) Command() (string, error) {
	return t.CommandFor(nil)
}

// CommandFor reads the command from either inline cmd or from a file,
// and compiles templates with the provided server.
func (t *Task) CommandFor(server any) (string, error) {
	if t.CmdTemplate != "" {
		tmpl, err := template.New(t.Name).Parse(t.CmdTemplate)
		if err != nil {
			return "", fmt.Errorf("failed to parse command template for task %s: %w", t.Name, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, server); err != nil {
			return "", fmt.Errorf("failed to execute command template for task %s: %w", t.Name, err)
		}
		return buf.String(), nil
	}

	if t.CmdTemplateFile != "" {
		content, err := os.ReadFile(t.CmdTemplateFile)
		if err != nil {
			return "", fmt.Errorf("failed to read command template file %s: %w", t.CmdTemplateFile, err)
		}
		tmpl, err := template.New(t.Name).Parse(string(content))
		if err != nil {
			return "", fmt.Errorf("failed to parse command template file %s: %w", t.CmdTemplateFile, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, server); err != nil {
			return "", fmt.Errorf("failed to execute command template file %s: %w", t.CmdTemplateFile, err)
		}
		return buf.String(), nil
	}

	if t.Cmd != "" {
		return t.Cmd, nil
	}

	if t.CmdFile != "" {
		// Read command from file
		content, err := os.ReadFile(t.CmdFile)
		if err != nil {
			return "", fmt.Errorf("failed to read command file %s: %w", t.CmdFile, err)
		}
		return string(content), nil
	}

	return "", fmt.Errorf("task %s has no command specified", t.Name)
}

// validate runs some validations against the task checking that it is valid for execution.
func (t Task) validate() error {
	if t.Timeout < 0 {
		return fmt.Errorf("task %s timeout must not be negative", t.Name)
	}

	count := 0
	if t.Cmd != "" {
		count++
	}
	if t.CmdFile != "" {
		count++
	}
	if t.CmdTemplate != "" {
		count++
	}
	if t.CmdTemplateFile != "" {
		count++
	}
	if count > 1 {
		return fmt.Errorf("task %s cannot have more than one of cmd, cmd_file, cmd_template, cmd_template_file specified", t.Name)
	}

	copies := t.getCopies()
	hasAction := count > 0 || len(copies) > 0
	if !hasAction {
		return fmt.Errorf("task %s has no command specified", t.Name)
	}

	if t.CmdFile != "" {
		if _, err := os.Stat(t.CmdFile); os.IsNotExist(err) {
			return fmt.Errorf("command file %s does not exist", t.CmdFile)
		}
	}

	if t.CmdTemplateFile != "" {
		if _, err := os.Stat(t.CmdTemplateFile); os.IsNotExist(err) {
			return fmt.Errorf("command template file %s does not exist", t.CmdTemplateFile)
		}
	}

	for _, c := range copies {
		src := c.Src
		if c.SrcFile != "" && src == "" {
			src = c.SrcFile
		}
		if c.Template != "" && c.SrcTpl == "" {
			c.SrcTpl = c.Template
		}
		if c.SrcTmpl != "" && c.SrcTpl == "" {
			c.SrcTpl = c.SrcTmpl
		}
		if c.Content != "" {
			// content is fine
		} else if c.SrcTpl != "" {
			if _, err := os.Stat(c.SrcTpl); os.IsNotExist(err) {
				return fmt.Errorf("copy template %s does not exist", c.SrcTpl)
			}
		} else if src == "" {
			return fmt.Errorf("task %s copy requires src", t.Name)
		} else {
			if _, err := os.Stat(src); os.IsNotExist(err) {
				return fmt.Errorf("copy source %s does not exist", src)
			}
		}
		if c.Dst == "" && c.DstPath == "" && c.Dest == "" && c.Dir == "" {
			return fmt.Errorf("task %s copy requires dst or dir", t.Name)
		}
	}

	return nil
}

func (t *Task) getCopies() []Copy {
	var out []Copy
	out = append(out, t.Copy...)
	out = append(out, t.Copies...)
	out = append(out, t.Upload...)
	out = append(out, t.Files...)
	return out
}

// TaskExec is a execution task to be run on a server.
type TaskExec struct {
	task        Task
	command     string
	logger      *slog.Logger
	server      *ssh.Client
	serverName  string
	gorunServer *Server
	gorunUUID   string
	createdAt   time.Time
	startedAt   time.Time
	completedAt time.Time
	stdout      []byte
	stderr      []byte
	exitCode    int
}

// contextErr turns a canceled or timed-out context into an error that explains
// why the task stopped.
func (t *TaskExec) contextErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("task %q timed out after %s", t.task.Name, t.task.TimeoutDuration())
	}

	return fmt.Errorf("task %q was interrupted", t.task.Name)
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

// Fail will fail a task.
func (t *TaskExec) Fail(ctx context.Context, err error) error {
	ctx, span := tracer.Start(ctx, "Fail")
	defer span.End()

	span.SetAttributes(
		attribute.String("task.name", t.task.Name),
		attribute.Bool("task.sudo", t.task.Sudo),
	)

	if err != nil {
		// report the command's real exit code when there is one
		var sshErr *ssh.ExitError
		var execErr *exec.ExitError

		ctxErr := ctx.Err()

		switch {
		case errors.Is(ctxErr, context.DeadlineExceeded):
			t.exitCode = 124 // same convention as timeout(1)
			err = t.contextErr(ctxErr)
		case errors.Is(ctxErr, context.Canceled):
			t.exitCode = 130 // interrupted by SIGINT
			err = t.contextErr(ctxErr)
		case errors.As(err, &sshErr):
			t.exitCode = sshErr.ExitStatus()
		case errors.As(err, &execErr):
			t.exitCode = execErr.ExitCode()
		case errors.Is(err, exec.ErrWaitDelay):
			t.exitCode = 2
			err = fmt.Errorf("task %q finished but left a background process holding its output open", t.task.Name)
		default:
			t.exitCode = 2
		}

		if ctxErr != nil {
			t.appendStderr(err.Error())
		}

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		t.logger.ErrorContext(
			ctx,
			"task failed",
			slog.Int("exitcode", t.exitCode),
			slog.String("stderr", string(t.stderr)),
			slog.String("stdout", string(t.stdout)),
			slog.String("reason", err.Error()),
		)

		span.SetAttributes(attribute.Int("task.exit_code", t.exitCode))

		return err
	}

	t.exitCode = 0
	span.SetAttributes(attribute.Int("task.exit_code", t.exitCode))
	span.SetStatus(codes.Ok, "")
	return nil
}

// Terminates the task.
func (t *TaskExec) Terminate(ctx context.Context, err error) error {
	ctx, span := tracer.Start(ctx, "Terminate")
	defer span.End()

	span.SetAttributes(attribute.String("task.name", t.task.Name))

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			t.exitCode = 124 // same convention as timeout(1)
			err = t.contextErr(err)
		} else {
			t.exitCode = 130 // interrupted by SIGINT
			err = t.contextErr(err)
		}
		t.appendStderr(err.Error())

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		t.logger.WarnContext(
			ctx,
			"task terminated",
			slog.Int("exitcode", t.exitCode),
			slog.String("stderr", string(t.stderr)),
			slog.String("stdout", string(t.stdout)),
			slog.String("reason", err.Error()),
		)

		span.SetAttributes(attribute.Int("task.exit_code", t.exitCode))

		return err
	}

	t.exitCode = 0
	span.SetAttributes(attribute.Int("task.exit_code", t.exitCode))
	span.SetStatus(codes.Ok, "")
	return nil
}

// Completed marks the task as completed.
func (t *TaskExec) Completed(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "Completed")
	defer span.End()

	span.SetAttributes(
		attribute.String("task.name", t.task.Name),
		attribute.Int("task.exit_code", 0),
	)
	span.SetStatus(codes.Ok, "")

	t.exitCode = 0

	t.logger.DebugContext(
		ctx,
		"task completed",
		slog.Int("exitcode", t.exitCode),
		slog.String("stderr", string(t.stderr)),
		slog.String("stdout", string(t.stdout)),
	)

	return nil
}

// NewTaskExec creates and sets up a task to be executed on the remote session.
func NewTaskExec(server *ssh.Client, task ...Task) ([]*TaskExec, error) {
	tasks := make([]*TaskExec, len(task))

	for i, t := range task {
		if err := t.validate(); err != nil {
			return nil, err
		}

		logger := NewLogger(t.LogLevel, logOutput, false)

		envVarList := []string{}
		for key, value := range t.EnvVars {
			envVarList = append(envVarList, fmt.Sprintf("%s=%s", key, value))
		}

		logger = logger.With(
			slog.String("name", t.Name),
			slog.String("shell", t.Shell),
			slog.Bool("sudo", t.Sudo),
			slog.String("cmd_file", t.CmdFile),
			slog.String("cmd_template_file", t.CmdTemplateFile),
			slog.String("cmd_template", t.CmdTemplate),
			slog.Any("depends_on", t.DependsOn),
			slog.Any("envvars", envVarList),
		)

		if server != nil {
			logger = logger.With(
				slog.Group(
					"server",
					slog.String("user", server.User()),
					slog.String("session", server.RemoteAddr().String()),
				),
			)
		} else {
			logger = logger.With(
				slog.Group(
					"server",
					slog.String("user", envvars.Get("USER")),
					slog.String("session", "localhost"),
				),
			)
		}

		tasks[i] = &TaskExec{
			task:      t,
			server:    server,
			logger:    logger,
			createdAt: time.Now(),
		}
	}

	return tasks, nil
}

// timeoutContext applies the task's timeout to ctx.
func (t *TaskExec) timeoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := t.task.TimeoutDuration()
	if timeout <= 0 {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, timeout)
}

// LocalExecute executes the task as a local task.
func (t *TaskExec) LocalExecute(ctx context.Context) error {
	ctx, cancel := t.timeoutContext(ctx)
	defer cancel()

	return t.localExecute(ctx)
}

func (t *TaskExec) localExecute(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "TaskExec.LocalExecute")
	defer span.End()

	span.SetAttributes(
		attribute.String("task.name", t.task.Name),
		attribute.Bool("task.sudo", t.task.Sudo),
	)

	t.logger.InfoContext(ctx, "running local task")

	if t.serverName == "" {
		t.serverName = "localhost"
	}

	defer func() {
		// make sure that the completed at always is set at the end
		t.completedAt = time.Now()
	}()

	defer func() {
		status := "ok"
		if t.exitCode != 0 {
			status = "error"
		}
		attrs := metric.WithAttributes(
			attribute.String("task.name", t.task.Name),
			attribute.String("server.name", t.serverName),
			attribute.String("task.status", status),
		)
		taskExecutions.Add(ctx, 1, attrs)
		if !t.startedAt.IsZero() {
			taskDuration.Record(ctx, time.Since(t.startedAt).Seconds(), attrs)
		}
	}()

	var serverArg any
	if t.gorunServer != nil {
		serverArg = t.gorunServer
	} else if t.serverName != "" {
		serverArg = Server{Name: t.serverName}
	}

	if err := CopyFiles(t.server, serverArg, &t.task); err != nil {
		return t.Fail(ctx, err)
	}

	command, err := t.task.CommandFor(serverArg)
	if err != nil {
		return t.Fail(ctx, err)
	}

	// default to running the command through sh so multi-word commands work
	name, args := "sh", []string{"-c", command}

	if t.task.Shell != "" {
		scmd := strings.Split(t.task.Shell, " ")
		name = scmd[0]

		args = append([]string{}, scmd[1:]...)
		args = append(args, command)
	}

	if t.task.Sudo {
		// -n: fail instead of hanging on a password prompt
		args = append([]string{"-n", name}, args...)
		name = "sudo"
	}

	cmd := exec.CommandContext(ctx, name, args...)
	configureCommand(cmd)

	t.command = cmd.String()

	for key, value := range t.task.EnvVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", strings.ToUpper(key), value))
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.startedAt = time.Now()

	err = cmd.Run()

	t.stderr = stderr.Bytes()
	t.stdout = stdout.Bytes()

	if err := t.Fail(ctx, err); err != nil {
		return err
	}

	return t.Completed(ctx)
}

// execResult carries the output of a remote command back to Execute.
type execResult struct {
	stdout []byte
	stderr []byte
	err    error
}

// Execute executes the task with the attached SSH client.
func (t *TaskExec) Execute(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "TaskExec.Execute")
	defer span.End()

	if t.serverName == "" {
		t.serverName = "localhost"
	}

	span.SetAttributes(
		attribute.String("task.name", t.task.Name),
		attribute.String("server.name", t.serverName),
		attribute.Bool("task.sudo", t.task.Sudo),
	)

	ctx, cancel := t.timeoutContext(ctx)
	defer cancel()

	if t.server == nil {
		return t.localExecute(ctx)
	}

	defer func() {
		status := "ok"
		if t.exitCode != 0 {
			status = "error"
		}
		attrs := metric.WithAttributes(
			attribute.String("task.name", t.task.Name),
			attribute.String("server.name", t.serverName),
			attribute.String("task.status", status),
		)
		taskExecutions.Add(ctx, 1, attrs)
		if !t.startedAt.IsZero() {
			taskDuration.Record(ctx, time.Since(t.startedAt).Seconds(), attrs)
		}
	}()

	t.logger.InfoContext(ctx, "running task")

	defer func() {
		// make sure that the completed at always is set at the end
		t.completedAt = time.Now()
	}()

	session, err := t.server.NewSession()
	if err != nil {
		return t.Fail(ctx, fmt.Errorf("error creating SSH session: %w", err))
	}
	defer func() { _ = session.Close() }()

	var serverArg any
	if t.gorunServer != nil {
		serverArg = t.gorunServer
	} else if t.serverName != "" {
		serverArg = Server{Name: t.serverName}
	}

	command, err := t.task.CommandFor(serverArg)
	if err != nil {
		return t.Fail(ctx, fmt.Errorf("error running the task on the remote server: %w", err))
	}

	if t.task.Sudo {
		// wrap in single quotes, escaping any single quotes in the command
		command = "sudo -n sh -c '" + strings.ReplaceAll(command, "'", `'\''`) + "'"
	}

	t.command = command
	t.startedAt = time.Now()

	result := make(chan execResult, 1)

	go func() {
		var stdout, stderr bytes.Buffer

		session.Stdout = &stdout
		session.Stderr = &stderr

		for key, value := range t.task.EnvVars {
			_ = session.Setenv(strings.ToUpper(key), value)
		}

		if err := CopyFiles(t.server, serverArg, &t.task); err != nil {
			result <- execResult{
				stdout: stdout.Bytes(),
				stderr: stderr.Bytes(),
				err:    fmt.Errorf("error running the task on the remote server: %w", err),
			}
			return
		}

		err := session.Run(command)
		if err != nil {
			err = fmt.Errorf("error running the task on the remote server: %w", err)
		}

		result <- execResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), err: err}
	}()

	select {
	case res := <-result:
		t.stdout = res.stdout
		t.stderr = res.stderr
		if res.err != nil {
			return t.Fail(ctx, res.err)
		}
		return t.Completed(ctx)
	case <-ctx.Done():
		// The command may have finished at the same moment the deadline hit.
		// Prefer its result over reporting a timeout.
		select {
		case res := <-result:
			t.stdout = res.stdout
			t.stderr = res.stderr
			if res.err != nil {
				return t.Fail(ctx, res.err)
			}
			return t.Completed(ctx)
		default:
		}

		// Ask the remote command to stop. Closing the client also unblocks a
		// stuck copy or command, so Execute can return promptly.
		_ = session.Signal(ssh.SIGTERM)
		_ = t.server.Close()

		select {
		case res := <-result:
			t.stdout = res.stdout
			t.stderr = res.stderr
		case <-time.After(taskKillGrace):
			// The connection didn't close in time; don't hold up the whole run.
		}

		return t.Terminate(ctx, ctx.Err())
	}
}
