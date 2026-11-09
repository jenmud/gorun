package history_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/history"
)

const gorunfile = `title = "history test"

[tasks.hello]
cmd = "echo hello from history"
local = true

[tasks.broken]
cmd = "echo oops >&2; exit 3"
local = true

[servers]
tasks = ["hello", "broken"]

[servers.hosts.localhost]
`

// runOnce runs the test Gorunfile once, recording it to h, and returns the run's id.
func runOnce(t *testing.T, h *history.Store) string {
	t.Helper()
	var cfg gorun.Config
	if err := cfg.Load(strings.NewReader(gorunfile)); err != nil {
		t.Fatal(err)
	}
	var seen atomic.Int32
	cfg.OnEvent = func(any) { seen.Add(1) } // OnEvent still gets every event alongside the recorder
	cfg.Recorder = h
	_ = cfg.Run(context.Background()) // the broken task fails the run on purpose
	if seen.Load() == 0 {
		t.Fatal("OnEvent got no events with a recorder set")
	}
	return cfg.GorunUUID
}

func open(t *testing.T) *history.Store {
	t.Helper()
	h, err := history.Open(filepath.Join(t.TempDir(), "nested", "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func TestRecordAndReadARun(t *testing.T) {
	h := open(t)
	id := runOnce(t, h)
	ctx := context.Background()

	runs, err := h.Runs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != id || runs[0].Title != "history test" {
		t.Fatalf("runs = %+v, want the one run", runs)
	}
	if r := runs[0]; !r.Finished || r.Success || r.Failed != 1 || r.TotalTasks != 2 {
		t.Errorf("summary = %+v, want finished, failed, 1 of 2 tasks failed", r)
	}

	detail, err := h.Run(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Tasks) != 2 {
		t.Fatalf("tasks = %+v", detail.Tasks)
	}

	hello, err := h.Task(ctx, id, "localhost", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hello.Stdout, "hello from history") || hello.State != int(gorun.TaskDone) {
		t.Errorf("hello = %+v", hello)
	}
	broken, err := h.Task(ctx, id, "localhost", "broken")
	if err != nil {
		t.Fatal(err)
	}
	if broken.ExitCode != 3 || !strings.Contains(broken.Stderr, "oops") || broken.State != int(gorun.TaskFailed) {
		t.Errorf("broken = %+v", broken)
	}
}

func TestMissingRunsAndTasksAreNotFound(t *testing.T) {
	h := open(t)
	ctx := context.Background()
	if _, err := h.Run(ctx, "nope"); !errors.Is(err, history.ErrNotFound) {
		t.Errorf("Run: err = %v, want ErrNotFound", err)
	}
	if _, err := h.Task(ctx, "nope", "localhost", "hello"); !errors.Is(err, history.ErrNotFound) {
		t.Errorf("Task: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteAndKeep(t *testing.T) {
	h := open(t)
	ctx := context.Background()
	first := runOnce(t, h)
	runOnce(t, h)
	newest := runOnce(t, h)

	if err := h.Delete(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Run(ctx, first); !errors.Is(err, history.ErrNotFound) {
		t.Errorf("deleted run still readable: %v", err)
	}

	if err := h.Keep(ctx, 1); err != nil {
		t.Fatal(err)
	}
	runs, err := h.Runs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != newest {
		t.Errorf("after Keep(1), runs = %+v, want only the newest", runs)
	}
	if err := h.Keep(ctx, -1); err == nil {
		t.Error("Keep(-1) was accepted")
	}
}

func TestDefaultPathFollowsGORUN_DB_PATH(t *testing.T) {
	t.Setenv("GORUN_DB_PATH", "/data/gorun/history.db")
	if got := history.DefaultPath(); got != "/data/gorun/history.db" {
		t.Errorf("DefaultPath = %q", got)
	}
	t.Setenv("GORUN_DB_PATH", "")
	if got := history.DefaultPath(); !strings.HasSuffix(got, filepath.Join(".gorun", "gorun.db")) {
		t.Errorf("DefaultPath = %q, want ~/.gorun/gorun.db", got)
	}
}

// A Recorder of your own receives the same events, so runs can be kept anywhere.
func TestACustomRecorderGetsEveryEvent(t *testing.T) {
	var cfg gorun.Config
	if err := cfg.Load(strings.NewReader(gorunfile)); err != nil {
		t.Fatal(err)
	}
	var started, finished atomic.Int32
	cfg.Recorder = gorun.RecorderFunc(func(event any) {
		switch event.(type) {
		case gorun.ProcessStartedEvent:
			started.Add(1)
		case gorun.ProcessFinishedEvent:
			finished.Add(1)
		}
	})
	_ = cfg.Run(context.Background())
	if started.Load() != 1 || finished.Load() != 1 {
		t.Errorf("custom recorder saw %d starts and %d finishes, want 1 each", started.Load(), finished.Load())
	}
}

// A server that can't run keeps its error message, which travels as text in the event's JSON.
func TestRecordKeepsServerErrors(t *testing.T) {
	h := open(t)
	h.Record(gorun.ProcessStartedEvent{Type: "process_started", GorunUUID: "r1", Title: "t", Servers: []string{"web-1"}})
	h.Record(gorun.ServerErrorEvent{Type: "server_error", GorunUUID: "r1", Server: "web-1", Err: errors.New("ssh: connection refused")})
	detail, err := h.Run(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got := detail.ServerErrors["web-1"]; got != "ssh: connection refused" {
		t.Errorf("server error = %q", got)
	}
}

// The git includes a run read are saved with it, with the commit each one used.
func TestRecordKeepsIncludeCommits(t *testing.T) {
	h := open(t)
	h.Record(gorun.ProcessStartedEvent{Type: "process_started", GorunUUID: "r2", Title: "t", Includes: []gorun.IncludeSource{
		{Source: "https://github.com/you/tasks.git", Ref: "v1.2.0", Commit: "0123456789abcdef0123456789abcdef01234567", Dir: "/cache/x"},
	}})
	detail, err := h.Run(context.Background(), "r2")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Includes) != 1 {
		t.Fatalf("includes = %+v", detail.Includes)
	}
	if inc := detail.Includes[0]; inc.Source != "https://github.com/you/tasks.git" || inc.Ref != "v1.2.0" || inc.Commit != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("include = %+v", inc)
	}
}

func TestRecordKeepsTheSchedule(t *testing.T) {
	h := open(t)
	h.Record(gorun.ProcessStartedEvent{Type: "process_started", GorunUUID: "r3", Title: "backup", Schedule: "nightly"})
	h.Record(gorun.ProcessStartedEvent{Type: "process_started", GorunUUID: "r4", Title: "by hand"})

	detail, err := h.Run(context.Background(), "r3")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Schedule != "nightly" {
		t.Errorf("run schedule = %q, want nightly", detail.Schedule)
	}
	runs, err := h.Runs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range runs {
		got[r.ID] = r.Schedule
	}
	if got["r3"] != "nightly" || got["r4"] != "" {
		t.Errorf("schedules by run = %v, want r3 nightly and r4 none", got)
	}
}
