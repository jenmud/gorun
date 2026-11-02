# The Gorunfile

A Gorunfile is a TOML file that says **what to run** (tasks) and **where to run it** (servers).
New to gorun? Start with the [tutorial](tutorial.md). This page explains every option; the
[reference](reference.md#gorunfile-keys) lists them all in one table.

**Contents**

- [The shape of a Gorunfile](#the-shape-of-a-gorunfile)
- [Tasks](#tasks)
  - [Commands](#commands) · [Templates](#templates) · [Copying files](#copying-files) · [Dependencies](#dependencies)
  - [Environment variables and secrets](#environment-variables-and-secrets) · [Timeouts](#timeouts) · [Retries](#retries) · [Descriptions and rerun safety](#descriptions-and-rerun-safety) · [Local-only tasks](#local-only-tasks)
- [Servers](#servers)
  - [SSH or local](#ssh-or-local) · [Choosing a server's tasks](#choosing-a-servers-tasks)
- [Bundles](#bundles)
- [Server groups and tags](#server-groups-and-tags)
- [How settings combine](#how-settings-combine)
- [Parallelism](#parallelism)
- [Paths and mistakes](#paths-and-mistakes)

## The shape of a Gorunfile

```toml
title = "Provision app servers"      # shown in the picker, the live view and the history

[tasks.timezone]                      # TASKS: what can run, each defined once
cmd = "timedatectl set-timezone UTC"
sudo = true

[bundles.baseline]                    # BUNDLES: a name for a list of tasks (optional)
tasks = ["timezone"]

[server_groups.prod]                  # SERVER GROUPS: settings shared by servers (optional)
tags = ["prod"]
    [server_groups.prod.ssh]
    username = "deploy"
    identityfile = "~/.ssh/id_ed25519"

[servers]                             # SERVERS: what every server runs...
tasks = ["@baseline"]

[servers.hosts.web-01]                # ...and the servers themselves
hostname = "web-01.example.com"
groups = ["prod"]
```

The config calls each server a *host* (`[servers.hosts.<name>]`); the flags and these docs say
*server*. They mean the same thing.

`gorun -dump` prints a starting Gorunfile that runs as-is and documents every option in comments.
[`recipes/Gorunfile`](../recipes/Gorunfile) and the [tutorial's file](tutorial/Gorunfile) are
complete examples.

## Tasks

### Commands

A task has **one** command source:

| Key | Runs |
|---|---|
| `cmd` | a shell command |
| `cmd_file` | a local script, sent to the server |
| `cmd_template` | a [template](#templates), rendered for each server |
| `cmd_template_file` | the same, from a local file |

```toml
[tasks.packages]
cmd_file = "./scripts/packages.sh"
sudo = true
```

- **`sudo = true`** runs the command with `sudo -n`, so the login user needs passwordless sudo.
- **`shell`** sets the shell for local servers (default `sh -c`). Over SSH the command runs in the
  remote login shell, or `sh -c` with sudo.
- A task can also have only [copies](#copying-files) and no command.

### Templates

Templates are [Go templates](https://pkg.go.dev/text/template) that see the server they run on:

| Template | Gives |
|---|---|
| `{{.Name}}` | the server's name (`web-01`) |
| `{{.Hostname}}` | its address (`web-01.example.com`) |
| `{{.SSH.Port}}` | its SSH port |
| `{{.Tags}}` | its tags, e.g. `{{range .Tags}}{{.}} {{end}}` |
| `{{index .EnvVars "REGION"}}` | one of its [env vars](#environment-variables-and-secrets), including its groups' |

```toml
[tasks.greet]
cmd_template = "echo 'hello from {{.Name}} on {{.Hostname}}'"
```

### Copying files

Add one or more `copy` tables to a task. Copies run **before** the command.

```toml
[tasks.nginx-config]
cmd = "mv /tmp/site.conf /etc/nginx/conf.d/site.conf && nginx -s reload"
sudo = true
[[tasks.nginx-config.copy]]
src_template = "./templates/site.conf.tmpl"   # rendered for each server
dst = "/tmp/site.conf"
```

| Source | Copies |
|---|---|
| `src` | a local file, or a directory (recursively, like `scp -r`) |
| `src_template` | a template file, rendered for each server |
| `content` | inline text |

| Target | Means |
|---|---|
| `dst` | the target path |
| `dir` | a target directory, keeping the file's name (with `src` only) |

Good to know:

- **Copies don't use sudo.** They run as the login user, so copy to somewhere writable like
  `/tmp` and move the file into place in the command.
- Files are written with mode `0644` and overwrite what's there. Directory copies keep local
  permissions.
- A directory copy creates the target and its parents, leaves other files alone and follows
  symlinks. For a single file, the parent directory must already exist.
- Each file is read into memory, which is fine for configs and scripts but not for large trees.

### Dependencies

```toml
[tasks.timezone]
cmd = "timedatectl set-timezone UTC"
depends_on = ["packages"]
```

A task's `depends_on` tasks always run first, once, **even if the server doesn't list them**.
Cycles are reported before anything runs.

### Environment variables and secrets

`envvars` are exported to the task's command, on local and SSH servers alike, also under sudo.
They can be set globally, on a [group](#server-groups-and-tags), on a server, or on a task. The
more specific one wins: task, then server, then its groups, then global. Keys are upper-cased.

```toml
envvars = { APP_ENV = "production" }   # every task

[tasks.deploy]
cmd = 'echo "deploying to $APP_ENV"'
envvars = { LOG_FORMAT = "json" }       # just this task
```

**Secrets:** values expand `$VAR` and `${VAR}` from gorun's own environment, including a `.env`
file in the directory you run gorun from. So the secret stays out of the Gorunfile:

```toml
[tasks.registry-login]
cmd = 'printf %s "$TOKEN" | docker login -u me --password-stdin'
envvars = { TOKEN = "${REGISTRY_TOKEN}" }
```

- Use `$$` for a literal `$`. An unset variable becomes an empty string.
- Values are sent to the task over stdin, so they don't appear in gorun's logs, the history, NATS
  events or the server's process list.

### Timeouts

Every task has a timeout of **30 minutes** unless it sets `timeout` (`90s`, `10m`, `2h`, ...):

```toml
[tasks.migrate]
cmd = "docker compose run --rm migrate"
timeout = "45m"
```

- It covers the task's copies and its command.
- A timed-out task fails with exit code **124** (like `timeout(1)`), and the rest of that
  server's tasks are skipped.
- Local tasks are killed with their whole process group. SSH tasks get `SIGTERM` and the
  connection is closed.
- There is no "run forever"; give a long task a long timeout.

### Retries

Retries are **off** unless a task sets `retries`, the number of extra attempts:

```toml
[tasks.packages]
cmd_file = "./scripts/packages.sh"
retries = 2          # up to three attempts
retry_delay = "1s"   # default 50ms; "0s" retries straight away
```

- Each attempt gets a fresh timeout and empty output.
- A task that succeeds on a retry isn't a failure.
- Timeouts and cancelled runs are never retried.
- `rerun = "unsafe"` tasks aren't retried, unless they also set `retry_unsafe = true`.

### Descriptions and rerun safety

```toml
[tasks.migrate]
cmd = "docker compose run --rm migrate"
description = "Apply database migrations"      # one line: lists, the picker and the live view
details = "Runs every pending migration."        # shown when the task is expanded
rerun = "safe"
```

`rerun` says whether running the task again is harmless:

| Value | Means |
|---|---|
| `safe` | it only converges, like installing a package |
| `unsafe` | it may change the server again or fail, like appending a line. Shown with ⚠, and not [retried](#retries) |
| unset | unknown |

It's a label, not a guard: gorun still runs unsafe tasks when you ask.

### Local-only tasks

`local = true` makes a task run only on [local servers](#ssh-or-local). On SSH servers it is left
out, together with any task that depends on it.

## Servers

```toml
[servers]
tasks = ["@baseline"]              # what every server runs, unless it says otherwise

[servers.hosts.web-01]
hostname = "web-01.example.com"    # the server's name is used when unset
tags = ["web"]
```

### SSH or local

> [!IMPORTANT]
> A server runs over SSH **only when it has SSH settings**, in its own `ssh` table or from a
> [group](#server-groups-and-tags). A server without them runs its tasks **on your own machine**.

```toml
[servers.hosts.db-01]
hostname = "db-01.internal"
    [servers.hosts.db-01.ssh]
    username = "root"
    port = 22
    identityfile = "~/.ssh/id_ed25519"   # or password = "...", but prefer keys
```

- Anything missing (`username`, `port`, `identityfile`, and the real hostname of an alias) is
  read from your SSH config: `config_file` if set, otherwise `~/.ssh/config`.
- Share logins through a [server group](#server-groups-and-tags).
- The global `[ssh]` table only uses `config_file`, and setting it gives **every** server SSH
  settings, so servers that would have run locally then use SSH too.
- gorun opens one SSH connection per server and reuses it for every task.
- Host keys aren't verified, so use gorun on networks you trust.

### Choosing a server's tasks

| On a server | Does |
|---|---|
| nothing | runs `[servers] tasks`, plus its groups' `tasks` |
| `add_tasks = [...]` | runs these as well |
| `skip_tasks = [...]` | leaves these out |
| `tasks = [...]` | replaces the list completely |

`gorun Gorunfile --list-servers` shows every server's final list. The exact rules are under
[how settings combine](#how-settings-combine).

## Bundles

A bundle is a name for a list of tasks. Use it as `@name` anywhere a task list goes: `[servers]
tasks`, a server's `tasks`, `add_tasks` and `skip_tasks`, a group's `tasks`, another bundle, and
`--task @name`.

```toml
[bundles.baseline]
description = "Minimal setup every box gets"   # shown by --list-bundles
tasks = ["timezone", "monitoring-tools"]

[bundles.standard]
tasks = ["greet", "@baseline", "verify"]      # bundles can include bundles
```

A bundle expands to its tasks in order, repeats are dropped, and `depends_on` still applies.

## Server groups and tags

A **server group** holds settings that servers share by joining it: `tags`, `tasks`, `envvars`,
`log_level` and an `ssh` table.

**Tags** label servers so you can pick them (`--tag web`, or in the picker). A server's tags are
its own plus its groups'.

```toml
[server_groups.lab]                # every machine in the lab shares one login
tags = ["lab"]
    [server_groups.lab.ssh]
    username = "testuser"
    identityfile = "./ssh-keys/id_ecdsa"

[server_groups.eu]
tags = ["eu"]
tasks = ["server-info"]            # added to what eu servers run
envvars = { REGION = "eu-west-1" }

[servers.hosts.worker-eu-b]
hostname = "10.0.4.12"
groups = ["lab", "eu"]
envvars = { REGION = "eu-central-1" }   # the server wins over its groups
    [servers.hosts.worker-eu-b.ssh]
    port = 2229                         # the rest comes from the lab group
```

## How settings combine

Settings are layered: global, then each group **in the order the server lists them**, then the
server itself. A later layer wins.

| Setting | Combines like this |
|---|---|
| `log_level`, each `ssh` field | the last one set wins |
| `envvars` | merged by key, the last one wins (a task's own still win) |
| `tags` | all of them, without repeats |

A server's task list is built like this:

1. If the server sets `tasks`, that list is used, and the global and group lists are ignored.
2. Otherwise it's `[servers] tasks`, then each group's `tasks`, in order.
3. `add_tasks` are added and `skip_tasks` removed. Bundles are expanded first, so
   `skip_tasks = ["@baseline"]` skips the whole bundle. Repeats are dropped.

## Parallelism

| Setting | Controls | Default |
|---|---|---|
| `max_parallel` | servers at once | `0`, all of them |
| `max_task_parallel` | tasks at once on each server | `0`, one at a time; `-1` means all independent tasks together |

Both can be set at the top of the file; `max_task_parallel` can also be set per server.
`--parallel N` overrides `max_parallel` for one run.

## Paths and mistakes

- Relative paths (`cmd_file`, copy sources, `identityfile`, ...) are relative to the Gorunfile,
  so you can run gorun from anywhere. `~/` is your home directory.
- **Mistakes are errors, before anything runs:** unknown keys (a typo like `cmd_flie`), unknown
  tasks, groups or bundles, dependency cycles, and skipping a task that another of the server's
  tasks depends on.
- An unknown task in a server's own list is only reported for the servers you're running on, so
  `--server` can still run the others.
