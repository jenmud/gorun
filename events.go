package main

import "time"

// TaskState is the lifecycle state of a task on a server.
type TaskState int

const (
	TaskPending TaskState = iota
	TaskRunning
	TaskDone
	TaskFailed
	TaskSkipped
)

// PlanEvent is sent once per server, before any task runs, with the tasks in execution order.
type PlanEvent struct {
	Server  string
	Address string
	Tasks   []string
}

// TaskEvent is sent when a task changes state.
type TaskEvent struct {
	Server   string
	Task     string
	State    TaskState
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
	Duration time.Duration
}

// ServerErrorEvent is sent when a server can't run its tasks at all (e.g. SSH connection failed).
type ServerErrorEvent struct {
	Server string
	Err    error
}

// RunDoneEvent is sent when every server has finished.
type RunDoneEvent struct {
	Err error
}

// emit sends an event to the OnEvent callback, if one is set.
func (c *Config) emit(event any) {
	if c.OnEvent != nil {
		c.OnEvent(event)
	}
}
