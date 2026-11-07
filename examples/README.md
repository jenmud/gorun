# Examples: gorun as a Go library

Two small programs built on the `gorun` package. Each follows a run through gorun's progress
events and shows it its own way. Recordings of both are on the website's
[Library page](https://go-run.dev/library).

| Example | What it shows |
|---|---|
| [`servicedeck`](servicedeck) | A web app that manages services across a fleet. Pick a tier (web, api, worker), press an action (health check, restart nginx, deploy, rotate logs), and every server's progress streams to the browser live over server-sent events, using [Datastar](https://data-star.dev). |
| [`rollout`](rollout) | A command-line release tool that draws its own progress bars, one per server, from the events: the running task, retries, failures and a summary. |

## Running them

From a gorun checkout:

```bash
cd examples/servicedeck && go run .     # then open http://localhost:8090
cd examples/rollout && go run .         # every server
cd examples/rollout && go run . -tag edge
```

Both fleets are local servers whose tasks only `echo` and `sleep`, so they run anywhere and touch
nothing. To manage real servers, give each host an `[servers.hosts.<name>.ssh]` table, remove
`local = true` from the tasks, and swap in the real commands (they're in each Gorunfile's
comments).

Each example is its own Go module, with `replace github.com/jenmud/gorun => ../..` so it always
builds against the gorun code beside it. `make examples` in the repository root vets and builds
both.

## Using them in your own project

Copy the example, then point its `go.mod` at a released gorun instead of the checkout:

```bash
go mod edit -dropreplace github.com/jenmud/gorun
go get github.com/jenmud/gorun@latest
```

## The pattern

Load a Gorunfile, set `OnEvent`, run:

```go
var cfg gorun.Config
if err := cfg.LoadFile("Gorunfile"); err != nil {
	log.Fatal(err)
}

// called for every progress event, from one goroutine per server: lock your state
cfg.OnEvent = func(event any) {
	switch ev := event.(type) {
	case gorun.PlanEvent: // a server's tasks, in the order they will run
		fmt.Println(ev.Server, "will run", ev.Tasks)
	case gorun.TaskEvent: // a task changed state
		if ev.State == gorun.TaskDone || ev.State == gorun.TaskFailed {
			fmt.Println(ev.Server, ev.Task, "exit", ev.ExitCode, "in", ev.Duration)
		}
	case gorun.ProcessFinishedEvent: // the run is over
		fmt.Println(ev.TotalTasks, "tasks,", ev.Failed, "failed")
	}
}

if err := cfg.Run(context.Background()); err != nil {
	log.Fatal(err)
}
```

| Event | Sent | Useful fields |
|---|---|---|
| `ProcessStartedEvent` | once, when the run starts | `Title`, `Servers`, `StartedAt` |
| `PlanEvent` | once per server, before its first task | `Server`, `Address`, `Tasks`, `TaskMeta` |
| `TaskEvent` | every time a task changes state | `Server`, `Task`, `State`, `Stdout`, `Stderr`, `ExitCode`, `Duration`, `Attempt`, `Attempts` |
| `ServerErrorEvent` | when a server can't run at all (e.g. SSH refused) | `Server`, `Err` |
| `ProcessFinishedEvent` | once, when every server is done | `Success`, `Duration`, `TotalTasks`, `Failed`, `Skipped` |

A `TaskEvent`'s `State` is `TaskRunning`, `TaskDone`, `TaskFailed` or `TaskSkipped`; a running
event with `Attempt` above 1 is a retry.

Things worth knowing:

- **Lock your state.** Servers run in parallel, so `OnEvent` is called concurrently.
- **Choose what runs** with `cfg.SelectServers(gorun.ServerFilter{Tags: ...})` (like `--tag`),
  then `cfg.Run(ctx, servers...)`, or run one task and its dependencies with
  `cfg.RunTask(ctx, cfg.Tasks["name"], servers...)`.
- **Logs go to stdout** by default. A program that draws on the terminal should send them
  elsewhere: `f, _ := gorun.OpenLogFile(gorun.LogFilePath()); gorun.SetLogOutput(f)`.
- **One run per `Config`.** `Run` keeps the run ID it generated; load a fresh `Config` for each
  run, as `servicedeck` does, or clear `cfg.GorunUUID` first.

The `// docs:begin` and `// docs:end` comments mark the code the website shows; keep them when
editing.
