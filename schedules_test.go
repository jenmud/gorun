package gorun

import (
	"strings"
	"testing"
	"time"
)

const scheduleBase = `
[tasks.hello]
cmd = "echo hi"
local = true

[bundles.nightly]
tasks = ["hello"]
`

func TestScheduleLoads(t *testing.T) {
	c, err := loadTree(t, map[string]string{"Gorunfile": scheduleBase + `
[[schedule]]
name = "health"
description = "check"
target = "hello"
cron = "*/15 * * * *"
timezone = "Australia/Sydney"

[[schedule]]
name = "backup"
target = "@nightly"
at = "2026-12-01T02:00"
timezone = "Europe/London"
missed = "run"
`})
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(c.Schedules) != 2 {
		t.Fatalf("got %d schedules, want 2", len(c.Schedules))
	}
	if h := c.Schedules[0]; h.Name != "health" || h.Missed != "" || h.Source == "" {
		t.Errorf("health = %+v", h)
	}
	if b := c.Schedules[1]; b.Target != "@nightly" || b.Missed != MissedRun {
		t.Errorf("backup = %+v", b)
	}
}

func TestScheduleValidation(t *testing.T) {
	tests := map[string]struct {
		body string
		want string // substring of the error; empty means valid
	}{
		"cron ok":        {`cron = "0 2 * * *"`, ""},
		"at ok":          {`at = "2026-12-01T02:00"`, ""},
		"at with offset": {`at = "2026-12-01T02:00:00+10:00"`, ""},
		"bundle ok":      {"cron = \"0 2 * * *\"\ntarget = \"@nightly\"", ""},
		"neither":        {`description = "x"`, "exactly one of cron or at"},
		"both":           {"cron = \"0 2 * * *\"\nat = \"2026-12-01T02:00\"", "exactly one of cron or at"},
		"bad cron":       {`cron = "0 2 *"`, "cron"},
		"bad at":         {`at = "tomorrow"`, "at"},
		"bad timezone":   {"cron = \"0 2 * * *\"\ntimezone = \"Mars/Olympus\"", "timezone"},
		"bad missed":     {"cron = \"0 2 * * *\"\nmissed = \"catchup\"", "missed"},
		"unknown target": {"cron = \"0 2 * * *\"\ntarget = \"nope\"", "nope"},
		"no name":        {"cron = \"0 2 * * *\"\nname = \"\"", "name is required"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			head := "\n[[schedule]]\nname = \"s\"\n"
			if strings.Contains(tc.body, "name =") {
				head = "\n[[schedule]]\n"
			}
			if !strings.Contains(tc.body, "target =") {
				head += "target = \"hello\"\n"
			}
			body := scheduleBase + head + tc.body + "\n"
			_, err := loadTree(t, map[string]string{"Gorunfile": body})
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestScheduleDuplicateAcrossIncludes(t *testing.T) {
	_, err := loadTree(t, map[string]string{
		"Gorunfile": `include = ["extra.toml"]` + "\n" + scheduleBase + `
[[schedule]]
name = "s"
target = "hello"
cron = "0 2 * * *"
`,
		"extra.toml": `
[[schedule]]
name = "s"
target = "hello"
at = "2026-12-01T02:00"
`,
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate schedule") {
		t.Fatalf("error = %v, want duplicate schedule", err)
	}
}

func TestScheduleNext(t *testing.T) {
	// Sydney is on AEDT (UTC+11) in November, so 02:00 local on 9 Nov is 15:00 UTC on 8 Nov.
	daily := Schedule{Name: "d", Cron: "0 2 * * *", Timezone: "Australia/Sydney"}
	after := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	got, ok, err := daily.Next(after)
	if err != nil || !ok {
		t.Fatalf("Next: %v, ok %v", err, ok)
	}
	if want := time.Date(2026, 11, 8, 15, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("cron Next = %v, want %v", got, want)
	}

	once := Schedule{Name: "o", At: "2026-12-01T02:00", Timezone: "Europe/London"}
	at, ok, err := once.Next(after)
	if err != nil || !ok {
		t.Fatalf("at Next: %v, ok %v", err, ok)
	}
	if want := time.Date(2026, 12, 1, 2, 0, 0, 0, time.UTC); !at.Equal(want) {
		t.Errorf("at Next = %v, want %v", at, want)
	}
	if _, ok, _ := once.Next(at); ok {
		t.Error("one-off schedule still due after its time")
	}
}
