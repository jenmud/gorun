# Gorun

Gorun is a tool for running tasks on multiple servers with an interactive TUI.

![gorun provisioning ten servers in parallel](docs/screenshot.png)

## Features

- Run tasks on multiple servers in parallel
- Interactive TUI for task monitoring and server selection
- Task dependency management
- SSH key support

## Usage

```bash
# Run all tasks on all servers
go run . example/Gorunfile

# Run tasks with interactive server selection
go run . example/Gorunfile --select

# Run tasks with verbose output
go run . example/Gorunfile -v

# Run tasks with verbose output and interactive server selection
go run . example/Gorunfile --select -v
```

Verbose mode (`-v`, or press `v` while running) lists every task per server; use ↑/↓ and enter to
expand a task's output:

![gorun verbose view listing each server's tasks](docs/screenshot-verbose.png)

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

## SSH Keys

Relative paths in the Gorunfile (`identityfile` and `cmd_file`) are resolved relative to the Gorunfile, not the current working directory.

## Configuration

Tasks are defined once in a task library and servers pick them by name:

- `[tasks.<name>]` defines a task: `cmd` or `cmd_file`, plus optional `sudo`, `depends_on`, `envvars`, `shell` and `log_level`.
- `[servers]` `tasks` is the global list every server runs.
- `[servers.hosts.<name>]` defines a server. If it sets its own `tasks`, that list **replaces** the global one.
- A task's `depends_on` are always included and run first, so a list only needs the tasks you care about.

```toml
# example/Gorunfile
title = "Provision app servers"

[tasks.packages]
cmd_file = "./scripts/packages.sh"
sudo = true

[tasks.timezone]
cmd = "cp /usr/share/zoneinfo/UTC /etc/localtime"
sudo = true
depends_on = ["packages"]

[tasks.go-smoke-test]
cmd_file = "./scripts/go-smoke-test.sh"
depends_on = ["packages"]

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
