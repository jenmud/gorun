package nats

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jenmud/gorun/internal/store"
)

func TestUIEventCarriesTaskMetadataAndAttempts(t *testing.T) {
	ev := Event{
		Type:      "task",
		GorunUUID: "run-1",
		Server:    "web-1",
		Task:      "deploy",
		State:     1,
		Attempt:   2,
		Attempts:  3,
		TaskMeta: []TaskMeta{{
			Name:        "deploy",
			Description: "Deploy the app",
			Details:     "Replaces the running binary.",
			Rerun:       "unsafe",
		}},
	}

	got := ev.UIEvent()
	if got.Attempt != 2 || got.Attempts != 3 {
		t.Errorf("attempt = %d/%d, want 2/3", got.Attempt, got.Attempts)
	}
	if len(got.TaskMeta) != 1 {
		t.Fatalf("task metadata = %v, want one entry", got.TaskMeta)
	}
	meta := got.TaskMeta[0]
	if meta.Name != "deploy" || meta.Description != "Deploy the app" || meta.Details != "Replaces the running binary." || meta.Rerun != "unsafe" {
		t.Errorf("task metadata = %+v, want the event's description, details and rerun value", meta)
	}
}

func TestUpdateDatabaseRecordsPlannedTasksAndServerErrors(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	UpdateDatabase(ctx, st, &Event{Type: "process_started", GorunUUID: "r1", Title: "demo", Servers: []string{"web-1"}})
	UpdateDatabase(ctx, st, &Event{Type: "plan", GorunUUID: "r1", Server: "web-1", Address: "10.0.0.1", Tasks: []string{"a", "b"}})
	UpdateDatabase(ctx, st, &Event{Type: "server_error", GorunUUID: "r1", Server: "web-1", Error: "connection refused"})
	UpdateDatabase(ctx, st, &Event{Type: "process_finished", GorunUUID: "r1", Failed: 2})

	run, err := st.GetRun(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got := run.ServerErrors["web-1"]; got != "connection refused" {
		t.Errorf("server error = %q, want the reason stored", got)
	}
	if len(run.Tasks) != 2 {
		t.Fatalf("tasks = %d, want the 2 planned tasks recorded", len(run.Tasks))
	}

	// A task that already ran must not be reset by a replayed plan.
	UpdateDatabase(ctx, st, &Event{Type: "task", GorunUUID: "r1", Server: "web-1", Task: "a", State: 2, Stdout: "ok"})
	UpdateDatabase(ctx, st, &Event{Type: "plan", GorunUUID: "r1", Server: "web-1", Tasks: []string{"a"}})
	a, err := st.GetTask(ctx, "r1", "web-1", "a")
	if err != nil || a.State != 2 || a.Stdout != "ok" {
		t.Errorf("task a = %+v, %v; want it left as it ran", a, err)
	}
}
