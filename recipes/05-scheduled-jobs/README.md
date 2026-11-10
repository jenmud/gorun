# Recipe 5: scheduled jobs

Runs three kinds of schedule on this machine, with no servers to set up:

- `heartbeat` every minute (cron `* * * * *`), so you can watch runs arrive in History;
- `nightly`, a bundle run at 02:00 in Sydney time, with `missed = "run"`, so one catch-up run
  happens if the web service was paused or down at 02:00;
- `one-off-check`, a single run at a set time in London, given as `at` and saved in UTC.

The tasks write to `/tmp/gorun-scheduled/`, so you can see what ran.

## Plain run: schedules don't fire

```sh
gorun recipes/05-scheduled-jobs/Gorunfile -r
```

This runs only the server's own tasks (`heartbeat`), once. Schedules are ignored by a plain run.

## Scheduled run: the web service

```sh
gorun -web recipes/05-scheduled-jobs/Gorunfile
```

Open http://localhost:8080 (or the address in `GORUN_ADDRESS`). Then:

1. **Schedules** lists the three schedules, the time each is next due in your browser's time zone
   (change it with the picker), and their status. During the nightly backup the status reads
   *running*; **Pause**, **Resume** and **Skip next run** act on that schedule only.
2. **History** gets a `Recipe 5: scheduled jobs` run every minute from `heartbeat`.
3. **Edit** on a schedule changes it in this Gorunfile straight away. Try moving `heartbeat` to
   every two minutes with `cron = "*/2 * * * *"`.
4. The task and bundle names on the Schedules, Tasks and Bundles pages are links to their cards.

The web service starts its own NATS server, so live updates work with no setup. If you already
run NATS, set `GORUN_NATS_CLIENT_URL` to use it.

## Checks in scheduled runs

A task's `check` and `verify` run in a scheduled run just as in any other:

- `backup` has a check that looks for today's `backup done` line, so it runs at most once a day.
  Start `@nightly` by hand from the Run page, then again: the second time `backup` shows
  *unchanged* and only `report` runs.
- `backup` and `report` have a `verify`, so a run that didn't write its log line fails, and the
  schedule's run shows as failed in History, with the verify's output in its own drop-down.
- `heartbeat` has no check: a task without one always runs, which is what a heartbeat wants.
- Tick **Check only** on the Run page for a dry run: it says what would run and changes nothing.

## Try the missed-run setting

Pause `nightly` on the Schedules page, wait past 02:00 Sydney time, then resume it. With
`missed = "run"` the backup runs once to catch up; with `missed = "skip"` it would be dropped. If
the backup already ran that day, the catch-up run finds it done and leaves it unchanged.

## Clean up

```sh
rm -rf /tmp/gorun-scheduled
```
