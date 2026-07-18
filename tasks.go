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
	server      *ssh.Session
	createdAt   time.Time
	completedAt time.Time
	stdout      []byte
	stderr      []byte
	exitCode    int
}

// Execute executes the task with the attached SSH client.
func (t *TaskExec) Execute(ctx context.Context) error {
	slog.Info("running task", slog.String("name", t.task.Name))
	return nil
}
