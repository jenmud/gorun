package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestApplyRecordsPlannedTasksAndServerErrors(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	st.Apply(ctx, &Event{Type: "process_started", GorunUUID: "r1", Title: "demo", Servers: []string{"web-1"}})
	st.Apply(ctx, &Event{Type: "plan", GorunUUID: "r1", Server: "web-1", Address: "10.0.0.1", Tasks: []string{"a", "b"}})
	st.Apply(ctx, &Event{Type: "server_error", GorunUUID: "r1", Server: "web-1", Error: "connection refused"})
	st.Apply(ctx, &Event{Type: "process_finished", GorunUUID: "r1", Failed: 2})

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
	st.Apply(ctx, &Event{Type: "task", GorunUUID: "r1", Server: "web-1", Task: "a", State: 2, Stdout: "ok"})
	st.Apply(ctx, &Event{Type: "plan", GorunUUID: "r1", Server: "web-1", Tasks: []string{"a"}})
	a, err := st.GetTask(ctx, "r1", "web-1", "a")
	if err != nil || a.State != 2 || a.Stdout != "ok" {
		t.Errorf("task a = %+v, %v; want it left as it ran", a, err)
	}
}

func TestApplyKeepsCheckAndVerify(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	st.Apply(ctx, &Event{Type: "process_started", GorunUUID: "r1"})
	st.Apply(ctx, &Event{Type: "plan", GorunUUID: "r1", Server: "web-1", Tasks: []string{"deploy"}})
	st.Apply(ctx, &Event{Type: "task", GorunUUID: "r1", Server: "web-1", Task: "deploy", State: StateUnchanged,
		Check: &Step{Command: "test -f /etc/app", ExitCode: 0, Stdout: "present"}})

	task, err := st.GetTask(ctx, "r1", "web-1", "deploy")
	if err != nil {
		t.Fatal(err)
	}
	if task.State != StateUnchanged || task.Check == nil || task.Check.Stdout != "present" || task.Verify != nil {
		t.Errorf("task = %+v, check = %+v; want unchanged with the check kept", task, task.Check)
	}
}
