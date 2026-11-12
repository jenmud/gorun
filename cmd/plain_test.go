package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jenmud/gorun"
)

// lastLine returns the last non-empty line of s.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// feed sends events to a fresh plainPrinter and returns what it printed.
func feed(events ...any) string {
	var buf bytes.Buffer
	p := &plainPrinter{w: &buf}
	for _, ev := range events {
		p.event(ev)
	}
	return buf.String()
}

func TestPlainSummaryLine(t *testing.T) {
	out := feed(
		gorun.ProcessStartedEvent{Servers: []string{"web-1", "web-2"}},
		gorun.TaskEvent{Server: "web-1", Task: "a", State: gorun.TaskDone},
		gorun.TaskEvent{Server: "web-2", Task: "a", State: gorun.TaskDone},
		gorun.TaskEvent{Server: "web-1", Task: "b", State: gorun.TaskFailed, ExitCode: 1},
		gorun.TaskEvent{Server: "web-1", Task: "c", State: gorun.TaskSkipped},
		gorun.ProcessFinishedEvent{TotalTasks: 5, Failed: 1, Skipped: 1, Unchanged: 1, Duration: 1500 * time.Millisecond},
	)

	want := "summary: 2 servers, 5 tasks, 2 ok, 1 unchanged, 1 skipped, 1 failed (1.5s)"
	if got := lastLine(out); got != want {
		t.Errorf("last line = %q, want %q\nfull output:\n%s", got, want, out)
	}
}

func TestPlainSummaryZeroCounts(t *testing.T) {
	out := feed(
		gorun.ProcessStartedEvent{Servers: []string{"local"}},
		gorun.TaskEvent{Server: "local", Task: "a", State: gorun.TaskDone},
		gorun.ProcessFinishedEvent{TotalTasks: 1, Success: true, Duration: 4213 * time.Millisecond},
	)

	want := "summary: 1 servers, 1 tasks, 1 ok, 0 unchanged, 0 skipped, 0 failed (4.213s)"
	if got := lastLine(out); got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
}

func TestPlainSummaryReplacesUnchangedLine(t *testing.T) {
	out := feed(
		gorun.ProcessStartedEvent{Servers: []string{"local"}},
		gorun.TaskEvent{Server: "local", Task: "a", State: gorun.TaskUnchanged},
		gorun.ProcessFinishedEvent{TotalTasks: 1, Unchanged: 1, Success: true},
	)

	if strings.Contains(out, "their checks found nothing to do") {
		t.Errorf("output still has the old unchanged line:\n%s", out)
	}
	if !strings.Contains(lastLine(out), "1 unchanged") {
		t.Errorf("last line = %q, want it to count 1 unchanged", lastLine(out))
	}
}

func TestPlainSummaryCheckOnly(t *testing.T) {
	out := feed(
		gorun.ProcessStartedEvent{Servers: []string{"local"}, CheckOnly: true},
		gorun.TaskEvent{Server: "local", Task: "a", State: gorun.TaskWouldRun},
		gorun.TaskEvent{Server: "local", Task: "b", State: gorun.TaskUnchanged},
		gorun.ProcessFinishedEvent{TotalTasks: 2, WouldRun: 1, Unchanged: 1},
	)

	if got, want := lastLine(out), "1 would run, 1 unchanged"; got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
	if strings.Contains(out, "summary:") {
		t.Errorf("check-only output has a summary: line:\n%s", out)
	}
}

func TestPlainSummaryOnFailure(t *testing.T) {
	t.Setenv("GORUN_SKIP_NATS", "1")

	var buf bytes.Buffer
	c := gorun.Config{
		Tasks: map[string]gorun.Task{
			"good": {Name: "good", Cmd: "true", Local: true},
			"bad":  {Name: "bad", Cmd: "exit 3", Local: true},
		},
		OnEvent: (&plainPrinter{w: &buf}).event,
	}
	server := gorun.Server{Name: "local", TaskNames: []string{"good", "bad"}}

	if err := c.Run(context.Background(), server); err == nil {
		t.Fatalf("Run returned no error, want one for the failing task\n%s", buf.String())
	}

	got := lastLine(buf.String())
	if !strings.HasPrefix(got, "summary: 1 servers, 2 tasks, 1 ok, 0 unchanged, 0 skipped, 1 failed (") {
		t.Errorf("last line = %q, want the summary line\nfull output:\n%s", got, buf.String())
	}
}
