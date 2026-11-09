package gorun

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Missed settings: what a schedule does with runs that fell due while it was paused or
// the web server was down.
const (
	MissedSkip = "skip" // drop them (the default)
	MissedRun  = "run"  // run the target once to catch up
)

// atLayout is the local time form of a one-off schedule's at value, read in its timezone.
const atLayout = "2006-01-02T15:04"

// Schedules only run inside the long-running web server, never in a plain CLI run, so a
// one-off `gorun -r` does not fire them. Loading still validates them.
// Schedule runs a target, a task or an "@bundle", on every server, either on a cron pattern
// or once at a time. It is defined as [[schedule]] in a Gorunfile; docs/schedules.md
// describes the design.
type Schedule struct {
	Name        string `toml:"name"`
	Source      string `toml:"-"` // the file that defines it
	Description string `toml:"description"`
	Target      string `toml:"target"`   // a task name, or "@bundle"
	Cron        string `toml:"cron"`     // five fields, such as "0 2 * * *"; set this or at
	At          string `toml:"at"`       // one time: RFC 3339, or "2006-01-02T15:04" in Timezone
	Timezone    string `toml:"timezone"` // IANA name, such as "Australia/Sydney"; default UTC
	Missed      string `toml:"missed"`   // skip (default) or run
}

// location returns the schedule's time zone, UTC when none is set.
func (s Schedule) location() (*time.Location, error) {
	if s.Timezone == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, fmt.Errorf("timezone %q: not a known IANA zone", s.Timezone)
	}
	return loc, nil
}

// Next returns the first time after the given one that the schedule is due, in UTC.
// The bool is false when a one-off schedule has already fired or a cron has no future time.
func (s Schedule) Next(after time.Time) (time.Time, bool, error) {
	if s.Cron != "" {
		spec, err := s.cronSpec()
		if err != nil {
			return time.Time{}, false, err
		}
		return spec.Next(after.In(time.UTC)).UTC(), true, nil
	}
	at, err := s.atTime()
	if err != nil {
		return time.Time{}, false, err
	}
	return at, at.After(after), nil
}

// cronSpec parses the cron pattern in the schedule's time zone.
func (s Schedule) cronSpec() (cron.Schedule, error) {
	tz := s.Timezone
	if tz == "" {
		tz = "UTC"
	}
	// CRON_TZ binds the pattern to the zone, so DST changes follow the zone's rules.
	spec, err := cron.ParseStandard("CRON_TZ=" + tz + " " + s.Cron)
	if err != nil {
		return nil, fmt.Errorf("cron %q: %w", s.Cron, err)
	}
	return spec, nil
}

// atTime returns the one-off time in UTC. A value with no offset is read in the schedule's zone.
func (s Schedule) atTime() (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s.At); err == nil {
		return t.UTC(), nil
	}
	loc, err := s.location()
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.ParseInLocation(atLayout, s.At, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("at %q: want RFC 3339 or \"2006-01-02T15:04\"", s.At)
	}
	return t.UTC(), nil
}

// validateSchedule checks one schedule. It is called when a Gorunfile loads, so a bad
// schedule stops the file rather than failing silently later.
func (c *Config) validateSchedule(s Schedule) error {
	if s.Name == "" {
		return fmt.Errorf("schedule: name is required")
	}
	if (s.Cron == "") == (s.At == "") {
		return fmt.Errorf("schedule %q: set exactly one of cron or at", s.Name)
	}
	if _, err := s.location(); err != nil {
		return fmt.Errorf("schedule %q: %w", s.Name, err)
	}
	if s.Cron != "" {
		if _, err := s.cronSpec(); err != nil {
			return fmt.Errorf("schedule %q: %w", s.Name, err)
		}
	} else if _, err := s.atTime(); err != nil {
		return fmt.Errorf("schedule %q: %w", s.Name, err)
	}
	if s.Missed != "" && s.Missed != MissedSkip && s.Missed != MissedRun {
		return fmt.Errorf("schedule %q: missed %q must be \"skip\" or \"run\"", s.Name, s.Missed)
	}
	if strings.TrimSpace(s.Target) == "" {
		return fmt.Errorf("schedule %q: target is required", s.Name)
	}
	if _, err := c.ExpandTaskRefs(s.Target); err != nil {
		return fmt.Errorf("schedule %q: %w", s.Name, err)
	}
	return nil
}

// scheduleNames lists the schedule names for the duplicate check across include files.
func scheduleNames(schedules []Schedule) []string {
	names := make([]string, 0, len(schedules))
	for _, s := range schedules {
		names = append(names, s.Name)
	}
	return names
}

// CheckSchedule reports whether s would load in this config: its timing, time zone, missed
// setting and target. It is what the Gorunfile loader checks, for code that builds schedules.
func (c *Config) CheckSchedule(s Schedule) error {
	return c.validateSchedule(s)
}
