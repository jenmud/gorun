# Build: 002-plain-abort-message (round 2)

The human rework (round-1/review-human.md) arrived mid-build and is folded into this one commit.

## Changes
- `cmd/plain.go`: `abortLine` ("abort: received interrupt, waiting for running tasks to stop (press ctrl+c again to force quit)") and `forceQuitLine`. `watchAbort(ctx, interrupts, quit) (stop func())` waits for ctx; on cancel it prints `abortLine` once under `p.mu`, then calls `interrupts()` to listen for a second interrupt, and on one prints `forceQuitLine` and calls `quit`. It registers for signals only after the first abort, so the interrupt that cancelled ctx does not count as the second. `stop()` sets `watchStopped` under the mutex, unblocks the goroutine, waits for it and unregisters the signal channel, so nothing prints after `Run` returns. The `aborted` and `watchStopped` fields keep it to one line per run.
- `cmd/main.go`: the plain path keeps the printer, starts `watchAbort(ctx, notifyInterrupts, func() { os.Exit(130) })` just before `cfg.Run` and stops it right after, before the error and `fatal` handling. `notifyInterrupts` wraps `signal.Notify`/`signal.Stop` for `os.Interrupt`.
- `cmd/plain_test.go`: the tests below, plus `syncBuffer` and `waitFor` helpers.
- `docs/running.md`: the plain-output paragraph now covers the abort line, the wait, the summary at the end, and a second ctrl+c that quits at once with exit 130.

## Acceptance criteria
- AC1 ✅ TestPlainAbortMessage
- AC2 ✅ TestPlainAbortPrintedOnce (cancels twice and calls `abort()` again: one line)
- AC3 ✅ TestPlainAbortSilentAfterStop
- AC4 ✅ TestPlainAbortBeforeSummary (`sleep 30` local task, cancelled on TaskRunning. It returns in ms, and the abort line comes before `summary:`)
- AC5 ✅ TestPlainAbortCheckOnly (check `sleep 30`, CheckOnly. The abort line comes before `N would run`, and there is no `summary:`)
- AC6 ✅ docs/running.md plain output paragraph
- AC7 ✅ `go vet ./...` clean, `make test` passes
- Human: second ctrl+c force quits ✅ TestPlainAbortSecondInterruptForceQuits (prints the force-quit line, calls quit, unregisters)
- Human: the abort line says so ✅ TestPlainAbortLineSaysCtrlCAgainForceQuits
- Also: TestPlainAbortSecondInterruptAfterStopDoesNothing and TestPlainNoInterruptNoSignalWatch (no signal registration without an interrupt)

## Checks
- `gofmt -l .`: only the known examples/ files and vendor/, none of mine
- `go vet ./...`: clean
- `make test`: all packages ok (cmd 74s)
- Smoke with the real binary and SIGINT:
  - Local `sleep 8` with one SIGINT: abort line, FAILED (interrupted), summary, exit 1.
  - Same run with no SIGINT: no abort line.
  - SSH to the unreachable host 10.255.255.1: the first SIGINT leaves gorun waiting, and the second prints the force-quit line and exits 130 at once.

## Review fixes
- round-1 review-human: "a second ctrl-c must quit immediately": `watchAbort` listens for a second interrupt after the first and calls `os.Exit(130)` via `quit`. "The first abort line must say so": the line now ends "(press ctrl+c again to force quit)". Both are tested and documented in docs/running.md.
- Note: a force quit skips deferred cleanup (the history DB close), so that run's history record may stay unfinished. That fits "quit without waiting".
