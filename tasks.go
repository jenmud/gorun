package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Task is a task to execute on one or more servers.
type Task struct {
	LogLevel  string            `toml:"log_level"`
	Name      string            `toml:"name"`
	Sudo      bool              `toml:"sudo"`
	Cmd       string            `toml:"cmd,omitempty"`
	CmdFile   string            `toml:"cmd_file,omitempty"`
	EnvVars   map[string]string `toml:"envvars"`
	DependsOn []string          `toml:"depends_on"`
	Shell     string            `toml:"shell"`
}

// Command reads the command from either inline cmd or from a file
func (t *Task) Command() (string, error) {
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
	if t.Cmd != "" && t.CmdFile != "" {
		return fmt.Errorf("task %s cannot have both cmd and cmd_file specified", t.Name)
	}

	if t.CmdFile != "" {
		if _, err := os.Stat(t.CmdFile); os.IsNotExist(err) {
			return fmt.Errorf("command file %s does not exist", t.CmdFile)
		}
	}

	return nil
}

// TaskExec is a execution task to be run on a server.
type TaskExec struct {
	task        Task
	logger      *slog.Logger
	server      *ssh.Client
	createdAt   time.Time
	startedAt   time.Time
	completedAt time.Time
	stdout      []byte
	stderr      []byte
	exitCode    int
}

// Fail will fail a task.
func (t *TaskExec) Fail(ctx context.Context, err error) error {
	ctx, span := tracer.Start(ctx, "Fail")
	defer span.End()

	if err != nil {
		span.RecordError(err)

		// report the command's real exit code when there is one
		var sshErr *ssh.ExitError
		var execErr *exec.ExitError

		switch {
		case errors.As(err, &sshErr):
			t.exitCode = sshErr.ExitStatus()
		case errors.As(err, &execErr):
			t.exitCode = execErr.ExitCode()
		default:
			t.exitCode = 2
		}

		t.logger.ErrorContext(
			ctx,
			"task failed",
			slog.Int("exitcode", t.exitCode),
			slog.String("stderr", string(t.stderr)),
			slog.String("stdout", string(t.stdout)),
			slog.String("reason", err.Error()),
		)

		return err
	}

	t.exitCode = 0
	return nil
}

// Terminates the task.
func (t *TaskExec) Terminate(ctx context.Context, err error) error {
	ctx, span := tracer.Start(ctx, "Terminate")
	defer span.End()

	if err != nil {
		span.RecordError(err)
		t.stderr = []byte(err.Error())
		t.exitCode = 1

		t.logger.WarnContext(
			ctx,
			"task terminated",
			slog.Int("exitcode", t.exitCode),
			slog.String("stderr", string(t.stderr)),
			slog.String("stdout", string(t.stdout)),
			slog.String("reason", err.Error()),
		)

		return err
	}

	t.exitCode = 0
	return nil
}

// Completed marks the task as completed.
func (t *TaskExec) Completed(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "Completed")
	defer span.End()

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

		envvars := []string{}
		for key, value := range t.EnvVars {
			envvars = append(envvars, fmt.Sprintf("%s=%s", key, value))
		}

		logger = logger.With(
			slog.String("name", t.Name),
			slog.String("shell", t.Shell),
			slog.Bool("sudo", t.Sudo),
			slog.String("cmd_file", t.CmdFile),
			slog.Any("depends_on", t.DependsOn),
			slog.Any("envvars", envvars),
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
					slog.String("user", os.Getenv("USER")),
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

// LocalExecute executes the task as a local task.
func (t *TaskExec) LocalExecute(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "TaskExec.LocalExecute")
	defer span.End()

	t.logger.InfoContext(ctx, "running local task")

	defer func() {
		// make sure that the completed at always is set at the end
		t.completedAt = time.Now()
	}()

	command, err := t.task.Command()
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

// Execute executes the task with the attached SSH client.
func (t *TaskExec) Execute(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "TaskExec.Execute")
	defer span.End()

	if t.server == nil {
		return t.LocalExecute(ctx)
	}

	t.logger.InfoContext(ctx, "running task")

	defer func() {
		// make sure that the completed at always is set at the end
		t.completedAt = time.Now()
	}()

	wg := sync.WaitGroup{}

	errChan := make(chan error, 1)
	doneChan := make(chan bool, 1)

	session, err := t.server.NewSession()

	if err := t.Fail(ctx, err); err != nil {
		err = fmt.Errorf("error creating SSH session: %w", err)
		return err
	}

	defer session.Close()

	wg.Go(func() {
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		defer func() {
			t.stdout = stdout.Bytes()
			t.stderr = stderr.Bytes()
		}()

		session.Stdout = &stdout
		session.Stderr = &stderr

		for key, value := range t.task.EnvVars {
			session.Setenv(strings.ToUpper(key), value)
		}

		t.startedAt = time.Now()

		command, err := t.task.Command()
		if err != nil {
			errChan <- fmt.Errorf("error running the task on the remote server: %w", err)
			return
		}

		if t.task.Sudo {
			// wrap in single quotes, escaping any single quotes in the command
			command = "sudo -n sh -c '" + strings.ReplaceAll(command, "'", `'\''`) + "'"
		}

		if err := session.Run(command); err != nil {
			errChan <- fmt.Errorf("error running the task on the remote server: %w", err)
			return
		}

		t.stderr = stderr.Bytes()
		t.stdout = stdout.Bytes()
		doneChan <- true
	})

	wg.Wait()

	select {
	case <-ctx.Done():
		err := ctx.Err()
		session.Signal(ssh.SIGTERM)
		return t.Terminate(ctx, err)
	case err := <-errChan:
		return t.Fail(ctx, err)
	case <-doneChan:
		return t.Completed(ctx)
	}
}
