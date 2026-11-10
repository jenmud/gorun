# The web UI

`gorun -web` runs a web service for a Gorunfile. It shows the tasks, bundles, schedules and run
history, runs tasks on servers, adds and edits them, and fires the schedules while it runs. It is
the long-running host for [schedules](schedules.md).

```bash
gorun -web Gorunfile                       # open http://localhost:8080
GORUN_ADDRESS=127.0.0.1:9000 gorun -web    # no file yet: load one on the Config page
```

## Start it

| Setting | Default | What it does |
|---|---|---|
| `GORUN_ADDRESS` | `:8080` | address to listen on |
| `GORUN_DB_PATH` | `~/.gorun/gorun.db` | history database |
| `GORUN_NATS_CLIENT_URL` | none | a NATS server to use; when set, none is started |
| `GORUN_NATS_SERVER_CONFIG` | none | a NATS server config file for the server the web service starts |
| `GORUN_EVENT_TOPIC` | `gorun.events` | the NATS subject run events go to |
| `GORUN_SKIP_NATS` | unset | never start or use NATS |

The web service starts its own NATS server on `127.0.0.1:4222` when nothing answers there, so live
updates and history work with no setup. The header shows how NATS is reached, next to **live**:

- `embedded`: this web service started the server. It stops when `gorun -web` exits.
- `external`: an existing server, named by `GORUN_NATS_CLIENT_URL` or already running on the
  default address.
- `off`: no connection, with the reason (for example `GORUN_SKIP_NATS` is set). Runs and live
  updates are off.

The **Config** page has a NATS panel with the same facts: the mode, the address, the config file
the embedded server was started with (or the defaults), and how many clients are connected to it.

```bash
# use a NATS server that's already running
GORUN_NATS_CLIENT_URL=nats://nats.internal:4222 gorun -web Gorunfile

# start the embedded server on the LAN, with its own config
printf 'host: "0.0.0.0"\nport: 4222\n' > nats.conf
GORUN_NATS_SERVER_CONFIG=nats.conf gorun -web Gorunfile
```

The embedded server stops when `gorun -web` exits.

> **No login.** The web UI listens on all interfaces by default, so anyone who can reach the port
> can run tasks, browse folders and edit the Gorunfile. Keep it on a trusted network, or set
> `GORUN_ADDRESS=127.0.0.1:8080` and reach it over an SSH tunnel:
> `ssh -L 8080:127.0.0.1:8080 you@gorun-host`.

## The pages

Every task and bundle name in the UI is a link to its card. The link opens the card and scrolls to
it, so you can click through from a schedule to its target, from a bundle to its tasks, or from a
task to the bundles that include it.

### Run

