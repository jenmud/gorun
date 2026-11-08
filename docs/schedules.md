# Schedules

A schedule runs a target on every server, on a cron pattern or once at a set time. Targets
are a task or an `@bundle`, as `-task` takes them. Schedules are part of the Gorunfile, so
the schedule sits next to the tasks it runs.

## Status

Parsing, validation and next-run times are built. The scheduler, pause and skip, the history
entries and the Config page dialog are not built yet. The scheduler runs only inside
`gorun -web`.

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

## Decisions

- **Gorunfile, not a separate file.** Includes merge schedules the same way they merge tasks.
- **Cron plus one-off `at`.** Cron comes from `github.com/robfig/cron/v3`, the standard
  parser for this syntax.
- **Scheduler in `-web` only.** It checks each minute and fires due runs through the same
  path as the web UI's run button. A plain CLI run (`gorun -r`) never fires schedules; it still
  validates them when the file loads.
- **Skip overlaps.** A run still going when the next one is due is skipped and logged.
- **Library users can add schedules too.** The types and parsing live in the root package.

## Planned

- **Pause and resume.** Pausing holds the runs that fall due. On resume, `missed` decides
  what happens to them.
- **Skip one occurrence.** Cancels the next run only, for maintenance. It does not carry
  forward, and the skipped run is recorded in history.
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
