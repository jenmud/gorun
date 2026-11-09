package gorun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const scheduleEditSource = `# keep this banner
title = "x"

[tasks.check]
cmd = "true"

# the nightly one
[[schedule]]
name = "nightly"
target = "check"
cron = "0 2 * * *"

[[schedule]]
name = "weekly"
target = "check"
at = "2099-01-01T00:00"
`

func TestSaveChangesAddsAndRemovesSchedules(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "Gorunfile")
	writeFiles(t, dir, map[string]string{"Gorunfile": scheduleEditSource})

	add := Change{Kind: KindSchedule, Name: "hourly", File: root, Body: map[string]any{
		"target":   "check",
		"cron":     "0 * * * *",
		"timezone": "Australia/Sydney",
		"missed":   "run",
	}}
	if err := SaveChanges([]Change{add}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(root)
	text := string(data)
	if !strings.Contains(text, "[[schedule]]\nname = \"hourly\"") {
		t.Errorf("added schedule not written as a [[schedule]] table:\n%s", text)
	}
	if !strings.Contains(text, "# the nightly one") || !strings.Contains(text, "# keep this banner") {
		t.Errorf("comments were lost:\n%s", text)
	}

	c := &Config{}
	if err := c.LoadFile(root); err != nil {
		t.Fatalf("the saved file does not load: %v\n%s", err, text)
	}
	if len(c.Schedules) != 3 {
		t.Fatalf("got %d schedules after add, want 3", len(c.Schedules))
	}

	if err := SaveChanges([]Change{{Kind: KindSchedule, Name: "nightly", File: root, Remove: true}}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(root)
	if strings.Contains(string(data), `name = "nightly"`) || strings.Contains(string(data), "# the nightly one") {
		t.Errorf("removed schedule still in the file:\n%s", data)
	}
	c = &Config{}
	if err := c.LoadFile(root); err != nil {
		t.Fatalf("the file does not load after removal: %v", err)
	}
	if len(c.Schedules) != 2 || c.Schedules[0].Name != "weekly" {
		t.Errorf("schedules after removal = %+v, want only weekly and hourly", c.Schedules)
	}
}

func TestSaveChangesRefusesADuplicateSchedule(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "Gorunfile")
	writeFiles(t, dir, map[string]string{"Gorunfile": scheduleEditSource})

	err := SaveChanges([]Change{{Kind: KindSchedule, Name: "weekly", File: root, Body: map[string]any{
		"target": "check", "cron": "0 3 * * *",
	}}})
	if err == nil || !strings.Contains(err.Error(), "already defined") {
		t.Fatalf("error = %v, want a duplicate refusal", err)
	}
}

func TestCheckScheduleRejectsBadSchedules(t *testing.T) {
	c := &Config{Tasks: map[string]Task{"check": {Name: "check"}}}
	if err := c.CheckSchedule(Schedule{Name: "ok", Target: "check", Cron: "0 2 * * *"}); err != nil {
		t.Errorf("valid schedule rejected: %v", err)
	}
	if err := c.CheckSchedule(Schedule{Name: "both", Target: "check", Cron: "0 2 * * *", At: "2099-01-01T00:00"}); err == nil {
		t.Error("a schedule with cron and at was accepted")
	}
}
