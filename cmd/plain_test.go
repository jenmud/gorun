package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
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

// syncBuffer is a bytes.Buffer safe to read while a run writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor polls until s() contains want, failing the test after a few seconds.
func waitFor(t *testing.T, s func() string, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(s(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("output never had %q:\n%s", want, s())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// noInterrupts stands in for signal delivery when no second interrupt comes.
func noInterrupts() (<-chan os.Signal, func()) { return nil, func() {} }

// failQuit fails the test if a force quit happens.
func failQuit(t *testing.T) func() {
	return func() { t.Error("quit called, want no force quit") }
}

func TestPlainAbortMessage(t *testing.T) {
	var buf syncBuffer
	p := &plainPrinter{w: &buf}
	ctx, cancel := context.WithCancel(context.Background())
	stop := p.watchAbort(ctx, noInterrupts, failQuit(t))
	defer stop()

	cancel()
	waitFor(t, buf.String, abortLine)

	if got, want := buf.String(), abortLine+"\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestPlainAbortPrintedOnce(t *testing.T) {
	var buf syncBuffer
	p := &plainPrinter{w: &buf}
	ctx, cancel := context.WithCancel(context.Background())
	stop := p.watchAbort(ctx, noInterrupts, failQuit(t))

	cancel()
	cancel()
	waitFor(t, buf.String, abortLine)
	p.abort()
	stop()

	if n := strings.Count(buf.String(), abortLine); n != 1 {
		t.Errorf("abort line printed %d times, want 1:\n%s", n, buf.String())
	}
}

func TestPlainAbortSilentAfterStop(t *testing.T) {
	var buf syncBuffer
	p := &plainPrinter{w: &buf}
	ctx, cancel := context.WithCancel(context.Background())
	stop := p.watchAbort(ctx, noInterrupts, failQuit(t))

	stop()
	cancel()
	p.abort()
	time.Sleep(20 * time.Millisecond)

	if out := buf.String(); out != "" {
		t.Errorf("output = %q, want nothing after stop", out)
	}
}

// runAborted runs one long local task with the plain printer, cancels the run once the task
// is running, and returns the output.
func runAborted(t *testing.T, task gorun.Task, checkOnly bool) string {
	t.Helper()
	t.Setenv("GORUN_SKIP_NATS", "1")

	var buf syncBuffer
	p := &plainPrinter{w: &buf}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running := make(chan struct{})
	var once sync.Once
	c := gorun.Config{
		Tasks:     map[string]gorun.Task{task.Name: task},
		CheckOnly: checkOnly,
		OnEvent: func(ev any) {
			if te, ok := ev.(gorun.TaskEvent); ok && te.State == gorun.TaskRunning {
				once.Do(func() { close(running) })
			}
			p.event(ev)
		},
	}
	server := gorun.Server{Name: "local", TaskNames: []string{task.Name}}

	stop := p.watchAbort(ctx, noInterrupts, failQuit(t))
	go func() {
		<-running
		cancel()
	}()
	start := time.Now()
	_ = c.Run(ctx, server)
	stop()

	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Run took %s after the interrupt, want a few seconds at most", d)
	}
	return buf.String()
}

func TestPlainAbortBeforeSummary(t *testing.T) {
	out := runAborted(t, gorun.Task{Name: "slow", Cmd: "sleep 30", Local: true}, false)

	abort := strings.Index(out, abortLine)
	summary := strings.Index(out, "summary:")
	if abort < 0 || summary < 0 || abort > summary {
		t.Errorf("want the abort line before the summary line:\n%s", out)
	}
	if n := strings.Count(out, abortLine); n != 1 {
		t.Errorf("abort line printed %d times, want 1:\n%s", n, out)
	}
}

func TestPlainAbortCheckOnly(t *testing.T) {
	out := runAborted(t, gorun.Task{Name: "slow", Cmd: "true", Check: "sleep 30", Local: true}, true)

	abort := strings.Index(out, abortLine)
	wouldRun := strings.Index(out, " would run, ")
	if abort < 0 || wouldRun < 0 || abort > wouldRun {
		t.Errorf("want the abort line before the would run line:\n%s", out)
	}
	if strings.Contains(out, "summary:") {
		t.Errorf("check-only output has a summary: line:\n%s", out)
	}
}

func TestPlainAbortLineSaysCtrlCAgainForceQuits(t *testing.T) {
	if !strings.Contains(abortLine, "press ctrl+c again to force quit") {
		t.Errorf("abort line = %q, want it to say a second ctrl+c force quits", abortLine)
	}
}

func TestPlainAbortSecondInterruptForceQuits(t *testing.T) {
	var buf syncBuffer
	p := &plainPrinter{w: &buf}
	ctx, cancel := context.WithCancel(context.Background())
	again := make(chan os.Signal, 1)
	unregistered := make(chan struct{})
	interrupts := func() (<-chan os.Signal, func()) { return again, func() { close(unregistered) } }
	quit := make(chan struct{})
	stop := p.watchAbort(ctx, interrupts, func() { close(quit) })
	defer stop()

	cancel()
	waitFor(t, buf.String, abortLine)
	again <- os.Interrupt

	select {
	case <-quit:
	case <-time.After(5 * time.Second):
		t.Fatalf("second interrupt did not force quit:\n%s", buf.String())
	}
	<-unregistered
	if got, want := buf.String(), abortLine+"\n"+forceQuitLine+"\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestPlainAbortSecondInterruptAfterStopDoesNothing(t *testing.T) {
	var buf syncBuffer
	p := &plainPrinter{w: &buf}
	ctx, cancel := context.WithCancel(context.Background())
	again := make(chan os.Signal, 1)
	unregistered := make(chan struct{})
	interrupts := func() (<-chan os.Signal, func()) { return again, func() { close(unregistered) } }
	stop := p.watchAbort(ctx, interrupts, failQuit(t))

	cancel()
	waitFor(t, buf.String, abortLine)
	stop()
	<-unregistered
	again <- os.Interrupt
	time.Sleep(20 * time.Millisecond)

	if strings.Contains(buf.String(), forceQuitLine) {
		t.Errorf("force quit line printed after stop:\n%s", buf.String())
	}
}

func TestPlainNoInterruptNoSignalWatch(t *testing.T) {
	var buf syncBuffer
	p := &plainPrinter{w: &buf}
	called := false
	stop := p.watchAbort(context.Background(), func() (<-chan os.Signal, func()) {
		called = true
		return nil, func() {}
	}, failQuit(t))
	stop()

	if called || buf.String() != "" {
		t.Errorf("watcher without an interrupt registered for signals (%v) or printed %q", called, buf.String())
	}
}
