package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	_ "github.com/joho/godotenv/autoload"
	"go.opentelemetry.io/otel"
	"golang.org/x/term"

	"github.com/jenmud/gorun"
	web "github.com/jenmud/gorun/cmd/websrv"
	"github.com/jenmud/gorun/history"
)

var version = "0.1.0"

// emptyGorunfile is the starting Gorunfile printed by -dump.
//
//go:embed Gorunfile.empty
var emptyGorunfile string

// restoreStdout is replaced by init when the instrumented build needs to keep
// its startup logs out of the terminal. It is a no-op otherwise.
var restoreStdout = func() {}

func init() {
	if instrumentedBuild() {
		restoreStdout = redirectStdoutToLog()
	}

	// The OTELC build enables an OTLP exporter for every signal by default,
	// pointed at localhost:4318. Most gorun runs have no collector there, which
	// used to fill the TUI with "traces export: connection refused" errors.
	// This init runs before the generated SDK initializer in the same package,
	// so the environment below takes effect.
	configureOTELDefaults()

	// Keep -dump/-help/-version output clean for the OTELC-instrumented build
	// even when the binary is invoked directly (no wrapper script).
	for _, a := range os.Args {
		switch a {
		case "-dump", "--dump", "-dump-tasks", "--dump-tasks", "-help", "--help", "-h", "-version", "--version":
			_ = os.Setenv("OTEL_LOG_LEVEL", "error")
			_ = os.Setenv("OTEL_SDK_DISABLED", "true")
		}
	}
}

// instrumentedBuild reports whether this binary was built with the OTELC tool.
func instrumentedBuild() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, dep := range info.Deps {
		if dep.Path == "go.opentelemetry.io/otelc" || strings.HasPrefix(dep.Path, "go.opentelemetry.io/otelc/") {
			return true
		}
	}
	return false
}

// configureOTELDefaults disables a signal's exporter unless the user asked for
// one, so the instrumented build stays quiet when no collector is configured.
func configureOTELDefaults() {
	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") {
		return
	}

	for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
		if otelSignalConfigured(os.Getenv, signal) {
			continue
		}
		_ = os.Setenv("OTEL_"+signal+"_EXPORTER", "none")
	}
}

// otelSignalConfigured reports whether the environment configures an exporter
// for one signal (traces, metrics or logs), either through its own variables or
// the shared OTLP endpoint/protocol.
func otelSignalConfigured(getenv func(string) string, signal string) bool {
	for _, key := range []string{
		"OTEL_" + signal + "_EXPORTER",
		"OTEL_EXPORTER_OTLP_" + signal + "_ENDPOINT",
		"OTEL_EXPORTER_OTLP_" + signal + "_PROTOCOL",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
	} {
		if getenv(key) != "" {
			return true
		}
	}
	return false
}

// stringSlice is a flag that can be given more than once.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }

// Set adds one more value.
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

// options are the command line flags. printUsage documents each one.
type options struct {
	verbose, plain, run, dump, version, serve, history, updateIncludes, force, checkOnly bool
	listTasks, listServers, listTags, listGroups, listBundles                            bool
	tasks, bundles, servers, exclude, tags, groups                                       stringSlice
	parallel                                                                             int
	parallelSet                                                                          bool // -parallel was given
	dumpTasksDir, complete, filename                                                     string
}

// lists reports whether a flag that only prints and exits was given.
func (o *options) lists() bool {
	return o.listTasks || o.listServers || o.listTags || o.listGroups || o.listBundles || o.dumpTasksDir != ""
}

// parseFlags reads the command line. Flags may come before or after the Gorunfile.
func parseFlags() *options {
	flag.CommandLine.Usage = printUsage
	return parseArgs(flag.CommandLine, os.Args[1:])
}

// parseArgs defines gorun's flags on fs and parses args with it.
func parseArgs(fs *flag.FlagSet, args []string) *options {
	o := &options{}
	for name, dst := range map[string]*bool{
		"v": &o.verbose, "plain": &o.plain, "run": &o.run, "r": &o.run, "dump": &o.dump, "version": &o.version,
		"serve": &o.serve, "web": &o.serve, "history": &o.history,
		"update-includes": &o.updateIncludes, "force": &o.force, "check": &o.checkOnly,
		"list-tasks": &o.listTasks, "list-servers": &o.listServers, "list-tags": &o.listTags,
		"list-groups": &o.listGroups, "list-server-groups": &o.listGroups, "list-bundles": &o.listBundles,
	} {
		fs.BoolVar(dst, name, false, "")
	}
	for name, dst := range map[string]*stringSlice{
		"task": &o.tasks, "bundles": &o.bundles, "server": &o.servers,
		"exclude-server": &o.exclude, "tag": &o.tags, "group": &o.groups,
	} {
		fs.Var(dst, name, "")
	}
	fs.IntVar(&o.parallel, "parallel", 0, "")
	fs.StringVar(&o.dumpTasksDir, "dump-tasks", "", "")
	fs.StringVar(&o.complete, "complete", "", "")
	_ = fs.Parse(args)

	o.filename = fs.Arg(0)
	// the flag package stops at the first positional argument, so parse again
	// to allow flags after the filename, e.g. `gorun Gorunfile --run -v`
	if fs.NArg() > 1 && !o.version && o.complete == "" && !o.dump {
		_ = fs.Parse(fs.Args()[1:])
	}
	fs.Visit(func(f *flag.Flag) { o.parallelSet = o.parallelSet || f.Name == "parallel" })
	return o
}

