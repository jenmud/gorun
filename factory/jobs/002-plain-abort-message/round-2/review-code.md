VERDICT: PASS
Tests: gofmt clean apart from the known examples/ files, go vet clean, go test -race ./... all ok (cmd 73s).

- [nit] cmd/plain.go watchAbort: a second ctrl+c pressed between the first and `interrupts()` registering is swallowed (only the NotifyContext channel sees it). The window is microseconds; acceptable.
- [nit] cmd/main.go: `os.Exit(130)` skips the deferred history close, so the run record may stay unfinished. build.md already notes this and it fits "quit without waiting".
- [nit] cmd/plain_test.go: TestPlainAbortLineSaysCtrlCAgainForceQuits only checks a constant. Cheap, but it adds little over the other tests.
- AC1-AC7 each have a real test or doc change. AC1/AC2 compare against the amended `abortLine` (human rework overrides the spec text). The force-quit path is tested with injected signal channel and quit func, including unregister and silence after stop. No new TOML keys or env reads; docs/running.md updated; nothing stray committed.
