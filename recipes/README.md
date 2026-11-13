# Recipes

A set of example Gorunfiles that push gorun to its limits. Each one runs against
the demo docker lab in `recipes/docker-compose.yml`.

Most of them use **checks**: a task's `check` runs first and skips the task when
there is nothing to do, and its `verify` runs after the command and fails the
task when the change didn't take. Run a recipe twice to see it: the second run
marks the finished steps `=` *unchanged*. Add `--check` to any of the commands
below for a dry run, or `--force` to run every task anyway. See
[checks](../docs/gorunfile.md#checks-run-only-when-needed).

## Order

1. `recipes/Gorunfile` - start the docker-compose lab (a hundred SSH servers + NATS) locally with `gorun recipes/Gorunfile -r`. Its checks make it a no-op when the lab is already up.
2. `01-parallel-basics` - ten independent read-only tasks that finish together. No checks: they always run.
3. `02-dependency-dag` - a diamond DAG: fan out, run in parallel, fan back in, with a `verify` on each step.
4. `03-mega-pipeline` - ~20 tasks across six tiers, bundles and fan-in. A rerun only runs the tests; `make`-style checks rebuild only what is out of date.
5. `04-dev-server` - ~230 tasks across fetch/install/configure/smoke tiers, bundles and fan-in. A rerun leaves about 175 of them unchanged.
6. `large-inventory` - split a large Gorunfile across multiple files with `include`; demonstrates organizing tasks, groups and hosts, with Ansible-style ok/changed checks.
7. `local-run` - Go library example: run tasks on the local machine with `RunLocal`, and read each task's state, check and verify from its events.
8. `05-scheduled-jobs` - schedules on this machine: cron, a one-off time, missed runs, and the Schedules page in `gorun -serve`. A check keeps the backup to once a day.
9. `06-task-library` - a Gorunfile with no tasks of its own: it includes a shared task library from git, pinned to a commit. Library tasks can have checks too.
10. `07-big-upload` - upload a 1 GB release and a folder of 200 files to three servers, with a progress bar per upload. `check_copies` makes a rerun send nothing that is already there. Run `07-big-upload/make-files.sh` first.

## One-time: allow docker access

Set your sudo password in the environment (never in the Gorunfile):

```sh
export SUDO_PASSWORD='your sudo password'
gorun recipes/Gorunfile -r -task setup-user
```

Log out and back in afterwards. If you prefer, run `sudo usermod -aG docker $USER` by hand instead.

## Run everything

From the repository root:

```sh
gorun recipes/Gorunfile -r            # up
gorun recipes/Gorunfile -r -task down  # down
gorun recipes/01-parallel-basics/Gorunfile -r
gorun recipes/02-dependency-dag/Gorunfile -r
gorun recipes/03-mega-pipeline/Gorunfile -r
gorun recipes/03-mega-pipeline/Gorunfile -r --check  # dry run: what would change
gorun recipes/04-dev-server/Gorunfile -r
gorun recipes/large-inventory/Gorunfile -r
gorun recipes/05-scheduled-jobs/Gorunfile -r        # a plain run; schedules need -serve
gorun recipes/06-task-library/Gorunfile -r          # tasks fetched from git (needs network once)
recipes/07-big-upload/make-files.sh && gorun recipes/07-big-upload/Gorunfile -r  # upload progress
```

## Tear down

```sh
gorun recipes/Gorunfile -r -task down
```
EOF