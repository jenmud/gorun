# Recipe 3: mega pipeline

A larger DAG with about twenty tasks spread over six tiers: a bootstrap step fans
out into three installs, which feed four configs (one of them needs two installs),
which feed four tests plus an integration step, which feed packaging, deploy and
docs, and finally a single `verify-all` gate.

Every step that builds something has a `check`, and most have a `verify`, so the
pipeline only does what is missing, and a step that didn't produce its file fails
on the spot.

## Prerequisites

Start the lab first: see [the recipes README](../README.md#order).

## Run it

```sh
gorun recipes/03-mega-pipeline/Gorunfile -r            # first run: builds everything
gorun recipes/03-mega-pipeline/Gorunfile -r            # again: only the tests run
gorun recipes/03-mega-pipeline/Gorunfile -r --check    # dry run: what would change
gorun recipes/03-mega-pipeline/Gorunfile -r --force    # rebuild everything anyway
gorun recipes/03-mega-pipeline/Gorunfile -r --task cleanup   # start again from nothing
```

## What to look for

- With `max_task_parallel = -1`, every tier's independent tasks run together.
- The three one-second installs overlap, as do the tests and the integration step.
- The first run takes roughly four seconds instead of the ~ten a sequential run
  would need.
- On the second run the installs, configs, `package`, `deploy` and `docs` show
  `=` *unchanged*: their checks found nothing to do. The tests have no check, so
  they run every time.
- `config-ab` and `package` work like `make`: their checks look for an input
  newer than the output. Touch `/tmp/recipe3/bin/a` on a server and run again:
  only `config-ab`, `package` and `deploy` run there.
- `--check` lists each task as *would run* or *unchanged* and changes nothing.
- Use `--task @tests` to run only the test bundle plus its dependencies.
