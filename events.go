package gorun

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	_ "github.com/joho/godotenv/autoload"
	natsCore "github.com/nats-io/nats.go"

	"github.com/jenmud/gorun/internal/nats"
)

// publisher is the NATS connection events are published on, once StartNATS has connected.
var (
	publisher *natsCore.Conn
	startNATS sync.Once
)

// StartNATS connects to NATS so that runs publish their events (see docs/integrations.md). It
// connects once, however often it is called. NATS is optional: when it can't be reached, or
// GORUN_SKIP_NATS is set, runs carry on without publishing.
func StartNATS() {
	if os.Getenv("GORUN_SKIP_NATS") != "" {
		return
	}
	startNATS.Do(func() {
		conn, err := nats.NewClient()
		if err != nil {
			log.Printf("gorun: NATS publisher disabled: %v", err)
			return
		}
		publisher = conn
		log.Printf("gorun: NATS publisher connected")
	})
}

// TaskState is the lifecycle state of a task on a server. The values are stored in the history
// database, so new states are only ever added at the end.
type TaskState int

const (
	TaskPending   TaskState = iota // planned, not started
	TaskRunning                    // running; a running event is sent for each phase and attempt
	TaskDone                       // finished successfully
	TaskFailed                     // failed: the command, its check or its verify failed
	TaskSkipped                    // not run: a task it depends on failed, or the run was cancelled
	TaskUnchanged                  // its check found nothing to do, so its command didn't run; a success
	TaskWouldRun                   // a check-only run: its check found something to do, or it has no check
)

// String is the state's name, as gorun prints it: "done", "unchanged", "would run" and so on.
func (s TaskState) String() string {
	names := [...]string{"pending", "running", "done", "failed", "skipped", "unchanged", "would run"}
	if s < 0 || int(s) >= len(names) {
		return "state " + strconv.Itoa(int(s))
	}
	return names[s]
}

// Phase is the step of a task an event is about. A task with a check runs it first, then (when
// the check says there is something to do) its copies and command, then its verify.
type Phase string

const (
	PhaseCheck  Phase = "check"
	PhaseRun    Phase = "run"
	PhaseVerify Phase = "verify"
)

// StepResult is how a task's check or verify command went.
type StepResult struct {
	Command  string        `json:"command"`
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout,omitempty"`
	Stderr   string        `json:"stderr,omitempty"`
	Duration time.Duration `json:"duration"`
}

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

	// CheckOnly is set for a check-only run (--check): only checks run, and nothing changes.
	CheckOnly bool `json:"check_only,omitempty"`

	// Includes are the git includes the Gorunfile read, with the commit each one used, so a run
	// records exactly which version of a task library it ran.
	Includes []IncludeSource `json:"includes,omitempty"`

	// Schedule is the name of the schedule that started the run, or empty when it wasn't
	// started by a schedule.
	Schedule string `json:"schedule,omitempty"`
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
	Unchanged  int           `json:"unchanged"` // tasks whose check found nothing to do
	WouldRun   int           `json:"would_run"` // in a check-only run, tasks that would run
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
	// Check, CheckCopies and Verify are the task's check and verify, so a view can show them
	// before they run.
	Check       string `json:"check,omitempty"`
	CheckCopies bool   `json:"check_copies,omitempty"`
	Verify      string `json:"verify,omitempty"`
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
	// Phase is the step a running event is about, or on a final event the step that decided
	// the outcome. Empty for a task with no check or verify that never started.
	Phase Phase `json:"phase,omitempty"`
	// Check and Verify are the results of the task's check and verify commands, when they ran.
	Check  *StepResult `json:"check,omitempty"`
	Verify *StepResult `json:"verify,omitempty"`
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
