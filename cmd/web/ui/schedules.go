package ui

// ScheduleRow is one schedule as the schedules page shows it.
type ScheduleRow struct {
	Name        string
	Description string
	Target      string
	When        string // how it is timed, such as `cron "0 2 * * *"` or `once at 2026-12-01T02:00`
	Zone        string // the time zone the schedule is written in
	Status      string // scheduled, paused, running, next run skipped, finished or not running
	Missed      int    // runs missed while paused, not yet resumed
	NextUTC     string // the next run as an RFC 3339 instant; empty when none is left
	NextText    string // the same instant as UTC text, shown before the picker's script runs
}

// SchedulesPageData is what the schedules page shows.
type SchedulesPageData struct {
	Loaded  bool
	Path    string
	Running bool // whether this web server's scheduler is running
	Rows    []ScheduleRow
}
