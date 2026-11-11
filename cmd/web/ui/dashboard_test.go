package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jenmud/gorun/internal/store"
)

func TestDashboardPlanCarriesTaskMetadata(t *testing.T) {
	var state DashboardState
	state.Apply(store.Event{Type: "process_started", GorunUUID: "run-1", Title: "demo"})
	state.Apply(store.Event{
		Type:      "plan",
		GorunUUID: "run-1",
		Server:    "web-1",
		Tasks:     []string{"deploy"},
		TaskMeta: []store.TaskMeta{{
			Name:        "deploy",
			Description: "Deploy the app",
			Details:     "Replaces the running binary.",
			Rerun:       "unsafe",
		}},
	})

	process := state.Processes["run-1"]
	if process == nil {
		t.Fatal("expected the run in the dashboard")
	}
	server := process.Servers["web-1"]
	if server == nil {
		t.Fatal("expected the server in the dashboard")
	}
	task := server.Tasks["deploy"]
	if task == nil {
		t.Fatal("expected the task in the dashboard")
	}
	if task.Description != "Deploy the app" || task.Details != "Replaces the running binary." || task.Rerun != "unsafe" {
		t.Errorf("task metadata = %+v, want the plan's description, details and rerun value", task)
	}

	var buf bytes.Buffer
	if err := Dashboard(state).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Deploy the app", "Replaces the running binary.", "Not safe to rerun"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("dashboard output does not contain %q", want)
		}
	}
}

func TestDashboardShowsAttemptProgress(t *testing.T) {
	var state DashboardState
	state.Apply(store.Event{Type: "process_started", GorunUUID: "run-2", Title: "demo"})
	state.Apply(store.Event{
		Type:      "plan",
		GorunUUID: "run-2",
		Server:    "web-1",
		Tasks:     []string{"flaky"},
	})
	state.Apply(store.Event{
		Type:      "task",
		GorunUUID: "run-2",
		Server:    "web-1",
		Task:      "flaky",
		State:     1,
		Attempt:   2,
		Attempts:  3,
	})

	task := state.Processes["run-2"].Servers["web-1"].Tasks["flaky"]
	if task.Attempt != 2 || task.Attempts != 3 {
		t.Errorf("attempt = %d/%d, want 2/3", task.Attempt, task.Attempts)
	}

	var buf bytes.Buffer
	if err := Dashboard(state).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "attempt 2/3") {
		t.Error("dashboard output does not show the attempt progress")
	}
}

func TestDashboardShowsAServerThatCouldNotRun(t *testing.T) {
	var state DashboardState
	state.Apply(store.Event{Type: "process_started", GorunUUID: "run-1", Title: "demo"})
	state.Apply(store.Event{Type: "plan", GorunUUID: "run-1", Server: "web-1", Tasks: []string{"a", "b"}})
	state.Apply(store.Event{Type: "server_error", GorunUUID: "run-1", Server: "web-1", Error: "dial tcp 10.0.0.1:22: connection refused"})
	state.Apply(store.Event{Type: "process_finished", GorunUUID: "run-1", Failed: 2})

	p := state.Processes["run-1"]
	if !processHasFailed(p) || serversFailed(p) != 1 {
		t.Fatalf("failed = %v, servers failed = %d, want the run marked failed", processHasFailed(p), serversFailed(p))
	}
	var buf bytes.Buffer
	if err := Dashboard(state).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{"connection refused", "could not run its tasks", "Its 2 tasks did not run.", "not run"} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard does not contain %q", want)
		}
	}
	if strings.Contains(html, "pending") {
		t.Error("tasks that will never run must not read as pending")
	}
	if strings.Contains(html, "<details data-key=\"run-1\" class=\"group/run overflow-hidden rounded-2xl border border-red-900/50 bg-red-950/10\" open") {
		t.Error("a run should start collapsed")
	}
}

func TestFinishedRunListsSlowestTaskFirst(t *testing.T) {
	var state DashboardState
	state.Apply(store.Event{Type: "plan", GorunUUID: "r", Server: "s", Tasks: []string{"fast", "slow", "mid"}})
	for name, d := range map[string]int64{"fast": 1, "slow": 9, "mid": 5} {
		state.Apply(store.Event{Type: "task", GorunUUID: "r", Server: "s", Task: name, State: 2, Duration: d})
	}
	p := state.Processes["r"]
	srv := p.Servers["s"]

	if got := sortedTasks(p, srv)[0].Name; got != "fast" {
		t.Errorf("while running the first task = %s, want plan order (fast)", got)
	}
	state.Apply(store.Event{Type: "process_finished", GorunUUID: "r"})
	var names []string
	for _, task := range sortedTasks(p, srv) {
		names = append(names, task.Name)
	}
	if strings.Join(names, ",") != "slow,mid,fast" {
		t.Errorf("finished order = %v, want slow,mid,fast", names)
	}
}

