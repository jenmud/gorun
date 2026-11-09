# Schedules

A schedule runs a target on every server, on a cron pattern or once at a set time. Targets
are a task or an `@bundle`, as `-task` takes them. Schedules are part of the Gorunfile, so
the schedule sits next to the tasks it runs.

## Status

Parsing, validation, next-run times and the scheduler are built. The library's
`gorun.NewScheduler` has start, stop, pause, resume, skip and status. `gorun -web` starts one
for the loaded Gorunfile, with pause, resume and skip on the Schedules page. Skipped and missed
runs appear in History. Schedules can be added, edited and removed on the Config page.

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

## Web UI

The **Schedules** page lists each schedule with its status and next run, and has pause, resume
and skip buttons for the loaded file. Edit opens a form for the schedule's target, timing, zone,
missed setting and description; saving writes the change to the file it came from straight away,
without touching changes queued on the Config page. A schedule with unsaved Config changes must
be saved or discarded there first. Next runs show in the browser's zone, with a picker to change
it; the choice lasts for the browser session. The page refreshes every 30 seconds.

Any reload of the file, from the Config page or the schedules page, keeps the pauses and skips
on schedules that still exist.

Scheduled runs appear in **History** and in the live view whether or not NATS is connected.
Without NATS, the web server applies their events itself. Each one carries a
**⏱ scheduled · name** badge, live and in History, that links to its schedule on the Schedules
page. Skipped, missed and failed runs carry it too.

A scheduled run that fails before it starts, such as a target no server can run (a task that is
not `local = true` on a local server), is written to **History** as a failed run with the reason.
Its target runs on every server, so a task has to suit every server it reaches.

Skipped and missed occurrences are written to **History** as runs whose title says what
happened. Runs that start are recorded by their own events, so they are not duplicated.

## Validation

Loading fails with an error when:

- `name` is missing, or a name is used twice in the Gorunfile and its includes (names may have
  spaces and dots, but not quotes, tabs or line breaks);
- both or neither of `cron` and `at` are set;
- `cron` is not a valid five-field pattern, or `at` is not a valid time;
- `timezone` is not a known IANA zone;
- `missed` is not `skip` or `run`;
- `target` is missing, or names a task or bundle that does not exist.

## Config page

The Config page adds, edits and removes schedules like the other kinds. The add dialog asks for
the name, target, cron or once-at time, time zone, missed setting and description. Its time zone
dropdown starts on the browser's zone. A schedule with both cron and once-at, or a target that
doesn't exist, is refused before it is queued. Saving writes the `[[schedule]]` table into its
file and reloads the config.

## One-off times in the web UI

The once-at field is a date and time picker. The time is read in the zone chosen next to it, and
saved in the file as a UTC instant (for example `at = "2026-12-01T15:00:00Z"`), so the same
moment is kept whatever zone the file is read in. A new one starts on the browser's current
time. Editing a saved one shows it again in the schedule's zone.
