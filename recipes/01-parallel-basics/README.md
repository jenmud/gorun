# Recipe 1: parallel basics

Ten independent tasks run on every server. Because `max_task_parallel = -1`, gorun
starts all of them at the same time instead of one after another, so the whole
batch finishes in roughly the time of the slowest task.

## Prerequisites

Start the lab first: see .

## Run it

```sh
gorun recipes/01-parallel-basics/Gorunfile -r
```

## What to look for

- Every task shows `running` at once in the TUI.
- The total wall time is close to the slowest single command, not the sum.
- Re-running is safe: every command is read-only.
EOF