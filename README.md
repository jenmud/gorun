# Gorun

[![Go Reference](https://pkg.go.dev/badge/github.com/jenmud/gorun.svg)](https://pkg.go.dev/github.com/jenmud/gorun)
[![XERJ Corpus Hub contributor](https://hub.xerj.org/contributors/jenmud/badge.svg)](https://hub.xerj.org/contributors/jenmud)

**Run plain shell commands on a pile of servers, in parallel, and watch it happen.**

One Go binary, one TOML file and SSH. Nothing to install on the servers.

![gorun provisioning ten servers in parallel](docs/screenshot.png)

- **[Install](#install)**
- **[Quick start](#quick-start)**: running in two minutes
- **[Tutorial](docs/tutorial.md)**: from one task to a full server deployment, step by step
- **[Documentation](#documentation)**

> [!WARNING]
> **Use at your own risk.** gorun runs commands on your servers, often as root. It comes with no
> warranty of any kind (see the [MIT license](LICENSE)). Read your Gorunfiles and try them on
> non-production servers first.

## What it does

You write tasks as shell commands in a **Gorunfile**, and say which servers run which tasks:

```toml
[tasks.update]
cmd = "apt-get update && apt-get upgrade -y"
sudo = true

[servers]
tasks = ["update"]

[servers.hosts.web-1]
hostname = "web-1.example.com"
    [servers.hosts.web-1.ssh]
    username = "ubuntu"
```

Then `gorun Gorunfile` lets you pick the servers, runs the tasks on all of them at once, and shows
each server's progress live.

- **Plain commands.** A task is a shell command or script. When something breaks, you can copy
  the command and run it by hand.
- **Parallel.** Every server runs at the same time. A failure stops that server, not the others.
- **Nothing runs by accident.** You pick servers and tasks in a picker first, and every task has a
  timeout.
- **Grows with you.** Dependencies, bundles, server groups, tags, templates with per-task
  variables, file copies, retries and secrets from your environment, when you need them.
- **Remembers.** Every run is saved, with its commands and output, to look back on later.
- **Exports.** Dump every task as a standalone, runnable shell file with `--dump-tasks`, to audit
  or run by hand.
- **Task browser.** The picker’s **Tasks** tab previews each task’s description, details, command,
  execution settings, env vars, and vars beside the dependency tree, with collapsible sections and
  direct editing of file-backed commands.
- **History.** Past runs are saved with commands and output; expanded servers list tasks slowest
  first.
**When to use something else:** gorun runs commands; it doesn't enforce state. It has no modules,
facts, drift detection, dry runs, dynamic inventories or secrets vault. For that, use Ansible,
Salt, Puppet or Nix.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/jenmud/gorun/master/scripts/install.sh | sh
```

Or download a binary from the [releases](https://github.com/jenmud/gorun/releases), or build it
yourself with `go build -o gorun ./cmd`.

<details>
<summary>Two builds: instrumented (default) and standard</summary>

The install script installs the **instrumented** build, which adds
[OpenTelemetry](docs/integrations.md#logs-and-opentelemetry) support. It does nothing until you
set an `OTEL_*` endpoint. For the **standard** build without it:

```bash
curl -fsSL https://raw.githubusercontent.com/jenmud/gorun/master/scripts/install.sh | INSTALL_VARIANTS=1 sh
```

Prebuilt binaries are for Linux x86-64; elsewhere the script builds from source.

</details>

## Quick start

```bash
gorun -dump > Gorunfile      # write a starting Gorunfile; it runs as-is on this machine
gorun Gorunfile              # pick what to run: space selects, enter runs
gorun Gorunfile --run        # or run everything, without the picker
```

The starting file runs on your own machine, and documents every option in comments. To run on a
real server, add one with an `ssh` table:

```toml
[servers.hosts.web-01]
hostname = "web-01.example.com"
    [servers.hosts.web-01.ssh]
    username = "deploy"
    identityfile = "~/.ssh/id_ed25519"
```

Then check what it will run with `gorun Gorunfile --list-servers`.

```bash
gorun Gorunfile --dump-tasks ./tasks   # write each task as a runnable shell file
```

**Next:** the [tutorial](docs/tutorial.md) builds up a real deployment one step at a time.

## Documentation

| Read | To |
|---|---|
| [Tutorial](docs/tutorial.md) | learn gorun step by step, from hello world to a full deployment |
| [The Gorunfile](docs/gorunfile.md) | understand every option: tasks, copies, secrets, servers, bundles, groups |
| [Running gorun](docs/running.md) | use the picker, the live view, filters, CI mode and the history |
| [Integrations](docs/integrations.md) | use the web UI, OpenTelemetry, NATS events, the Go library and shell completion |
| [Reference](docs/reference.md) | look up any flag, Gorunfile key, environment variable or shortcut |
| [Recipes](recipes/README.md) | try bigger examples against a lab of a hundred SSH servers |

## Development

```bash
make test                     # run the tests
make build                    # build the standard and instrumented binaries
make up                       # start the example lab: a hundred SSH servers and NATS
gorun recipes/Gorunfile -r    # run the lab starter
make web-dev                  # the web UI with live reload
```

The terminal screenshots are made by [`docs/screenshots/capture.sh`](docs/screenshots/capture.sh);
run it again after changing the UI. The web dashboard screenshots are taken by hand.
