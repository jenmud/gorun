package gorun

import (
	"encoding/json"
	"github.com/jenmud/gorun/internal/nats"
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
	Type      string            `json:"type"`
	GorunUUID string            `json:"gorun_uuid"`
	Title     string            `json:"title"`
	Servers   []string          `json:"servers"`
	StartedAt time.Time         `json:"started_at"`
	Hostname  string            `json:"hostname"`
	PID       int               `json:"pid"`
	EnvVars   map[string]string `json:"envvars,omitempty"`
	Vars      map[string]any    `json:"vars,omitempty"`

	// Includes are the git includes the Gorunfile read, with the commit each one used, so a run
	// records exactly which version of a task library it ran.
	Includes []IncludeSource `json:"includes,omitempty"`
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
	Type      string            `json:"type"`
	GorunUUID string            `json:"gorun_uuid"`
	Server    string            `json:"server"`
	Address   string            `json:"address"`
	Tasks     []string          `json:"tasks"`
	TaskMeta  []TaskMeta        `json:"task_meta,omitempty"`
	EnvVars   map[string]string `json:"envvars,omitempty"`
	Vars      map[string]any    `json:"vars,omitempty"`
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
	Type      string            `json:"type"`
	GorunUUID string            `json:"gorun_uuid"`
	Server    string            `json:"server"`
	Task      string            `json:"task"`
	State     TaskState         `json:"state"`
	Stdout    string            `json:"stdout,omitempty"`
	Stderr    string            `json:"stderr,omitempty"`
	Command   string            `json:"command,omitempty"`
	EnvVars   map[string]string `json:"envvars,omitempty"`
	Vars      map[string]any    `json:"vars,omitempty"`
	ExitCode  int               `json:"exit_code"`
	Err       error             `json:"error,omitempty"`
	Duration  time.Duration     `json:"duration"`
	// Attempt and Attempts are set when a task has retries configured. A
	// running event with Attempt greater than one means the previous attempt
	// failed and the task is being retried.
	Attempt  int `json:"attempt,omitempty"`
	Attempts int `json:"attempts,omitempty"`
}

// errText is the message of err, or "" for nil. An error value marshals as {}, so events
// carry its text instead.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// taskEventJSON has TaskEvent's fields without its MarshalJSON method.
type taskEventJSON TaskEvent

// MarshalJSON publishes Err as its message.
func (e TaskEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		taskEventJSON
		Err string `json:"error,omitempty"`
	}{taskEventJSON(e), errText(e.Err)})
}

// CopyProgressEvent is sent while a task copies a file or directory to its server: once at 0%,
// then at most every quarter of a second, and once more with Done set when the copy has
// finished. A copy that fails sends no Done event; the task's failed event follows instead.
// Copies of inline content and templates are not reported.
type CopyProgressEvent struct {
	Type      string `json:"type"` // "copy_progress"
	GorunUUID string `json:"gorun_uuid"`
	Server    string `json:"server"`
	Task      string `json:"task"`
	// File is the destination on the server, as the task's copy names it.
	File string `json:"file"`
	// Bytes is how many bytes have been sent so far, out of Total. For a directory, Total is
	// the size of every file in it.
	Bytes          int64         `json:"bytes"`
	Total          int64         `json:"total"`
	Percent        float64       `json:"percent"`
	BytesPerSecond float64       `json:"bytes_per_second"`
	Elapsed        time.Duration `json:"elapsed"`
	Done           bool          `json:"done"`
}

// ServerErrorEvent is sent when a server can't run its tasks at all (e.g. SSH connection failed).
type ServerErrorEvent struct {
	Type      string `json:"type"`
	GorunUUID string `json:"gorun_uuid"`
	Server    string `json:"server"`
	Err       error  `json:"error"`
}

// MarshalJSON publishes Err as its message.
func (e ServerErrorEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type      string `json:"type"`
		GorunUUID string `json:"gorun_uuid"`
		Server    string `json:"server"`
		Err       string `json:"error"`
	}{e.Type, e.GorunUUID, e.Server, errText(e.Err)})
}

// RunDoneEvent is sent when every server has finished.
type RunDoneEvent struct {
	Type      string `json:"type"`
	GorunUUID string `json:"gorun_uuid"`
	Err       error  `json:"error,omitempty"`
}
