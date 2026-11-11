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

// A task's check and verify show in the history once the task's details load, with their
// commands and output, around the task's own command.
func TestHistoryShowsCheckAndVerify(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	const runID = "checks-run"
	recordEvent(ctx, st, gorun.ProcessStartedEvent{Type: "process_started", GorunUUID: runID, Title: "deploy", Servers: []string{"web-1"}, StartedAt: time.Now()})
	recordEvent(ctx, st, gorun.PlanEvent{Type: "plan", GorunUUID: runID, Server: "web-1", Tasks: []string{"install"}})
	recordEvent(ctx, st, gorun.TaskEvent{
		Type: "task", GorunUUID: runID, Server: "web-1", Task: "install", State: gorun.TaskDone,
		Command: "apt-get install -y nginx", Stdout: "installed\n",
		Check:  &gorun.StepResult{Command: "dpkg -s nginx", ExitCode: 1, Stdout: "check-out\n", Stderr: "not installed\n"},
		Verify: &gorun.StepResult{Command: "nginx -v", Stdout: "verify-out\n"},
	})

	m := newHistoryModel(st)
	m.width, m.height = 140, 80
	next, _ := m.Update(m.loadRun(runID))
	m = next.(historyModel)
	m.state = historyDetail
	for i, it := range m.items {
		if !it.isServer {
			m.cursor = i
		}
	}
	next, _ = m.Update(m.loadTask(runID, "web-1", "install"))
	m = next.(historyModel)

	_, sections := m.detailSections()
	titles := []string{}
	body := ""
	for _, s := range sections {
		titles = append(titles, s.title)
		body += strings.Join(s.lines, "\n") + "\n"
	}
	joined := strings.Join(titles, " | ")
	for _, want := range []string{"Check command", "Check stdout", "Check stderr", "Command", "Stdout", "Verify command", "Verify stdout"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no %q section; sections: %s", want, joined)
		}
	}
	for _, want := range []string{"$ dpkg -s nginx", "check-out", "not installed", "$ nginx -v", "verify-out"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q missing from the sections:\n%s", want, body)
		}
	}
}

// An expanded task in the live view shows its check and verify: their commands from the
// plan before they run, which step is running, and each one's result and output after.
func TestLiveViewShowsCheckAndVerify(t *testing.T) {
	m := newTUIModel("deploy", true, func() {})
	m.width, m.height = 140, 60
	step := func(msg any) {
		next, _ := m.Update(tuiBatchMsg{events: []any{msg}})
		m = next.(tuiModel)
	}
	step(gorun.PlanEvent{Server: "web-1", Tasks: []string{"install"}, TaskMeta: []gorun.TaskMeta{
		{Name: "install", Check: "dpkg -s nginx", Verify: "nginx -v"},
	}})
	task := m.servers[0].tasks[0]
	task.expanded = true
	box := ansi.Strip(m.outputBox(task))
	for _, want := range []string{"check", "$ dpkg -s nginx", "verify", "$ nginx -v"} {
		if !strings.Contains(box, want) {
			t.Errorf("before running, %q missing from:\n%s", want, box)
		}
	}

	step(gorun.TaskEvent{Server: "web-1", Task: "install", State: gorun.TaskRunning, Phase: gorun.PhaseCheck, Command: "dpkg -s nginx"})
	if box := ansi.Strip(m.outputBox(task)); !strings.Contains(box, "checking…") {
		t.Errorf("a running check isn't marked:\n%s", box)
	}
	step(gorun.TaskEvent{Server: "web-1", Task: "install", State: gorun.TaskRunning, Phase: gorun.PhaseRun, Command: "apt-get install -y nginx"})
	step(gorun.TaskEvent{Server: "web-1", Task: "install", State: gorun.TaskDone, Phase: gorun.PhaseVerify, Stdout: "installed\n",
		Check:  &gorun.StepResult{Command: "dpkg -s nginx", ExitCode: 1, Stderr: "not installed\n"},
		Verify: &gorun.StepResult{Command: "nginx -v", Stdout: "nginx/1.24\n"}})
	box = ansi.Strip(m.outputBox(task))
	for _, want := range []string{"$ dpkg -s nginx", "exit 1", "not installed", "command", "$ apt-get install -y nginx", "installed", "$ nginx -v", "exit 0", "nginx/1.24"} {
		if !strings.Contains(box, want) {
			t.Errorf("after running, %q missing from:\n%s", want, box)
		}
	}
	if i, j := strings.Index(box, "dpkg"), strings.Index(box, "nginx -v"); i < 0 || j < i {
		t.Errorf("the check should come before the verify:\n%s", box)
	}
}

// A task without a check or verify shows its output as it always did.
func TestLiveViewPlainTaskUnchanged(t *testing.T) {
	m := newTUIModel("deploy", true, func() {})
	m.width = 120
	next, _ := m.Update(tuiBatchMsg{events: []any{
		gorun.PlanEvent{Server: "web-1", Tasks: []string{"hello"}},
		gorun.TaskEvent{Server: "web-1", Task: "hello", State: gorun.TaskDone, Stdout: "hi\n"},
	}})
	m = next.(tuiModel)
	box := ansi.Strip(m.outputBox(m.servers[0].tasks[0]))
	if strings.Contains(box, "check") || strings.Contains(box, "verify") || !strings.Contains(box, "hi") {
		t.Errorf("plain task box:\n%s", box)
	}
}

// An expanded task in the live view lists the files it copies, before its command, and says
// when it is copying.
func TestLiveViewShowsCopies(t *testing.T) {
	m := newTUIModel("deploy", true, func() {})
	m.width, m.height = 140, 60
	step := func(msg any) {
		next, _ := m.Update(tuiBatchMsg{events: []any{msg}})
		m = next.(tuiModel)
	}
	step(gorun.PlanEvent{Server: "web-1", Tasks: []string{"compose-up"}, TaskMeta: []gorun.TaskMeta{
		{Name: "compose-up", Copies: []string{"scripts/stack-ok.sh → /home/ubuntu/stack-ok.sh"}},
	}})
	task := m.servers[0].tasks[0]
	box := ansi.Strip(m.outputBox(task))
	if !strings.Contains(box, "copy") || !strings.Contains(box, "scripts/stack-ok.sh → /home/ubuntu/stack-ok.sh") {
		t.Errorf("planned copies missing:\n%s", box)
	}
	step(gorun.TaskEvent{Server: "web-1", Task: "compose-up", State: gorun.TaskRunning, Phase: gorun.PhaseRun, Command: "sh compose-up.sh"})
	step(gorun.CopyProgressEvent{Server: "web-1", Task: "compose-up", File: "/home/ubuntu/stack-ok.sh", Total: 100, Bytes: 10})
	if box := ansi.Strip(m.outputBox(task)); !strings.Contains(box, "copying…") {
		t.Errorf("a running copy isn't marked:\n%s", box)
	}
	step(gorun.TaskEvent{Server: "web-1", Task: "compose-up", State: gorun.TaskDone, Stdout: "up\n"})
	box = ansi.Strip(m.outputBox(task))
	if i, j := strings.Index(box, "stack-ok.sh →"), strings.Index(box, "$ sh compose-up.sh"); i < 0 || j < 0 || j < i || strings.Contains(box, "copying…") {
		t.Errorf("after the run, the copy should be listed before the command, and no longer copying:\n%s", box)
	}
}
