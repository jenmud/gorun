# Questions people ask

Short answers, each with where to read more.

## Where can I learn gorun?

The website, [go-run.dev](https://go-run.dev), has an overview, these docs, tutorial videos at
[go-run.dev/videos](https://go-run.dev/videos) and a playground that loads a Gorunfile in your
browser and shows its plan and a simulated run. The source is at
[github.com/jenmud/gorun](https://github.com/jenmud/gorun). Start with the [tutorial](tutorial.md);
[the Gorunfile](gorunfile.md) covers every option. `gorun -dump` prints a starting Gorunfile that
documents every key.

## How do I install gorun?

```bash
curl -fsSL https://raw.githubusercontent.com/jenmud/gorun/master/scripts/install.sh | sh
```

It installs the latest release (`VERSION=v0.31.0` picks another), and `gorun -version` says which
you have. Or download a binary from the [releases](https://github.com/jenmud/gorun/releases), or
build it with `go build -o gorun ./cmd`. Nothing is installed on the servers: gorun only needs SSH.
On Linux, the assistant's built-in model needs the libffi 8 library, which most distributions
have: install it with `apt install libffi8`, `dnf install libffi` or `apk add libffi`. gorun
v0.36.0 to v0.37.1 needed it just to start, and stopped with
`Error loading shared library libffi.so.8` without it.

## How do I run gorun in CI?

`gorun Gorunfile -r -plain`: `-r` runs without the picker, and `-plain` prints plain log lines.
`-plain` is the default when stdout isn't a terminal, and `-r` is then required. The exit code is
non-zero when a task fails. See [running gorun](running.md).

## How do I send gorun's events to other systems?

Every run publishes its events as JSON to NATS, on the `gorun.events` topic (`GORUN_EVENT_TOPIC`),
at `GORUN_NATS_CLIENT_URL`; `GORUN_SKIP_NATS=1` turns it off. The instrumented build also sends
OpenTelemetry traces, metrics and logs to the `OTEL_*` endpoint you set. See
[integrations](integrations.md) and [recording runs](history.md).

## Is the web UI safe to expose?

Turn the login on: add the first user on the **Users** page (they are an admin), or set
`GORUN_ADMIN_PASSWORD` in gorun's environment. Then anyone can look and ask the assistant, but
running tasks and editing the Gorunfile need **Sign in**, and managing users and models needs an
admin. Users are kept in the history database with bcrypt-hashed passwords. Behind an auth proxy,
list its admin users in `GORUN_ADMINS` instead. Without any of these, anyone who can reach its
port can run tasks and edit the Gorunfile, and it listens on all interfaces by default: keep it
on a trusted network, or bind it with `GORUN_ADDRESS=127.0.0.1:8080` and use an SSH tunnel.
Serve it over HTTPS, usually through a proxy. See [signing in](web.md#signing-in).

## How do I reset a forgotten password?

Restart the web UI with a new `GORUN_ADMIN_PASSWORD` in its environment, sign in as `admin` with
it, and use **Set** on the Users page to give the user a new password; their old sessions end.
`GORUN_ADMIN_PASSWORD` is never stored, so a forgotten one is reset the same way: set a new value
and restart. Another admin can also set the password on the Users page, with no restart.

## Can gorun run in Docker?

Yes: the repository's Dockerfile builds an image that runs `gorun -serve` on port 8080. Keep
`/var/lib/gorun` in a volume; it holds the history database, the log, git include checkouts and the
Gorunfiles kept in the database. `make docker` builds a larger image with the assistant's model
built in, so it works offline. See [running in Docker](web.md#running-in-docker).

## How does gorun connect to servers?

Over SSH, with a server's `ssh` table (or one of its groups'): `username`, `port` and
`identityfile`. Anything missing is read from your SSH config. A server with no `ssh` table is a
local one: only tasks marked `local = true` run there, on the machine gorun runs on. See
[servers](gorunfile.md#servers).

## My command has `{{` in it and gorun breaks it

Set `raw = true` on the task. Its command, check, verify and copies are then used exactly as
written, so `docker ps --format '{{.Names}}'` isn't read as a template.

## Where should I keep my Gorunfile?

In `gorun -serve`'s database, the main way: start `gorun -serve` without a Gorunfile and it keeps
them in its history database (SQLite, Postgres or MySQL), every save a version, shared by every
gorun on the same database. Keep one as a file when you run it from a terminal, a script or CI
(`gorun Gorunfile -r`), or want it in git next to its scripts; `gorun -serve Gorunfile` serves a
file too. Import a file on the Config page to move it into the database; export a zip to take one
out. See [the web UI](web.md).

## Can gorun keep more than one Gorunfile?

Yes, `gorun -serve` started without a Gorunfile keeps several in its database, and the Gorunfiles
page switches between them. One can share another's tasks with `include = ["stored::base"]`. See
[the web UI](web.md).

## Can gorun keep its history in Postgres or MySQL?

Yes. Set `GORUN_DB_PATH` to a URL instead of a file:
`GORUN_DB_PATH=postgres://gorun:secret@db:5432/gorun` or `mysql://gorun:secret@db:3306/gorun`. The
database must exist; gorun creates and migrates its tables on start. Every gorun pointed at it,
laptops, CI and the web UI, then shares one history. SQLite (`~/.gorun/gorun.db`) stays the default.
See [storage](storage.md).

## What does the assistant do, and which model is it?

The Assistant tab in `gorun -serve` explains failed runs, summarises runs, answers questions and
writes tasks and schedules. It is experimental. By default it runs gorun's own fine-tune of Qwen3
4B Instruct (Apache 2.0) on this machine, downloaded on the first question. `GORUN_LLM_MODEL`
points at another GGUF file or URL, and `GORUN_LLM_URL` at a server such as Ollama. The Assistant
page's Models panel also downloads GGUF models from Hugging Face and switches between them. See
[the assistant](web.md#assistant).

## Can I help train the assistant?

Yes. An admin turns on capture on the Assistant page's **Training data** page, or sets
`GORUN_ASSISTANT_CAPTURE=1`: every answer is then kept in the history database with its prompt,
whether its tasks loaded, were saved and ran, and the 👍 or 👎 it got. **Export JSONL** downloads
them, with addresses and keys replaced, for `training/`. Nothing is sent anywhere by gorun. See
[training data](web.md#training-data).
