# Schedules

A schedule runs a target on every server, on a cron pattern or once at a set time. Targets
are a task or an `@bundle`, as `-task` takes them. Schedules are part of the Gorunfile, so
the schedule sits next to the tasks it runs.

## Status

Parsing, validation, next-run times and the scheduler are built. The library's
`gorun.NewScheduler` has start, stop, pause, resume, skip and status. `gorun -web` starts one
for the loaded Gorunfile. Still to come: skipped runs in history, and the Config page's controls.

## Syntax

```toml
[[schedule]]
name = "nightly-backup"            # unique across the Gorunfile and its includes
description = "Back up the databases"
target = "@backup"                 # a task, or "@bundle"
cron = "0 2 * * *"                 # five fields, minute first; set cron or at, not both
timezone = "Australia/Sydney"      # IANA name; default UTC
missed = "skip"                    # skip (default) or run

[[schedule]]
name = "maintenance-window"
target = "migrate"
at = "2026-12-01T02:00"            # a one-off time: RFC 3339, or local in timezone
timezone = "Europe/London"
```

- **Target:** `target` names one task or one bundle (`"@name"`). It runs on every server,
  as `gorun -task <target>` does.
- **When:** exactly one of `cron` or `at`.
  - `cron` takes five fields, `minute hour day month weekday`, read in `timezone`. It uses the
    standard cron parser, so `*/15 * * * *` and `0 2 * * 1-5` both work.
  - `at` runs once. An RFC 3339 value keeps its own offset. A value with no offset,
    `"2006-01-02T15:04"`, is read in `timezone`.
- **Time zone:** `timezone` is an IANA name. Times are stored in UTC, and daylight saving
  changes follow the zone's rules.
- **Missed runs:** a run is missed when it falls due while the schedule is paused or the web
  server is down.
  - `skip` (default) drops it.
  - `run` runs the target once to catch up, however many runs were missed. It does not run
    once for each one.

## Library

```go
var cfg gorun.Config
if err := cfg.LoadFile("Gorunfile"); err != nil {
	return err
}
s := gorun.NewScheduler(&cfg, func(ev gorun.ScheduleEvent) {
	log.Printf("%s %s %s", ev.Schedule, ev.Kind, ev.Reason)
})
if err := s.Start(ctx); err != nil {
	return err
}
defer s.Stop()

_ = s.Pause("nightly-backup")
_ = s.Resume("nightly-backup")
_ = s.Skip("hourly-check")
for _, st := range s.Status() {
	fmt.Println(st.Name, st.Paused, st.Next)
}
```

## Decisions

- **Gorunfile, not a separate file.** Includes merge schedules the same way they merge tasks.
- **Cron plus one-off `at`.** Cron comes from `github.com/robfig/cron/v3`, the standard
  parser for this syntax.
- **Scheduler in `-web` only.** It checks each minute and fires due runs through the same
  path as the web UI's run button. A plain CLI run (`gorun -r`) never fires schedules; it still
  validates them when the file loads.
- **Skip overlaps.** A run still going when the next one is due is skipped and logged.
- **Library users run the scheduler too.** `NewScheduler` is in the root package, and nothing
  fires until `Start`.

## Controls

- **Pause and resume.** Pausing holds the runs that fall due, and they are reported as missed.
  On resume, `missed` decides what happens to them: `run` starts the target once to catch up,
  `skip` reports them as skipped and drops them.
- **Skip one occurrence.** Cancels the next run only, for maintenance. It does not carry
  forward. The skip is reported as an event.
- **Overlaps.** A run still going when the next one is due is skipped, and reported as skipped.
- **Status.** Each schedule shows whether it is paused, running, has a skip pending, how many
  runs were missed, and when it is next due.

The state is in memory. Pauses and skips are lost when the process exits, and runs that fell
due while the process was down are not caught up.

## Planned

- **Skipped runs in history.** Skipped and missed occurrences are events today, but the
  history view does not show them yet.
- **Config page.** A schedule dialog with a time zone picker, plus pause, resume and skip
  controls.

## Validation

Loading fails with an error when:

- `name` is missing, or a name is used twice in the Gorunfile and its includes;
- both or neither of `cron` and `at` are set;
- `cron` is not a valid five-field pattern, or `at` is not a valid time;
- `timezone` is not a known IANA zone;
- `missed` is not `skip` or `run`;
- `target` is missing, or names a task or bundle that does not exist.
