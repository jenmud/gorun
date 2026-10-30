package nats

import (
	"context"
	"encoding/json"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
}

// TaskMeta carries the documentation attached to a task in a plan event.
type TaskMeta struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Details     string `json:"details,omitempty"`
	Rerun       string `json:"rerun,omitempty"`
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
