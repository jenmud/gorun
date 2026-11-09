# Recipes

A set of example Gorunfiles that push gorun to its limits. Each one runs against
the demo docker lab in `recipes/docker-compose.yml`.

## Order

1. `recipes/Gorunfile` - start the docker-compose lab (a hundred SSH servers + NATS) locally with `gorun recipes/Gorunfile -r`.
2. `01-parallel-basics` - ten independent tasks that finish together.
3. `02-dependency-dag` - a diamond DAG: fan out, run in parallel, fan back in.
4. `03-mega-pipeline` - ~20 tasks across six tiers, bundles and fan-in.
5. `04-dev-server` - ~230 tasks across fetch/install/configure/smoke tiers, bundles and fan-in.
6. `large-inventory` - split a large Gorunfile across multiple files with `include`; demonstrates organizing tasks, groups and hosts.
7. `local-run` - Go library example: run tasks on the local machine with `RunLocal`.
8. `05-scheduled-jobs` - schedules on this machine: cron, a one-off time, missed runs, and the Schedules page in `gorun -web`.
9. `06-task-library` - a Gorunfile with no tasks of its own: it includes a shared task library from git, pinned to a commit.

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
gorun recipes/04-dev-server/Gorunfile -r
gorun recipes/large-inventory/Gorunfile -r
gorun recipes/05-scheduled-jobs/Gorunfile -r        # a plain run; schedules need -web
gorun recipes/06-task-library/Gorunfile -r          # tasks fetched from git (needs network once)
```

## Tear down

```sh
gorun recipes/Gorunfile -r -task down
```
EOF