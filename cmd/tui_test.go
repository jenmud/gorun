package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jenmud/gorun"
)

// bigTUIModel builds a model the size of recipes/04-dev-server against a
// struggling lab: 100 servers × 240 tasks, with a fifth failed and most of the
// rest skipped.
func bigTUIModel(tb testing.TB) tuiModel {
	tb.Helper()
	m := newTUIModel("big", false, func() {})
	m.width, m.height = 160, 50

	const servers, tasks = 100, 240
	events := []any{}
	for s := range servers {
		server := fmt.Sprintf("server%d", s+1)
		names := make([]string, tasks)
		meta := make([]gorun.TaskMeta, tasks)
		for t := range tasks {
			names[t] = fmt.Sprintf("task-%03d", t)
			meta[t] = gorun.TaskMeta{Name: names[t], Description: "Do the thing " + names[t], Details: "Some details about " + names[t]}
		}
		events = append(events, gorun.PlanEvent{Server: server, Address: "localhost:22", Tasks: names, TaskMeta: meta})
		for t, name := range names {
			ev := gorun.TaskEvent{Server: server, Task: name, State: gorun.TaskDone, Stdout: "ok\n", Duration: time.Second}
			switch {
			case t%5 == 0:
				ev.State = gorun.TaskFailed
				ev.ExitCode = 255
				ev.Err = errors.New("ssh: handshake failed: connection reset by peer")
				ev.Stderr = "line 1\nline 2\nline 3\n"
			case t%5 != 1:
				ev.State = gorun.TaskSkipped
				ev.Err = errors.New("dependency " + names[t-t%5] + " failed")
			}
			events = append(events, ev)
		}
	}
	next, cmd := m.Update(tuiBatchMsg{events: events})
	m = next.(tuiModel)
	// run the background box render the batch kicked off
	next, _ = m.Update(cmd())
	m = next.(tuiModel)
	if len(m.boxLines) == 0 {
		tb.Fatal("failure boxes were not rendered")
	}
	return m
}

// TestTUIStaysResponsive guards against the TUI freezing on big runs: every
// keypress and every render must stay cheap no matter how many tasks there are.
func TestTUIStaysResponsive(t *testing.T) {
	m := bigTUIModel(t)

	keys := []string{"v", "end", "home", "pgdown", "pgup", "down", "up", "enter", "v", "end", "home", "pgdown", "pgup"}
	for _, k := range keys {
		start := time.Now()
		next, _ := m.Update(keyPress(k))
		m = next.(tuiModel)
		_ = m.View()
		if d := time.Since(start); d > 100*time.Millisecond {
			t.Errorf("key %q took %s to handle and render", k, d)
		}
	}

	// a batch of events landing must not stall the UI either
	start := time.Now()
	next, _ := m.Update(tuiBatchMsg{events: []any{gorun.TaskEvent{Server: "server1", Task: "task-001", State: gorun.TaskDone}}})
	m = next.(tuiModel)
	_ = m.View()
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("event batch took %s to handle and render", d)
	}

	next, cmd := m.Update(keyPress("q"))
	if !next.(tuiModel).quitting || cmd == nil {
		t.Fatal("q did not quit")
	}
}

func keyPress(k string) tea.KeyPressMsg {
	switch k {
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

func BenchmarkTUICompactView(b *testing.B) {
	m := bigTUIModel(b)
	b.ResetTimer()
	for range b.N {
		_ = m.View()
	}
}

// TestCompactWindow checks the windowed compact view matches rendering every
// line and slicing, for any scroll offset.
func TestCompactWindow(t *testing.T) {
	m := newTUIModel("small", false, func() {})
	m.width, m.height = 120, 12
	next, cmd := m.Update(tuiBatchMsg{events: []any{
		gorun.PlanEvent{Server: "a", Address: "a:22", Tasks: []string{"one", "two", "three"}},
		gorun.PlanEvent{Server: "b", Address: "b:22", Tasks: []string{"one"}},
		gorun.TaskEvent{Server: "a", Task: "one", State: gorun.TaskFailed, ExitCode: 1, Err: errors.New("boom")},
		gorun.TaskEvent{Server: "a", Task: "two", State: gorun.TaskSkipped, Err: errors.New("dependency one failed")},
		gorun.ServerErrorEvent{Server: "b", Err: errors.New("no route to host")},
	}})
	m = next.(tuiModel)
	next, _ = m.Update(cmd())
	m = next.(tuiModel)

	full := m.compactHead()
	for _, s := range m.servers {
		full = append(full, strings.Split(m.serverSummary(s), "\n")...)
		full = append(full, "")
	}
	full = append(full, strings.Split(m.failureBox(m.servers[0], m.servers[0].tasks[0]), "\n")...)
	full = append(full, strings.Split(m.skipBox("a", m.servers[0].tasks[1]), "\n")...)
	full = append(full, strings.Split(m.serverErrorBox(m.servers[1]), "\n")...)
	if got := m.compactLineCount(); got != len(full) {
		t.Fatalf("line count = %d, want %d", got, len(full))
	}
	if !strings.Contains(strings.Join(full, "\n"), "skipped 1 tasks") {
		t.Error("failure box does not name the skipped task")
	}

	for from := range len(full) {
		for to := from; to <= len(full)+2; to++ {
			got := strings.Join(m.compactWindow(from, to), "\n")
			want := strings.Join(full[from:min(to, len(full))], "\n")
			if got != want {
				t.Fatalf("window [%d,%d):\n%s\nwant:\n%s", from, to, got, want)
			}
		}
	}
}

// TestVerboseViewFitsScreen checks the selected task stays on screen, along
// with the footer, even when expanded failure boxes fill the view.
func TestVerboseViewFitsScreen(t *testing.T) {
	m := bigTUIModel(t)
	m.verbose = true
	for _, cursor := range []int{0, 1, 7, 500, 12345, len(m.flatTasks()) - 1} {
		m.cursor = cursor
		view := m.verboseView()
		if n := len(strings.Split(view, "\n")); n > m.height {
			t.Errorf("cursor %d: view is %d lines, screen is %d", cursor, n, m.height)
		}
		if !strings.Contains(view, "▸ ") {
			t.Errorf("cursor %d: selected task is not on screen", cursor)
		}
		if !strings.Contains(view, "q quit") {
			t.Errorf("cursor %d: footer is not on screen", cursor)
		}
	}
}
