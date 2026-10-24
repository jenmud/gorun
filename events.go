package gorun

import (
	// "sync"
	"time"

	_ "github.com/joho/godotenv/autoload"
	// "github.com/nats-io/nats.go"
)

// var mux sync.Mutex
// var once sync.Once
// var publisher *nats.Conn
//
// func init() {
// 	once.Do(func() {
// 		mux.Lock()
// 		defer mux.Unlock()
//
// 		options := nats.GetDefaultOptions()
// 		options.US
// 	})
// }

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
