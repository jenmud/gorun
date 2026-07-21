package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Task is a task to execute on one or more servers.
type Task struct {
	Name      string            `toml:"name"`
	Cmd       string            `toml:"cmd"`
	EnvVars   map[string]string `toml:"envvars"`
	DependsOn []string          `toml:"depends_on"`
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

// NewTaskExec creates and sets up a task to be executed on the remote session.
func NewTaskExec(server *ssh.Client, task ...Task) []*TaskExec {
	tasks := make([]*TaskExec, len(task))

	logger := slog.Default()

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

	for i, t := range task {
		tasks[i] = &TaskExec{
			task:   t,
			server: server,
			logger: logger.With(
				slog.Group(
					"task",
					slog.String("name", t.Name),
				),
			),
			createdAt: time.Now(),
		}
	}

	return tasks
}

func (t *TaskExec) LocalExecute(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "TaskExec.LocalExecute")
	defer span.End()

	t.logger.InfoContext(ctx, "running local task", slog.String("name", t.task.Name))
	return nil
}

// Execute executes the task with the attached SSH client.
func (t *TaskExec) Execute(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "TaskExec.Execute")
	defer span.End()

	if t.server == nil {
		t.logger.WarnContext(ctx, "no SSH client connected, running local task execution...")
		return t.LocalExecute(ctx)
	}

	t.logger.InfoContext(ctx, "running task", slog.String("name", t.task.Name))

	defer func() {
		// make sure that the completed at always is set at the end
		t.completedAt = time.Now()
	}()

	wg := sync.WaitGroup{}

	errChan := make(chan error, 1)
	doneChan := make(chan bool, 1)

	session, err := t.server.NewSession()
	if err != nil {
		err = fmt.Errorf("error creating SSH session: %w", err)
		span.RecordError(err)
		return err
	}

	defer session.Close()

	wg.Go(func() {
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		session.Stdout = &stdout
		session.Stderr = &stderr

		for key, value := range t.task.EnvVars {
			session.Setenv(key, value)
		}

		t.startedAt = time.Now()
		if err := session.Run(t.task.Cmd); err != nil {
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
		span.RecordError(err)
		session.Signal(ssh.SIGTERM)
		t.logger.InfoContext(ctx, "task terminated", slog.String("reason", err.Error()))
		t.stderr = []byte(err.Error())
		t.exitCode = 1
		return err
	case err := <-errChan:
		span.RecordError(err)
		t.logger.ErrorContext(ctx, "task failed", slog.String("reason", err.Error()))
		t.stderr = []byte(err.Error())
		t.exitCode = 2
		return err
	case <-doneChan:
		t.logger.DebugContext(ctx, "task completed")
		t.exitCode = 0
	}

	return nil
}
