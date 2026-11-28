# The web UI

`gorun -serve` is the main way to use gorun. It keeps your Gorunfiles in its history database,
shows the tasks, bundles, schedules and run history, runs tasks on servers, adds and edits them,
and fires the schedules while it runs. It is the long-running host for [schedules](schedules.md).

```bash
gorun -serve                                 # open http://localhost:8080; Gorunfiles in the database
gorun -serve Gorunfile                       # or run and edit a Gorunfile kept as a file
gorun -serve -live                           # watch only: Live, History and Assistant
```

Started **without a Gorunfile**, as it is meant to be, it keeps Gorunfiles in its history
database (`GORUN_DB_PATH`: SQLite, Postgres or MySQL, see [storage](storage.md)), so they last
across restarts, every save is a version, and every gorun on a shared database sees them. It can
keep several, each with a name:

- **The first start** saves a starter Gorunfile called `default`, with only a title. Add servers,
  tasks, bundles and schedules on the Config page, or have the [assistant](#assistant) write and
  change tasks.
- **The Gorunfiles page** lists them. **Open** one, and the Run, Tasks, Bundles, Schedules,
  Config and Assistant pages show it; the menu names the open one. **Create and open** adds a new
  one, and **Delete** removes one that isn't open and no other includes. Gorun starts on the one
  saved most recently.
- **Every one's schedules run**, whichever is open.
- **Every save** is kept as a new version. The startup message names the open one:
  `Gorunfile  web, kept in the history database (version 3), and 2 more whose schedules also run`.
- **Sharing tasks**: a Gorunfile can use the tasks and bundles of another kept here by including
  it, `include = ["stored::base"]`. Like a [git include](gorunfile.md#tasks-from-a-git-repository), only tasks and
  bundles come along: `base`'s servers, groups and schedules stay its own. It is live, so editing
  a shared task (from either Gorunfile) changes it for every Gorunfile that includes `base`.
  `stored::` includes only load in `gorun -serve`.
- **Only in the database**: runs, the editor, the assistant and the schedulers read them from
  the database, and no file is written for them. An edit is checked to load before it is saved,
  so a broken one never becomes a version. A stored Gorunfile can include only other stored ones
  (`stored::<name>`): local and git includes need a folder, and it has none. Relative paths in it,
  such as a copy's `src`, are relative to the folder gorun was started in, so use absolute paths.
- **Import and export**: the Config page imports a Gorunfile into the open one, and exports it as
  a zip with each task and bundle in a file of its own; see [Config](#config).
- **The Config page** names the open one, with **Switch or create**, **Export as zip** and
  **Import**. It has no file browser: to use a Gorunfile file as it is, start
  `gorun -serve <Gorunfile>`; to bring one into the database, import it.

Started **with a Gorunfile**, the service runs and edits that file instead, and keeps no
Gorunfile in its database: useful when the file lives in git, next to the scripts it copies.

With **`-live`**, or if the history database doesn't open, the service only watches, and the
stored Gorunfiles are left alone, schedules included. The menu then has just **Live**,
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
| `GORUN_ADMIN_PASSWORD` | unset | a password that signs in as `admin`; or add users on the Users page, see [signing in](#signing-in) |
| `GORUN_ADMINS` | unset | users an auth proxy signs in who are admins |
| `GORUN_DB_PATH` | `~/.gorun/gorun.db` | history database: an SQLite file, or a `postgres://` or `mysql://` URL (see [storage](storage.md)) |
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

## Signing in

**Running tasks always needs a signed-in user**, so every run started from the web UI records who
started it (its **Web user**, shown first on the run in Live and History, and sent to
OpenTelemetry as `enduser.id`). With no login set up, the Run page says to add a user first.

Without a login, anyone who can reach the port can browse folders, edit the Gorunfile and download
models, and the web UI listens on all interfaces by default. There are three ways to turn the
login on; any of them can be used together.

**Users.** Open the **Users** page and add the first user. They are an admin, and the browser
that added them is signed in as them; from then on, changes need signing in. An admin adds more
users there, each a plain user or an admin, sets their passwords and deletes them. Users are kept
in the history database, whichever it is (SQLite, Postgres or MySQL, see [storage](storage.md)),
with their passwords as bcrypt hashes; every gorun on the same database shares them.

**An admin password**, set in gorun's environment (never in the Gorunfile), signs in as `admin`:

```bash
GORUN_ADMIN_PASSWORD="$(cat ~/.gorun/admin-password)" gorun -serve Gorunfile
```

It works without the history database, and keeps a way in if every admin forgets their password.

**An auth proxy** such as oauth2-proxy or Authelia: list its users who are admins in
`GORUN_ADMINS=alice,bob`, and those who are plain users in `GORUN_PROXY_USERS=sam,kim`, or
`GORUN_PROXY_USERS=*` for anyone the proxy signs in. gorun trusts the user the proxy names in
`X-Forwarded-User`, `X-Auth-Request-User` or `Remote-User`, so gorun must be reachable only
through the proxy: anyone who can reach it directly can send that header. A name the proxy sends
that neither lists is not signed in, and is never recorded on a run.

With a login on, anyone may still look: the Live, History, Tasks and other pages, and asking the
assistant. Changes need **Sign in**, at the top right:

| | Anyone | User | Admin |
|---|---|---|---|
| Look at every page, ask the assistant | yes | yes | yes |
| Start runs (recorded as theirs) | | yes | yes |
| Cancel runs, save tasks the assistant wrote, edit the Gorunfile and schedules, delete history | | yes | yes |
| Open, close, delete and download the assistant's models | | | yes |
| Add users, set their passwords, delete them | | | yes |

A form sent without signing in goes to the sign in page. A session is a cookie that lasts 7 days.
It is signed with a key kept in the history database (or, without one, made from the admin
password), so it survives a restart, and it ends when that user's password changes or they are
deleted. A wrong name or password waits a second. An admin can't delete themselves, so one is
always left.

The startup message's `login` line says which of these is on. Serve over HTTPS (a proxy in front
is the usual way) so passwords and cookies aren't sent in the clear; the cookie is marked
`Secure` when gorun sees HTTPS or `X-Forwarded-Proto: https`.

> **Without a login** keep the web UI on a trusted network, or set
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

The image is Alpine with gorun, and it runs as the `gorun` user. Its state lives in
`/var/lib/gorun`: the history database, the log and git include checkouts. Mount that folder to keep
them. Local tasks run with the image's `/bin/sh`. Tasks over SSH need the keys and `known_hosts`
mounted into the container, with the Gorunfile's `[ssh]` settings pointing at them.

> **Set a login.** A published port gives anyone who can reach it the web UI. Pass
> `-e GORUN_ADMIN_PASSWORD` (see [signing in](#signing-in)), or publish it only on a trusted
> network, for example `-p 127.0.0.1:8080:8080`.

### With the assistant's model built in

The default image has no model: llama.cpp's Linux build needs glibc, which Alpine lacks, so its
assistant only works with `GORUN_LLM_URL` pointing at a model server. The Dockerfile's `llm` stage
builds a Debian image instead, with llama.cpp and gorun's model already inside, so the assistant
answers its first question without downloading anything, and works offline:

```bash
make docker                                   # or: docker build --target llm -t gorun:llm .
docker run --rm -p 127.0.0.1:8080:8080 -v gorun-data:/var/lib/gorun gorun:llm
```

The image is about 3 GB, most of it the model. The model is in `/opt/gorun/llm`
(`GORUN_LLM_DIR`), outside the `/var/lib/gorun` volume, so mounting a volume doesn't hide it. It
runs on the CPU, which is slower than a GPU: a short answer takes several seconds, and the first
loads the model, about 4 GB of memory. The build downloads llama.cpp and the model with
`gorun -pull-model`, checked against the same digests as a download in the web UI, and keeps them in
a Docker build cache, so rebuilding after a code change doesn't download them again.
`make docker-slim` builds the default image.

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

### Publish from a release

Every version tag `vX.Y.Z` builds an image for `linux/amd64` and `linux/arm64`, and pushes it as
`X.Y.Z`, `X.Y` and `latest`. The workflow is `.github/workflows/docker.yml`. It always pushes to
GitHub Container Registry, with the built-in token and no setup:

```bash
docker pull ghcr.io/jenmud/gorun:latest
```

The first push creates the package as private. To make it public, open the package's settings on
GitHub and change its visibility.

To also push to Docker Hub, set the repository variable `DOCKERHUB_IMAGE` to `youruser/gorun`, and
add the secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` (a Docker Hub access token). Then the same
tags go to Docker Hub too.

To push by hand, sign in to the registry, tag the image, and push it:

```bash
docker login
docker tag gorun:dev youruser/gorun:0.34.0
docker push youruser/gorun:0.34.0
```

## The pages

Every task and bundle name in the UI is a link to its card. The link opens the card and scrolls to
it, so you can click through from a schedule to its target, from a bundle to its tasks, or from a
task to the bundles that include it.

### Live

The front page (`/`) shows what is running now: runs started from the Run page, from the
terminal, or by a schedule. Stats and **Recent runs** bars sit above it, and each bar links to
the run. Runs are listed as on the [History](#history) page: newest first, each run's servers in
the order it planned them, and, once a run is over, each server's tasks slowest first. While a run
is going its tasks stay in the order they run, so rows don't jump as they finish. Each run, server
and task starts collapsed. A task that is uploading a file shows a bar
with the size and rate, and a closed server shows the upload on its line
([screenshot](screenshot-web-upload.png)). **Stop** cancels a run started from the web UI,
**Ask the assistant** opens the [assistant](#assistant) on that run, to summarise it or explain a
failure, and **Clear finished** drops the runs that are over. **Start a run** opens the Run page.

### Run

The picker (`/run`): tick servers, then tasks or bundles (not both), and press **Run selected**.
It needs a signed-in user, recorded on the run (see [signing in](#signing-in)).
The run starts and the page moves to **Live** to watch it. **Force** runs every task even when its
[check](gorunfile.md#checks-run-only-when-needed) says there is nothing to do, and **Check only**
is a dry run that only runs the checks: its tasks end *unchanged* or *would run*, and the run is
marked *check only*. Runs read the saved Gorunfile, so save first. A run that can't start stays
on the Run page and says why.

Ticked tasks marked **Local** (`local = true`) run on this machine without a server ticked. Every
other task runs only over SSH, so it needs a ticked SSH server. The page names the tasks that need
**Local** ticked: ticked ones when the Gorunfile has no SSH server, and the ones a local task
depends on, which this machine would otherwise skip.

### Tasks

Every task with its command, settings, dependencies, the bundles that include it and the servers
that run it, slowest first by recent average. A task's card shows its **copies** (each file or folder and
where it goes on the server), its **check** and its **verify** under its command, folded like
it, with what each does; a task without them says so. The `check` and
`verify` badges mark the tasks that have them.

- **Add a task**: name, command, files to copy, depends on, timeout, retries, check, verify,
  check copies, vars, env vars, description, sudo, local, and the file to save into. It is
  written to the file straight away.
- **Edit** on a task: change any of those. Clear a field to remove it. Settings the form doesn't
  show, such as `cmd_file` or `raw`, are kept.
- **Vars** and **Env vars** are TOML, one `name = value` per line. Vars are what the task's
  [templates](gorunfile.md#task-variables) read as `{{ .vars.name }}`, and can be numbers,
  strings, lists or `[table]` sections. Env vars are exported to the command and must be quoted
  strings; write a secret as `TOKEN = "${REGISTRY_TOKEN}"`, so the Gorunfile keeps the reference
  and gorun fills it in from its own environment when the task runs.
- **Files to copy** takes one copy per line: a local file or folder, `->`, and where it goes on
  the server, such as `scripts/stack-ok.sh -> /home/ubuntu/stack-ok.sh`. Start a line with
  `template` to render a Go template for each server. Local paths are relative to the Gorunfile.
  After the destination, add `mode=0640`, `owner=name` or `group=name` to set the permissions and
  owner (owner and group need the task's sudo). Those words always come last on the line.
  Copies of inline text (`content = ...`) can't be written this way, so the form says how many
  there are and keeps them.

The Config page's **Add a task** form and **Edit** dialog have the same check, verify, vars and env
vars fields.

**Filter tasks** searches each task's name, description, details, command, copies, check, verify,
dependencies, flags and source file. Every word typed must match, so `deploy sudo` finds the sudo
tasks that mention deploy. The count under the box says how many are shown, and **esc** clears it.

### Bundles

Each bundle, its tasks in order, and what it expands to, with **Run this bundle**.

**Filter bundles** searches each bundle's name, description, source file, the tasks it expands to,
and the bundles that use it, with the same word matching and count as the Tasks page.

- **Add a bundle**: name, tasks, description, file.
- **Edit** on a bundle: change its tasks and description.

Tasks are picked from a list of the Gorunfile's tasks and `@bundles`, and shown as labels in the
order picked, which is the order they run. **×** on a label removes it; the filter box above the
list narrows it, and enter picks the first match. The Config page's bundle and server group forms
pick tasks the same way. Without JavaScript the field is a comma separated list.

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

**Export as zip**, next to the list of files, downloads the saved Gorunfile, such as the one kept
in the history database, as a folder that loads as it is:

```
gorunfile/
  Gorunfile            settings, servers, groups and schedules; includes the files below
  tasks/<name>.toml    one task each
  bundles/<name>.toml  one bundle each
```

Local included files are folded in, and so are the tasks and bundles of `stored::` includes, so
the zip loads anywhere. Git includes are kept, with `Gorunfile.lock`. Unsaved changes,
comments, and the files tasks copy or run (`copy` sources, `cmd_file`) aren't in the zip.

**Import**, shown when the open Gorunfile is one kept in the history database, replaces it with an
uploaded Gorunfile, or a zip holding one and the files it includes, such as an export. Local
includes are folded into one file. It is checked to load before anything changes. The Gorunfile
before it is kept in the database as an earlier version, and unsaved changes are dropped. Uploads
are limited to 10 MB.

**Generate tasks** (experimental) asks a language model to write tasks from a description, such
as "install postgres and back it up every night". The tasks it writes are checked so they load,
then queued as unsaved changes: read and fix each one before you save, because the model gets
commands wrong. The model is Qwen3 4B Instruct (Apache 2.0) fine-tuned on gorun, run inside `gorun -serve` on the GPU
when one is found. It needs about 4 GB of GPU memory; when there isn't that much free, such as
when another program or another `gorun -serve` is using it, the model runs on the CPU instead,
which is slower, and the chat says so. The first use downloads llama.cpp (about 36 MB) and the model (2.5 GB) into
`GORUN_LLM_DIR` (default `GORUN_CACHE_DIR/llm`); `gorun -pull-model` downloads them ahead, and the
[Docker image with the model](#with-the-assistants-model-built-in) has them built in. Only the web service does this; the command line and the TUI never load a
model. On Linux the built-in model also needs the libffi 8 library; when it is missing, the chat
says so before downloading anything (`apt install libffi8`, `dnf install libffi` or
`apk add libffi`). To use Ollama or another OpenAI-compatible server instead, set `GORUN_LLM_URL` (and
`GORUN_LLM_MODEL` to its model name).

### Assistant

A chat (experimental) with the same language model. Ask it about the loaded Gorunfile's tasks and
servers, have it summarise runs or explain why a task failed, or have it write tasks and
schedules. Each
question sends it:

- the Gorunfile's tasks, bundles, schedules and servers, with each server's tasks in run order;
- the tasks and schedules the question names, as TOML, so it can change them;
- for a question about gorun, the two sections of these docs that match it best (from `docs/`,
  built into gorun), so it answers from them rather than from memory; requests, such as for
  tasks, and questions about runs aren't sent any;
- the ten newest runs, leaving out a schedule's missed times (runs of no tasks);
- the newest run in detail, or the one you asked from: how many tasks ended in each state, every
  task's state, the command, exit code and end of the output of each failed task, and, while
  there is room, the end of what each task that passed printed. A run of more than 40 tasks is
  counted per server instead, and only its tasks that didn't pass are listed.

The **Model** panel above the chat says where the model stands: not downloaded yet, downloaded
but not loaded, or loaded on the GPU or the CPU, and why it is on the CPU. Once loaded it shows
the model and its size, the GPU and its free memory, the context size, how long loading took,
and how fast the last answer was read and written. It is drawn again after each answer. With
`GORUN_LLM_URL`, it names the server instead, as gorun can't see that server's memory.

The **Models** panel below it lists gorun's own model and every GGUF model in
`GORUN_LLM_DIR/models`, with the one in use. With a [login](#signing-in) only admins can change
it; others see the list.

- **Open** switches to a model and loads it, freeing the one loaded before; **Close** frees the
  model's memory until the next question loads it again. Everyone shares the one model, so these
  are refused while someone is using it: an answer is being written or waiting, or a question
  finished in the last 5 minutes, when their chat may still be going. The refusal offers to
  **Open it anyway** or **Close it anyway**, which still waits for the answer being written. The
  model opened last is used again after a restart, unless `GORUN_LLM_MODEL` names one.
- **Delete** removes a downloaded model, once another one is in use. gorun's own can't be deleted.
- **Search** finds GGUF models on [Hugging Face](https://huggingface.co/models?library=gguf), the
  most downloaded first, with their licences: one that isn't Apache 2.0 or MIT is shown in amber,
  to check before you use it. Open one to list its GGUF files, smallest first, and **Download**
  one. It downloads in the background, one at a time, with its progress shown and a **Cancel**,
  and is checked against the SHA-256 Hugging Face publishes for it before it can be opened.
  Gated models, which need a Hugging Face login, and models split over several files can't be
  downloaded this way.

Any model llama.cpp can run works, but gorun's prompts and training are made for its own model, so
others usually answer worse, and a bigger one needs more memory and is slower. A download doesn't
touch the model in use, so it can run while others ask questions. Without a login, anyone who can
open the web UI can download models onto this machine's disk.

While it answers, the chat shows what it is doing and for how long: downloading the model on first
use (with how far along), loading it, reading the question, then the reply as it is written.
**Stop** ends an answer early. gorun loads the model once and answers one question at a time, whoever asks, so
more people using it never costs more memory, only a wait: the chat says how many questions are
ahead of yours, and with 8 waiting the next is asked to try again in a minute. Each `gorun -serve`
process loads its own copy, so run one per machine.

Env vars and template variables are never sent. **Ask the assistant** on a run's History page,
or on its card on the Live page, starts the chat on that run instead.

The assistant writes `local = true` on tasks meant for this machine, and on every task when the
Gorunfile has no server that runs over SSH.

It writes small tasks, one step each, chained with `depends_on`, and every one has a `check`
(exits 0 when the step is done, so it is skipped) and a `verify` (fails the task when the step
didn't work). A reply with a task missing either is sent back to the model to fix before it is
offered.

When a reply holds tasks that load, the chat offers **Save these tasks**, which saves them into
the Gorunfile straight away, as an edit on the Tasks page does; there is nothing more to save on
the Config page. A task with unsaved changes on the Config page is left alone until you save or
discard them. Tasks that don't load are reported, so you can ask it to fix them.

It writes schedules too: ask "run backup every night at 2am, Sydney time", or "and run it every
Monday" after a request for tasks, and the reply holds a `[[schedule]]` entry. It is checked as
the loader checks one (its target exists, exactly one of `cron` or `at`, a real time zone) and
saved by the same button. Ask it to change a schedule by name, such as "move nightly-backup to
4am", and the reply replaces that schedule in place. Pausing and skipping stay on the
[Schedules](#schedules) page.

It can change tasks too: ask, for example, "add a timeout to the migrate task". When a question
names a task or schedule, the model is sent it as it is now. A task or schedule in the reply with
the name of one the Gorunfile has replaces it, in the file it's defined in, and the chat says
which ones it replaces. Generate tasks only writes new ones. A
task may copy or run a file that isn't there yet; create it before you save, as the Gorunfile
doesn't load without it.

The page says whether the chat is private. It is when the model runs inside gorun, or behind a
`GORUN_LLM_URL` on this machine (`localhost` or a loopback address). With any other URL, each
question sends the Gorunfile and run output to that host, and the page says so in amber.

#### Training data

To help train gorun's model on real questions, an admin can keep the assistant's answers: the
**Training data** link on the Assistant page, **Turn on**, or `GORUN_ASSISTANT_CAPTURE=1` in
gorun's environment. It is off until then, and needs the history database. While it is on, the
Assistant page says so to everyone who uses it, and each answer has 👍 and 👎 to rate it.

For every answer, from the chat and from **Generate tasks**, it keeps, in the history database:

- the prompt exactly as the model got it: the question, the conversation, and the context (the
  Gorunfile's tasks and servers, recent runs, doc sections). Env vars and template variables are
  never in it;
- the reply, whether its tasks loaded or had to be sent back to fix, and why not;
- whether someone saved its tasks, and how those tasks ran after: the page counts the runs of
  them that passed and failed since they were saved;
- the rating, and who asked, when there is a [login](#signing-in).

The Training data page lists the newest 100, deletes one or all of them, and **Export JSONL**
downloads them all, one per line, in the shape of `training/data/train.jsonl` (`messages`, the
reply last) with a `meta` object saying what became of it. The export replaces IP addresses,
e-mail addresses, private keys and tokens that look like AWS, GitHub, Slack or OpenAI ones, and
leaves out who asked; hostnames and names in your Gorunfile stay, so read it before you share
it. `?redact=0` exports it as kept. Nothing is sent anywhere by gorun. See
[training](../training/README.md#real-questions-from-gorun--serve) for using it.

The model is small: it reads failures well when the output says what went wrong, but its tasks
can hold placeholders, such as paths and passwords, to fill in. It is Qwen3 4B Instruct
fine-tuned on gorun by [`training/`](../training): on Gorunfiles, gorun's docs, writing small
tasks that each have a check and a verify, and diagnosing runs. It is published at
[jenmud/gorun-qwen3-4b-GGUF](https://huggingface.co/jenmud/gorun-qwen3-4b-GGUF). To use the stock
Qwen3 4B Instruct, or any other GGUF file, point `GORUN_LLM_MODEL` at the file or its URL.
`make model` downloads the built-in model ahead of the first question.

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
