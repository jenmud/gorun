# Recipe 2: dependency DAG

A diamond-shaped pipeline: `prepare` fans out into three branches that sleep for
one second each and run side by side, then `merge` waits for all three and
`verify` checks the result.

## Prerequisites

Start the lab first: see .

## Run it

```sh
gorun recipes/02-dependency-dag/Gorunfile -r
```

## What to look for

- `branch-a`, `branch-b` and `branch-c` all show `running` at the same time.
- The whole run takes about two seconds (one for the branches, one for the rest),
  not four.
- If you change one branch to fail, only `merge`, `verify` and `cleanup` are
  skipped; the other branches still run.
EOF