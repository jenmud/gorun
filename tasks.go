package main

import "time"

// Task is a task to execute on one or more servers.
type Task struct {
	Name      string   `toml:"name"`
	Cmd       string   `toml:"cmd"`
	DependsOn []string `toml:"depends_on"`
}

// TaskExec is a execution task to be run on a server.
type TaskExec struct {
	task        Task
	server      Server
	createdAt   time.Time
	completedAt time.Time
	stdout      []byte
	stderr      []byte
	exitCode    int
}
