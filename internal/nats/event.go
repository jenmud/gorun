package nats

import (
	"context"
	"encoding/json"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/jenmud/gorun/cmd/web/ui"
)

// Event represents a NATS message payload.
type Event struct {
	Type       string     `json:"type"`
	GorunUUID  string     `json:"gorun_uuid"`
	Title      string     `json:"title"`
	Servers    []string   `json:"servers"`
	StartedAt  string     `json:"started_at"`
	Hostname   string     `json:"hostname"`
	PID        int        `json:"pid"`
	Success    bool       `json:"success"`
	Duration   int64      `json:"duration"`
	TotalTasks int        `json:"total_tasks"`
	Failed     int        `json:"failed"`
	Skipped    int        `json:"skipped"`
	FinishedAt string     `json:"finished_at"`
	Server     string     `json:"server"`
	Address    string     `json:"address"`
	Tasks      []string   `json:"tasks"`
	TaskMeta   []TaskMeta `json:"task_meta,omitempty"`
	Task       string     `json:"task"`
	State      int        `json:"state"`
	Stdout     string     `json:"stdout"`
	Stderr     string     `json:"stderr"`
	Command    string     `json:"command"`
	ExitCode   int        `json:"exit_code"`
	Attempt    int        `json:"attempt,omitempty"`
	Attempts   int        `json:"attempts,omitempty"`
}

// TaskMeta carries the documentation attached to a task in a plan event.
type TaskMeta struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Details     string `json:"details,omitempty"`
	Rerun       string `json:"rerun,omitempty"`
}

// UIEvent converts the event into the type used by the web dashboard. Keeping
// the mapping in one place means new fields cannot be silently dropped by one
// of the listeners.
func (e Event) UIEvent() ui.Event {
	return ui.Event{
		Type:       e.Type,
		GorunUUID:  e.GorunUUID,
		Title:      e.Title,
		Servers:    e.Servers,
		StartedAt:  e.StartedAt,
		Hostname:   e.Hostname,
		PID:        e.PID,
		Success:    e.Success,
		Duration:   e.Duration,
		TotalTasks: e.TotalTasks,
		Failed:     e.Failed,
		Skipped:    e.Skipped,
		FinishedAt: e.FinishedAt,
		Server:     e.Server,
		Address:    e.Address,
		Tasks:      e.Tasks,
		TaskMeta:   taskMeta(e.TaskMeta),
		Task:       e.Task,
		State:      e.State,
		Stdout:     e.Stdout,
		Stderr:     e.Stderr,
		Command:    e.Command,
		ExitCode:   e.ExitCode,
		Attempt:    e.Attempt,
		Attempts:   e.Attempts,
	}
}

// taskMeta converts event metadata for the web UI package without making the
// two packages share a type.
func taskMeta(meta []TaskMeta) []ui.TaskMeta {
	if len(meta) == 0 {
		return nil
	}
	out := make([]ui.TaskMeta, len(meta))
	for i, m := range meta {
		out[i] = ui.TaskMeta{Name: m.Name, Description: m.Description, Details: m.Details, Rerun: m.Rerun}
	}
	return out
}

// UnmarshalJSON unmarshals the event with tracing and metrics.
type rawEvent Event

func (e *Event) UnmarshalJSON(data []byte) error {
	startTime := time.Now()
	if err := json.Unmarshal(data, (*rawEvent)(e)); err != nil {
		messageUnmarshalErrors.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("event_type", string(data[:20])),
		))
		return err
	}

	eventProcessingDuration.Record(context.Background(), float64(time.Since(startTime)), metric.WithAttributes(
		attribute.String("type", e.Type),
		attribute.String("gorun_uuid", e.GorunUUID),
	))

	eventTypes.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("type", e.Type),
	))

	return nil
}
