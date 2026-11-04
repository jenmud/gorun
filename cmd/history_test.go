package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

	// load the task's hidden detail (command) on demand
	next, _ = m.Update(m.loadTask(runID, "host-1", "jj-config"))
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

func TestHistoryServerTasksSortedSlowestFirst(t *testing.T) {
	run := &store.RunDetail{
		Servers: []string{"web-1"},
		Tasks: []store.TaskDetail{
			{Server: "web-1", Name: "fast", Duration: time.Second},
			{Server: "web-1", Name: "slow", Duration: 9 * time.Second},
			{Server: "web-1", Name: "medium", Duration: 4 * time.Second},
			{Server: "web-1", Name: "also-fast", Duration: time.Second},
		},
	}
	m := newHistoryModel(nil)
	m.buildItemsForRun(run)

	var got []string
	for _, item := range m.items {
		if !item.isServer {
			got = append(got, run.Tasks[item.taskIdx].Name)
		}
	}
	want := []string{"slow", "medium", "fast", "also-fast"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tasks = %v, want %v", got, want)
	}
}

// TestHistoryShowsServerError checks that a server which couldn't run at all (no
// task events, just a server error) shows why when it is expanded in the history.
func TestHistoryShowsServerError(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	recordEvent(ctx, st, gorun.ProcessStartedEvent{Type: "process_started", GorunUUID: "r1", Title: "run", Servers: []string{"web-1"}, StartedAt: time.Now()})
	recordEvent(ctx, st, gorun.ServerErrorEvent{Type: "server_error", GorunUUID: "r1", Server: "web-1", Err: errors.New("error creating SSH connection: connection refused")})

	run, err := st.GetRun(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got := run.ServerErrors["web-1"]; got != "error creating SSH connection: connection refused" {
		t.Fatalf("the server's error should be recorded, got %q", got)
	}

	m := newHistoryModel(st)
	m.width, m.height = 120, 30
	next, _ := m.Update(historyRunMsg{run: run})
	m = next.(historyModel)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(historyModel)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "✗ error creating SSH connection: connection refused") {
		t.Errorf("expanding the server should show its error, got:\n%s", view)
	}
}
