# AGENTS.md

## Layout

- The root package `github.com/jenmud/gorun` **is the library**; `cmd/` is a thin CLI/TUI over it.
  `doc.go` documents the library API and is the source of truth for usage examples (`example_test.go`
  is a runnable one).
- Library files: `config.go` (load/validate/resolve/select/run), `tasks.go` (task exec, retries,
  timeouts), `groups.go` (bundles + server groups), `ssh.go` (SSH/SCP, session limits),
  `events.go` (NATS publishing), `env.go` (secret expansion), `servers.go`.
- `cmd/` Bubble Tea TUI: `select.go`/`selectpanes.go` (picker), `tui.go` (live view),
  `listview.go` (`-list-*` trees), `historyview.go`, `plain.go` (`-plain`, used when stdout is not a
  terminal). `cmd/websrv` is the web server, `cmd/web/ui` the templ components,
  `internal/store` the sqlite history, `internal/nats` events, `internal/envvars` env access.

## Commands

```bash
make test                                  # go test -race ./...  (~8s, hermetic)
go test -race -run TestName ./cmd          # one test
go build ./... && go vet ./...             # no golangci config; keep gofmt clean
make build                                 # builds/gorun + builds/gorun-otel-CTI (gitignored)
make vendor                                # go mod tidy && go mod vendor — vendor/ IS committed
make templ-gen                             # regenerate committed cmd/web/ui/*_templ.go
make web-dev                               # templ watch + go run ./cmd -web (proxy :8080)
make up / make down                        # recipes lab: 100 SSH containers + NATS (docker compose)
go run ./cmd recipes/Gorunfile -r          # run the lab's own starter recipe
GORUN_SKIP_NATS=1 go run ./cmd cmd/Gorunfile.empty -r -plain   # smoke test: local echo tasks only
```

Tests need no docker, network or NATS: `ssh_session_test.go` runs an in-process SSH server and other
tests execute tasks locally.

## Gotchas

- `vendor/` is committed. After any dependency change run `make vendor` and commit the result.
- `*_templ.go` files are generated and committed — edit the `.templ` file, then `make templ-gen`.
  `templ` must be installed at `$(go env GOPATH)/bin/templ`; it is not a `go.mod` tool.
- `make build-otel` runs `go tool otelc go build ./cmd` (~30s). It builds in the gitignored
  `.otelc-build/` with its own module and does **not** modify `go.mod`/`vendor/`.
- Adding a TOML field to `Config`, `Task`, `Copy`, `Bundle`, `ServerGroup`, `Server` or `SSH` fails
  `TestEmptyGorunfileDocumentsEveryKey` until the key appears in `cmd/Gorunfile.empty`. That file is
  embedded for `-dump` and is also test-executed, so it must load and run as-is. Update
  `docs/reference.md` and `docs/gorunfile.md` too.
- `.env` in the repo root is autoloaded by `godotenv` from both `cmd` and the root package, so it
  affects `go run ./cmd` and `go test ./...` (`GORUN_DB_PATH`, `GORUN_NATS_CLIENT_URL`, `OTEL_*`).
  It is gitignored — never commit it. Use `GORUN_SKIP_NATS=1` to stop runs dialing NATS.
- `cmd/aaa_env.go` and the OTEL setup in `cmd/main.go`'s `init` rely on Go's per-file init order
  relative to otelc's generated `otelc.init_otelsdk.go` (injected into package `main` at build time,
  never written into `cmd/`). Don't rename them.
- The instrumented build writes startup logs to stdout before `main`; `instrumentedBuild()` and
  `restoreStdout` in `cmd/main.go` exist to keep the TUI clean. `scripts/install.sh` wraps the
  binary for the same reason.
- `cmd/listview_test.go` opens `../recipes/04-dev-server/Gorunfile`; keep that path working.
- Flags may come after the Gorunfile name (`gorun Gorunfile -r -v`) because `main` parses
  `flag.Args()[1:]` a second time (cmd/main.go:184); keep that when touching flag handling.
- The picker and live view need a TTY: without one `-plain` is forced and `-r` is required, so drive
  the binary from an agent shell with `-r -plain` (or the `-list-*` flags).
- Library code reads env through `internal/envvars.Get`/`Bool`, not `os.Getenv` directly.
- Secrets never go in a Gorunfile: `env.go` expands `$VAR` from gorun's environment and feeds values
  over stdin (`envPrelude`) so they survive `sudo` and stay out of logs, events and the process list.
- Releases (`.goreleaser.yaml`, tag `v*`) ship `gorun` (CGO_ENABLED=0, linux/darwin/windows,
  amd64+arm64) and `gorun-otel` (linux/amd64 only, built via `scripts/go-otel.sh`). Keep sqlite on
  `modernc.org/sqlite` so CGO stays off.
- CI (`.github/workflows/go.yml`, on `master` pushes/PRs) runs the otelc build and
  `go test -race -v ./...` — a failing otelc build breaks CI even though `make build-go` passes.
- After TUI changes, regenerate the terminal screenshots with `docs/screenshots/capture.sh`
  (needs tmux, chromium, ImageMagick, python3). Dashboard screenshots are taken by hand.
- The working copy is also a colocated jj repo (`.jj/`); git commands work normally.

## Docs

`docs/tutorial.md` (step by step), `docs/gorunfile.md` (every option), `docs/running.md` (picker,
live view, CI mode, history), `docs/integrations.md` (web UI, OpenTelemetry, NATS, Go library,
completion), `docs/reference.md` (flags/keys/env), `recipes/README.md` (demo lab).
