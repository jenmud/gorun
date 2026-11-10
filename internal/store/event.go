package store

import (
	"context"
	"log/slog"
	"time"
)

// Event is a run event as it travels as JSON: over NATS, and from a Recorder. It has the fields
// of every event type in the gorun package; Type says which ones are set.
type Event struct {
	Type      string `json:"type"`
	GorunUUID string `json:"gorun_uuid"`

	// process_started and process_finished
	Title      string    `json:"title"`
	Servers    []string  `json:"servers"`
	StartedAt  string    `json:"started_at"`
	Hostname   string    `json:"hostname"`
	PID        int       `json:"pid"`
	Includes   []Include `json:"includes,omitempty"`
	Schedule   string    `json:"schedule,omitempty"`
	Success    bool      `json:"success"`
	TotalTasks int       `json:"total_tasks"`
	Unchanged  int       `json:"unchanged"`
	Failed     int       `json:"failed"`
	Skipped    int       `json:"skipped"`
	FinishedAt string    `json:"finished_at"`

	// plan, task and server_error
	Server   string            `json:"server"`
	Address  string            `json:"address"`
	Tasks    []string          `json:"tasks"`
	TaskMeta []TaskMeta        `json:"task_meta,omitempty"`
	Task     string            `json:"task"`
	State    int               `json:"state"`
	Command  string            `json:"command"`
	Stdout   string            `json:"stdout"`
	Stderr   string            `json:"stderr"`
	ExitCode int               `json:"exit_code"`
	Duration int64             `json:"duration"`
	Attempt  int               `json:"attempt,omitempty"`
	Attempts int               `json:"attempts,omitempty"`
	EnvVars  map[string]string `json:"envvars,omitempty"`
	Vars     map[string]any    `json:"vars,omitempty"`
	Error    string            `json:"error,omitempty"`
	Phase    string            `json:"phase,omitempty"`
	Check    *Step             `json:"check,omitempty"`
	Verify   *Step             `json:"verify,omitempty"`

	// copy_progress
	File           string  `json:"file,omitempty"`
	Bytes          int64   `json:"bytes,omitempty"`
	Total          int64   `json:"total,omitempty"`
	Percent        float64 `json:"percent,omitempty"`
	BytesPerSecond float64 `json:"bytes_per_second,omitempty"`
	Done           bool    `json:"done,omitempty"`
}

// Step is how a task's check or verify command went.
type Step struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Duration int64  `json:"duration"`
}

// TaskMeta is the documentation a plan event carries for a task.
type TaskMeta struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Details     string `json:"details,omitempty"`
	Rerun       string `json:"rerun,omitempty"`
}

// Apply saves one event. Events can be replayed: a plan never resets a task that already ran.
// Failures are logged, not returned, so one bad write doesn't stop a run being recorded.
func (s *Store) Apply(ctx context.Context, ev *Event) {
	if s == nil || ev == nil {
		return
	}
	check := func(what string, err error) {
		if err != nil {
			slog.Error("gorun history: "+what, "gorun_uuid", ev.GorunUUID, "error", err)
		}
	}

	switch ev.Type {
	case "process_started":
		check("create run", s.CreateRun(ctx, ev.GorunUUID, ev.Title, ev.Hostname, ev.PID, parseTime(ev.StartedAt)))
		for _, server := range ev.Servers {
			check("add server", s.AddServer(ctx, ev.GorunUUID, server, ""))
		}
		if ev.Schedule != "" {
			check("set run schedule", s.SetRunSchedule(ctx, ev.GorunUUID, ev.Schedule))
		}
		if len(ev.Includes) > 0 {
			check("set run includes", s.SetRunIncludes(ctx, ev.GorunUUID, ev.Includes))
		}
		check("set run envvars", s.SetRunEnvVars(ctx, ev.GorunUUID, ev.EnvVars, ev.Vars))
	case "plan":
		if ev.Server == "" {
			return
		}
		check("add server", s.AddServer(ctx, ev.GorunUUID, ev.Server, ev.Address))
		check("set server envvars", s.SetServerEnvVars(ctx, ev.GorunUUID, ev.Server, ev.EnvVars, ev.Vars))
		for _, name := range ev.Tasks {
			check("plan task", s.PlanTask(ctx, ev.GorunUUID, ev.Server, name))
		}
	case "server_error":
		if ev.Server != "" && ev.Error != "" {
			check("set server error", s.SetServerError(ctx, ev.GorunUUID, ev.Server, ev.Error))
		}
	case "task":
		if ev.Server == "" || ev.Task == "" {
			return
		}
		check("save task", s.UpsertTask(ctx, ev.GorunUUID, ev.Server, ev.Task, ev.State, ev.Command, ev.Stdout, ev.Stderr, ev.ExitCode, time.Duration(ev.Duration)))
		check("set task envvars", s.SetTaskEnvVars(ctx, ev.GorunUUID, ev.Server, ev.Task, ev.EnvVars, ev.Vars))
		if ev.Check != nil || ev.Verify != nil {
			check("set task steps", s.SetTaskSteps(ctx, ev.GorunUUID, ev.Server, ev.Task, ev.Check, ev.Verify))
		}
	case "process_finished":
		check("finish run", s.FinishRun(ctx, ev.GorunUUID, ev.Success, ev.TotalTasks, ev.Failed, ev.Skipped, time.Duration(ev.Duration), parseTime(ev.FinishedAt)))
	}
}

// parseTime reads an event's timestamp, or returns now when it has none.
func parseTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return time.Now()
}
