package websrv

import (
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