func main() {
	// Startup logs from the instrumented build have been sent to the log file;
	// take the terminal's stdout back before parsing flags or drawing the TUI.
	restoreStdout()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	o := parseFlags()
	switch {
	case o.version:
		fmt.Println(version)
		return
	case o.complete != "":
		os.Setenv("GORUN_SKIP_NATS", "1")
		names, err := completionNames(o.complete, o.filename)
		check(err)
		for _, name := range names {
			fmt.Println(name)
		}
		return
	case o.dump:
		fmt.Print(emptyGorunfile)
		return
	}

	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	useTUI := !o.plain && stdoutTTY
	// the server picker needs to read keys as well as draw
	interactive := stdoutTTY && term.IsTerminal(int(os.Stdin.Fd()))

	// Set up logging before anything interactive starts, so the TUI, the
	// history view and the web server all keep library logs (including OTEL
	// export errors) out of the terminal.
	logPath, logFile := setupLogging(useTUI)
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		log.Printf("opentelemetry: %v", err)
	}))

	switch {
	case o.history:
		check(runHistoryTUI())
		return
	case o.serve:
		web.Version = version
		web.Run(o.filename, logPath)
		return
	case o.filename == "":
		printUsage()
		os.Exit(1)
	}

	slog.Info("parsing gorun file", slog.String("filename", o.filename))
	if o.lists() {
		os.Setenv("GORUN_SKIP_NATS", "1")
	}
	gorun.StartNATS()

	inFile := func(err error) { // check, naming the Gorunfile
		if err != nil {
			fatal(fmt.Errorf("%s: %w", o.filename, err))
		}
	}
	cfg := &gorun.Config{UpdateIncludes: o.updateIncludes, Origin: gorun.Origin{Version: version, Trigger: "cli"}}
	inFile(cfg.LoadFile(o.filename))
	if o.updateIncludes {
		if len(cfg.Includes) == 0 {
			fmt.Println("no git includes")
		}
		for _, inc := range cfg.Includes {
			fmt.Printf("%s @ %s -> %s\n", inc.Source, inc.Ref, inc.Commit)
		}
		return
	}
	// relative paths in the config are relative to the config file, not the working directory
	cfg.ResolvePaths(filepath.Dir(o.filename))

	selectedTasks, filtered := applySelection(cfg, o)

	if showList(cfg, o, interactive, selectedTasks, filtered) {
		return
	}

	// check every server's task list up front, so config mistakes such as an unknown task
	// are reported before the TUI starts rather than lost inside it
	servers, err := cfg.ServersPipeline()
	inFile(err)
	// a server with no tasks of its kind has nothing to run, so it isn't listed or offered
	for name, server := range servers {
		if len(server.Tasks) == 0 {
			delete(servers, name)
		}
	}

	switch {
	case o.listServers:
		list(cfg, o, interactive, "servers", serverTreeNodes(servers), func() { printServers(os.Stdout, servers) })
		return
	case o.dumpTasksDir != "":
		check(dumpTasks(cfg, o.dumpTasksDir, servers, selectedTasks))
		return
	}

	// Runs start with the server picker unless --run says to go straight ahead, so
	// nothing is deployed without an explicit choice.
	if !o.run {
		if !interactive {
			fatal(errors.New("the server picker needs an interactive terminal; pass --run (-r) to run without it"))
		}
		// the picker lists each server's resolved pipeline, so tasks can be left out
		choices := make([]gorun.Server, 0, len(cfg.Servers))
		for _, s := range cfg.Servers {
			if pipeline, ok := servers[s.Name]; ok {
				choices = append(choices, pipeline)
			}
		}
		selected, err := runServerSelectionTUI(cfg, choices)
		if errors.Is(err, errTaskCommandEditorClosed) {
			return
		}
		check(err)
		if len(selected) == 0 {
			fmt.Println("no servers selected")
			return
		}
		cfg.Servers = selected
	}

	if useTUI {
		cfg.Origin.Trigger = "tui"
		if err := runTUI(ctx, cancel, cfg, o.verbose); err != nil {
			os.Exit(1)
		}
		return
	}

	// plain output: the plan, then a line per task as it finishes
	slog.Info("parsed config file")
	servers, err = cfg.ServersPipeline()
	inFile(err)
	for _, server := range servers {
		if len(server.Tasks) == 0 {
			continue
		}
		names := make([]string, len(server.Tasks))
		for i, task := range server.Tasks {
			names[i] = task.Name
		}
		fmt.Printf("%s: %s\n", server.Name, strings.Join(names, " -> "))
	}

	printer := &plainPrinter{w: os.Stdout, verbose: o.verbose}
	cfg.OnEvent = printer.event
	if h, err := history.Open(getHistoryDBPath()); err == nil {
		defer func() { _ = h.Close() }()
		cfg.Recorder = h
	}
	stopAbortWatch := printer.watchAbort(ctx, notifyInterrupts, func() { os.Exit(130) })
	err = cfg.Run(ctx)
	stopAbortWatch()
	if err != nil {
		slog.Error("error executing tasks", slog.String("reason", err.Error()))
		if logPath != "" {
			err = fmt.Errorf("%w (logs: %s)", err, logPath)
		}
		fatal(err)
	}
}

