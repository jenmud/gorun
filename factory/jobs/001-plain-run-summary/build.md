# Build: 001-plain-run-summary (round 1)

Bookmark `factory/001-plain-run-summary` → commit `c0b5b285` ("feat: print a summary line at the end of -plain runs"), workspace `.factory-ws/001-plain-run-summary`.

## Changes
- `cmd/plain.go` → `plainPrinter` gains `servers` (set from `len(ProcessStartedEvent.Servers)`) and `ok` (incremented on each `TaskDone`). On `ProcessFinishedEvent` a non-check run always prints `summary: N servers, N tasks, N ok, N unchanged, N skipped, N failed (<duration rounded to ms>)`; this replaces the old `N unchanged (their checks found nothing to do)` line. The `--check` line is unchanged. `Run` emits `ProcessFinishedEvent` before returning its error, so failed runs get the line too.
- `cmd/plain_test.go` (new) → one test per acceptance criterion (AC1–AC5).
- `docs/running.md` → the plain output paragraph now describes the summary line, gives an example and says that `--check` keeps its own line.

## Acceptance criteria
- AC1 ✅ `TestPlainSummaryLine`: the last line is exactly `summary: 2 servers, 5 tasks, 2 ok, 1 unchanged, 1 skipped, 1 failed (1.5s)`.
- AC2 ✅ `TestPlainSummaryZeroCounts`: prints `... 1 ok, 0 unchanged, 0 skipped, 0 failed (4.213s)`.
- AC3 ✅ `TestPlainSummaryReplacesUnchangedLine`.
- AC4 ✅ `TestPlainSummaryCheckOnly`: the last line is `1 would run, 1 unchanged` and there is no `summary:` line.
- AC5 ✅ `TestPlainSummaryOnFailure`: a `Config.Run` with a local `exit 3` task returns an error, and the printer's last line is the summary with `1 failed`.
- AC6 ✅ `docs/running.md`, plain output paragraph.
- AC7 ✅ `go vet ./...` is clean and `make test` passes.

## Checks
- `go test -race -run TestPlainSummary -v ./cmd` → all 5 pass.
- `go vet ./...` → clean.
- `make test` → all packages ok.
- `gofmt -l .` → no files I changed. It does list `examples/rollout/main.go` and `examples/servicedeck/main.go`, which were already unformatted on master and are not touched here.
- `GORUN_SKIP_NATS=1 go run ./cmd cmd/Gorunfile.empty -r -plain` → ends with `summary: 1 servers, 2 tasks, 2 ok, 0 unchanged, 0 skipped, 0 failed (718ms)`.
- The library API was not touched, so `make examples` was not needed.

## Review fixes
None (round 1).
