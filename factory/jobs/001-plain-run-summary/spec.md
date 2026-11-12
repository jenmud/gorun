# Plain run summary line

## Problem
A `-plain` run (CI, cron, piped output) prints one line per task and then stops.
To know how the run went you must scroll and count lines.
One closing line with the totals makes logs easy to read and to `grep`.

## Behaviour
- At the end of every `-plain` run that is not `--check`, gorun prints one line:
  `summary: 3 servers, 12 tasks, 9 ok, 1 unchanged, 1 skipped, 1 failed (4.213s)`
- It is printed by `plainPrinter` when it gets `gorun.ProcessFinishedEvent`.
- `servers` is the number of servers in `ProcessStartedEvent.Servers`.
- `tasks` is `ProcessFinishedEvent.TotalTasks`.
- `ok` is the number of `TaskDone` events the printer saw.
- `unchanged`, `skipped` and `failed` come from `ProcessFinishedEvent`.
- The time is `ProcessFinishedEvent.Duration` rounded to the millisecond, as task lines are.
- All six counts are always printed, even when zero, so the line has one fixed shape.
- It replaces the current `N unchanged (their checks found nothing to do)` line.
- It is printed on failed and cancelled runs too, before gorun exits non-zero.
- `--check` runs keep their current `N would run, N unchanged` line. Nothing changes there.
- No new flags or Gorunfile keys.

## Out of scope
- A summary in the TUI live view or the web UI.
- Changing the `--check` summary line.
- Per-server breakdowns, colours, or JSON output.

## Files likely to change
- `cmd/plain.go` (count servers and ok tasks, print the line)
- `cmd/plain_test.go` (new)
- `docs/running.md` (the plain output paragraph)

## Acceptance criteria
- [ ] AC1 `TestPlainSummaryLine`: feeding a `plainPrinter` a `ProcessStartedEvent` with 2 servers,
  2 `TaskDone`, 1 `TaskFailed`, 1 `TaskSkipped` and a `ProcessFinishedEvent{TotalTasks: 5, Failed: 1,
  Skipped: 1, Unchanged: 1, Duration: 1500*time.Millisecond}` prints exactly
  `summary: 2 servers, 5 tasks, 2 ok, 1 unchanged, 1 skipped, 1 failed (1.5s)` as its last line.
- [ ] AC2 `TestPlainSummaryZeroCounts`: a run where all tasks pass still prints every count, with
  `0 unchanged, 0 skipped, 0 failed`.
- [ ] AC3 `TestPlainSummaryReplacesUnchangedLine`: the output no longer contains
  `their checks found nothing to do`.
- [ ] AC4 `TestPlainSummaryCheckOnly`: with `ProcessStartedEvent{CheckOnly: true}` the printer prints
  `N would run, N unchanged` and no `summary:` line.
- [ ] AC5 `TestPlainSummaryOnFailure`: `go run ./cmd cmd/Gorunfile.empty -r -plain` style run (or a
  `Config.Run` with a failing local task and `OnEvent` set to a `plainPrinter`) prints the
  `summary:` line even though `Run` returns an error.
- [ ] AC6 `docs/running.md` describes the summary line in the plain output paragraph.
- [ ] AC7 `go vet ./...` and `make test` pass.

## UI
TUI: none. Web: none. Plain output gains one closing `summary:` line.