func TestDashboardShowsCopyProgress(t *testing.T) {
	var state DashboardState
	state.Apply(store.Event{Type: "process_started", GorunUUID: "run-3", Title: "demo"})
	state.Apply(store.Event{Type: "plan", GorunUUID: "run-3", Server: "web-1", Tasks: []string{"upload"}})
	state.Apply(store.Event{Type: "task", GorunUUID: "run-3", Server: "web-1", Task: "upload", State: 1})
	state.Apply(store.Event{
		Type: "copy_progress", GorunUUID: "run-3", Server: "web-1", Task: "upload",
		File: "/srv/app/release.tar.gz", Bytes: 440 << 20, Total: 1 << 30, Percent: 42.97, BytesPerSecond: 12 << 20,
	})

	var buf bytes.Buffer
	if err := Dashboard(state).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{"↑ release.tar.gz", "440.0 MB / 1.0 GB · 12.0 MB/s", `aria-valuenow="42"`, "width: 43.0%", "↑ release.tar.gz 42% · 0/1 tasks"} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard does not show %q", want)
		}
	}

	state.Apply(store.Event{Type: "task", GorunUUID: "run-3", Server: "web-1", Task: "upload", State: 2})
	if state.Processes["run-3"].Servers["web-1"].Tasks["upload"].Copy != nil {
		t.Error("a finished task still shows its copy")
	}
}

// A live task shows its check and verify: from the plan before they run, which one is
// running, and their command and output once they have.
func TestDashboardShowsCheckAndVerify(t *testing.T) {
	var state DashboardState
	state.Apply(store.Event{Type: "process_started", GorunUUID: "run-1", Title: "demo"})
	state.Apply(store.Event{Type: "plan", GorunUUID: "run-1", Server: "web-1", Tasks: []string{"install"},
		TaskMeta: []store.TaskMeta{{Name: "install", Check: "dpkg -s nginx", Verify: "nginx -v"}}})
	render := func() string {
		var buf bytes.Buffer
		if err := Dashboard(state).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	if html := render(); !strings.Contains(html, "$ dpkg -s nginx") || !strings.Contains(html, "$ nginx -v") {
		t.Errorf("planned check and verify missing:\n%s", html)
	}
	state.Apply(store.Event{Type: "task", GorunUUID: "run-1", Server: "web-1", Task: "install", State: store.StateRunning, Phase: "check", Command: "dpkg -s nginx"})
	if html := render(); !strings.Contains(html, "checking…") {
		t.Errorf("running check not marked")
	}
	if task := state.Processes["run-1"].Servers["web-1"].Tasks["install"]; task.Command != "" {
		t.Errorf("a running check's command shown as the task's: %q", task.Command)
	}
	state.Apply(store.Event{Type: "task", GorunUUID: "run-1", Server: "web-1", Task: "install", State: store.StateDone,
		Command: "apt-get install -y nginx",
		Check:   &store.Step{Command: "dpkg -s nginx", ExitCode: 1, Stderr: "not installed"},
		Verify:  &store.Step{Command: "nginx -v", Stdout: "nginx/1.24"}})
	html := render()
	for _, want := range []string{"Check command", "not installed", "Verify command", "nginx/1.24", "apt-get install -y nginx"} {
		if !strings.Contains(html, want) {
			t.Errorf("%q missing after the run", want)
		}
	}
}

// A live task lists the files it copies, from the plan.
func TestDashboardShowsCopies(t *testing.T) {
	var state DashboardState
	state.Apply(store.Event{Type: "process_started", GorunUUID: "run-1", Title: "demo"})
	state.Apply(store.Event{Type: "plan", GorunUUID: "run-1", Server: "web-1", Tasks: []string{"compose-up"},
		TaskMeta: []store.TaskMeta{{Name: "compose-up", Copies: []string{"scripts/stack-ok.sh → /home/ubuntu/stack-ok.sh"}}}})
	var buf bytes.Buffer
	if err := Dashboard(state).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if html := buf.String(); !strings.Contains(html, "scripts/stack-ok.sh → /home/ubuntu/stack-ok.sh") || !strings.Contains(html, ">Copies<") {
		t.Errorf("copies missing from the live task:\n%s", html)
	}
}
