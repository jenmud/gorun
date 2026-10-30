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
	"sort"
	"strings"

	_ "github.com/joho/godotenv/autoload"
	"go.opentelemetry.io/otel"
	"golang.org/x/term"

	"github.com/jenmud/gorun"
	web "github.com/jenmud/gorun/cmd/websrv"
	"github.com/jenmud/gorun/internal/store"
)

var version = "dev"

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
		case "-dump", "--dump", "-help", "--help", "-h", "-version", "--version":
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

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	// Startup logs from the instrumented build have been sent to the log file;
	// take the terminal's stdout back before parsing flags or drawing the TUI.
	restoreStdout()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	verbose := flag.Bool("v", false, "verbose: show every task and its output")
	plain := flag.Bool("plain", false, "plain log output instead of the TUI (default when stdout is not a terminal)")
	var runNow bool
	flag.BoolVar(&runNow, "run", false, "run without the interactive server picker (needed when there is no terminal)")
	flag.BoolVar(&runNow, "r", false, "shorthand for -run")
	dump := flag.Bool("dump", false, "print a starting Gorunfile that works as-is and documents every option")
	versionFlag := flag.Bool("version", false, "print version and exit")
	webFlag := flag.Bool("web", false, "start the web server")
	historyFlag := flag.Bool("history", false, "view historical runs")
	taskFlags := stringSlice{}
	flag.Var(&taskFlags, "task", "run only the named task, glob or @bundle on any selected server (repeatable); dependencies are included")
	serverFlags := stringSlice{}
	flag.Var(&serverFlags, "server", "run only the named server or glob (repeatable)")
	excludeFlags := stringSlice{}
	flag.Var(&excludeFlags, "exclude-server", "skip the named server or glob (repeatable)")
	tagFlags := stringSlice{}
	flag.Var(&tagFlags, "tag", "run only servers with this tag (repeatable; servers need every tag)")
	groupFlags := stringSlice{}
	flag.Var(&groupFlags, "group", "run only servers in this server group (repeatable; servers need every group)")
	parallel := flag.Int("parallel", 0, "maximum number of hosts to run in parallel (0 = all)")
	listTasks := flag.Bool("list-tasks", false, "list available tasks and exit")
	listServers := flag.Bool("list-servers", false, "list servers and their task pipelines and exit")
	listTags := flag.Bool("list-tags", false, "list tags and the servers that have them and exit")
	listBundles := flag.Bool("list-bundles", false, "list task bundles and what they expand to and exit")
	complete := flag.String("complete", "", "print shell completion candidates (tasks, servers, tags or groups) and exit")

	flag.Usage = printUsage
	flag.Parse()

	if *versionFlag {
		fmt.Println(version)
		return
	}

	if *complete != "" {
		os.Setenv("GORUN_SKIP_NATS", "1")
		names, err := completionNames(*complete, flag.Arg(0))
		if err != nil {
			fatal(err)
		}
		for _, name := range names {
			fmt.Println(name)
		}
		return
	}

	if *dump {
		os.Setenv("GORUN_SKIP_NATS", "1")
		fmt.Print(emptyGorunfile)
		return
	}

	filename := flag.Arg(0)

	// the flag package stops at the first positional argument, so parse again
	// to allow flags after the filename, e.g. `gorun Gorunfile --run -v`
	if flag.NArg() > 1 {
		_ = flag.CommandLine.Parse(flag.Args()[1:])
	}

	useTUI := !*plain && term.IsTerminal(int(os.Stdout.Fd()))
	// the server picker needs to read keys as well as draw
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))

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

	if *historyFlag {
		if err := runHistoryTUI(); err != nil {
			fatal(err)
		}
		return
	}

	if *webFlag {
		os.Setenv("GORUN_SKIP_NATS", "1")
		web.Run()
		return
	}

	if filename == "" {
		printUsage()
		os.Exit(1)
	}

	slog.Info("parsing gorun file", slog.String("filename", filename))
	if *listTasks || *listServers || *listTags || *listBundles {
		os.Setenv("GORUN_SKIP_NATS", "1")
	}
	gorun.StartNATS()

	cfg := gorun.Config{}
	r, err := os.Open(filename)
	if err != nil {
		fatal(err)
	}

	if err := cfg.Load(r); err != nil {
		fatal(fmt.Errorf("%s: %w", filename, err))
	}

	// relative paths in the config are relative to the config file, not the working directory
	cfg.ResolvePaths(filepath.Dir(filename))

	if *parallel < 0 {
		fatal(errors.New("parallel must be 0 or greater"))
	}
	parallelSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "parallel" {
			parallelSet = true
		}
	})
	if parallelSet {
		cfg.MaxParallel = *parallel
	}

	// validate and apply --task filter. A named task is run on every selected
	// server even when it is not part of that server's configured pipeline, so
	// one-off tasks work on any host.
	var selectedTasks []string
	if len(taskFlags) > 0 {
		selectedTasks, err = cfg.ExpandTaskPatterns(taskFlags...)
		if err != nil {
			fatal(err)
		}
		for i := range cfg.Servers {
			cfg.Servers[i].TaskNames = slices.Clone(selectedTasks)
		}
		cfg.SelectedTasks = selectedTasks
	}

	// validate and apply the server filters
	filter := gorun.ServerFilter{Names: serverFlags, Exclude: excludeFlags, Tags: tagFlags, Groups: groupFlags}
	if cfg.Servers, err = cfg.SelectServers(filter); err != nil {
		fatal(err)
	}

	if *listBundles {
		printBundles(os.Stdout, &cfg)
		return
	}

	if *listTags {
		printTags(os.Stdout, &cfg, cfg.Servers, !filter.IsZero())
		return
	}

	if *listTasks {
		// With a filter, list only the tasks the selection would run.
		var shown map[string]bool
		if len(selectedTasks) > 0 || !filter.IsZero() {
			shown = map[string]bool{}
			servers, err := cfg.ServersPipeline()
			if err != nil {
				fatal(fmt.Errorf("%s: %w", filename, err))
			}
			for _, server := range servers {
				for _, t := range server.Tasks {
					shown[t.Name] = true
				}
			}
		}

		// Build a reverse dependency tree: a task's children are the tasks that
		// depend on it, so dependencies appear above their dependents.
		children := map[string][]string{}
		roots := []string{}
		for name := range cfg.Tasks {
			if shown != nil && !shown[name] {
				continue
			}
			if len(cfg.Tasks[name].DependsOn) == 0 {
				roots = append(roots, name)
			}
			for _, dep := range cfg.Tasks[name].DependsOn {
				children[dep] = append(children[dep], name)
			}
		}
		sort.Strings(roots)
		for _, kids := range children {
			sort.Strings(kids)
		}

		label := func(name string) string {
			task := cfg.Tasks[name]
			s := name
			if task.Description != "" {
				s += " — " + task.Description
			}
			if task.Rerun == gorun.RerunUnsafe {
				s += " (⚠ not safe to rerun)"
			}
			if task.Retries > 0 {
				s += fmt.Sprintf(" (retries: %d)", task.Retries)
			}
			return s
		}

		visited := map[string]bool{}
		var printNode func(name, prefix string, last, root bool)
		printNode = func(name, prefix string, last, root bool) {
			connector := "├── "
			if last {
				connector = "└── "
			}
			if visited[name] {
				if root {
					fmt.Printf("%s (already shown)\n", label(name))
				} else {
					fmt.Printf("%s%s%s (already shown)\n", prefix, connector, label(name))
				}
				return
			}
			if root {
				fmt.Println(label(name))
			} else {
				fmt.Printf("%s%s%s\n", prefix, connector, label(name))
			}
			visited[name] = true
			kids := children[name]
			childPrefix := prefix
			if last {
				childPrefix += "    "
			} else {
				childPrefix += "│   "
			}
			for i, child := range kids {
				printNode(child, childPrefix, i == len(kids)-1, false)
			}
		}

		for i, root := range roots {
			_ = i
			printNode(root, "", true, true)
		}

		return
	}

	// check every server's task list up front, so config mistakes such as an unknown task
	// are reported before the TUI starts rather than lost inside it
	servers, err := cfg.ServersPipeline()
	if err != nil {
		fatal(fmt.Errorf("%s: %w", filename, err))
	}

	if *listServers {
		names := make([]string, 0, len(servers))
		for name := range servers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			server := servers[name]
			fmt.Printf("%s (%s)\n", server.Name, server.Address())
			if len(server.Groups) > 0 {
				fmt.Printf("    groups: %s\n", strings.Join(server.Groups, ", "))
			}
			if len(server.Tags) > 0 {
				fmt.Printf("    tags: %s\n", strings.Join(server.Tags, ", "))
			}
			for i, t := range server.Tasks {
				connector := "├── "
				if i == len(server.Tasks)-1 {
					connector = "└── "
				}
				label := t.Name
				if t.Retries > 0 {
					label += fmt.Sprintf(" (retries: %d)", t.Retries)
				}
				if t.Rerun == gorun.RerunUnsafe {
					label += " ⚠"
				}
				fmt.Printf("    %s%s\n", connector, label)
			}
			fmt.Println()
		}
		return
	}

	// Runs start with the server picker unless --run says to go straight ahead, so
	// nothing is deployed without an explicit choice.
	if !runNow {
		if !interactive {
			fatal(errors.New("the server picker needs an interactive terminal; pass --run (-r) to run without it"))
		}

		// the picker lists each server's resolved pipeline, so tasks can be left out
		choices := make([]gorun.Server, 0, len(cfg.Servers))
		for _, s := range cfg.Servers {
			choices = append(choices, servers[s.Name])
		}

		selected, err := runServerSelectionTUI(cfg.Title, choices)
		if err != nil {
			fatal(err)
		}
		if len(selected) == 0 {
			fmt.Println("no servers selected")
			return
		}
		cfg.Servers = selected

		// the plain output below prints the plan, which is now just the selection
		if servers, err = cfg.ServersPipeline(); err != nil {
			fatal(fmt.Errorf("%s: %w", filename, err))
		}
	}

	if useTUI {
		if err := runTUI(ctx, cancel, &cfg, *verbose); err != nil {
			os.Exit(1)
		}
		return
	}

	slog.Info("parsed config file")

	for _, server := range servers {
		names := []string{}

		for _, task := range server.Tasks {
			names = append(names, task.Name)
		}

		fmt.Printf("%s: %s\n", server.Name, strings.Join(names, " -> "))
	}

	st, _ := store.Open(getHistoryDBPath())
	defer func() {
		if st != nil {
			_ = st.Close()
		}
	}()

	printer := &plainPrinter{w: os.Stdout, verbose: *verbose}

	cfg.OnEvent = func(event any) {
		printer.event(event)
		recordEvent(ctx, st, event)
	}

	if err := cfg.Run(ctx); err != nil {
		slog.Error("error executing tasks", slog.String("reason", err.Error()))
		if logPath != "" {
			err = fmt.Errorf("%w (logs: %s)", err, logPath)
		}
		fatal(err)
	}
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

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: gorun [options] <Gorunfile>