// notifyInterrupts returns a channel of further interrupts, for a second ctrl+c to force quit,
// and the func that stops delivering them.
func notifyInterrupts() (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	return ch, func() { signal.Stop(ch) }
}

// applySelection applies -parallel, -task or -bundles, and the server filters to cfg. It
// returns the tasks selected, if any, and whether the servers were filtered.
func applySelection(cfg *gorun.Config, o *options) (selectedTasks []string, filtered bool) {
	if o.parallel < 0 {
		fatal(errors.New("parallel must be 0 or greater"))
	}
	if o.parallelSet {
		cfg.MaxParallel = o.parallel
	}
	if o.force && o.checkOnly {
		fatal(errors.New("--force and --check cannot be used together"))
	}
	cfg.Force, cfg.CheckOnly = o.force, o.checkOnly

	var err error
	switch {
	case len(o.tasks) > 0 && len(o.bundles) > 0:
		fatal(errors.New("--task and --bundles cannot be used together"))
	case len(o.bundles) > 0:
		selectedTasks, err = selectBundles(cfg, o.bundles...)
		check(err)
	case len(o.tasks) > 0:
		// A named task is run on every selected server even when it is not part
		// of that server's configured pipeline, so one-off tasks work on any host.
		selectedTasks, err = cfg.ExpandTaskPatterns(o.tasks...)
		check(err)
		for i := range cfg.Servers {
			cfg.Servers[i].TaskNames = slices.Clone(selectedTasks)
		}
		cfg.SelectedTasks = selectedTasks
	}

	filter := gorun.ServerFilter{Names: o.servers, Exclude: o.exclude, Tags: o.tags, Groups: o.groups}
	cfg.Servers, err = cfg.SelectServers(filter)
	check(err)
	return selectedTasks, !filter.IsZero()
}

// list shows one of the -list-* trees: a collapsible tree in a terminal, or print's plain
// output with -plain, when not interactive, or when there is nothing to browse.
func list(cfg *gorun.Config, o *options, interactive bool, kind string, nodes []*treeNode, print func()) {
	if !interactive || o.plain || len(nodes) == 0 {
		print()
		return
	}
	check(runTreeTUI(cfg.Title, kind, nodes))
}

// showList handles -list-bundles, -list-tags, -list-groups and -list-tasks, and reports
// whether one of them was given.
func showList(cfg *gorun.Config, o *options, interactive bool, selectedTasks []string, filtered bool) bool {
	switch {
	case o.listBundles:
		list(cfg, o, interactive, "bundles", bundleTreeNodes(cfg), func() { printBundles(os.Stdout, cfg) })
	case o.listTags:
		list(cfg, o, interactive, "tags", tagTreeNodes(cfg, cfg.Servers, filtered), func() { printTags(os.Stdout, cfg, cfg.Servers, filtered) })
	case o.listGroups:
		list(cfg, o, interactive, "groups", groupTreeNodes(cfg, cfg.Servers, filtered), func() { printGroups(os.Stdout, cfg, cfg.Servers, filtered) })
	case o.listTasks:
		// with a selection, list only the tasks it would run
		var shown map[string]bool
		if len(selectedTasks) > 0 || filtered {
			shown = map[string]bool{}
			servers, err := cfg.ServersPipeline()
			if err != nil {
				fatal(fmt.Errorf("%s: %w", o.filename, err))
			}
			for _, server := range servers {
				for _, t := range server.Tasks {
					shown[t.Name] = true
				}
			}
		}
		list(cfg, o, interactive, "tasks", taskTreeNodes(cfg, shown), func() { printTasks(os.Stdout, cfg, shown) })
	default:
		return false
	}
	return true
}

