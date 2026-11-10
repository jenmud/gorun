# Recipe 2: dependency DAG

A diamond-shaped pipeline: `prepare` fans out into three branches that sleep for
one second each and run side by side, then `merge` waits for all three. Each
step has a `verify`, so a step that didn't do its job fails where it happened:
`merge` checks that all three branches made it into the merged file.

## Prerequisites

Start the lab first: see [the recipes README](../README.md#order).

## Run it

```sh
gorun recipes/02-dependency-dag/Gorunfile -r
```

## What to look for

- `branch-a`, `branch-b` and `branch-c` all show `running` at the same time.
- The whole run takes about two seconds (one for the branches, one for the rest),
  not four.
- If you change one branch to fail, only `merge` and `cleanup` are skipped; the
  other branches still run.
- Change `branch-b` to write the wrong word: its command still exits 0, but its
  `verify` fails the task, with the reason in stderr. Without the verify, the
  run would have passed.
- Press **v** in the live view, or open the run in `gorun -history`: each task
  shows its verify command, exit code and output in its own drop-down.
EOF