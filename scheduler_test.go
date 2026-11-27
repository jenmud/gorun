package gorun

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// schedulerBody is a Gorunfile with one local task, a bundle and a server that runs the task.
const schedulerBody = `
[tasks.hello]
cmd = "echo hello-ran"
local = true

[tasks.migrate]
cmd = "echo migrate-ran"
local = true

[servers.hosts.localhost]
`

// schedulerFixture loads a Gorunfile and a scheduler for it. The scheduler's clock starts at
// start and is only moved by the tests calling tick, so nothing depends on real time.
type schedulerFixture struct {
	cfg    *Config
	sched  *Scheduler
	mu     sync.Mutex
	events []ScheduleEvent
	tasks  map[string]bool // tasks that finished, by name
}

func newSchedulerFixture(t *testing.T, schedules string, start time.Time) *schedulerFixture {
	t.Helper()
	c, err := loadTree(t, map[string]string{"Gorunfile": schedulerBody + schedules})
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	f := &schedulerFixture{cfg: c, tasks: map[string]bool{}}
	c.OnEvent = func(event any) {
		if ev, ok := event.(TaskEvent); ok && ev.State == TaskDone {
			f.mu.Lock()
			f.tasks[ev.Task] = true
			f.mu.Unlock()
		}
	}
	f.sched = NewScheduler(c, func(ev ScheduleEvent) {
		f.mu.Lock()
		f.events = append(f.events, ev)
		f.mu.Unlock()
	})
	f.sched.now = func() time.Time { return start }
	f.sched.mu.Lock()
	_ = f.sched.prime(start)
	f.sched.mu.Unlock()
	return f
}

// kinds returns the event kinds seen so far, in order, as "kind:schedule".
func (f *schedulerFixture) kinds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, ev := range f.events {
		out = append(out, string(ev.Kind)+":"+ev.Schedule)
	}
	return out
}

func (f *schedulerFixture) ranTask(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tasks[name]
}

// settle waits for the runs started by the scheduler to finish.
func (f *schedulerFixture) settle() {
	f.sched.runs.Wait()
}

