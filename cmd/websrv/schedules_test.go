package websrv

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jenmud/gorun"
)

const schedulesGorunfile = `[tasks.check]
cmd = "echo check"
local = true

[[schedule]]
name = "nightly"
description = "Every night at two"
target = "check"
cron = "0 2 * * *"
timezone = "Australia/Sydney"

[[schedule]]
name = "one-off"
target = "check"
at = "2099-12-01T02:00"

[servers.hosts.local-1]
tasks = ["check"]
`

func TestSchedulesPageShowsNextRunsInUTC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Gorunfile")
	if err := os.WriteFile(path, []byte(schedulesGorunfile), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp(t)
	if err := a.loadConfig(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopScheduler)

	rec := get(t, a, "/schedules")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"nightly", "one-off", "Every night at two", "tz-picker", "Australia/Sydney", "2099-12-01 02:00 UTC"} {
		if !strings.Contains(body, want) {
			t.Errorf("schedules page missing %q", want)
		}
	}
	if strings.Contains(body, "scheduler is not running") {
		t.Error("the page says the scheduler is not running, but it was started")
	}

	// the next run of the cron schedule is 02:00 in Sydney, sent as a UTC instant
	sydney, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}
	next := extractDataUTC(t, body, "nightly")
	if next.Location() != time.UTC {
		t.Errorf("next run is not in UTC: %v", next)
	}
	if local := next.In(sydney); local.Hour() != 2 || local.Minute() != 0 {
		t.Errorf("next run is %v in Sydney, want 02:00", local)
	}
	if !next.After(time.Now()) {
		t.Errorf("next run %v is not in the future", next)
	}
}

func TestSchedulesPageWithoutSchedules(t *testing.T) {
	a, _ := newTestApp(t)
	body := get(t, a, "/schedules").Body.String()
	if !strings.Contains(body, "defines no schedules") {
		t.Error("an empty Gorunfile should say it has no schedules")
	}
}

func TestScheduleRowsShowLiveStatus(t *testing.T) {
	cfg := &gorun.Config{}
	if err := cfg.Load(strings.NewReader(schedulesGorunfile)); err != nil {
		t.Fatal(err)
	}
	s := gorun.NewScheduler(cfg, nil)
	if err := s.Pause("nightly"); err != nil {
		t.Fatal(err)
	}
	if err := s.Skip("one-off"); err != nil {
		t.Fatal(err)
	}

	rows := scheduleRows(cfg, s, time.Now())
	status := map[string]string{}
	for _, r := range rows {
		status[r.Name] = r.Status
	}
	if status["nightly"] != "paused" || status["one-off"] != "next run skipped" {
		t.Errorf("status = %v", status)
	}
}

// extractDataUTC returns the data-utc instant shown for the named schedule's row.
func extractDataUTC(t *testing.T, body, name string) time.Time {
	t.Helper()
	row := body[strings.Index(body, ">"+name+"<"):]
	start := strings.Index(row, `data-utc="`)
	if start < 0 {
		t.Fatalf("no next run for %s in the page", name)
	}
	rest := row[start+len(`data-utc="`):]
	value := rest[:strings.Index(rest, `"`)]
	got, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("data-utc %q: %v", value, err)
	}
	return got
}

