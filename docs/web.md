# The web UI

`gorun -serve` runs a web service for a Gorunfile. It shows the tasks, bundles, schedules and run
history, runs tasks on servers, adds and edits them, and fires the schedules while it runs. It is
the long-running host for [schedules](schedules.md).

```bash
gorun -serve Gorunfile                       # open http://localhost:8080
GORUN_ADDRESS=127.0.0.1:9000 gorun -serve    # no Gorunfile: the one kept in the history database
gorun -serve -live                           # watch only: Live, History and Assistant
```

Started **with a Gorunfile**, the service runs and edits that file.

Started **without one**, it keeps the Gorunfile in its history database (`GORUN_DB_PATH`), so it
lasts across restarts:

- **The first start** saves a starter Gorunfile with only a title. Add servers, tasks, bundles and
  schedules on the Config page, or have the [assistant](#assistant) write and change tasks.
- **Every save** is kept as a new version. The startup message names the newest:
  `Gorunfile  kept in the history database (version 3)`.
- **Runs, the editor and the scheduler** use a working copy at `gorunfile/Gorunfile` next to the
  database, written from the newest version at each start. Don't edit that file: the next start
  replaces it. Relative paths in it, such as a copy's `src`, are relative to its folder, so use
  absolute paths.
- **Loading a file** on the Config page switches to that file until the service restarts.

With **`-live`**, or if the history database doesn't open, the service only watches, and the
stored Gorunfile is left alone. The menu then has just **Live**,
**History** and **Assistant**, for runs started elsewhere, such as `gorun Gorunfile -r` in a
terminal that reaches the same NATS server. The pages that need a Gorunfile (Run, Tasks, Bundles,
Schedules and Config) don't exist: they aren't in the menu, and their addresses answer 404.

`-web` is an alias for `-serve`. Once it is listening, gorun prints where to find it and what it
started:

```
gorun is serving on http://localhost:8080 (listening on :8080, all interfaces)
  Gorunfile  /abs/path/Gorunfile, 2 schedules, scheduler running
  NATS       embedded server at nats://127.0.0.1:4222, events on gorun.events
  history    /home/u/.gorun/gorun.db
  log        /home/u/.gorun/gorun.log
Press ctrl+c to stop.
```

If the address is taken, it prints `gorun: listen <addr>: <err>` and exits with status 1.

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

- `embedded`: this web service started the server. It stops when `gorun -serve` exits.
- `external`: an existing server, named by `GORUN_NATS_CLIENT_URL` or already running on the
  default address.
- `off`: no connection, with the reason (for example `GORUN_SKIP_NATS` is set). Runs and live
  updates are off.

The **Config** page has a NATS panel with the same facts: the mode, the address, the config file
the embedded server was started with (or the defaults), and how many clients are connected to it.

```bash
# use a NATS server that's already running
GORUN_NATS_CLIENT_URL=nats://nats.internal:4222 gorun -serve Gorunfile

# start the embedded server on the LAN, with its own config
printf 'host: "0.0.0.0"\nport: 4222\n' > nats.conf
GORUN_NATS_SERVER_CONFIG=nats.conf gorun -serve Gorunfile
```

The embedded server stops when `gorun -serve` exits.

> **No login.** The web UI listens on all interfaces by default, so anyone who can reach the port
> can run tasks, browse folders and edit the Gorunfile. Keep it on a trusted network, or set
> `GORUN_ADDRESS=127.0.0.1:8080` and reach it over an SSH tunnel:
> `ssh -L 8080:127.0.0.1:8080 you@gorun-host`.

## Running in Docker

The image runs `gorun -serve` on port 8080. Configure it with the variables above, plus `GORUN_FILE`,
the Gorunfile to load. Without one the service uses the Gorunfile kept in its history database, so
keep `/var/lib/gorun` in a volume.

```bash
docker build -t gorun .

# no Gorunfile: the one kept in the history database, in a named volume
docker run --rm -p 8080:8080 -v gorun-data:/var/lib/gorun gorun

# watch only: live and history
docker run --rm -p 8080:8080 gorun -live

# with a Gorunfile, kept in ./Gorunfile on the host, and history kept in a named volume
docker run --rm -p 8080:8080 \
  -v "$PWD/Gorunfile:/etc/gorun/Gorunfile" -e GORUN_FILE=/etc/gorun/Gorunfile \
  -v gorun-data:/var/lib/gorun gorun

# join a NATS server that's already running, instead of starting one
docker run --rm -p 8080:8080 -e GORUN_NATS_CLIENT_URL=nats://nats.internal:4222 gorun
```

The image is Alpine with a static gorun, and it runs as the `gorun` user. Its state lives in
`/var/lib/gorun`: the history database, the log and git include checkouts. Mount that folder to keep
them. Local tasks run with the image's `/bin/sh`. Tasks over SSH need the keys and `known_hosts`
mounted into the container, with the Gorunfile's `[ssh]` settings pointing at them.

> **No login, again.** A published port gives anyone who can reach it the web UI. Publish it only on
> a trusted network, for example `-p 127.0.0.1:8080:8080`.

### Build your own image

Build from a checkout of this repository, in its root (the build reads the whole folder, minus
what `.dockerignore` leaves out):

```bash
docker build -t gorun:dev --build-arg VERSION="$(git describe --tags --always)" .
docker run --rm -p 127.0.0.1:8080:8080 gorun:dev
```

`VERSION` is what `gorun -version` reports. To build for
other machines as well, use buildx, which can build an image for several CPU types at once:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t gorun:dev --build-arg VERSION=v0.34.0 .
```

A multi-platform build can't be loaded into the local Docker; add `--push` to send it to a registry
(see below), or build one platform with `--load`.

### Publish to Docker Hub

Sign in once, then tag the image with your Docker Hub name and push it:

```bash
docker login
docker tag gorun:dev youruser/gorun:0.34.0
docker push youruser/gorun:0.34.0
```

To publish from a release instead, fork or clone the repository, set the repository variable
`DOCKERHUB_IMAGE` to `youruser/gorun`, and add the secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`
(a Docker Hub access token). Every version tag `vX.Y.Z` then builds an image for `linux/amd64` and
`linux/arm64`, and pushes it as `X.Y.Z`, `X.Y` and `latest`. The workflow is
`.github/workflows/docker.yml`.

## The pages

Every task and bundle name in the UI is a link to its card. The link opens the card and scrolls to
it, so you can click through from a schedule to its target, from a bundle to its tasks, or from a
task to the bundles that include it.

### Live

The front page (`/`) shows what is running now: runs started from the Run page, from the
terminal, or by a schedule. Stats and **Recent runs** bars sit above it, and each bar links to
the run. Each run, server and task starts collapsed. A task that is uploading a file shows a bar
with the size and rate, and a closed server shows the upload on its line
([screenshot](screenshot-web-upload.png)). **Stop** cancels a run started from the web UI, and
**Clear finished** drops the runs that are over. **Start a run** opens the Run page.

### Run

The picker (`/run`): tick servers, then tasks or bundles (not both), and press **Run selected**.
The run starts and the page moves to **Live** to watch it. **Force** runs every task even when its
[check](gorunfile.md#checks-run-only-when-needed) says there is nothing to do, and **Check only**
is a dry run that only runs the checks: its tasks end *unchanged* or *would run*, and the run is
marked *check only*. Runs read the Gorunfile on disk, so save first. A run that can't start stays
on the Run page and says why.

### Tasks

Every task with its command, settings, dependencies, the bundles that include it and the servers
that run it, slowest first by recent average. A task's card shows its **copies** (each file or folder and
where it goes on the server), its **check** and its **verify** under its command, folded like
it, with what each does; a task without them says so. The `check` and
`verify` badges mark the tasks that have them.

- **Add a task**: name, command, files to copy, depends on, timeout, retries, check, verify,
  check copies, description, sudo, local, and the file to save into. It is written to the file
  straight away.
- **Edit** on a task: change any of those. Clear a field to remove it. Settings the form doesn't
  show, such as templates or env vars, are kept.
- **Files to copy** takes one copy per line: a local file or folder, `->`, and where it goes on
  the server, such as `scripts/stack-ok.sh -> /home/ubuntu/stack-ok.sh`. Start a line with
  `template` to render a Go template for each server. Local paths are relative to the Gorunfile.
  After the destination, add `mode=0640`, `owner=name` or `group=name` to set the permissions and
  owner (owner and group need the task's sudo). Those words always come last on the line.
  Copies of inline text (`content = ...`) can't be written this way, so the form says how many
  there are and keeps them.

The Config page's **Add a task** form and **Edit** dialog have the same check and verify fields.

**Filter tasks** searches each task's name, description, details, command, copies, check, verify,
dependencies, flags and source file. Every word typed must match, so `deploy sudo` finds the sudo
tasks that mention deploy. The count under the box says how many are shown, and **esc** clears it.

### Bundles

Each bundle, its tasks in order, and what it expands to, with **Run this bundle**.

**Filter bundles** searches each bundle's name, description, source file, the tasks it expands to,
and the bundles that use it, with the same word matching and count as the Tasks page.

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

**Generate tasks** (experimental) asks a language model to write tasks from a description, such
as "install postgres and back it up every night". The tasks it writes are checked so they load,
then queued as unsaved changes: read and fix each one before you save, because the model gets
commands wrong. The model is Qwen3 4B Instruct (Apache 2.0), run inside `gorun -serve` on the GPU
when one is found. The first use downloads llama.cpp (about 36 MB) and the model (2.5 GB) into
`GORUN_CACHE_DIR/llm`. Only the web service does this; the command line and the TUI never load a
model. To use Ollama or another OpenAI-compatible server instead, set `GORUN_LLM_URL` (and
`GORUN_LLM_MODEL` to its model name).

### Assistant

A chat (experimental) with the same language model. Ask it about the loaded Gorunfile's tasks and
servers, have it summarise runs or explain why a task failed, or have it write tasks. Each
question sends it:

- the Gorunfile's tasks, bundles and servers, with each server's tasks in run order;
- the tasks the question names, as TOML, so it can change them;
- the ten newest runs;
- the newest run in detail: every task's state, and the command, exit code and end of the output
  of each failed task. A run of more than 40 tasks is counted per server instead, and only its
  tasks that didn't pass are listed.

Env vars and template variables are never sent. **Ask the assistant** on a run's History page
starts the chat on that run instead.

When a reply holds tasks that load, the chat offers to add them as unsaved changes, to review and
save on the Config page. Tasks that don't load are reported, so you can ask it to fix them.

It can change tasks too: ask, for example, "add a timeout to the migrate task". When a question
names a task, the model is sent that task as it is now. A task in the reply with the name of one
the Gorunfile has replaces it, in the file it's defined in, and the chat says which tasks it
replaces. Generate tasks only writes new ones. A
task may copy or run a file that isn't there yet; create it before you save, as the Gorunfile
doesn't load without it.

The page says whether the chat is private. It is when the model runs inside gorun, or behind a
`GORUN_LLM_URL` on this machine (`localhost` or a loopback address). With any other URL, each
question sends the Gorunfile and run output to that host, and the page says so in amber.

The model is small: it reads failures well when the output says what went wrong, but its tasks
often hold placeholders, such as paths and passwords, to fill in. [`training/`](../training) builds
a version of it fine-tuned on gorun; point `GORUN_LLM_MODEL` at the GGUF file it writes.

## Walkthrough: from nothing to a running schedule

This takes about two minutes and uses the [scheduled jobs recipe](../recipes/05-scheduled-jobs/README.md),
which runs only on this machine.

1. Start the web service with the recipe's Gorunfile:

   ```bash
   gorun -serve recipes/05-scheduled-jobs/Gorunfile
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
