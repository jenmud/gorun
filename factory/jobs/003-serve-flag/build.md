# Build: 003-serve-flag (round 3)

## Changes

- `cmd/main.go` → `options.web` renamed `serve`; flags `-serve` and `-web` both set it. Flag
  parsing moved into `parseArgs(fs, args)` (`parseFlags` wraps it with `flag.CommandLine`) so it is
  testable; the second parse after the Gorunfile is kept. `printUsage` now calls
  `writeUsage(w)`; the usage lists `-serve [Gorunfile]  start the web service (scheduler, NATS,
  web UI); -web is an alias`, and the `GORUN_ADDRESS`/`GORUN_NATS_SERVER_CONFIG` lines say
  `-serve`. `web.Run` gets the log path.
- `cmd/websrv/web.go` → `Run(configPath, logPath)` calls `run`, which returns an error; `Run`
  prints `gorun: <err>` to stderr and exits 1 (deferred cleanup runs first). New
  `listenAndServe(srv, addr, started)`: `net.Listen` (error wrapped as `listen <addr>: <err>`),
  then `started(ln.Addr())`, then `srv.Serve`. `openStore(path)` now returns its error so the
  history line can say `off (<err>)`. The Gorunfile load error is kept for the message. The
  existing `log.Printf` lines stay.
- `cmd/websrv/startup.go` (new) → `startupInfo` and pure `startupMessage(w, info)` with the URL,
  Gorunfile, NATS, history and log lines. Wildcard hosts (``, `0.0.0.0`, `::`) show as
  `localhost` with `all interfaces`. One extra case beyond the spec: if the file has schedules
  but the scheduler failed to start, the line says `scheduler not started, see the log` instead
  of falsely saying `scheduler running`.
- `cmd/web/ui/view.go`, `cmd/web/ui/config.templ` → `gorun -web` becomes `gorun -serve`;
  `config_templ.go` regenerated with `go run github.com/a-h/templ/cmd/templ@v0.3.1070` (the go.mod
  version; installed templ is v0.3.1001), so its diff is large but only from the generator version.
- Docs and text: `README.md`, `Makefile`, `AGENTS.md`, `cmd/Gorunfile.empty`, `history/history.go`,
  `docs/{reference,web,integrations,running,tutorial,history,gorunfile,schedules}.md`,
  `recipes/README.md`, `recipes/0{5,6,7}-*/README.md` → `gorun -serve` / `./cmd -serve`.
  `docs/reference.md` row is `-serve [Gorunfile]` with `-web` as an alias; `docs/web.md` gains the
  alias note, the startup message and the bind error. Bash completion list has `-serve`/`--serve`.
- Round 2: `cmd/websrv/nats.go` → `withoutUserinfo` (parse, `u.User = nil`) for the NATS URL in
  `natsStartState`, so the startup message, header and Config page never show a user, password
  or token; `scrubUserinfo` (regex over `scheme://...@`) cleans the connect error used as the
  `off (<reason>)` text. `recipes/05-scheduled-jobs/Gorunfile` says `gorun -serve`.
- Round 3: `cmd/websrv/nats.go` → when the connect error is a `*url.Error` (an unparseable
  `GORUN_NATS_CLIENT_URL`), the off reason is the fixed `GORUN_NATS_CLIENT_URL is not a valid URL`
  with no URL or inner error. `userinfoRE` is now `scheme://[^\s"]*@`, so it runs to the last `@`
  before whitespace or a quote. `withoutUserinfo` returns "" on a parse error.
- Tests: `cmd/serve_test.go`, `cmd/websrv/startup_test.go`, `cmd/web/ui/natslabel_test.go`.

## Acceptance criteria

