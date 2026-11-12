# Recording runs

The gorun library doesn't keep runs by itself. It hands every event of a run to
`Config.Recorder`, a `gorun.Recorder`:

```go
type Recorder interface {
	Record(event any)
}
```

You can use the built-in SQLite recorder in the `history` package, which is what the gorun CLI
and web UI use, or write your own to keep runs in your own database, a log file, a queue or
metrics.

- [The built-in SQLite recorder](#the-built-in-sqlite-recorder)
- [Writing your own recorder](#writing-your-own-recorder)
- [The events](#the-events)
- [Example: a JSON lines recorder](#example-a-json-lines-recorder)

## The built-in SQLite recorder

```go
import "github.com/jenmud/gorun/history"

h, err := history.Open(history.DefaultPath())
if err != nil {
	log.Fatal(err)
}
defer h.Close()

cfg.Recorder = h        // saves every event of the run
cfg.OnEvent = myHandler // still gets every event, for your own display
err = cfg.Run(ctx)
```

`history.DefaultPath()` is the database the CLI and web UI use: `GORUN_DB_PATH`, or
`~/.gorun/gorun.db`. Runs you save there show in `gorun -history` and `gorun -web` too. To keep a
separate history, pass your own path to `Open`, which creates the file and its folder.

Read the runs back with:

| Method | Returns |
|---|---|
| `Runs(ctx)` | every run, newest first, as `[]history.Run` |
| `Run(ctx, id)` | one run with its servers and tasks, as `*history.RunDetail` |
| `Task(ctx, runID, server, name)` | one task with its command and output, as `history.Task` |
| `Delete(ctx, id)` | removes one run and everything saved for it |
| `Keep(ctx, n)` | deletes all but the newest `n` runs |

`Run` and `Task` return an error wrapping `history.ErrNotFound` when nothing was saved under that
id. A run's id is its `GorunUUID`.

**`history.Run`** (a summary): `ID`, `Title`, `Hostname`, `PID`, `StartedAt`, `FinishedAt`,
`Finished` (false while it is still running, or if it was cut off), `Success`, `TotalTasks`,
`Failed`, `Skipped`, `Duration`, `Servers` (how many), and `Schedule` (the schedule that started
the run, or empty).

**`history.RunDetail`**: the summary plus `Servers` (names), `Tasks`, `ServerErrors` (by server:
why it couldn't run at all), `ServerAddress`, `ServerEnvVars` and `ServerVars` (by server, from
its plan), `RunEnvVars` and `RunVars`, and `Includes`: the git includes the run read, with the
commit each one used.

**`history.Task`**: `Server`, `Name`, `State` (a `gorun.TaskState`), `Command`, `Stdout`,
`Stderr`, `ExitCode`, `Duration`, `EnvVars` and `Vars`. A task that was planned but never ran is
`gorun.TaskPending`.

The store is safe for concurrent use, and each save has its own five-second deadline, so the end
of a cancelled run is still recorded.

## Writing your own recorder

Any type with a `Record(event any)` method is a recorder. `gorun.RecorderFunc` turns a function
into one:

```go
cfg.Recorder = gorun.RecorderFunc(func(event any) {
	if ev, ok := event.(gorun.ProcessFinishedEvent); ok {
		metrics.RunFinished(ev.Success, ev.Duration)
	}
})
```

What to expect:

- **Every event, synchronously.** `Run` calls `Record` with each event, right after `OnEvent`,
  and waits for it to return. Keep it quick: slow work in `Record` slows the run. To do heavier
  work, hand the events to a goroutine through a buffered channel, as the gorun TUI does.
- **Concurrently.** Each server runs in its own goroutine, and so does each task when a server
  runs tasks in parallel. `Record` is called from all of them at once, so guard shared state with
  a mutex.
- **One value per event, by value.** Switch on the type. Values of other types can be ignored.
- **The same events as `OnEvent` and NATS.** `json.Marshal(event)` gives exactly the JSON gorun
  publishes on NATS (see [the event table](integrations.md#events-nats)). Errors marshal as their
  message.
- **Identifying things.** `GorunUUID` identifies the run and is on every event. A task in a run is
  identified by `GorunUUID`, `Server` and `Task` together.

### The order of a run's events

```
ProcessStartedEvent                      once, first
PlanEvent                                once per server, all of them before any task starts
ServerErrorEvent                         instead of tasks, when a server can't run at all
TaskEvent  running                       as each phase (check, run, verify) of each attempt starts
CopyProgressEvent                        while a running task copies a file or directory
TaskEvent  done | failed | skipped       once per task, when it finishes
           | unchanged
ProcessFinishedEvent                     once, last, after every server has finished
```

Server errors and task events for different servers interleave. For one task, the running events come before its
copy progress, and both come before its final event. `ProcessFinishedEvent` is always sent, even when tasks fail or the run is cancelled, so a
recorder can rely on it to close a run off.

## The events

Every event has a `Type` (the string below) and the run's `GorunUUID`.

### `ProcessStartedEvent` (`process_started`)

| Field | Meaning |
|---|---|
| `Title` | the Gorunfile's `title` |
| `Servers` | the names of the servers that will run |
| `StartedAt` | when the run started |
| `Hostname`, `PID` | the machine and process running it |
| `EnvVars`, `Vars` | the Gorunfile's top-level `envvars` and `vars` |
| `CheckOnly` | set for a check-only run (`--check`): only checks ran, and nothing changed |
| `Includes` | the [git includes](gorunfile.md#tasks-from-a-git-repository) the Gorunfile read: each one's `Source`, `Ref` and the `Commit` it used |
| `Schedule` | the [schedule](schedules.md) that started the run, or empty when it was started any other way |

### `PlanEvent` (`plan`)

| Field | Meaning |
|---|---|
| `Server`, `Address` | the server, and the address gorun connects to (`local` for a local server) |
| `Tasks` | the task names it will run, in order |
| `TaskMeta` | each task's `Name`, `Description`, `Details` and `Rerun` setting, its `Check`, `CheckCopies` and `Verify`, and `Copies`, one line per file it copies (`src → dst`, as `Task.CopySummaries` gives them), so a view can show them before they run |
| `EnvVars`, `Vars` | the server's envvars and vars, including its groups' and the global ones |

### `TaskEvent` (`task`)

| Field | Meaning |
|---|---|
| `Server`, `Task` | which task, on which server |
| `State` | `TaskRunning`, `TaskDone`, `TaskFailed`, `TaskSkipped`, `TaskUnchanged` or `TaskWouldRun` |
| `Phase` | `PhaseCheck`, `PhaseRun` or `PhaseVerify`: the step a running event starts, or on the final event the step that decided the outcome. Empty for a task that never started |
| `Check`, `Verify` | the [check and verify](gorunfile.md#checks-run-only-when-needed) results, when they ran: each one's `Command`, `ExitCode`, `Stdout`, `Stderr` and `Duration` |
| `Command` | the command as run, with templates rendered; on a running event, the command that phase runs |
| `Stdout`, `Stderr` | the output, on the final event |
| `ExitCode` | the exit code, on the final event; 124 when the task timed out |
| `Err` | why it failed or was skipped |
| `Duration` | how long it took, on the final event |
| `Attempt`, `Attempts` | which attempt this is, out of how many, when the task has retries |
| `EnvVars` | the server's envvars with the task's own on top |
| `Vars` | the task's own vars |

- **Running** is sent as each phase of an attempt starts: the check, then the command, then the
  verify, for a task that has them, and just the command otherwise. A retry's first running
  event has `Attempt` above 1 and carries the previous attempt's `ExitCode`, `Stderr` and `Err`,
  so you can show why it is running again. Once the check has run, the command's and the
  verify's running events carry its result in `Check`, and the verify's running event carries
  the command's `Stdout` and `Stderr`, so a view can show each step as soon as it is done.
- **Done** and **failed** are sent once, with the output, exit code and duration. A task that
  failed its check or verify has `Phase` set to that step and its `ExitCode`.
- **Would run** is sent once per task in a check-only run (`--check`), when its check found
  something to do, or it has no check. Nothing ran but the check: `Check` has its output.
- **Unchanged** is sent once when the task's check exited 0, so its command didn't run. It is a
  success: `ExitCode` is 0, `Stdout` and `Stderr` are empty, and `Check` has the check's output.
- **Skipped** is sent once when a task doesn't run: a task it depends on failed, an earlier task
  on that server failed, or the run was cancelled. `Err` and `Stderr` say which.
- `TaskPending` is never sent. It is what the SQLite store records for a planned task that hasn't
  run.

### `ServerErrorEvent` (`server_error`)

| Field | Meaning |
|---|---|
| `Server` | the server that couldn't run |
| `Err` | why, for example the SSH connection was refused |

A server with this error runs no tasks. It fails the run, and its planned tasks count as failed in
`ProcessFinishedEvent`.

### `CopyProgressEvent` (`copy_progress`)

Sent while a task copies a `src` file or directory to its server, so you can show an upload's
progress.

| Field | Meaning |
|---|---|
| `Server`, `Task` | which task is copying, on which server |
| `File` | where the copy goes on the server, as the task's `dst` (or `dir`) names it |
| `Bytes`, `Total` | bytes sent so far, out of the total; for a directory, the size of every file in it |
| `Percent` | `Bytes` as a percentage of `Total`, from 0 to 100 |
| `BytesPerSecond` | the average upload rate so far |
| `Elapsed` | how long the copy has been running |
| `Done` | true on the last event for this copy, once the server has confirmed every byte |

- One event is sent at 0% as the copy starts, then at most one every quarter of a second, then one
  with `Done` set. A task with two copies sends a series for each, one after the other.
- A copy that fails or is cancelled sends no `Done` event; the task's failed event follows.
- `content` copies and files with `render = true` send none.
- They come often, so the built-in SQLite recorder doesn't save them. A recorder that writes each
  event somewhere slow may want to skip them, or keep only the `Done` ones.

```go
cfg.OnEvent = func(event any) {
	if ev, ok := event.(gorun.CopyProgressEvent); ok && !ev.Done {
		fmt.Printf("%s: %s %.0f%%\n", ev.Server, ev.File, ev.Percent)
	}
}
```

To copy files from your own code, outside a run, `gorun.CopyFilesContext` takes a context and a
`func(gorun.CopyProgress)` that gets the same progress.

### `ProcessFinishedEvent` (`process_finished`)

| Field | Meaning |
|---|---|
| `Success` | true when no task failed and every server could run |
| `Duration`, `FinishedAt` | how long the run took, and when it ended |
| `TotalTasks` | how many tasks were planned, across every server |
| `Failed`, `Skipped` | how many failed (including every task of a server that couldn't run) and were skipped |
| `Unchanged` | how many ended unchanged, because their check found nothing to do |
| `WouldRun` | in a check-only run, how many would run |

### Not sent by `Run`

`RunDoneEvent` is a message the gorun TUI sends itself after `Run` returns. A recorder never
receives it.

### Secrets

`EnvVars` hold values as written in the Gorunfile. A `$VAR` reference is not expanded, so secrets
taken from gorun's environment never reach a recorder.

## Example: a JSON lines recorder

This recorder appends each event to a file as one line of JSON, the same JSON NATS carries. It is
safe for concurrent use, and it writes `process_finished` last, as the run ends.

```go
type jsonLines struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (j *jsonLines) Record(event any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	_ = j.enc.Encode(event)
}

f, err := os.OpenFile("runs.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
if err != nil {
	log.Fatal(err)
}
defer f.Close()
cfg.Recorder = &jsonLines{enc: json.NewEncoder(f)}
```

To keep both, record to the built-in store and pass events on:

```go
cfg.Recorder = gorun.RecorderFunc(func(event any) {
	h.Record(event)
	lines.Record(event)
})
```