func TestSchedulerFiresAtTime(t *testing.T) {
	f := newSchedulerFixture(t, `
[[schedule]]
name = "maintenance"
target = "migrate"
at = "2026-12-01T02:00"
timezone = "Europe/London"
`, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.sched.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer f.sched.Stop()

	f.sched.tick(time.Date(2026, 12, 1, 2, 0, 1, 0, time.UTC))
	f.settle()

	if got := f.kinds(); len(got) != 1 || got[0] != "fired:maintenance" {
		t.Fatalf("events = %v, want [fired:maintenance]", got)
	}
	if !f.ranTask("migrate") {
		t.Error("the target did not run")
	}
	if st := f.sched.Status()[0]; !st.Next.IsZero() {
		t.Errorf("one-off schedule still has a next run: %+v", st)
	}
}

func TestSchedulerPauseHoldsRunsUntilResume(t *testing.T) {
	start := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	f := newSchedulerFixture(t, `
[[schedule]]
name = "held"
target = "migrate"
cron = "0 2 * * *"
missed = "skip"
`, start)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.sched.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer f.sched.Stop()

	if err := f.sched.Pause("held"); err != nil {
		t.Fatal(err)
	}
	f.sched.tick(time.Date(2026, 12, 1, 2, 0, 1, 0, time.UTC))
	f.settle()
	if got := f.sched.Status()[0].Missed; got != 1 {
		t.Errorf("missed while paused = %d, want 1", got)
	}
	if f.ranTask("migrate") {
		t.Error("a paused schedule ran its target")
	}

	// missed = skip: resuming reports the held run and drops it
	if err := f.sched.Resume("held"); err != nil {
		t.Fatal(err)
	}
	f.settle()
	if got := f.kinds(); strings.Join(got, ",") != "missed:held,skipped:held" {
		t.Fatalf("events = %v, want missed then skipped", got)
	}
	if f.ranTask("migrate") {
		t.Error("missed = skip still ran the target on resume")
	}
}

func TestSchedulerResumeRunsOnceWhenMissedRun(t *testing.T) {
	start := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	f := newSchedulerFixture(t, `
[[schedule]]
name = "catchup"
target = "migrate"
cron = "0 2 * * *"
missed = "run"
`, start)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.sched.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer f.sched.Stop()

	if err := f.sched.Pause("catchup"); err != nil {
		t.Fatal(err)
	}
	f.sched.tick(time.Date(2026, 12, 1, 2, 0, 1, 0, time.UTC))
	f.sched.tick(time.Date(2026, 12, 2, 2, 0, 1, 0, time.UTC))
	if got := f.sched.Status()[0].Missed; got != 2 {
		t.Fatalf("missed = %d, want 2", got)
	}

	if err := f.sched.Resume("catchup"); err != nil {
		t.Fatal(err)
	}
	f.settle()
	if got := f.kinds(); strings.Join(got, ",") != "missed:catchup,missed:catchup,fired:catchup" {
		t.Errorf("events = %v, want two missed then one catch-up fire", got)
	}
	if !f.ranTask("migrate") {
		t.Error("missed = run did not run the target on resume")
	}
}

func TestSchedulerSkipsOneOccurrence(t *testing.T) {
	start := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	f := newSchedulerFixture(t, `
[[schedule]]
name = "daily"
target = "migrate"
cron = "0 2 * * *"
`, start)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.sched.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer f.sched.Stop()

	if err := f.sched.Skip("daily"); err != nil {
		t.Fatal(err)
	}
	f.sched.tick(time.Date(2026, 12, 1, 2, 0, 1, 0, time.UTC))
	f.settle()
	if got := f.kinds(); strings.Join(got, ",") != "skipped:daily" {
		t.Fatalf("events = %v, want skipped:daily", got)
	}
	if f.ranTask("migrate") {
		t.Error("the skipped occurrence ran")
	}

	// the skip does not carry forward: the next day runs
	f.sched.tick(time.Date(2026, 12, 2, 2, 0, 1, 0, time.UTC))
	f.settle()
	if got := f.kinds(); strings.Join(got, ",") != "skipped:daily,fired:daily" {
		t.Errorf("events = %v, want the next day to fire", got)
	}
	if !f.ranTask("migrate") {
		t.Error("the occurrence after the skip did not run")
	}
}

func TestSchedulerSkipsWhilePreviousRunGoing(t *testing.T) {
	f := newSchedulerFixture(t, `
[[schedule]]
name = "daily"
target = "migrate"
cron = "0 2 * * *"
`, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.sched.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer f.sched.Stop()

	f.sched.mu.Lock()
	f.sched.states["daily"].running = true
	f.sched.mu.Unlock()

	f.sched.tick(time.Date(2026, 12, 1, 2, 0, 1, 0, time.UTC))
	f.settle()
	if got := f.kinds(); strings.Join(got, ",") != "skipped:daily" {
		t.Fatalf("events = %v, want skipped:daily", got)
	}
	f.mu.Lock()
	reason := f.events[0].Reason
	f.mu.Unlock()
	if reason != "previous run still going" {
		t.Errorf("reason = %q", reason)
	}
}

func TestSchedulerControlErrors(t *testing.T) {
	f := newSchedulerFixture(t, `
[[schedule]]
name = "once"
target = "migrate"
at = "2026-12-01T02:00"
`, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))

	if err := f.sched.Pause("nope"); err == nil {
		t.Error("Pause of an unknown schedule did not fail")
	}
	if err := f.sched.Resume("nope"); err == nil {
		t.Error("Resume of an unknown schedule did not fail")
	}
	if err := f.sched.Skip("nope"); err == nil {
		t.Error("Skip of an unknown schedule did not fail")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.sched.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.sched.Start(ctx); err == nil {
		t.Error("starting twice did not fail")
	}
	f.sched.tick(time.Date(2026, 12, 1, 2, 0, 1, 0, time.UTC))
	f.settle()
	if err := f.sched.Skip("once"); err == nil {
		t.Error("Skip with no upcoming run did not fail")
	}
	f.sched.Stop()
	f.sched.Stop() // a second Stop is a no-op
}

func TestSchedulerStatusSortedByName(t *testing.T) {
	f := newSchedulerFixture(t, `
[[schedule]]
name = "b"
target = "hello"
cron = "0 2 * * *"

[[schedule]]
name = "a"
target = "migrate"
cron = "0 3 * * *"
`, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))

	st := f.sched.Status()
	if len(st) != 2 || st[0].Name != "a" || st[1].Name != "b" {
		t.Fatalf("status = %+v, want a then b", st)
	}
	if st[0].Target != "migrate" || st[0].Next.IsZero() {
		t.Errorf("status a = %+v", st[0])
	}
}

func TestScheduledRunNamesItsSchedule(t *testing.T) {
	f := newSchedulerFixture(t, `
[[schedule]]
name = "nightly backup"
target = "migrate"
at = "2026-12-01T02:00"
`, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))
	var mu sync.Mutex
	var started []ProcessStartedEvent
	f.cfg.OnEvent = func(event any) {
		if ev, ok := event.(ProcessStartedEvent); ok {
			mu.Lock()
			started = append(started, ev)
			mu.Unlock()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.sched.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer f.sched.Stop()
	f.sched.tick(time.Date(2026, 12, 1, 2, 0, 1, 0, time.UTC))
	f.settle()

	mu.Lock()
	defer mu.Unlock()
	if len(started) != 1 || started[0].Schedule != "nightly backup" {
		t.Fatalf("process started events = %+v, want one naming the schedule", started)
	}
	if f.cfg.Schedule != "" {
		t.Errorf("the scheduler changed the shared config's Schedule to %q", f.cfg.Schedule)
	}
}

// TestScheduledRunUsesTheLatestGorunfile edits the Gorunfile after the scheduler started, as an
// editor or a git pull would: the run uses the edited file, and a broken or emptied one fails
// with the reason instead of running the old copy.
func TestScheduledRunUsesTheLatestGorunfile(t *testing.T) {
	const sched = `
[[schedule]]
name = "nightly"
target = "migrate"
cron = "0 2 * * *"
`
	f := newSchedulerFixture(t, sched, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))
	path := f.cfg.Path
	onEvent := f.cfg.OnEvent
	f.sched.LoadLatest(func() (*Config, error) {
		c := &Config{}
		return c, c.LoadFile(path)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.sched.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer f.sched.Stop()
	fire := func(day int) {
		t.Helper()
		f.sched.tick(time.Date(2026, 12, day, 2, 0, 1, 0, time.UTC))
		f.settle()
	}
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// the schedule now targets hello, and the run reports to the scheduler's OnEvent
	write(schedulerBody + strings.Replace(sched, `"migrate"`, `"hello"`, 1))
	fire(1)
	if !f.ranTask("hello") || f.ranTask("migrate") {
		t.Fatalf("ran hello %v, migrate %v: want the edited target", f.ranTask("hello"), f.ranTask("migrate"))
	}
	if f.cfg.OnEvent == nil || onEvent == nil {
		t.Fatal("lost the OnEvent hook")
	}

	write("this isn't TOML")
	fire(2)
	write(schedulerBody)
	fire(3)
	f.mu.Lock()
	defer f.mu.Unlock()
	var reasons []string
	for _, ev := range f.events {
		if ev.Kind == ScheduleFailed {
			reasons = append(reasons, ev.Reason)
		}
	}
	if len(reasons) != 2 || !strings.Contains(reasons[0], "doesn't load") || !strings.Contains(reasons[1], "no longer in the Gorunfile") {
		t.Errorf("failures = %q, want a broken file then a removed schedule", reasons)
	}
}