- AC1 ✅ TestServeFlagStartsWebService (cmd)
- AC2 ✅ TestUsageDocumentsServeAndWebAlias (cmd)
- AC3 ✅ TestStartupMessageAllInterfaces (websrv; also checks `0.0.0.0:8080` and `:8080`)
- AC4 ✅ TestStartupMessageSpecificHost (websrv)
- AC5 ✅ TestStartupMessageGorunfile (websrv)
- AC6 ✅ TestStartupMessageNATS (websrv)
- AC7 ✅ TestStartupMessageHistoryAndLog (websrv)
- AC8 ✅ TestRunReportsBindError (websrv, via `listenAndServe`)
- AC9 ✅ TestDocsUseServeFlag (cmd; now also scans `recipes/**/Gorunfile`)
- AC10 ✅ TestCompletionListsServe (cmd)
- AC11 ✅ TestNATSModeLabelSaysServe (cmd/web/ui)
- Extra ✅ TestStartupMessageHidesNATSCredentials (real in-process NATS server with user:password
  and with a token; fails without the fix), TestNATSOffReasonHidesCredentials,
  TestStartupMessageSchedulerNotStarted; round 3: TestNATSOffReasonHidesCredentialsInABadURL
  (real `inats.NewClient()` errors for `nats://u:a/b@...` and `nats://u:a#b@...`),
  TestScrubUserinfoRunsToTheLastAt
- AC12 ✅ `make test`, `go vet ./...`, `make examples` pass; `config_templ.go` regenerated

## Checks

- `gofmt -l .` → only the pre-existing `examples/rollout/main.go`, `examples/servicedeck/main.go`
- `go vet ./...` → clean
- `make test` → all packages ok
- `make examples` → ok
- Smoke: `gorun recipes/05-scheduled-jobs/Gorunfile -serve` printed the six-line startup message
  (3 schedules, scheduler running). `gorun -web` on a taken port printed nothing to stdout,
  `gorun: listen 127.0.0.1:18090: listen tcp 127.0.0.1:18090: bind: address already in use` to
  stderr, exit 1.
- Round 2: same checks again, all pass; `TestStartupMessageHidesNATSCredentials` fails when
  `withoutUserinfo` is bypassed (checked by hand, then restored).

- Round 3: gofmt (only the pre-existing examples), `go vet ./...`, `make test` and `make examples`
  all pass.

## Review fixes

Round 2:
- security [must] an unparseable URL leaks the password in the off reason → fixed reason for any
  `*url.Error`; greedy regex `[^\s"]*@`; tests for `u:a/b@` and `u:a#b@` built from real
  `inats.NewClient()` errors. Limit: a bare token with `/` (`nats://tok/en@host`) parses as a
  valid URL with host `tok`, so nats.go's DNS error names `tok`. That is how the URL parses, not a
  parse error, so the scrubber can't tell it is a secret. It is not in the test because it needs
  a DNS lookup.
- security [nit] the `withoutUserinfo` fallback → returns "".
- security [nit] `internal/nats/client.go` logs, metric and span attributes, and
  `internal/natsserver/natsserver.go:56` → not changed, out of scope; a follow-up can reuse
  `withoutUserinfo`.
- ux/ui/code nits → no action; they are outside the spec or already fixed.

Round 1:
- security [must] NATS URL leaks userinfo → `withoutUserinfo` in `natsStartState` (fixes stdout
  and the pages); test with `user:pass@` and `token@` URLs against a real server.
- security [nit] scrub the off reason → `scrubUserinfo` on `connectErr.Error()`; tested.
- security [nit] `internal/natsserver/natsserver.go:56` logs the URL → not changed (out of scope,
  as the review says). Note: `internal/nats/client.go` also logs the full URL on connect
  ("connected to server url=..."), to the log file; a follow-up job could reuse the helper there.
- code [must] `recipes/05-scheduled-jobs/Gorunfile:2` → `gorun -serve`; AC9 test now scans
  `recipes/**/Gorunfile` too.
- code [nit] 07-big-upload README comment alignment → fixed.
- code [nit] long flag map line → split in two.
- code [nit] untested "scheduler not started" branch → `TestStartupMessageSchedulerNotStarted`.
- ux nits (bind hint, no-login note, alias sentence placement) → left; outside the spec.
