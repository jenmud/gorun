# Plain abort message on ctrl+c

## Problem
In a `-plain` run, ctrl+c cancels the run, but nothing is printed until tasks wind down.
Running tasks can take a while to stop, so the run looks hung and users press ctrl+c again or kill it.
One line saying the interrupt was received, and that gorun is waiting, removes the doubt.

## Behaviour
- When a `-plain` run (`-r -plain`, or `-r` with stdout not a terminal) gets an interrupt (ctrl+c,
  SIGINT), gorun prints one line to stdout straight away:
  `abort: received interrupt, waiting for running tasks to stop`
- It is printed once per run. More interrupts print nothing more and change nothing (as today).
- It goes through `plainPrinter`, under its mutex, so it never splits another output line.
- After it, the run ends as it does today: running tasks stop, the rest are skipped, and the
  `summary:` line (or the `N would run, N unchanged` line for `--check`) is printed last.
- Nothing is printed when the run ends without an interrupt, including when it fails.
- `main` starts a small watcher on the plain path, just before `cfg.Run(ctx)`, and stops it as soon
  as `Run` returns, so the deferred `cancel()` at exit never prints the line.
- The watcher is a helper in `cmd/plain.go`, for example
  `func (p *plainPrinter) watchAbort(ctx context.Context) (stop func())`, so tests can drive it.
- No new flags or Gorunfile keys. The TUI and web UI are unchanged.

## Out of scope
- A second ctrl+c that force-quits without waiting.
- Naming the exact signal, or handling SIGTERM differently from today.
- Changing how the library cancels or stops tasks.
- Any message in the TUI live view or the web UI.

## Files likely to change
- `cmd/plain.go` (abort line and watcher)
- `cmd/main.go` (start and stop the watcher around `cfg.Run` on the plain path)
- `cmd/plain_test.go` (new tests)
- `docs/running.md` (the plain output paragraph)

## Acceptance criteria
- [ ] AC1 `TestPlainAbortMessage`: cancelling the context passed to `watchAbort` makes the printer
  write exactly `abort: received interrupt, waiting for running tasks to stop` as one line.
- [ ] AC2 `TestPlainAbortPrintedOnce`: the line appears exactly once, even if the watcher's context
  is cancelled and the abort path is triggered again.
- [ ] AC3 `TestPlainAbortSilentAfterStop`: calling `stop()` and then cancelling the context prints
  nothing.
- [ ] AC4 `TestPlainAbortBeforeSummary`: a `Config.Run` with one long local task (for example
  `sleep 30`) and `OnEvent` set to the printer, with the context cancelled once the task is
  `TaskRunning`, returns within a few seconds, and the output has the abort line before the
  `summary:` line.
- [ ] AC5 `TestPlainAbortCheckOnly`: in a `--check` run the abort line is printed before the
  `N would run, N unchanged` line, and no `summary:` line appears.
- [ ] AC6 `docs/running.md` says, in the plain output paragraph, that ctrl+c prints the abort line
  and gorun waits for running tasks to stop before the summary.
- [ ] AC7 `go vet ./...` and `make test` pass with `-race`.

## UI
TUI: none. Web: none. Plain output gains one `abort:` line when the run is interrupted.
