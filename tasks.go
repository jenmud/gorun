package main

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/crypto/ssh"
)

// Task is a task to execute on one or more servers.
type Task struct {
	Name      string   `toml:"name"`
	Cmd       string   `toml:"cmd"`
	DependsOn []string `toml:"depends_on"`
}

// TaskExec is a execution task to be run on a server.
type TaskExec struct {
	task        Task
	logger      *slog.Logger
	server      *ssh.Session
	createdAt   time.Time
	completedAt time.Time
	stdout      []byte
	stderr      []byte
	exitCode    int
}

// NewTaskExec creates and sets up a task to be executed on the remote session.
func NewTaskExec(server *ssh.Session, task ...Task) []*TaskExec {
	tasks := make([]*TaskExec, len(task))

	for i, t := range task {
		tasks[i] = &TaskExec{
			task:   t,
			server: server,
			logger: slog.With(
				slog.Group(
					"task",
					slog.String("name", t.Name),
				),
				slog.Group(
					"server",
					slog.String("session", "???"),
				),
			),
			createdAt: time.Now(),
		}
	}

	return tasks
}

// Execute executes the task with the attached SSH client.
func (t *TaskExec) Execute(ctx context.Context) error {
	slog.Info("running task", slog.String("name", t.task.Name))
	return nil
}
