# Changelog

Release notes for each tag are generated from the commit subjects (see `.goreleaser.yaml`), so
the subjects say what changed. This file lists the changes since the last tag, and is copied into
the release notes when a version is cut.

## v0.40.1 (since v0.40.0)

### Running tasks from the web UI needs a signed-in user
- Every run started from the web UI now needs a signed-in user, login set up or not, and records
  them as the run's web user: shown first in Live and History, and sent to OpenTelemetry as
  `enduser.id`. With no login yet, the Run page says to add a user on the Users page.
- A user an auth proxy names is no longer recorded unless gorun trusts them: `GORUN_ADMINS` lists
  its admins, and the new `GORUN_PROXY_USERS` its plain users, or `*` for anyone it names.
- The docs say who scheduled runs run as: the account `gorun -serve` runs as, with no web user.

## v0.40.0 (since v0.39.2)

### Keep the assistant's answers to train its model
- An admin can turn on capture on the Assistant page's new **Training data** page, or with
  `GORUN_ASSISTANT_CAPTURE=1`. Each answer, from the chat and Generate tasks, is kept in the
  history database (SQLite, Postgres or MySQL, through a new migration) with its prompt as sent,
  whether its tasks loaded or needed fixing, whether they were saved and how they ran after.
- Answers get 👍 and 👎. While capture is on, the Assistant page says so to everyone.
- **Export JSONL** downloads them in the training data's shape, with IP and e-mail addresses,
  keys and tokens replaced. Nothing leaves the machine otherwise. `training/README.md` shows how
  to pick the good ones for training.

## v0.39.2 (since v0.39.1)

### Scheduled runs use the latest Gorunfile
- `gorun -serve` loads the Gorunfile again before every scheduled run, so it runs what the file
  says then: edited on disk, pulled from git, or saved as a new version by another gorun on the
  same database. Before, it ran the copy loaded when the web service started or last saved. A
  Gorunfile that no longer loads, or no longer has the schedule, fails that run with the reason.
- `Scheduler.LoadLatest` does the same for programs built on the library.

## v0.39.1 (since v0.39.0)

### The database comes first in the docs
- The README, the web UI docs, the tutorial and the FAQ lead with `gorun -serve` keeping
  Gorunfiles in its database, every save a version; a Gorunfile kept as a file is for the
  terminal, scripts, CI and git. Nothing about how gorun runs has changed.
- The tutorial videos say "live" as in a live concert.

## v0.39.0 (since v0.38.0)

### Users and sign in for the web UI
- A **Users** page keeps users in the history database (SQLite, Postgres or MySQL, through a new
  migration), with bcrypt-hashed passwords. The first user added is an admin and turns the login
  on; admins add users or admins, set their passwords and delete them.
- With a login, anyone can still look and ask the assistant. Running tasks, editing the
  Gorunfile and schedules and deleting history need a user; managing users and the assistant's
  models needs an admin. Sessions last 7 days and end when the password changes.
- `GORUN_ADMIN_PASSWORD` signs in as `admin`, without the database too, and behind an auth proxy
  `GORUN_ADMINS` lists its users who are admins. With none of these, the web UI stays open as
  before, and the startup message says so.

### Switch models from the Assistant page
- A **Models** panel on the Assistant page lists gorun's own model and the downloaded ones. **Open**
  switches to one and loads it; **Close** frees its memory; **Delete** removes a downloaded one.
  The model opened last is used again after a restart, unless `GORUN_LLM_MODEL` names one.
- Opening and closing are refused while someone is using the assistant, or did in the last 5
  minutes, so the model doesn't change in the middle of someone's chat; **Open it anyway** does it
  regardless, once the answer being written is done.
- **Search** finds GGUF models on Hugging Face, with their licences, and downloads one in the
  background, checked against the SHA-256 Hugging Face publishes for it.

## v0.38.0 (since v0.37.2)

### A Docker image with the assistant's model built in
- `make docker` builds the web service image with llama.cpp and gorun's model inside (Debian,
  about 3 GB, the Dockerfile's `llm` stage), so the assistant answers its first question without
  downloading anything, and works offline. `make docker-slim` builds the default Alpine image,
  whose assistant needs `GORUN_LLM_URL`, since llama.cpp's Linux build doesn't run on Alpine.
