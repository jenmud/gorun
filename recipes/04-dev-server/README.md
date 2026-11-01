# Recipe 4: massive dev server

A massive DAG with over two hundred tasks: every tool is fetched, installed,
configured and smoke-tested in four parallel tiers, before integration,
packaging, checksums, a final `verify-all` gate and cleanup.

## Prerequisites

Start the lab first: see ../README.md.

## Run it

```sh
gorun recipes/04-dev-server/Gorunfile -r
```

## What to look for

- With `max_task_parallel = -1`, all independent tasks in a tier run together.
- The fetch, install, configure and smoke tiers each fan out across every tool.
- Re-running is safe: every step recreates `/tmp/devserver` from scratch.
- Use `--task @smoke-all` to run only the smoke tests plus their dependencies.
EOF