The picker: tick servers, then tasks or bundles (not both), and start. Stats and a live dashboard
update as runs go. Each run, server and task starts collapsed. A task that is uploading a file shows
a bar with the size and rate, and a closed server shows the upload on its line
([screenshot](screenshot-web-upload.png)). **Recent runs** bars link to the
run; **Stop** cancels a run started from the page. **Force** runs every task even when its
[check](gorunfile.md#checks-run-only-when-needed) says there is nothing to do, and **Check only**
is a dry run that only runs the checks: its tasks end *unchanged* or *would run*, and the run is
marked *check only*. Runs read the Gorunfile on disk, so save first.

### Tasks

Every task with its command, settings, dependencies, the bundles that include it and the servers
that run it, slowest first by recent average. A task's card shows its **check** and **verify**
under its command, folded like it, with what each does; a task without them says so. The `check` and
`verify` badges mark the tasks that have them.

- **Add a task**: name, command, check, verify, description, sudo, local, and the file to save
  into. It is written to the file straight away.
- **Edit** on a task: change the command, check, verify, description and flags, and turn
  `check_copies` on or off. Clear the check or verify to remove it. Settings the form doesn't
  show, such as copies or templates, are kept.

The Config page's **Add a task** form and **Edit** dialog have the same check and verify fields.

### Bundles

Each bundle, its tasks in order, and what it expands to, with **Run this bundle**.

- **Add a bundle**: name, tasks (comma separated, `@bundles` allowed), description, file.
- **Edit** on a bundle: change its tasks and description.

### Schedules

Each `[[schedule]]` with its target, timing, zone, status and next run.

- Times are stored in UTC and shown in your browser's zone. The picker above the table changes the
  zone for this browser session.
- **Pause**, **Resume** and **Skip next run** act on one schedule. A paused schedule's runs are
  reported as missed, and `missed = "run"` catches them up once when it resumes.
- **Add a schedule**: pick a time zone, then either type a cron pattern or pick a one-off date and
  time. The picker starts on the browser's current time and is saved in UTC. Typing a cron pattern
  clears the picker, and picking a time clears the cron field.
- **Edit** changes the schedule in its file straight away. A schedule with unsaved changes on the
  Config page must be saved or discarded there first.
- The page refreshes every 30 seconds, and a running schedule shows *running*.

### History

Every run, newest first, with its title, when it started, how long it took and the result. A run
has each server's tasks, slowest first; open a task for its exit code, duration, command, stdout,
stderr, env vars and vars.

A run whose Gorunfile reads [tasks from a git repository](gorunfile.md#tasks-from-a-git-repository)
lists them under **Task libraries**: each repository, the ref asked for and the commit it ran.

Adding or editing a task marked `local = true`, or a schedule for one, adds a local server
(`[servers.hosts.localhost]` with `tasks = []`) when the Gorunfile has none, so the task has
somewhere to run. The message after saving says when it did.

A run that a [schedule](schedules.md) started carries a **⏱ scheduled · name** badge, on the
live run while it executes and in History afterwards. Click it to open the Schedules page at that
schedule.

Skipped, missed and failed scheduled runs are listed too, with the reason in the title. A schedule
whose target can't run on a server (for example, a task that isn't `local = true` on a local
server) appears as a failed run.

**Trim history** keeps the 10 newest runs. Nothing is trimmed automatically.

### Config

Add, edit and remove servers, groups, tasks, bundles and schedules in the loaded file. Load a
Gorunfile by browsing folders on the machine running gorun, or by typing its path. Changes queue
until **Save**, which writes each one into the file it came from and reloads the file. Comments and
layout elsewhere in the file are kept.

## Walkthrough: from nothing to a running schedule

This takes about two minutes and uses the [scheduled jobs recipe](../recipes/05-scheduled-jobs/README.md),
which runs only on this machine.

1. Start the web service with the recipe's Gorunfile:

   ```bash
   gorun -web recipes/05-scheduled-jobs/Gorunfile
   ```

2. Open http://localhost:8080 and go to **Schedules**. The `heartbeat` schedule shows its next
   minute. Its target, `heartbeat`, is a link: click it to open the task.
3. Go to **History**. A `Recipe 5: scheduled jobs` run appears each minute.
4. On **Schedules**, click **Pause** on `heartbeat`, wait a minute, and check **History** for a
   missed entry. Click **Resume**: a missed entry is listed as skipped, since `heartbeat` uses the
   default `missed = "skip"`.
5. Click **Edit** on `heartbeat` and change the cron to `*/2 * * * *`. Save. The table shows the new
   timing at once, and the file changes too.
6. Click **Add a schedule**. Name it `my first check`, target `report`, and pick a one-off time a few
   minutes from now in another zone. Save. It appears in the table with its UTC time and the zone it
   was written in.

## Walkthrough: add a task and run it

1. Open **Tasks** and expand **Add a task**. Name it `hello-web`, command `echo hello from the web`,
   tick *run only on local servers*, and save. The notice confirms it was written to the file.
2. Open **Run**, tick `localhost`, tick `hello-web`, and start the run. The live dashboard shows it.
3. The run appears in **History**, with the task's output when you open it.

## Why a task might not run

- A scheduled or manual run whose target no server can run fails before it starts. It is listed in
  **History** as failed with the reason, such as *local servers only run tasks marked local = true*.
- A task that isn't in a server's list runs only with `--task` or a schedule's target, and only on
  the servers it suits.
- Runs and live updates need NATS. The header says when it isn't connected.
