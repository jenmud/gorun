# Where gorun keeps its history

Every run is recorded: its servers, tasks, commands, output and who started it. `gorun -serve`
started without a Gorunfile also keeps its Gorunfiles there, and the web UI keeps its
[users](web.md#signing-in), with bcrypt-hashed passwords. That history lives in one of three
databases, chosen by `GORUN_DB_PATH`:

| Storage | `GORUN_DB_PATH` | Use it when |
|---|---|---|
| **SQLite** (the default) | a file path, default `~/.gorun/gorun.db` | one machine, one person, nothing to set up |
| **Postgres** (tested on 17) | `postgres://user:password@host:5432/database` | a team: several machines, CI jobs and a web UI sharing one history |
| **MySQL** | `mysql://user:password@host:3306/database` | as Postgres, where MySQL (or MariaDB) is what you run |

All three keep the same data and behave the same in the CLI, `gorun -history`, the web UI and the
[history package](history.md). Nothing else changes: pick one with `GORUN_DB_PATH`, in the
environment or in a `.env` file next to where gorun runs.

- [SQLite](#sqlite)
- [Postgres](#postgres)
- [MySQL](#mysql)
- [Sharing one history](#sharing-one-history)
- [Migrations](#migrations)
- [Moving and backing up](#moving-and-backing-up)

## SQLite

Nothing to do. gorun creates the file and its folder on first use. The CLI uses `./gorun.db` when
there is one in the working directory, then `~/.gorun/gorun.db`; the web UI always uses
`GORUN_DB_PATH` or `~/.gorun/gorun.db`. One SQLite file is best kept on one machine: put it on a
network share and several gorun processes writing to it at once will wait on each other's locks.

## Postgres

### 1. A database and a user

gorun needs a database of its own and a user that may create tables in it. On a Postgres you
already run, as an admin:

```sql
CREATE USER gorun WITH PASSWORD 'change-me';
CREATE DATABASE gorun OWNER gorun;
```

Owning the database lets gorun create its tables in the `public` schema (Postgres 15 and newer no
longer let any user do that). To try it out, a throwaway Postgres in Docker does the same:

```bash
docker run -d --name gorun-postgres -p 127.0.0.1:5432:5432 \
  -e POSTGRES_USER=gorun -e POSTGRES_PASSWORD=change-me -e POSTGRES_DB=gorun \
  -v gorun-pg:/var/lib/postgresql/data postgres:17
```

### 2. Point gorun at it

Put the URL in a `.env` file, so the password stays out of your shell history and scripts:

```bash
# .env, next to where you run gorun (keep it out of git)
GORUN_DB_PATH=postgres://gorun:change-me@localhost:5432/gorun
```

Characters such as `@`, `:` or `/` in the password must be percent-encoded (`p@ss` is `p%40ss`).
For a server that needs TLS, add libpq's options to the URL, such as `?sslmode=require` or
`?sslmode=verify-full&sslrootcert=/etc/ssl/db-ca.pem`; see
[the libpq connection URI](https://www.postgresql.org/docs/current/libpq-connect.html#LIBPQ-CONNSTRING-URIS).

### 3. Start gorun

```bash
gorun -serve
```

The first start creates the tables. The startup message names the database, with the password
hidden:

```
gorun is serving on http://localhost:8080 (listening on all interfaces)
  Gorunfile  default, kept in the history database (version 1), no schedules
  NATS       embedded server at nats://127.0.0.1:4222, events on gorun.events
  history    postgres://gorun:xxxxx@localhost:5432/gorun
```

If gorun can't connect, the web UI still starts, with `history  off (…)` and the reason. Runs
from the terminal (`gorun Gorunfile -r`) and `gorun -history` use the same `GORUN_DB_PATH`, so
they record to and read from Postgres too.

### In Docker

The web service image takes the same variable. With Postgres alongside it in Docker Compose:

```yaml
services:
  postgres:
    image: postgres:17
    environment:
      POSTGRES_USER: gorun
      POSTGRES_PASSWORD: ${GORUN_DB_PASSWORD}
      POSTGRES_DB: gorun
    volumes: [gorun-pg:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "gorun"]
      interval: 2s
  gorun:
    image: gorun # built with `docker build -t gorun .` in the repository
    ports: ["127.0.0.1:8080:8080"]
    environment:
      GORUN_DB_PATH: postgres://gorun:${GORUN_DB_PASSWORD}@postgres:5432/gorun
    depends_on:
      postgres: { condition: service_healthy }
volumes:
  gorun-pg:
```

`GORUN_DB_PASSWORD` comes from a `.env` file beside `compose.yaml`. With the history in Postgres,
the gorun container keeps no state of its own; `/var/lib/gorun` only holds the log and git include
checkouts.

## MySQL

The same three steps. Create a database and a user that may create tables in it:

```sql
CREATE DATABASE gorun CHARACTER SET utf8mb4;
CREATE USER 'gorun'@'%' IDENTIFIED BY 'change-me';
GRANT ALL PRIVILEGES ON gorun.* TO 'gorun'@'%';
```

Then:

```bash
GORUN_DB_PATH=mysql://gorun:change-me@db.internal:3306/gorun gorun -serve
```

The port defaults to 3306. `?tls=true` (or `skip-verify`, or `preferred`) turns on TLS; any other
query parameter is passed to the server as a session variable. gorun is tested on MySQL 8.4 and
MariaDB 10.11.

## Sharing one history

With Postgres or MySQL, every gorun that sets the same `GORUN_DB_PATH` writes to one history:
laptops, CI jobs and the web UI. The History page and `gorun -history` then show all of their
runs, each with [who started it and where](integrations.md#who-ran-it-and-where).

The Live page is separate: it shows runs as they happen from their events, which travel over
NATS. For runs started elsewhere to appear live, point them at the web UI's NATS server too, with
`GORUN_NATS_CLIENT_URL` (see [integrations](integrations.md)). With only the database shared, they
appear in History once recorded, but not on Live.

Gorunfiles kept by `gorun -serve` are shared the same way: two web services on one database see
the same Gorunfiles and versions. Each runs every stored Gorunfile's schedules, though, so run
one `gorun -serve` per database, or schedules fire twice.

Web UI users are shared too, with the key that signs their sessions, so signing in to one web
service signs you in to every one on that database.

## Migrations

gorun creates and updates its own tables. Each database has numbered migration files,
[`internal/store/migrations/<database>/`](https://github.com/jenmud/gorun/tree/master/internal/store/migrations),
built into the binary. On open, gorun applies the ones the database hasn't had and records each in
its `schema_migrations` table, so an upgraded gorun updates the tables the first time it starts.

- It holds a lock while migrating (an advisory lock on Postgres, `GET_LOCK` on MySQL, a write
  transaction on SQLite), so several gorun processes starting together apply each migration once.
- On SQLite and Postgres a migration is all or nothing. MySQL commits each schema change by
  itself, so a migration that fails part way there needs fixing by hand before gorun starts.
- An SQLite file from a gorun older than migrations is upgraded in place, and keeps its runs.
- Don't create or change gorun's tables yourself: gorun's queries expect exactly its own.

## Moving and backing up

Switching `GORUN_DB_PATH` starts an empty history in the new database: gorun doesn't copy runs
between databases. The old SQLite file is left as it was, so you can go back to it by switching
again.

Back up with the database's own tools: copy the SQLite file while gorun isn't writing (or use
`sqlite3 gorun.db ".backup copy.db"`), `pg_dump gorun` for Postgres, or `mysqldump gorun` for MySQL.
To keep a history small, delete old runs from `gorun -history` (`t` keeps only the newest N).
