// Package gorun runs plain shell commands, defined in a Gorunfile, on one or more servers
// over SSH or on the local machine, in parallel.
//
// The gorun command (in ./cmd) is a thin layer over this package: it adds the picker, the live
// view and the history. Use the package directly to run Gorunfiles from your own code.
//
// To learn how to write a Gorunfile, start with the tutorial:
// https://github.com/jenmud/gorun/blob/master/docs/tutorial.md
//
// # Gorunfiles
//
// A Gorunfile is TOML. It has a library of named tasks ([Task]): a shell command, script or
// template, with optional file copies, sudo, env vars, dependencies, a timeout and retries. Servers
// ([Server]) pick tasks from the library by name, directly or through named task lists ([Bundle])
// and shared settings ([ServerGroup]). A server with SSH settings runs its tasks over SSH; one
// without them runs on the local machine. A task runs on one kind of server only: a task with
// local = true ([Task.Local]) runs only on local servers, and every other task runs only over SSH.
// Tasks of the other kind, and tasks that depend on them, are left out of a server's pipeline.
//
//	title = "Provision app servers"
//
//	[tasks.hello]
//	cmd_template = "echo hello from {{.Name}}"
//
//	[servers]
//	tasks = ["hello"]
//
//	[servers.hosts.web-01]
//	hostname = "web-01.example.com"
//	    [servers.hosts.web-01.ssh]
//	    username = "deploy"
//
// Every option is described in
// https://github.com/jenmud/gorun/blob/master/docs/gorunfile.md.
//
// # Loading and running
//
// [Config.LoadFile] reads and validates a Gorunfile and the files it includes, and
// [Config.ResolvePaths] makes its relative paths relative to the file. [Config.Run] then runs
// every server's tasks, or just the servers you pass it:
//
//	var cfg gorun.Config
//	if err := cfg.LoadFile("Gorunfile"); err != nil {
//		return err
//	}
//	cfg.ResolvePaths(filepath.Dir("Gorunfile"))
//	return cfg.Run(ctx)
//
// # Choosing servers and tasks
//
// [Config.SelectServers] picks servers the way the --server, --tag and --group flags do, and
// [Config.ExpandTaskPatterns] expands task names, globs and "@bundle" references the way --task
// does:
//
//	servers, err := cfg.SelectServers(gorun.ServerFilter{Tags: []string{"web"}})
//	if err != nil {
//		return err
//	}
//	tasks, err := cfg.ExpandTaskPatterns("@baseline")
//	if err != nil {
//		return err
//	}
//	for i := range servers {
//		servers[i].TaskNames = tasks
//	}
//	return cfg.Run(ctx, servers...)
//
// [Config.ServersPipeline] returns every server with its task list resolved, as
// gorun --list-servers shows it.
//
// # Running single tasks and running locally
//
// [Config.RunTask] runs one task (with its dependencies) on some or all servers, whether or not
// they normally run it, and returns an error when none of them can run it (see [Task.Local]).
// [Config.RunLocal] runs tasks on this machine without any SSH server; only tasks marked
// local = true run there. [LocalServer] and [Config.LocalServer] give a [Server] that runs locally.
//
// # Schedules
//
// [NewScheduler] fires a config's [Schedule]s as they come due. Nothing runs until [Scheduler.Start],
// so a plain run never fires them. [Scheduler.Pause] holds a schedule's runs, [Scheduler.Resume]
// lifts the pause and applies the schedule's missed setting, and [Scheduler.Skip] cancels only the
// next occurrence. [Scheduler.Status] reports each schedule, and every decision is sent to the
// callback as a [ScheduleEvent].
//
// # Events
//
// Set [Config.OnEvent] to follow a run as it happens. It receives a [ProcessStartedEvent], a
// [PlanEvent] per server, a [TaskEvent] for every change in a task's [TaskState], a
// [ServerErrorEvent] when a server can't run, and a [ProcessFinishedEvent] at the end:
//
//	cfg.OnEvent = func(event any) {
//		if ev, ok := event.(gorun.TaskEvent); ok && ev.State != gorun.TaskRunning {
//			fmt.Printf("%s: %s exit %d\n", ev.Server, ev.Task, ev.ExitCode)
//		}
//	}
package gorun