// loadSchedules loads the schedules Gorunfile into a test app, which starts its scheduler.
func loadSchedules(t *testing.T) *app {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Gorunfile")
	if err := os.WriteFile(path, []byte(schedulesGorunfile), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp(t)
	if err := a.loadConfig(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopScheduler)
	return a
}

func TestScheduleControlsPauseResumeAndSkip(t *testing.T) {
	a := loadSchedules(t)

	if body := get(t, a, "/schedules").Body.String(); !strings.Contains(body, "/schedules/pause") {
		t.Error("the page has no pause button while the scheduler runs")
	}

	rec := post(t, a, "/schedules/pause", url.Values{"schedule": {"nightly"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/schedules" {
		t.Fatalf("pause: status %d, location %q", rec.Code, rec.Header().Get("Location"))
	}
	if st := statusOf(a, "nightly"); st != "paused" {
		t.Errorf("after pause, status = %q", st)
	}

	post(t, a, "/schedules/resume", url.Values{"schedule": {"nightly"}})
	if st := statusOf(a, "nightly"); st == "paused" {
		t.Error("resume did not lift the pause")
	}

	post(t, a, "/schedules/skip", url.Values{"schedule": {"nightly"}})
	if st := statusOf(a, "nightly"); st != "next run skipped" {
		t.Errorf("after skip, status = %q", st)
	}
	for _, r := range scheduleRows(a.cfg, a.sched, time.Now()) {
		if r.Name == "nightly" && (r.CanSkip || !r.SkipPending) {
			t.Errorf("nightly row after skip: CanSkip=%v SkipPending=%v", r.CanSkip, r.SkipPending)
		}
	}
}

func TestScheduleControlReportsAFailure(t *testing.T) {
	a, _ := newTestApp(t)
	rec := post(t, a, "/schedules/pause", url.Values{"schedule": {"nightly"}})
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/schedules?error=") {
		t.Fatalf("location = %q, want an error", loc)
	}
	body := get(t, a, loc).Body.String()
	if !strings.Contains(body, "the scheduler is not running") {
		t.Error("the error banner does not show the reason")
	}
}

func TestSkippedAndMissedRunsAreInHistory(t *testing.T) {
	a := loadSchedules(t)
	at := time.Date(2026, 12, 1, 2, 0, 0, 0, time.UTC)

	a.recordScheduleEvent(gorun.ScheduleEvent{Schedule: "nightly", Kind: gorun.ScheduleSkipped, At: at, Reason: "skip requested"})
	a.recordScheduleEvent(gorun.ScheduleEvent{Schedule: "nightly", Kind: gorun.ScheduleFired, At: at})

	runs, err := a.store.ListRuns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, r := range runs {
		titles = append(titles, r.Title)
	}
	if len(runs) != 1 || !strings.Contains(runs[0].Title, "nightly skipped: skip requested") {
		t.Errorf("history titles = %v, want one skipped entry (fired runs are recorded by their own events)", titles)
	}
}

func statusOf(a *app, name string) string {
	for _, r := range scheduleRows(a.cfg, a.sched, time.Now()) {
		if r.Name == name {
			return r.Status
		}
	}
	return ""
}

// Without NATS, a scheduled run's events still reach the history.
func TestScheduledRunsReachHistoryWithoutNATS(t *testing.T) {
	a := loadSchedules(t)
	if a.cfg.OnEvent == nil {
		t.Fatal("no path for scheduled run events when NATS is down")
	}
	now := time.Now()
	a.cfg.OnEvent(gorun.ProcessStartedEvent{Type: "process_started", GorunUUID: "sched-1", Title: "datatime", Servers: []string{"localhost"}, StartedAt: now, Hostname: "h", PID: 1})
	a.cfg.OnEvent(gorun.ProcessFinishedEvent{Type: "process_finished", GorunUUID: "sched-1", Success: true, TotalTasks: 1, FinishedAt: now})

	runs, err := a.store.ListRuns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range runs {
		found = found || (r.ID == "sched-1" && r.Title == "datatime")
	}
	if !found {
		t.Errorf("the scheduled run is not in history: %+v", runs)
	}
}

// A scheduled run that fails before it starts is in history as a failed run.
func TestFailedScheduledRunIsInHistory(t *testing.T) {
	a := loadSchedules(t)
	a.recordScheduleEvent(gorun.ScheduleEvent{Schedule: "nightly", Kind: gorun.ScheduleFailed,
		At: time.Date(2026, 12, 1, 2, 0, 0, 0, time.UTC), Reason: "no tasks to run"})

	runs, err := a.store.ListRuns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if strings.Contains(r.Title, "nightly failed: no tasks to run") {
			if !r.Finished || r.Success {
				t.Errorf("failed run recorded as finished=%v success=%v", r.Finished, r.Success)
			}
			return
		}
	}
	t.Errorf("failed scheduled run not in history: %+v", runs)
}

func TestScheduleNamesMayHaveSpacesAndDots(t *testing.T) {
	for _, name := range []string{"datetime examples schedule", "nightly v2.1"} {
		if err := validName(kindSchedule, name); err != nil {
			t.Errorf("%q refused: %v", name, err)
		}
	}
	if err := validName(kindSchedule, `bad"name`); err == nil {
		t.Error("a schedule name with a quote was accepted")
	}
	if err := validName(kindTask, "two words"); err == nil {
		t.Error("a task name with a space was accepted")
	}
}