- `gorun -pull-model` downloads the assistant's llama.cpp and model ahead of its first question.
- `GORUN_LLM_DIR` says where they are kept (default `GORUN_CACHE_DIR/llm`).

## v0.37.2 (since v0.37.1)

- gorun starts on Linux without libffi again. Since v0.36.0 it loaded `libffi.so.8` at start, for
  the assistant's llama.cpp bindings, and stopped when it was missing (Debian 11, minimal
  containers). gorun now uses a patched copy of `github.com/jupiterrider/ffi` in `third_party/ffi`
  that loads libffi the first time the built-in model is used, and the assistant says how to install
  it, before anything is downloaded.

## v0.37.1 (since v0.37.0)

- The Docker image builds again. `.dockerignore` left out `docs/`, which is also the Go package
  holding the docs the assistant searches, so no image was published for v0.37.0.
- The image includes libffi. Since v0.36.0, gorun on Linux loads `libffi.so.8` when it starts, for
  the assistant's llama.cpp bindings, and stops if it is missing. Most Linux distributions have it;
  the README says how to install it where they don't.

## v0.37.0 (since v0.36.1)

### Docker images from GitHub
- The release workflow now pushes the web service image to GitHub Container Registry
  (`ghcr.io/jenmud/gorun`) on every version tag. It needs no secrets. Docker Hub is still
  optional, set by `DOCKERHUB_IMAGE`.

### History in Postgres or MySQL
- `GORUN_DB_PATH` takes a `postgres://` or `mysql://` URL as well as an SQLite file, so several
  gorun processes and machines can share one history, Gorunfiles kept by `gorun -serve` included.
  The startup message and logs hide the URL's password.
- The tables come from numbered migration files for each database, built into gorun and applied
  on open under a lock, and recorded in `schema_migrations`. An SQLite file from an older gorun
  is upgraded in place.
- `history.Open` takes the same URLs, for programs built on the library.

### Live lists runs as History does
- The Live page lists runs newest first, as History does; it listed them oldest first. History
  now always lists a run's servers in the order the run planned them, which Postgres and MySQL
  didn't promise.

### Picking a bundle's tasks
- The web UI's bundle forms, and the Config page's server group forms, list the Gorunfile's tasks
  and bundles to pick from, and show the picked ones as labels, in run order, each with a ×. Before,
  they were typed into a comma separated field.

## v0.36.1 (since v0.36.0)

- The macOS and Windows builds work again. The vendored `libffi` libraries they embed were left
  out of the repository by `.gitignore`, so v0.36.0 has no release binaries, and the module
  doesn't build for those systems.

## v0.36.0 (since v0.35.0)

### An assistant in the web UI (experimental)
- `gorun -serve` has an **Assistant** tab: a small language model that runs on your own machine.
  It explains failed runs, summarises runs, answers questions about the loaded Gorunfile, and
  writes tasks and schedules. A failed run on Live and History has an **Ask the assistant** link.
- The built-in model is gorun's own fine-tune of Qwen3 4B Instruct (Apache 2.0),
  `jenmud/gorun-qwen3-4b-GGUF`, about 2.5 GB. The first question downloads it and llama.cpp into
  gorun's cache folder. It runs on the GPU when there is room and on the CPU otherwise; the
  Model panel shows where, and how fast. Questions from many users wait in one queue.
- Tasks it writes are small, one step each, with a `check` and a `verify`; a bigger job becomes
  small tasks chained with `depends_on`. It writes `[[schedule]]` entries too, and changes an
  existing task or schedule by writing it again under the same name. Every reply is checked to
  load, and sent back to the model to repair when it doesn't. **Save** puts it in the Gorunfile.
- Questions about gorun carry the best-matching sections of its docs, embedded in the binary.
  A new [FAQ](docs/faq.md) answers the common ones.
- Your env vars and template variables are never sent to the model. `GORUN_LLM_MODEL` points at
  another GGUF file or URL; `GORUN_LLM_URL` at a server such as Ollama, with an amber warning
  when that server isn't on this machine.
- It is experimental and needs much more training: it writes working tasks about 8 times in 10
  on held-out tests, but can still make up options. Read everything it writes.