Run the tasks defined in a Gorunfile across one or more servers. By default gorun first
opens a picker to choose the servers (and tasks) to run; pass -run to skip it.

Options:
  -v        verbose: show every task and its output
  -plain    plain log output instead of the TUI (default when stdout is not a terminal)
  -run, -r  run without the interactive server picker (needed when there is no terminal)
  -task     run only the named task on any selected server (repeatable; dependencies included);
            accepts globs ('deploy-*') and bundles ('@base')
  -server   run only the named server (repeatable; accepts globs like 'web-*')
  -exclude-server  skip the named server (repeatable; accepts globs)
  -tag      run only servers with this tag (repeatable; servers need every tag)
  -group    run only servers in this server group (repeatable; servers need every group)
  -parallel N  run at most N hosts at once (0 = all servers)
  -list-tasks  list available tasks and exit
  -list-servers  list servers and their task pipelines and exit
  -list-tags  list tags and the servers that have them and exit
  -list-bundles  list task bundles and what they expand to and exit
  -dump     print a starting Gorunfile that works as-is and documents every option
  -version  print version and exit
  -web      start the web server
  -history  view historical runs

Environment variables (can be overridden via the process environment or a .env file
in the current working directory; .env values never override variables already set
in the environment):

  GORUN_NAME                          default: gorun
  GORUN_ADDRESS                       default: :8080 (web server listen address)
  GORUN_DB_PATH                       default: ~/.gorun/gorun.db (falls back to gorun.db)
  GORUN_LOG_FILE                      default: ~/.gorun/gorun.log (appended to on every run)
  GORUN_EVENT_TOPIC                   default: gorun.events
  GORUN_SKIP_NATS                     default: unset (set to skip NATS publishing)
  GORUN_NATS_CLIENT_URL               default: nats://127.0.0.1:4222
  GORUN_NATS_CLIENT_NAME              default: GORUN_NAME (or gorun)
  GORUN_NATS_CLIENT_ALLOW_RECONNECT   default: true
  GORUN_NATS_CLIENT_RECONNECT_WAIT    default: nats default (e.g. 2s)
  GORUN_NATS_CLIENT_TIMEOUT           default: nats default (e.g. 5s)

OpenTelemetry SDK variables (OTEL_*) are also honoured by the instrumented build.
Exporters are disabled unless an OTLP endpoint or exporter is configured.
`)
}
