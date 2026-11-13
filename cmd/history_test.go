package main

import (
	"context"
	"errors"
	"fmt"
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

	// Select the task and expand its command section in the inspector.
	m.expanded["host-1"] = true
	for i := range m.items {
		if !m.items[i].isServer {
			m.cursor = i
			break
		}
	}
	m.focusRight = true
	m.sectionsOpen["task:host-1\x00jj-config:command"] = true

	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"$ #!/bin/sh", "set -e", "if [ -d ~/.config/jj ]; then", "echo ok", "fi"} {
		if !strings.Contains(view, want) {
			t.Errorf("expanded command should contain %q, got:\n%s", want, view)
		}
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

func TestHistoryCapturesRunServerAndTaskContext(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	const runID = "context-run"
	recordEvent(ctx, st, gorun.ProcessStartedEvent{
		Type: "process_started", GorunUUID: runID, Title: "deploy", Servers: []string{"web-1"},
		StartedAt: time.Now(), EnvVars: map[string]string{"RUN_MODE": "prod"}, Vars: map[string]any{"region": "eu"},
	})
	recordEvent(ctx, st, gorun.PlanEvent{
		Type: "plan", GorunUUID: runID, Server: "web-1", Address: "web.example.com",
		EnvVars: map[string]string{"SERVER_MODE": "web"}, Vars: map[string]any{"host": "web-1"},
	})
	recordEvent(ctx, st, gorun.TaskEvent{
		Type: "task", GorunUUID: runID, Server: "web-1", Task: "verify", State: gorun.TaskDone,
		EnvVars: map[string]string{"TASK_MODE": "check"}, Vars: map[string]any{"attempt": 1},
	})

	run, err := st.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.RunEnvVars["RUN_MODE"] != "prod" || run.RunVars["region"] != "eu" {
		t.Errorf("run defaults not stored: env=%v vars=%v", run.RunEnvVars, run.RunVars)
	}
	if run.ServerAddress["web-1"] != "web.example.com" || run.ServerEnvVars["web-1"]["SERVER_MODE"] != "web" || run.ServerVars["web-1"]["host"] != "web-1" {
		t.Errorf("server context not stored: address=%v env=%v vars=%v", run.ServerAddress, run.ServerEnvVars, run.ServerVars)
	}
	task, err := st.GetTask(ctx, runID, "web-1", "verify")
	if err != nil {
		t.Fatal(err)
	}
	if task.EnvVars["TASK_MODE"] != "check" || task.Vars["attempt"] != float64(1) {
		t.Errorf("task context not stored: env=%v vars=%v", task.EnvVars, task.Vars)
	}
}

func TestHistoryTaskInspectorExpandsAndScrollsSections(t *testing.T) {
	command := make([]string, 120)
	for i := range command {
		command[i] = fmt.Sprintf("command-line-%03d", i)
	}
	run := &store.RunDetail{
		RunSummary:    store.RunSummary{ID: "inspector", Title: "deploy", Finished: true, TotalTasks: 1},
		Servers:       []string{"web-1"},
		RunEnvVars:    map[string]string{"RUN_MODE": "prod"},
		RunVars:       map[string]any{"region": "eu"},
		ServerEnvVars: map[string]map[string]string{"web-1": {"SERVER_MODE": "web"}},
		ServerVars:    map[string]map[string]any{"web-1": {"host": "web-1"}},
		Tasks: []store.TaskDetail{{
			Server: "web-1", Name: "verify", State: 3, ExitCode: 7, Duration: 3 * time.Second,
			Command: strings.Join(command, "\n"), Stdout: "stdout text", Stderr: "stderr text",
			EnvVars: map[string]string{"TASK_MODE": "verify"}, Vars: map[string]any{"attempt": 2},
		}},
	}
	m := newHistoryModel(nil)
	m.width, m.height = 120, 32
	m.run = run
	m.state = historyDetail
	m.buildItemsForRun(run)
	m.loadedTasks["web-1\x00verify"] = true

	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "verify") || !strings.Contains(view, "Servers and tasks") {
		t.Fatalf("servers should start collapsed in the left pane:\n%s", view)
	}

	// Expand the server, select its task, and inspect the collapsed task sections.
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(historyModel)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(historyModel)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(historyModel)
	view = ansi.Strip(m.View().Content)
	if strings.Contains(view, "command-line-000") || !strings.Contains(view, "Command") || !strings.Contains(view, "Run env vars") || !strings.Contains(view, "Task env vars") || !strings.Contains(view, "Task vars") {
		t.Fatalf("task sections should be listed collapsed by default:\n%s", view)
	}
	if !strings.Contains(view, "Task details") {
		t.Fatalf("history details should use the two-pane inspector:\n%s", view)
	}

	selectSection := func(name string) {
		meta, sections := m.detailSections()
		rows := m.detailPanelRows(meta, sections)
		for sectionIndex, section := range sections {
			if section.title != name {
				continue
			}
			for rowIndex, row := range rows {
				if row.sectionHead && row.section == sectionIndex {
					m.rightCursor = rowIndex
					return
				}
			}
		}
	}
	selectSection("Command")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(historyModel)
	view = ansi.Strip(m.View().Content)
	for _, want := range []string{"command-line-000", "Exit code: 7", "3s"} {
		if !strings.Contains(view, want) {
			t.Errorf("expanded task inspector should show %q, got:\n%s", want, view)
		}
	}

	for i := 0; i < 120; i++ {
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = next.(historyModel)
	}
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "command-line-119") {
		t.Fatalf("down should scroll through the full command:\n%s", view)
	}

	for _, sectionName := range []string{"Task env vars", "Task vars"} {
		selectSection(sectionName)
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = next.(historyModel)
		view = ansi.Strip(m.View().Content)
		want := "TASK_MODE"
		if sectionName == "Task vars" {
			want = "attempt"
		}
		if !strings.Contains(view, want) {
			t.Errorf("expanding %s should show %q, got:\n%s", sectionName, want, view)
		}
	}
}

// The history list and a run's header show who started the run, where and how.
func TestHistoryShowsWhoStartedARun(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const runID = "origin-run"
	recordEvent(context.Background(), st, gorun.ProcessStartedEvent{Type: "process_started", GorunUUID: runID, Title: "deploy",
		Servers: []string{"host-1"}, StartedAt: time.Now(), Hostname: "laptop",
		Origin: &gorun.Origin{User: "jen", Hostname: "laptop", Trigger: "tui", Version: "v0.21.0",
			Git: &gorun.GitInfo{Commit: "abc1234def", Branch: "main"}}})

	m := newHistoryModel(st)
	m.width, m.height = 160, 30
	runs, err := st.ListRuns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m.runs = runs
	if list := ansi.Strip(m.View().Content); !strings.Contains(list, "[jen@laptop · tui]") {
		t.Errorf("the runs list should say who and where, got:\n%s", list)
	}

	next, _ := m.Update(m.loadRun(runID))
	m = next.(historyModel)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "jen@laptop · tui · v0.21.0 · main@abc1234") {
		t.Errorf("the run header should say who and where, got:\n%s", view)
	}
}