- `-generate` writes tasks from the command line with the same model.

### Gorunfiles in the database
- Started without a Gorunfile, `gorun -serve` keeps Gorunfiles in its history database, starting
  with one called `default`, and every save is a new version. The **Gorunfiles** page creates,
  opens and deletes them, and one can use another's tasks with `include = ["stored::base"]`.
- **Export** downloads a Gorunfile as a zip, one file per task; **Import** takes a Gorunfile or a
  zip, checked to load before anything changes.
- `gorun -serve -live` shows only Live, History and the Assistant, to watch runs from elsewhere.

### Fixes
- A task that timed out over SSH closed the server's whole connection, so the server's other
  tasks, running alongside it or after it, failed with "use of closed network connection". Only
  the timed-out task's session is closed now.
- The Live page could deadlock `gorun -serve` with a stored Gorunfile open.
- Local tasks run from the web UI.
- ctrl-c stops `gorun -serve` while the assistant is answering.

## v0.35.0 (since v0.34.0)

### A Docker image for the web service
- A `Dockerfile` runs `gorun -serve` on Alpine, with a static binary. It is about 56 MB on amd64
  and runs as a non-root user. Configure it with the `GORUN_*` variables. `GORUN_FILE` names an
  optional Gorunfile; without one, only the Live and History pages run.
- `.github/workflows/docker.yml` publishes linux/amd64 and linux/arm64 images to Docker Hub on
  version tags, once the `DOCKERHUB_IMAGE` repository variable is set.

## v0.34.0 (since v0.33.0)

### OpenTelemetry logs, and nothing lost on exit
- The instrumented build now sends gorun's log lines as OpenTelemetry logs, with the trace and
  span they belong to. The docs said it did, but only traces and metrics were sent.
- gorun flushes traces, metrics and logs before it exits. A one-shot `gorun -r` run used to lose
  its `gorun.run` span, and the last logs and metrics, because the SDK only flushed on a signal.

## v0.33.0 (since v0.32.0)

### `gorun -serve` without a Gorunfile only watches
- Started without a Gorunfile, the web UI shows only **Live** and **History**, for runs started
  elsewhere. Run, Tasks, Bundles, Schedules and Config don't exist: they aren't in the menu,
  and their pages and forms answer 404.
  Task and schedule names in History are plain text, since their pages aren't there. Before, it
  showed every page and asked you to load a Gorunfile on the Config page. The startup message
  says `Gorunfile  none: only the Live and History pages, for runs started elsewhere`.

### Install the latest release, and say which
- The install script installs the latest release on every machine. The instrumented build is
  only published for Linux x86-64; elsewhere the script used to build master from source, which
  reported version 0.1.0. It now installs the standard prebuilt binary instead, and builds from
  source only when there is none, stamping the release's version in.
- When GitHub's API won't say which release is the latest (it limits how often it answers),
  the script asks the release page instead of building master.
- `gorun -version` no longer falls back to a hard-coded 0.1.0. A release says its tag; a build
  with `go install ...@v1.2.3` or in a clone at a tag says that version; any other build says
  `devel` and its commit.
- `make build` stamps the version from `git describe` (`v0.32.0`, or `v0.32.0-3-g1a2b3c4` between
  tags). A test fails if a release build, the install script or the Makefile stops stamping it.

## v0.32.0 (since v0.31.0)

### Web UI: Live and Run are separate pages
- The front page is now **Live**: the stats, recent runs and the dashboard of what is running,
  from the web UI, the terminal or a schedule. Picking servers and tasks moved to its own **Run**
  page at `/run`, with the servers, bundles and tasks side by side. Starting a run goes to Live;
  a run that can't start stays on Run with the reason. "Run this bundle" links open `/run`.
- The live dashboard keeps up with big fleets: task events now update open pages at most four
  times a second, as copy progress already did, instead of once per event. A 42-server run sent
  690 whole-dashboard updates and left a browser about ten seconds behind; it now sends about 50.
  A run still appears, and finishes, at once.

## v0.31.0 (since v0.30.0)

