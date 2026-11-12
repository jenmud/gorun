# Add -serve (with -web as an alias) and a startup message

## Problem

`gorun -web` prints nothing to the terminal. The "listening" line and every NATS and history
message go to the log file, so the user can't tell the service is up, where to point a browser,
or whether NATS and history work. "Serve" also names the job better: the process hosts the
scheduler and NATS as well as the UI.

## Behaviour

- `-serve [Gorunfile]` starts the web service. It does exactly what `-web` does today.
- `-web` stays as an alias. Both set the same option. Giving both is the same as giving one.
  Flags after the Gorunfile still work (`gorun Gorunfile -serve`).
- `-serve` is the documented name from now on. Docs, examples, the Makefile and the UI text use
  `gorun -serve`. `-web` is mentioned only as an alias: in `-h`, `docs/reference.md` and
  `docs/web.md`.
- The service binds its listener first (`net.Listen`, then `srv.Serve`). Only then does it print
  the startup message to stdout. If binding fails it prints `gorun: listen <addr>: <err>` to
  stderr and exits 1. Today `log.Fatal` writes that to the log file, so the user never sees it.
- Startup message, one fact per line:

  ```
  gorun is serving on http://localhost:8080 (listening on :8080, all interfaces)
    Gorunfile  /abs/path/Gorunfile, 2 schedules, scheduler running
    NATS       embedded server at nats://127.0.0.1:4222, events on gorun.events
    history    /home/u/.gorun/gorun.db
    log        /home/u/.gorun/gorun.log
  Press ctrl+c to stop.
  ```

  - URL: take the host and port from the bound listener (`ln.Addr()`). An empty, `0.0.0.0` or
    `::` host shows as `localhost`, and the parentheses then say `all interfaces`. A specific
    host is shown as given and has no `all interfaces` note.
  - Gorunfile line: the loaded path and its schedule count. With schedules it adds `scheduler
    running`. With no schedules it says `no schedules`. With no file it shows `none, load one on
    the Config page`. If loading fails it shows `not loaded: <err>`. Today that error only goes
    to the log.
  - NATS line, from `natsState`. `embedded server at <url>`, `external server at <url>` or
    `off (<reason>)`. When NATS is on, it adds `events on <topic>`. When NATS is off, it adds
    `live updates and runs are off`, to match `docs/web.md`.
  - history line: the DB path (`history.DefaultPath()`), or `off (<err>)` when the store did not
    open.
  - log line: the log file path from `main`. It is left out when logging goes to stderr.
- The existing `log.Printf` lines stay. They still go to the log file.
- `printUsage` lists `-serve [Gorunfile]  start the web service (scheduler, NATS, web UI); -web
  is an alias`. The `GORUN_ADDRESS` and `GORUN_NATS_SERVER_CONFIG` lines say `-serve`.

## Out of scope

- Renaming the `cmd/websrv` package, `web.Run`, the `make web`/`make web-dev` targets or env vars.
- Deprecation warnings for `-web`.
- New flags for the address or NATS. `GORUN_ADDRESS` and friends are unchanged.
- Retaking screenshots.

## Files likely to change

- `cmd/main.go`: `serve` option, `serve` and `web` flags, usage text, and passing the log path
  to `web.Run`.
- `cmd/websrv/web.go`: listen then serve, a bind error on stderr, and a pure
  `startupMessage(w io.Writer, info startupInfo)` called after binding. `Run` takes the log path
  as well.
- `cmd/websrv/web_test.go` (or a new `startup_test.go`), and a flag test in `cmd/`.
- `cmd/web/ui/view.go` (`natsModeLabel`), `cmd/web/ui/config.templ` plus a regenerated
  `config_templ.go`. Change the text `gorun -web` to `gorun -serve`.
- `cmd/Gorunfile.empty` (the schedules comment), `Makefile` (`go run ./cmd -serve`), `AGENTS.md`
  (the `make web-dev` comment).
- Docs: `docs/reference.md`, `docs/web.md`, `docs/integrations.md` (prose and the bash
  completion word list), `docs/running.md`, `docs/tutorial.md`, `docs/history.md`,
  `docs/gorunfile.md`, `docs/schedules.md`, `README.md`, `recipes/README.md`,
  `recipes/05-scheduled-jobs/README.md`, `recipes/06-task-library/README.md`,
  `recipes/07-big-upload/README.md`, `history/history.go` (doc comment).

## Acceptance criteria

- [ ] AC1 `TestServeFlagStartsWebService`: parsing `-serve`, `-web`, and `Gorunfile -serve` each
  sets the serve option. Parsing neither leaves it unset.
- [ ] AC2 `TestUsageDocumentsServeAndWebAlias`: the `printUsage` output has `-serve` and says
  `-web` is an alias.
- [ ] AC3 `TestStartupMessageAllInterfaces`: for listener `[::]:8080` the first line is
  `gorun is serving on http://localhost:8080`, and the first line says `all interfaces`.
- [ ] AC4 `TestStartupMessageSpecificHost`: for `127.0.0.1:9000` the URL is
  `http://127.0.0.1:9000`, with no `all interfaces`.
- [ ] AC5 `TestStartupMessageGorunfile`: covers the four Gorunfile cases. A file with schedules
  shows the count and `scheduler running`. A file without schedules shows `no schedules`. No
  file shows `load one on the Config page`. A load error shows `not loaded:` and the error.
- [ ] AC6 `TestStartupMessageNATS`: embedded, external and off each give their line. The first
  two include the URL and the topic. Off includes the reason.
- [ ] AC7 `TestStartupMessageHistoryAndLog`: shows the DB path or `off (<err>)`. Shows the log
  path, and leaves out the `log` line when the path is empty.
- [ ] AC8 `TestRunReportsBindError`: when the address is already in use, the error says
  `listen` and the address, and nothing is printed to stdout. To make this testable, put
  listen-and-serve behind a function that returns the error. `Run` does the exit.
- [ ] AC9 `TestDocsUseServeFlag`: no `gorun -web` or `./cmd -web` appears in `README.md`,
  `Makefile`, `AGENTS.md`, `cmd/Gorunfile.empty`, `docs/*.md` or `recipes/**/README.md`, except
  on a line that has the word `alias`.
- [ ] AC10 `TestCompletionListsServe`: the bash completion word list in `docs/integrations.md`
  has `-serve` and `--serve` as well as `-web` and `--web`.
- [ ] AC11 `TestNATSModeLabelSaysServe`: `natsModeLabel("embedded")` returns `embedded, started
  by gorun -serve`.
- [ ] AC12 `make test`, `go vet ./...` and `make examples` pass. The `_templ.go` files are
  regenerated.

## UI

- Terminal: `-serve`/`-web` print the startup message above to stdout. There is no TUI change.
- Web: in the header's NATS label and the Config page NATS panel, the text `gorun -web` becomes
  `gorun -serve`. The layout does not change.
