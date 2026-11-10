# Recipe 4: massive dev server

A massive DAG with over two hundred tasks: every tool is fetched, installed,
configured and smoke-tested in four parallel tiers, before integration,
packaging, checksums and a final `verify-all` gate.

Every fetch, install and configure step has a `check`, so a rerun only does what
is missing, and installs and configures have a `verify`.

## Prerequisites

Start the lab first: see [the recipes README](../README.md#order).

## Run it

```sh
gorun recipes/04-dev-server/Gorunfile -r                 # first run: sets everything up
gorun recipes/04-dev-server/Gorunfile -r                 # again: only the smoke tests run
gorun recipes/04-dev-server/Gorunfile -r --check         # dry run: what would change
gorun recipes/04-dev-server/Gorunfile -r --task cleanup  # start again from nothing
```

## What to look for

- With `max_task_parallel = -1`, all independent tasks in a tier run together.
- The fetch, install, configure and smoke tiers each fan out across every tool.
- On the second run about 175 tasks show `=` *unchanged*: their checks found
  the tool already fetched, installed and configured. The smoke tests have no
  check, so they run every time and still prove each tool works.
- Delete one tool on a server (`rm /tmp/devserver/bin/jq`) and run again: only
  `install-jq` runs there, then the smoke tests.
- `--check` on two hundred tasks shows at a glance how far each server is from
  done, and changes nothing.
- Use `--task @smoke-all` to run only the smoke tests plus their dependencies.
EOF