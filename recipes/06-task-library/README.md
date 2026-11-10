# Recipe 6: a shared task library

Write tasks once, keep them in a git repository, and include them in any Gorunfile. This recipe's
[Gorunfile](Gorunfile) defines no tasks at all: it includes the bundle `@lib-checks` from
[`library/`](library/checks.toml), fetched from this repository on GitHub at a pinned commit.
Everything runs on this machine.

```sh
gorun recipes/06-task-library/Gorunfile -r
```

The first run fetches the library into gorun's cache (`~/.cache/gorun`, or `GORUN_CACHE_DIR`) and
writes [`Gorunfile.lock`](Gorunfile.lock) with the commit it used. Later runs use the cache and
the lock file, so they work offline.

## What to look at

- **The include:** `git::https://github.com/jenmud/gorun.git//recipes/06-task-library/library/*.toml?ref=<commit>`.
  `git::` marks a git repository, `//` separates the repository from the files in it, and `?ref=`
  pins the version. A tag such as `?ref=v1.2.0` or a branch works too; the lock file then keeps
  every run on the same commit until you run `gorun recipes/06-task-library/Gorunfile -update-includes`.
- **The library** has tasks and a bundle, nothing else. A file from a git include can't add
  servers, groups, schedules or global variables.
- **Paths in the library** are relative to the root of its repository. `lib-hello` runs
  `recipes/06-task-library/library/scripts/hello.sh` because this library lives inside the gorun
  repository; in a repository of its own it would be `scripts/hello.sh`.
- **Checks travel with the task.** `lib-motd` has a `check` and a `verify` like any task: the
  first run writes `/tmp/gorun-library/motd`, and later runs show it *unchanged*.
- **History:** run it with `gorun -web recipes/06-task-library/Gorunfile` and press Run. The run's
  page in History lists the library under *Task libraries*, with the commit it used.

## Make your own

1. Create a repository with your tasks and bundles, for example `web/base.toml` and `scripts/`.
2. Tag a release: `git tag v0.1.0 && git push --tags`.
3. In any Gorunfile: `include = ["git::https://github.com/you/task-library.git//web/*.toml?ref=v0.1.0"]`,
   or `git::git@github.com:you/task-library.git//...` over SSH. Private repositories use your git
   credentials.
4. Commit the `Gorunfile.lock` the first run writes.
