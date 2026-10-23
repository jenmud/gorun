# Gorun

Gorun is a tool for running tasks on multiple servers with an interactive TUI.

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

## Example

```toml
# example/Gorunfile
title = "Provision app servers"

[[servers]]
name = "server1"
hostname = "localhost"
    [servers.ssh]
    port = 2221
    username = "testuser"
    identityfile = "./ssh-keys/id_ecdsa"

[[servers]]
name = "server2"
hostname = "localhost"
    [servers.ssh]
    port = 2222
    username = "testuser"
    identityfile = "./ssh-keys/id_ecdsa"
```