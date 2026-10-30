package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/store"
)

func TestRecordEventFinishesRunAfterCancellation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	const runID = "cancelled-run"
	started := time.Now().Add(-time.Second)

	recordEvent(context.Background(), st, gorun.ProcessStartedEvent{
		Type:      "process_started",
		GorunUUID: runID,
		Title:     "cancelled run",
		Servers:   []string{"host-1"},
		StartedAt: started,
	})

	// Both events are emitted after q/cancel, so they must still be persisted.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	recordEvent(ctx, st, gorun.TaskEvent{
		Type:      "task",
		GorunUUID: runID,
		Server:    "host-1",
		Task:      "deploy",
		State:     gorun.TaskFailed,
		ExitCode:  130,
	})
	recordEvent(ctx, st, gorun.ProcessFinishedEvent{
		Type:       "process_finished",
		GorunUUID:  runID,
		Success:    false,
		TotalTasks: 1,
		Failed:     1,
		FinishedAt: time.Now(),
	})

	run, err := st.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Finished {
		t.Fatal("run is still marked as running after cancellation")
	}
	if run.Success {
		t.Error("cancelled run should not be marked successful")
	}
	if run.Failed != 1 {
		t.Errorf("failed tasks = %d, want 1", run.Failed)
	}
	if len(run.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(run.Tasks))
	}
	if run.Tasks[0].State != int(gorun.TaskFailed) {
		t.Errorf("task state = %d, want %d", run.Tasks[0].State, gorun.TaskFailed)
	}
}

func TestHistoryShowsMultiLineCommands(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	const runID = "multi-line-run"
	script := "#!/bin/sh\nset -e\nif [ -d ~/.config/jj ]; then\n\techo ok\nfi\n"
	recordEvent(context.Background(), st, gorun.ProcessStartedEvent{Type: "process_started", GorunUUID: runID, Title: "run", Servers: []string{"host-1"}, StartedAt: time.Now()})
	recordEvent(context.Background(), st, gorun.TaskEvent{Type: "task", GorunUUID: runID, Server: "host-1", Task: "jj-config", State: gorun.TaskDone, Command: script})

	m := newHistoryModel(st)
	m.height = 40
	next, _ := m.Update(m.loadRun(runID))
	m = next.(historyModel)

	// expand the server, then its task
	for i := range m.items {
		m.items[i].expanded = true
	}

	view := ansi.Strip(m.View().Content)
	want := strings.Join([]string{
		"        $ #!/bin/sh",
		"          set -e",
		"          if [ -d ~/.config/jj ]; then",
		"              echo ok",
		"          fi",
	}, "\n")
	if !strings.Contains(view, want) {
		t.Errorf("the command should be shown line by line, as written; got:\n%s", view)
	}
}
