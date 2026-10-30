package gorun

import (
	"gorun/internal/nats"
	"log"
	"os"
	"sync"
	"time"

	_ "github.com/joho/godotenv/autoload"
	natsCore "github.com/nats-io/nats.go"
)

var mux sync.Mutex
var once sync.Once
var publisher *natsCore.Conn

func StartNATS() {
	if os.Getenv("GORUN_SKIP_NATS") != "" {
		return
	}
	once.Do(func() {
		mux.Lock()
		defer mux.Unlock()

		conn, err := nats.NewClient()
		if err != nil {
			// NATS is optional: keep running without a publisher.
			log.Printf("gorun: NATS publisher disabled: %v", err)
			return
		}

		publisher = conn
		log.Printf("gorun: NATS publisher connected")
	})
}

func init() {
	// Don't auto-connect; let caller decide
}

// TaskState is the lifecycle state of a task on a server.
type TaskState int

const (
	TaskPending TaskState = iota
	TaskRunning
	TaskDone
	TaskFailed
	TaskSkipped
)

// ProcessStartedEvent is published when a gorun process starts running.
type ProcessStartedEvent struct {
	Type      string    `json:"type"`
	GorunUUID string    `json:"gorun_uuid"`
	Title     string    `json:"title"`
	Servers   []string  `json:"servers"`
	StartedAt time.Time `json:"started_at"`
	Hostname  string    `json:"hostname"`
	PID       int       `json:"pid"`
}

// ProcessFinishedEvent is published when a gorun process finishes running.
type ProcessFinishedEvent struct {
	Type       string        `json:"type"`
	GorunUUID  string        `json:"gorun_uuid"`
	Success    bool          `json:"success"`
	Duration   time.Duration `json:"duration"`
	TotalTasks int           `json:"total_tasks"`
	Failed     int           `json:"failed"`
	Skipped    int           `json:"skipped"`
	FinishedAt time.Time     `json:"finished_at"`
}

// PlanEvent is sent once per server, before any task runs, with the tasks in execution order.
type PlanEvent struct {
	Type      string     `json:"type"`
	GorunUUID string     `json:"gorun_uuid"`
	Server    string     `json:"server"`
	Address   string     `json:"address"`
	Tasks     []string   `json:"tasks"`
	TaskMeta  []TaskMeta `json:"task_meta,omitempty"`
}

// TaskMeta carries the documentation attached to a task in a PlanEvent, so
// consumers such as the TUI and web dashboard can show descriptions and rerun
// warnings. Tasks still lists the bare names for older consumers.
type TaskMeta struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Details     string `json:"details,omitempty"`
	Rerun       string `json:"rerun,omitempty"`
}

// TaskEvent is sent when a task changes state.
type TaskEvent struct {
	Type      string        `json:"type"`
	GorunUUID string        `json:"gorun_uuid"`
	Server    string        `json:"server"`
	Task      string        `json:"task"`
	State     TaskState     `json:"state"`
	Stdout    string        `json:"stdout,omitempty"`
	Stderr    string        `json:"stderr,omitempty"`
	Command   string        `json:"command,omitempty"`
	ExitCode  int           `json:"exit_code"`
	Err       error         `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
}

// ServerErrorEvent is sent when a server can't run its tasks at all (e.g. SSH connection failed).
type ServerErrorEvent struct {
	Type      string `json:"type"`
	GorunUUID string `json:"gorun_uuid"`
	Server    string `json:"server"`
	Err       error  `json:"error"`
}

// RunDoneEvent is sent when every server has finished.
type RunDoneEvent struct {
	Type      string `json:"type"`
	GorunUUID string `json:"gorun_uuid"`
	Err       error  `json:"error,omitempty"`
}
