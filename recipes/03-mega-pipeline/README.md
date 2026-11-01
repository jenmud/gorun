# Recipe 3: mega pipeline

A larger DAG with about twenty tasks spread over six tiers: a bootstrap step fans
out into three installs, which feed four configs (one of them needs two installs),
which feed four tests plus an integration step, which feed packaging, deploy and
docs, and finally a single `verify-all` gate.

## Prerequisites

Start the lab first: see .

## Run it

```sh
gorun recipes/03-mega-pipeline/Gorunfile -r
```

## What to look for

- With `max_task_parallel = -1`, every tier's independent tasks run together.
- The three one-second installs overlap, as do the tests and the integration step.
- The whole run takes roughly four seconds instead of the ~ten a sequential run
  would need.
- Re-running is safe: every step recreates `/tmp/recipe3` from scratch.
- Use `--task @tests` to run only the test bundle plus its dependencies.
EOF