// setupLogging sends logs to the log file so they don't clutter the terminal or
// corrupt a TUI. It returns the log path (empty when the file couldn't be
// opened) and the open file, if any.
func setupLogging(useTUI bool) (string, *os.File) {
	logPath := gorun.LogFilePath()
	logFile, err := gorun.OpenLogFile(logPath)
	if err == nil {
		log.SetOutput(logFile)
		gorun.SetLogOutput(logFile)
		return logPath, logFile
	}

	if useTUI {
		log.SetOutput(io.Discard)
		gorun.SetLogOutput(io.Discard)
	} else {
		fmt.Fprintf(os.Stderr, "gorun: can't open log file, logging to stderr: %v\n", err)
		gorun.SetLogOutput(os.Stderr)
	}

	return "", nil
}

// fatal prints err and exits.
func fatal(err error) {
	fmt.Fprintf(os.Stderr, "gorun: %v\n", err)
	os.Exit(1)
}

// check exits with err, if there is one.
func check(err error) {
	if err != nil {
		fatal(err)
	}
}

// printUsage prints the help text to stderr.
func printUsage() { writeUsage(os.Stderr) }

// writeUsage writes the help text: every flag and environment variable.
func writeUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: gorun [options] <Gorunfile>

Run the tasks defined in a Gorunfile across one or more servers. By default gorun first
opens a picker to choose the servers (and tasks) to run; pass -run to skip it.

Options:
  -v        verbose: show every task and its output
  -plain    plain output instead of the TUI, and print the -list-* trees in full
            (default when stdout is not a terminal)
  -run, -r  run without the interactive server picker (needed when there is no terminal)
  -task     run only the named task on any selected server (repeatable; dependencies included);
            accepts globs ('deploy-*') and bundles ('@base')
  -bundles  run only the named bundle(s), replacing each server's task list (repeatable; globs allowed)
  -server   run only the named server (repeatable; accepts globs like 'web-*')
  -exclude-server  skip the named server (repeatable; accepts globs)
  -tag      run only servers with this tag (repeatable; servers need every tag)
  -group    run only servers in this server group (repeatable; servers need every group)
  -parallel N  run at most N hosts at once (0 = all servers)
  -force    run every task even when its check says there is nothing to do
  -check    only run the checks, as a dry run: say which tasks would run, change nothing
  -list-tasks  list available tasks and exit (a collapsible tree in a terminal)
  -list-servers  list servers and their task pipelines and exit (a collapsible tree in a terminal)
  -list-tags  list tags and the servers that have them and exit (a collapsible tree in a terminal)
  -list-groups  list server groups and the servers that have them and exit (a collapsible tree in a terminal)
  -list-bundles  list task bundles and what they expand to and exit (a collapsible tree in a terminal)
  -dump     print a starting Gorunfile that works as-is and documents every option
  -dump-tasks DIR  dump each task as a file in DIR and exit
  -update-includes  look up every git include's ref again, update the lock file and exit
  -version  print version and exit
  -serve [Gorunfile]  start the web service (scheduler, NATS, web UI); -web is an alias
  -history  view historical runs

Environment variables (can be overridden via the process environment or a .env file
in the current working directory; .env values never override variables already set
in the environment):

  GORUN_NAME                          default: gorun
  GORUN_ADDRESS                       default: :8080 (-serve listen address)
  GORUN_DB_PATH                       default: ~/.gorun/gorun.db (falls back to gorun.db)
  GORUN_CACHE_DIR                     default: ~/.cache/gorun (git includes are kept here)
  GORUN_LOG_FILE                      default: ~/.gorun/gorun.log (appended to on every run)
  GORUN_EVENT_TOPIC                   default: gorun.events
  GORUN_SKIP_NATS                     default: unset (set to skip NATS publishing)
  GORUN_LABELS                        default: unset (labels for every run: ticket=CHG-42,team=ops)
  GORUN_NATS_CLIENT_URL               default: nats://127.0.0.1:4222
  GORUN_NATS_CLIENT_NAME              default: GORUN_NAME (or gorun)
  GORUN_NATS_CLIENT_ALLOW_RECONNECT   default: true
  GORUN_NATS_CLIENT_RECONNECT_WAIT    default: nats default (e.g. 2s)
  GORUN_NATS_CLIENT_TIMEOUT           default: nats default (e.g. 5s)
  GORUN_NATS_SERVER_CONFIG            default: none (NATS server config for the server -serve starts)

OpenTelemetry SDK variables (OTEL_*) are also honoured by the instrumented build.
Exporters are disabled unless an OTLP endpoint or exporter is configured.
`)
}