### Vim keys and / to filter, on every screen
- The picker no longer filters as you type. Press **/** to filter, then type; **enter** keeps
  the filter and **esc** clears it. The search box above the servers is gone: the filter shows
  on the counts line. **/** also filters the Groups tab, the history's runs and an opened run's
  servers and tasks, the live view's servers, and the `--list-*` trees.
- Every screen takes vim keys as well as the plain ones: j/k, h/l, gg/G, ctrl+u/ctrl+d for half
  a page, H/L and ctrl+w h/l/w for panes, gt/gT for tabs, and 1 to 5 to jump to a tab. The
  arrows, pgup/pgdn, home/end, tab and shift+tab still work.
- The filter's matches are picked out in every list: server names and addresses, task and
  bundle names and descriptions, run titles, IDs and hosts. The row under the cursor keeps them
  picked out too.
- Selecting works the same everywhere: **alt+s** selects all and **alt+d** deselects all. On the
  servers they select every shown server (alt+s again deselects them); in the tasks pane they tick
  and untick the server's tasks; on Bundles they check and uncheck every bundle shown. The Servers
  tab's help is two lines: the first selects, the second moves.
- Each tab shows its number before its name (`1 Servers │ 2 Tasks │ …`), the key that goes to it.
- **?** opens the help, as ctrl+k does. The help lists each vim key next to its plain key, and
  scrolls with j/k when it is taller than the screen. The live view has a help too.
- **Removed:** `scripts/ansible2gorun.py`, the Ansible converter, with its tests and the
  `scripts/testdata/ansible-demo` example.
- **Changed:** select all shown servers moves from ctrl+a to **alt+s**, clear the selection
  from ctrl+d (which now moves half a page) to **alt+d**, and tick and untick a server's tasks
  from alt+t and ctrl+x to **alt+s** and **alt+d** in the tasks pane.
  A single **g** no longer jumps to the top; use **gg** or home.

## v0.30.0 (since v0.29.0)

### Writing a Gorunfile from Go
- `Config.Encode(w)` and `Config.Marshal()` write a config as a Gorunfile, the inverse of `Load`:
  settings, vars, envvars, ssh, tasks, bundles, server groups, schedules, and a
  `[servers.hosts.<name>]` table per server in `Config.Servers` order. Keys are sorted and empty
  values left out, so the output is stable and minimal; values are written exactly as they are,
  never filled in or expanded. A program that loads a task library and adds servers from its own
  inventory can show, save or diff the Gorunfile, and load the text to run it.
- The TOML tags on `Config`, `Task`, `Copy`, `Bundle`, `ServerGroup`, `Schedule`, `Server` and
  `SSH` gain `omitempty`/`omitzero`. Reading a Gorunfile is unchanged.

## v0.26.0 to v0.29.0 (since v0.25.0)

### Copies: permissions and ownership
- A copy takes `mode` (or `perm`), an octal permission such as `0644`, and `owner` and `group`.
  The mode applies to a copied file, or to each file in a copied folder. Owner and group are set
  with `chown`; with `sudo = true` on the task they run under `sudo -n`.
- `mode` was documented before but ignored, so copied files kept their local permissions. It now
  takes effect. A mode that isn't octal fails when the task is filled in.
- `check_copies` also compares the target's mode, owner and group, so a change to only the
  permissions copies the file again. The output says what differs, such as
  `changed  /etc/app.conf (mode 0640 wanted, rw------- found)`.
- Task summaries in every view show the settings after the destination, such as
  `app.conf → /etc/app.conf (mode 0640, owner nobody, group nogroup)`.
- The web form takes `mode=0640`, `owner=name` and `group=name` after a copy's destination.

### Converting Ansible
- `scripts/ansible2gorun.py` converts an Ansible inventory and playbooks into a Gorunfile. Tasks
  it can't convert are kept and fail on purpose, and are listed in the output. It doesn't handle
  Ansible facts yet. See `scripts/testdata/ansible-demo` for an example it converts in full.

### Search
- The Tasks and Bundles tabs of the TUI filter the tree: press **/**, type, then **enter** to keep
  the filter or **esc** to clear it. A task matches on its name, description, command, copies,
  check and verify.
- The web Tasks and Bundles pages search more of each task or bundle, match every word typed, and
  show how many are shown. **esc** clears the box.

### Fixes
- `copy` ignored a copy's `mode`, as above.
