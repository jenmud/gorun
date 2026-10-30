# Gorun

## Table of Contents

- [Why Gorun](#why-gorun)
  - [What it is good at](#what-it-is-good-at)
  - [What it is not great at](#what-it-is-not-great-at)
- [Features](#features)
- [Installation](#installation)
  - [Quick install (curl | sh)](#quick-install-curl--sh)
  - [Manual install](#manual-install)
- [OpenTelemetry](#opentelemetry)
- [Usage](#usage)
  - [Shell completion](#shell-completion)
  - [Generate a starting Gorunfile](#generate-a-starting-gorunfile)
  - [Failures](#failures)
  - [Task timeouts](#task-timeouts)
  - [Task retries](#task-retries)
  - [Task descriptions and rerun safety](#task-descriptions-and-rerun-safety)
  - [Logs](#logs)
- [Task selection and inspection](#task-selection-and-inspection)
- [Server Selection](#server-selection)
- [Using gorun as a library](#using-gorun-as-a-library)
- [Web UI](#web-ui)
  - [Dashboard](#dashboard)
- [Database Store](#database-store)
  - [Configuration](#configuration)
  - [Viewing Historical Runs](#viewing-historical-runs)
- [Events (NATS)](#events-nats)
  - [Connection](#connection)
  - [Topic](#topic)
  - [Event types](#event-types)
  - [Notes](#notes)
- [SSH Keys](#ssh-keys)
- [Configuration](#configuration)

## Why Gorun

Gorun is for the moment when you know exactly which commands you want to run, and you just
need them run on a pile of machines — without packaging every command into a playbook, role,
collection, module and support group first.

It is deliberately small: one Go binary, one TOML file and SSH. No Python on the controller,
no agent on the servers, no runtime to install, and nothing left behind on a machine beyond
what your commands actually did.

### What it is good at

- **Plain commands, all the way down.** A task is ultimately a shell command. Gorun shows you
  that command, runs it over SSH, and captures stdout, stderr and the exit code. When something
  breaks you can copy the command, ssh into the box and reproduce it by hand — no "what did the
  module actually do?" archaeology.
- **Parallel across servers, ordered within one.** Every server runs at the same time by
  default; set `max_parallel` (or `-parallel`) to run them N at a time. Within a server, tasks
  run in dependency order. If a task fails, the rest of that server's tasks are skipped, the
  other servers carry on, and gorun exits 1.
- **No accidental forever runs.** Every task times out after 30 minutes by default, and you can
  override that per task, so a hung command or a stuck download cannot hold the run hostage.
- **A task library instead of copy-paste.** Define a task once, give it `depends_on`, and let
  servers pick it by name. A host can replace the global task list with its own. Typos in the
  file, unknown tasks and dependency cycles are reported before anything runs.
- **Templating and file copies that stay readable.** Go `text/template` with a `Server{}` gets
  you per-host commands and config files (`{{.Name}}`, `{{.Hostname}}`, ...), and `copy`,
  `src_template` and `content` push files or rendered files to the host.
- **You can watch it happen.** The TUI shows per-server progress, expands task output inline
  and opens failures automatically. `--select` gives you a fuzzy server picker, `--task` and
  `--server` narrow a run, `-plain` is there for CI, and `-v` is there when you want the noise.
- **A memory and a paper trail.** Logs go to a file, runs are stored in SQLite, `-history`
  replays them, `-web` gives you a live dashboard, and events can go out over NATS. The
  instrumented build adds OpenTelemetry traces and metrics.
- **Small enough to embed.** The CLI is a thin layer over the `gorun` package: `Config.Load`,
  `ResolvePaths`, `ServersPipeline`, `Run` and the `OnEvent` callback.

### What it is not great at

- **Configuration management.** Gorun does not know the desired state of a machine; it has no
  facts, drift detection, check mode or diff. Idempotence is whatever your shell commands make
  it. If you want a system that keeps enforcing state forever, use Ansible, Puppet, Salt, Nix or
  similar — Gorun is the runner, not the police.
- **Modules.** There are no packaged `package`/`service`/`user` abstractions. The shell is the
  abstraction. That is the point, but it does mean more scripting than a module-heavy tool.
- **Dynamic inventories.** Servers live in the Gorunfile. There is no cloud discovery or
  inventory plugin; generate the file if your machines move around.
- **Fancy orchestration.** It runs every selected server at once by default, with no rolling
  batches, health checks or dry-run. Per-task retries are available, but there is no backoff or
  jitter. `max_parallel` can cap how many hosts run at the same time, but tasks on a host are
  sequential, and the run is only as smart as the commands you give it.
- **Secrets management.** SSH keys, optional passwords and env vars — not a vault. Keep
  secrets out of the Gorunfile.
- **Non-SSH targets.** SSH and a local shell only: no WinRM, no cloud APIs, no agent behind
  NAT. Local tasks assume a POSIX-ish `sh` environment.
- **Host-key verification.** The SSH client currently skips it, so treat gorun as a tool for
  trusted networks rather than the open internet.
- **Big transfers.** Copies are one file at a time and read fully into memory — fine for
  configs and scripts, not for rsync-sized trees.
- **Maturity and ecosystem.** Gorun is young and small. If you need a huge module ecosystem,
  vendor support and a decade of battle stories, it is the wrong tool. It is a small, sharp
  knife.

If that sounds like your kind of knife, the rest of this README is about how to use it.

![gorun provisioning ten servers in parallel](docs/screenshot.png)

## Features

- Run tasks on multiple servers in parallel
- Interactive TUI for task monitoring and server selection
- Task dependency management
- Per-task timeouts (30 minutes by default, exit code 124 on timeout)
- Opt-in retries for failed tasks (`retries` + optional `retry_delay`, off by default)
- Task descriptions, long details and rerun-safety hints (`description`, `details`, `rerun`)
- Optional host concurrency limit (`max_parallel` / `-parallel`; default is all servers)
- SSH key support

## Installation

### Quick install (curl | sh)

```bash
curl -fsSL https://raw.githubusercontent.com/jenmud/gorun/master/scripts/install.sh | sh
```

By default the script installs the OpenTelemetry-instrumented build as `gorun`.
Set `INSTALL_VARIANTS=1` to install the standard (non-OTELC) build instead:

```bash
curl -fsSL https://raw.githubusercontent.com/jenmud/gorun/master/scripts/install.sh | INSTALL_VARIANTS=1 sh
```

### Manual install

Download a prebuilt binary from the [releases](https://github.com/jenmud/gorun/releases), or build from source:

```bash
go build -o gorun ./cmd
mv gorun /usr/local/bin/
```

## OpenTelemetry

The instrumented build only exports telemetry when you configure it. If none of the OTLP
endpoint or exporter variables are set, traces, metrics and logs are disabled, so a run
without a collector never tries to connect to `localhost:4318` (and never logs connection
errors into the TUI).

Enable a signal by pointing it at a collector, or by selecting an exporter explicitly:

```bash
# Export all signals to an OTLP collector
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 gorun Gorunfile

# Export only traces, to a signal-specific endpoint
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://collector:4318/v1/traces gorun Gorunfile

# Print spans to stdout instead of exporting them (best with -plain)
OTEL_TRACES_EXPORTER=console gorun Gorunfile -plain
```

Signals are configured independently: setting only a traces variable leaves metrics and logs
disabled. `GORUN_NAME` is used as the OTEL service name unless `OTEL_SERVICE_NAME` is set. The
standard (non-OTELC) build has no OpenTelemetry instrumentation.

## Usage

```bash
# Run all tasks on all servers
gorun example/Gorunfile

# Run tasks with interactive server selection
gorun example/Gorunfile --select

# Run tasks with verbose output
gorun example/Gorunfile -v

# Run tasks with verbose output and interactive server selection
gorun example/Gorunfile --select -v

# Run at most 4 hosts at a time
gorun example/Gorunfile -parallel 4

# Start the web UI
gorun -web

# View historical runs in the TUI
gorun -history
```

### Shell completion

Bash can complete gorun's flags, Gorunfile paths, and the task and server names
from a Gorunfile. Add this function to `~/.bashrc`, then start a new shell or run
`source ~/.bashrc`:

```bash
_gorun_complete() {
    local cur prev file word i
    cur=${COMP_WORDS[COMP_CWORD]}
    prev=${COMP_WORDS[COMP_CWORD-1]}

    # Find the Gorunfile argument, skipping flags and their values.
    for ((i = 1; i < COMP_CWORD; i++)); do
        word=${COMP_WORDS[i]}
        case $word in
            -task|--task|-server|--server|-parallel|--parallel) ((i++)) ;;
            -*) ;;
            *) file=$word; break ;;
        esac
    done

    case $prev in
        -task|--task)
            COMPREPLY=($(compgen -W "$(gorun -complete tasks "$file" 2>/dev/null)" -- "$cur"))
            return
            ;;
        -server|--server)
            COMPREPLY=($(compgen -W "$(gorun -complete servers "$file" 2>/dev/null)" -- "$cur"))
            return
            ;;
    esac

    if [[ $cur == -* ]]; then
        COMPREPLY=($(compgen -W "-v -plain -select -task -server -parallel -list-tasks -list-servers -dump -version -web -history -h --task --server --parallel --select --plain --list-tasks --list-servers --dump --version --web --history --help" -- "$cur"))
    else
        COMPREPLY=($(compgen -f -- "$cur"))
    fi
}

complete -o filenames -F _gorun_complete gorun
```

`gorun -complete tasks <Gorunfile>` and `gorun -complete servers <Gorunfile>`
print the candidate names. Without a Gorunfile argument they look for
`./Gorunfile`, so tab after `-task` or `-server` also works in a directory that
contains one.

### Generate a starting Gorunfile

```bash
gorun -dump > Gorunfile
```

Verbose mode (`-v`, or press `v` while running) lists every task per server:

![gorun verbose view listing each server's tasks](docs/screenshot-verbose.png)

Select a task with ↑/↓ and press enter to expand its output (stdout and stderr) inline; press
enter again to collapse it. Failed tasks open automatically so you can see why straight away.
Once the run finishes, verbose mode stays open so you can browse the output before quitting with `q`:

![gorun verbose view with task output expanded](docs/screenshot-output.png)

### Failures

When a task fails, gorun always shows it in a red box with the reason, the exit code and the
tail of stderr (or stdout), even in the compact view. The rest of that server's tasks are
skipped, the other servers carry on, and gorun exits with status 1:

![gorun compact view showing a failed task](docs/screenshot-failure.png)

In verbose mode the failed task is marked ✗ and its output opens automatically:

![gorun verbose view with a failed task expanded](docs/screenshot-failure-verbose.png)

### Task timeouts

Every task is limited to **30 minutes** by default. Set `timeout` on a task to give it longer
(or shorter):

```toml
[tasks.migrate]
cmd = "docker compose run --rm migrate"
timeout = "45m"
```

The timeout is any Go duration (`90s`, `10m`, `2h`). It starts when the task starts, so file
copies count against it along with the command. A timeout is treated as a failure, so the rest
of that server's tasks are skipped while the other servers carry on. The reason is shown in the
TUI and plain output (`task "migrate" timed out after 45m0s`) and is also written to stderr, so
the web UI and history database show it too.

Timed-out tasks use exit code **124**, the same convention as `timeout(1)`. Local tasks are
killed together with their process group, so a child process cannot keep running in the
background. SSH tasks are sent `SIGTERM` and the connection is closed, which also unblocks a
stuck command or file copy.

Leaving `timeout` unset (or setting it to zero) uses the 30-minute default. Negative durations
are rejected before the task runs. There is no "run forever" switch; if a task really needs more
time, set a large duration for it.

### Task retries

Retries are **opt-in**. By default a failed task fails immediately and the rest of that server's
tasks are skipped. Set `retries` to run a failed task up to that many extra times before giving
up:

```toml
[tasks.packages]
cmd_file = "./scripts/packages.sh"
retries = 2          # up to three attempts in total
retry_delay = "1s"   # optional; defaults to 50ms, "0s" retries immediately
```

Each retry gets a fresh timeout, and the previous attempt's stdout, stderr and exit code are
cleared before the next one starts. The attempt count is shown while the task runs and in the
final result (`attempt 2/3`). A task that eventually succeeds is not counted as a failure, and an
exhausted task counts as **one** failed task no matter how many attempts it used. The CLI prints
`retrying (attempt 2/3)` in plain mode.

`retry_delay` is a Go duration and controls how long gorun waits before each retry. Leaving it
unset waits `50ms`; setting `"0s"` (or `0`) retries immediately. The wait is outside the task
timeout, so a slow retry delay does not count against the next attempt. It also stops early if
the run is canceled.

Timeouts and cancellations are never retried, because retrying after a timeout usually means the
command is stuck and a canceled run is being stopped on purpose. Negative `retries` or
`retry_delay` values are rejected. Retries are per task and per server; a failed task still stops
the rest of that server's pipeline once its attempts are used up.

### Task descriptions and rerun safety

Give a task a one-line `description` so `--list-tasks` and the TUI explain what it does. An
optional `details` string is shown when the task is expanded in the TUI and under "about" in
the web dashboard:

```toml
[tasks.migrate]
cmd = "docker compose run --rm migrate"
description = "Apply database migrations"
details = """
Runs every pending migration against the database and records the applied version.
"""
rerun = "safe"
```

`rerun` records whether running the task again is harmless:

- `safe` — rerunning has no additional effect, or only converges the server to the same state (installing a package, syncing a config file).
- `unsafe` — rerunning may change the server again or fail (appending a line, rotating a key, replacing a running binary).
- unset — unknown; don't guess.

Unsafe tasks get a `⚠` in `--list-tasks` and the TUI, and expanded details spell out that
rerunning may change the server. It is documentation, not a guard: gorun still runs the task.
Invalid `rerun` values are rejected before the task runs.

### Logs

gorun's own log messages go to a log file instead of the terminal: `~/.gorun/gorun.log`,
or `GORUN_LOG_FILE` if it's set. The file is appended to on every run, and each line carries
the run's `gorun_uuid`. In the instrumented build, OpenTelemetry's startup and export messages
are written there too. In `-plain` mode the terminal shows one line per task, and failed
tasks also show the tail of their stderr (add `-v` to print every task's output). When a run
fails, the error message includes the path to the log file.

## Task selection and inspection

```bash
# List available tasks as a dependency tree
gorun example/Gorunfile --list-tasks

# List servers and their resolved task pipelines
gorun example/Gorunfile --list-servers

# Run only the named tasks (their dependencies are included automatically).
# A named task runs on every selected server even if it is not part of that
# server's configured pipeline, so one-off tasks work on any host. Use the
# one-of task with caution!
gorun example/Gorunfile --task greet --task verify

# Run only the named servers
gorun example/Gorunfile --server server1 --server web-prod-1

# Run a one-off task on a single server
gorun example/Gorunfile --server localhost --task echo-hostname
```

## Server Selection

With `--select`, gorun shows a server picker before deploying. Type to fuzzy filter the
list by server name (falling back to hostname): `wp2` finds `web-prod-2`, and `eu` finds the
`*-eu*` servers.

- type to filter · backspace / ctrl+w / ctrl+u to edit the filter
- ↑/↓ (or ctrl+p / ctrl+n) to move
- space to select/deselect, tab to select and move down
- ctrl+a to select every server shown (again to deselect), ctrl+d to clear the selection
- enter (or ctrl+r, or clicking the **▶ Run** button) deploys to the selected servers, or the highlighted one if none are selected
- click a server to select it, scroll to move
- esc clears the filter, then quits; ctrl+c quits

## Using gorun as a library

The command-line tool and TUI live under `cmd`. The rest of the repository is the
`gorun` package, which you can import to drive deployments from your own code:

```go
import "gorun"

cfg := gorun.Config{}
// load, resolve paths, run ...
```

`gorun.Config` exposes `Load`, `ResolvePaths`, `ServersPipeline` and `Run`, plus the
`OnEvent` callback for progress events (`PlanEvent`, `TaskEvent`, ...).

## Web UI

A web frontend lives under `cmd/web` and is built with [a-h/templ](https://templ.guide),
[Datastar](https://data-star.dev) and Tailwind CSS (via CDN). It subscribes to the
`gorun.events` NATS topic and renders a realtime dashboard grouped by `gorun_uuid`.

```bash
# start the web server (single binary)
gorun -web

# or run directly
go run ./cmd -web
```

### Dashboard

The dashboard mirrors the TUI. Each running gorun process is a collapsible card:

![gorun web dashboard showing a failed run](docs/screenshot-dashboard.png)

- Collapsed: process badge, title, `gorun_uuid`, `done/total`, status, and a progress bar.
  Failed runs show their failure box (compact TUI view).
- Click a card to expand the verbose detail (per-server progress and task rows):

![gorun web dashboard expanded with verbose task detail](docs/screenshot-dashboard-verbose.png)

- Successful runs are removed automatically; use **Clear finished** to drop failed runs.
- Multiple concurrent gorun processes each get their own group.

## Database Store

Historical run data is stored in SQLite. Runs are recorded automatically
by gorun instances and can be viewed in the TUI or the web UI. By default,
runs are saved to `~/.gorun/gorun.db`.

### Configuration

Set `GORUN_DB_PATH` in your `.env` file (or environment) to specify the database location:

```bash
GORUN_DB_PATH=/path/to/your/runs.db
```

When a run occurs, gorun automatically:
- Creates a run record with UUID, title, hostname, and PID
- Records each server's name and SSH address
- Stores every task execution (stdout, stderr, exit code, duration)
- Marks runs as successful or failed

### Viewing Historical Runs

- **TUI:** `gorun -history` shows a navigable history with expandable servers, tasks, commands, stdout, and stderr.
  - Press `d` to delete the selected run, `t` to keep only the 10 most recent runs.
- **Web UI:** Start it with `gorun -web` (or `go run ./cmd -web`) and open http://localhost:8080/runs to see all historical runs.
  - Use the "Keep 10 most recent" button or the delete button on a run's detail page.

The `/runs` endpoint queries the database directly; if the store is unavailable, it returns a 503 error.

## Events (NATS)

Gorun publishes lifecycle events to [NATS](https://nats.io) so external tools (the web UI,
dashboards, other gorun instances) can observe runs. Events are published only when a NATS
server is reachable; otherwise gorun runs normally and events are only delivered to the
in-process `OnEvent` callback.

### Connection

Start the bundled NATS server (JetStream enabled):

```bash
make up   # or: docker compose -f example/docker-compose.yml up -d nats
```

Connection settings are read from environment variables (see `internal/nats`):

| Variable | Default | Description |
|---|---|---|
| `GORUN_NATS_CLIENT_URL` | `nats://127.0.0.1:4222` | NATS server URL |
| `GORUN_NATS_CLIENT_NAME` | `GORUN_NAME` (or `gorun`) | Client name |
| `GORUN_NATS_CLIENT_ALLOW_RECONNECT` | `true` (nats default) | Allow automatic reconnects |
| `GORUN_NATS_CLIENT_RECONNECT_WAIT` | (nats default) | Reconnect wait duration (e.g. `2s`) |
| `GORUN_NATS_CLIENT_TIMEOUT` | (nats default) | Connection timeout (e.g. `5s`) |

### Topic

Events are published to `gorun.events` by default. Override with:

```bash
GORUN_EVENT_TOPIC=gorun.events ./gorun ...
```

### Event types

Every event is JSON and carries a `gorun_uuid` so events from concurrent gorun processes
can be correlated. Published events:

- `ProcessStartedEvent` — process started: `gorun_uuid`, `title`, `servers`, `started_at`, `hostname`, `pid`.
- `ProcessFinishedEvent` — process finished: `gorun_uuid`, `success`, `duration`, `total_tasks`, `failed`, `skipped`, `finished_at`.
- `PlanEvent` — per-server plan: `gorun_uuid`, `server`, `address`, `tasks`.
- `TaskEvent` — task state change: `gorun_uuid`, `server`, `task`, `state`, `command`, `stdout`, `stderr`, `exit_code`, `error`, `duration`.
- `ServerErrorEvent` — server could not run tasks: `gorun_uuid`, `server`, `error`.
- `RunDoneEvent` — run completed: `gorun_uuid`, `error`.

### Notes

- Publishing is synchronous and flushed before `Run` returns, so events are delivered even
  for short-lived runs.
- If NATS is unavailable, `publisher` is `nil` and events are only delivered via `OnEvent`
  (the TUI); gorun itself keeps running.
- `GORUN_NAME` (default `gorun`) is used as the OTEL service name and as a fallback
  client name.

## SSH Keys

Relative paths in the Gorunfile (`identityfile`, `cmd_file` and `cmd_template_file`) are resolved relative to the Gorunfile, not the current working directory.

SSH connection settings can also be pulled from an SSH config file. Set `config_file`
(or `config`) under `[ssh]` or `[servers.hosts.<name>.ssh]` to a path; when unset,
gorun falls back to `~/.ssh/config` if it exists. Values in the config file fill in
missing `username`, `port`, `identityfile` and `hostname` (explicit Gorunfile values
always win). A server with no SSH block still runs locally.

## Configuration

`max_parallel` caps how many hosts run at the same time. It defaults to `0`, which means every
selected host at once. Set `max_parallel = 4` in the Gorunfile, or pass `-parallel 4` for a
single run; `-parallel 0` removes the limit. Hosts wait for a free slot before connecting, so
the limit covers SSH setup and that host's whole task pipeline.

Tasks are defined once in a task library and servers pick them by name:

- `[tasks.<name>]` defines a task: `cmd`, `cmd_file`, `cmd_template` or `cmd_template_file`, plus optional `sudo`, `depends_on`, `envvars`, `shell`, `log_level`, `timeout`, `retries`, `retry_delay`, `description`, `details`, `rerun`, and file copies (`copy`/`copies`/`upload`/`files`).
- `timeout` is a Go duration string (for example `"45m"`) and limits the task on each server, including its file copies. It defaults to 30 minutes when unset or zero, and negative values are rejected. A timed-out task fails with exit code 124, so that server's remaining tasks are skipped while the other servers carry on. See [Task timeouts](#task-timeouts).
- `retries` is how many extra times a failed task may run (default `0`), and `retry_delay` is the wait before each retry (default `50ms`; `"0s"` means immediately). Timeouts and cancellations are not retried. See [Task retries](#task-retries).
- `description` is a one-line summary, `details` is an optional longer explanation, and `rerun` is `"safe"` or `"unsafe"`. They are shown by `--list-tasks`, the TUI and the web dashboard. See [Task descriptions and rerun safety](#task-descriptions-and-rerun-safety).
- Tasks can specify a command source OR copies (or both). If both are specified, copies run first, then the command.
- `cmd_template` and `cmd_template_file` are compiled as Go text templates at task execution time with a `Server{}` passed in (so you can use `.Name`, `.Hostname`, etc.). The compiled result is the command that gets broadcast/sent to the server (NATS/web UI events show the compiled command).
- Copy files to remote servers with `[[tasks.<name>.copy]]` (or `copy = [...]`): set `src` (local path, resolved relative to Gorunfile) and `dst` (remote path) or `dir` (remote directory). Also works for local execution.
- Copy a directory recursively by pointing `src` at a local directory. Gorun copies the whole tree, like `scp -r`, and keeps file permissions:
  - `dst = "/opt/app"`: `/opt/app` gets the *contents* of `src`.
  - `dir = "/opt"`: the directory itself lands in `/opt/<basename of src>`.
  The target directory and its parents are created if missing. Existing files are overwritten, and remote files that aren't in `src` are left alone. Symlinks are followed.
- A task can consist only of copies; it doesn't need a `cmd`.
- Copy as a template: set `src_template`/`template` to a local template file (resolved relative to Gorunfile) or `content` inline; it will be compiled as a Go text template with `Server{}` before being pushed to `dst`/`dir`.
- `[servers]` `tasks` is the global list every server runs.
- `[servers.hosts.<name>]` defines a server. If it sets its own `tasks`, that list **replaces** the global one.
- A task's `depends_on` are always included and run first, so a list only needs the tasks you care about.

```toml
# example/Gorunfile
title = "Provision app servers"
max_parallel = 4                           # optional; 0 (default) means all servers at once

[tasks.packages]
cmd_file = "./scripts/packages.sh"
description = "Install the base package set"  # one-line summary
details = "Installs the packages every server needs."  # shown when expanded
rerun = "safe"                             # safe | unsafe; omit if unknown
retries = 1                                # optional; retry a failure once (off by default)
retry_delay = "1s"                         # optional; default 50ms, "0s" is immediate
sudo = true
timeout = "15m"                            # optional; defaults to 30m

[tasks.timezone]
cmd = "cp /usr/share/zoneinfo/UTC /etc/localtime"
description = "Set the system timezone to UTC"
sudo = true
depends_on = ["packages"]

[tasks.go-smoke-test]
cmd_file = "./scripts/go-smoke-test.sh"
depends_on = ["packages"]

[tasks.greet]
cmd_template = "echo 'hello from {{.Name}} on {{.Hostname}}'"

[tasks.greet-from-file]
cmd_template_file = "./templates/greet.sh.tmpl"

[tasks.deploy-config]
[[tasks.deploy-config.copy]]
src = "./config/app.conf"
dst = "/etc/app/app.conf"

[tasks.app-files]                          # copy a directory tree recursively
[[tasks.app-files.copy]]
src = "./files/app"                        # local directory
dst = "/tmp/app"                           # receives the contents of ./files/app

[servers]
tasks = ["timezone"]                       # every server runs packages, then timezone

[servers.hosts.server1]
hostname = "localhost"
tasks = ["timezone", "go-smoke-test"]      # replaces the global list
    [servers.hosts.server1.ssh]
    port = 2221
    username = "testuser"
    identityfile = "./ssh-keys/id_ecdsa"

[servers.hosts.server2]                    # inherits the global list
hostname = "localhost"
    [servers.hosts.server2.ssh]
    port = 2222
    username = "testuser"
    identityfile = "./ssh-keys/id_ecdsa"
```

Unknown keys (e.g. a typo like `cmd_flie`) are reported as errors rather than ignored.
