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
	"sync"
	"text/template"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/crypto/ssh"

	"gorun/internal/envvars"
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
	LogLevel         string            `toml:"log_level"`
	Name             string            `toml:"-"` // the <name> in [tasks.<name>]
	Sudo             bool              `toml:"sudo"`
	Cmd              string            `toml:"cmd,omitempty"`
	CmdFile          string            `toml:"cmd_file,omitempty"`
	CmdTemplate      string            `toml:"cmd_template,omitempty"`
	CmdTemplateFile  string            `toml:"cmd_template_file,omitempty"`
	Copy             []Copy            `toml:"copy,omitempty"`
	Copies           []Copy            `toml:"copies,omitempty"`
	Upload           []Copy            `toml:"upload,omitempty"`
	Files            []Copy            `toml:"files,omitempty"`
	EnvVars          map[string]string `toml:"envvars"`
	DependsOn        []string          `toml:"depends_on"`
	Shell            string            `toml:"shell"`
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

// Fail will fail a task.
func (t *TaskExec) Fail(ctx context.Context, err error) error {
	ctx, span := tracer.Start(ctx, "Fail")
	defer span.End()

	span.SetAttributes(
		attribute.String("task.name", t.task.Name),
		attribute.Bool("task.sudo", t.task.Sudo),
	)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

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
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
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

// LocalExecute executes the task as a local task.
func (t *TaskExec) LocalExecute(ctx context.Context) error {
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

	if t.server == nil {
		return t.LocalExecute(ctx)
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

	wg := sync.WaitGroup{}

	errChan := make(chan error, 1)
	doneChan := make(chan bool, 1)

	session, err := t.server.NewSession()

	if err := t.Fail(ctx, err); err != nil {
		err = fmt.Errorf("error creating SSH session: %w", err)
		return err
	}

	defer func() { _ = session.Close() }()

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
			_ = session.Setenv(strings.ToUpper(key), value)
		}

		t.startedAt = time.Now()

		var serverArg any
		if t.gorunServer != nil {
			serverArg = t.gorunServer
		} else if t.serverName != "" {
			serverArg = Server{Name: t.serverName}
		}

		if err := CopyFiles(t.server, serverArg, &t.task); err != nil {
			errChan <- fmt.Errorf("error running the task on the remote server: %w", err)
			return
		}

		command, err := t.task.CommandFor(serverArg)
		if err != nil {
			errChan <- fmt.Errorf("error running the task on the remote server: %w", err)
			return
		}

		if t.task.Sudo {
			// wrap in single quotes, escaping any single quotes in the command
			command = "sudo -n sh -c '" + strings.ReplaceAll(command, "'", `'\''`) + "'"
		}

		t.command = command

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
		_ = session.Signal(ssh.SIGTERM)
		return t.Terminate(ctx, err)
	case err := <-errChan:
		return t.Fail(ctx, err)
	case <-doneChan:
		return t.Completed(ctx)
	}
}
