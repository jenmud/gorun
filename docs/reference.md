# Reference

Everything on one page. For explanations, see [the Gorunfile](gorunfile.md) and
[running gorun](running.md).

**Contents**

- [Command line](#command-line)
- [Environment variables](#environment-variables)
- [Gorunfile keys](#gorunfile-keys)
- [Keyboard shortcuts](#keyboard-shortcuts)

## Command line

```
gorun [options] <Gorunfile>
```

Flags take one or two dashes and can go before or after the Gorunfile, except `-version`,
`-dump` and `-complete`, which must come first.

| Flag | Description |
|---|---|
| `-run`, `-r` | run without the picker (needed when there is no terminal) |
| `-v` | verbose: list every task and its output |
| `-plain` | plain lines instead of the TUI, and the `-list-*` trees printed in full (the default when stdout isn't a terminal) |
| `-server <name>` | only this server; repeatable, globs allowed |
| `-exclude-server <name>` | not this server; repeatable, globs allowed |
| `-tag <tag>` | only servers with this tag; repeatable, all must match |
| `-group <group>` | only servers in this group; repeatable, all must match |
| `-task <name>` | only this task (with its dependencies); repeatable, globs and `@bundles` allowed |
| `-bundles <name>` | replace each selected server's task list with these bundle(s); repeatable, globs allowed |
| `-parallel <n>` | at most n servers at a time; `0` means all (overrides `max_parallel`) |
| `-list-servers` | list servers with their groups, tags and task lists (a collapsible tree in a terminal) |
| `-list-tasks` | list tasks as a dependency tree (collapsible in a terminal) |
| `-list-tags` | list tags and their servers (a collapsible tree in a terminal) |
| `-list-groups` | list server groups and their servers (a collapsible tree in a terminal) |
| `-list-bundles` | list bundles and what they expand to (a collapsible tree in a terminal) |
| `-dump` | print a starting Gorunfile |
| `-dump-tasks DIR` | dump each task as a runnable shell file in DIR |
| `-history` | browse past runs |
| `-web [Gorunfile]` | start the web UI, optionally loading a Gorunfile |
| `-complete <kind>` | print shell completion candidates |
| `-version` | print the version |

## Environment variables

They can also be set in a `.env` file in the current directory, which never overrides variables
already set.

| Variable | Default | Description |
|---|---|---|
| `GORUN_NAME` | `gorun` | name for OpenTelemetry and NATS |
| `GORUN_DB_PATH` | `~/.gorun/gorun.db` | history database (see [History](running.md#history) for how it is found) |
| `GORUN_LOG_FILE` | `~/.gorun/gorun.log` | log file |
| `GORUN_ADDRESS` | `:8080` | web UI listen address |
| `GORUN_EVENT_TOPIC` | `gorun.events` | NATS topic for events |
| `GORUN_SKIP_NATS` | unset | set to skip NATS |
| `GORUN_NATS_CLIENT_URL` | `nats://127.0.0.1:4222` | NATS server |
| `GORUN_NATS_CLIENT_NAME` | `GORUN_NAME` | NATS client name |
| `GORUN_NATS_CLIENT_ALLOW_RECONNECT` | `true` | reconnect automatically |
| `GORUN_NATS_CLIENT_RECONNECT_WAIT` | NATS default | wait between reconnects, e.g. `2s` |
| `GORUN_NATS_CLIENT_TIMEOUT` | NATS default | connection timeout, e.g. `5s` |

The instrumented build also honours the standard `OTEL_*` variables (see
[Logs and OpenTelemetry](integrations.md#logs-and-opentelemetry)).

## Gorunfile keys

`gorun -dump` prints a Gorunfile that documents every one of these in place.

**Top level**

| Key | Description |
|---|---|
| `title` | shown in the picker, the TUI and the history |
| `include` | list of files or globs to merge in, relative to the file; [splitting a large Gorunfile](gorunfile.md#splitting-a-large-gorunfile) |
| `log_level` | `debug`, `info`, `warn` or `error`; servers and tasks inherit it |
| `max_parallel` | servers at once; `0` (default) means all |
| `max_task_parallel` | tasks at once per host; `0` (default) means one at a time, `-1` means all |
| `envvars` | env vars for every task |
| `vars` | template variables for every task on this run (a task's own vars win) |
| `[ssh]` | only `config_file` (or `config`) is used: the SSH config every host reads (and it makes every host use SSH) |

**`[tasks.<name>]`**

| Key | Description |
|---|---|
| `cmd`, `cmd_file`, `cmd_template`, `cmd_template_file` | the command (pick one) |
| `copy` (also `copies`, `upload`, `files`) | file copies: `src`, `src_template` or `content`; `dst` (or `dir` with `src`) |
| `depends_on` | tasks that must run first |
| `sudo` | run with `sudo -n` |
| `shell` | shell for local hosts (default `sh -c`) |
| `envvars` | env vars for this task |
| `vars` | [template variables](gorunfile.md#task-variables) for this task: a map, which may hold nested tables and lists |
| `log_level` | log level for this task |
| `timeout` | Go duration, default `30m` |
| `retries`, `retry_delay`, `retry_unsafe` | [retries](gorunfile.md#retries) |
| `description`, `details`, `rerun` | [descriptions and rerun safety](gorunfile.md#descriptions-and-rerun-safety) |
| `local` | run only on local (no-SSH) servers; without it, the task runs only over SSH |

**`[bundles.<name>]`**: `tasks` (task names and `@bundles`), `description` and `vars` (template variables for the tasks it pulls in).

**`[server_groups.<name>]`**: `tags`, `tasks`, `envvars`, `vars`, `log_level` and an `ssh` table.

**`[[schedule]]`**: `name`, `target` (a task or `@bundle`), `cron` or `at`, `timezone` and `missed` (`skip` or `run`); [schedules](schedules.md).

**`[servers]`**: `tasks`, the list every host runs.

**`[servers.hosts.<name>]`**

| Key | Description |
|---|---|
| `hostname` | address to connect to (the name is used when unset) |
| `groups` | server groups to join, in order |
| `tags` | tags for selection |
| `tasks` | replaces the global and group task lists |
| `add_tasks`, `skip_tasks` | append to / remove from the task list |
| `envvars` | env vars for every task on this host |
| `vars` | template variables for this host (read as `{{.Server.Vars.KEY}}`) |
| `log_level` | log level for this host |
| `ssh` | `username`, `port`, `identityfile`, `password`, `config_file` |

## Keyboard shortcuts

Press **ctrl+k** in the picker or the history for the full list.

| Screen | Keys |
|---|---|
| [Picker](running.md#the-picker) | space select (Bundles: check bundle) · → / ← tasks · type to filter · enter run · alt+a run all · shift+tab next tab · ctrl+c quit |
| [Live view](running.md#watching-a-run) | ↑ / ↓ move · enter, → / ← show / hide tasks · v verbose · e / E, c / C expand / collapse · q quit |
| [Lists](running.md#checking-a-gorunfile-before-you-run-it) (`--list-*`) | enter, → / ← open / close · e / E, c / C expand / collapse · q quit |
| [History](running.md#history) | enter open · y copy command · d delete · t keep the N newest · esc back |
