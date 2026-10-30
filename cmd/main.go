package main

import (
	"context"
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
	"sort"
	"strings"

	_ "github.com/joho/godotenv/autoload"
	"go.opentelemetry.io/otel"
	"golang.org/x/term"

	"gorun"
	web "gorun/cmd/websrv"
	"gorun/internal/store"
)

var version = "dev"

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
	selectServers := flag.Bool("select", false, "select servers to deploy to using TUI")
	dump := flag.Bool("dump", false, "dump an empty Gorunfile template to stdout")
	versionFlag := flag.Bool("version", false, "print version and exit")
	webFlag := flag.Bool("web", false, "start the web server")
	historyFlag := flag.Bool("history", false, "view historical runs")
	taskFlags := stringSlice{}
	flag.Var(&taskFlags, "task", "run only the named task on any selected server (repeatable); dependencies are included")
	serverFlags := stringSlice{}
	flag.Var(&serverFlags, "server", "run only the named server (repeatable)")
	parallel := flag.Int("parallel", 0, "maximum number of hosts to run in parallel (0 = all)")
	listTasks := flag.Bool("list-tasks", false, "list available tasks and exit")
	listServers := flag.Bool("list-servers", false, "list servers and their task pipelines and exit")

	flag.Usage = printUsage
	flag.Parse()

	if *versionFlag {
		fmt.Println(version)
		return
	}

	if *dump {
		os.Setenv("GORUN_SKIP_NATS", "1")
		fmt.Print(emptyGorunfile())
		return
	}

	filename := flag.Arg(0)

	// the flag package stops at the first positional argument, so parse again
	// to allow flags after the filename, e.g. `gorun Gorunfile --select -v`
	if flag.NArg() > 1 {
		_ = flag.CommandLine.Parse(flag.Args()[1:])
	}

	useTUI := !*plain && term.IsTerminal(int(os.Stdout.Fd()))

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
	if *listTasks || *listServers {
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

	if *listTasks {
		// Build a reverse dependency tree: a task's children are the tasks that
		// depend on it, so dependencies appear above their dependents.
		children := map[string][]string{}
		roots := []string{}
		for name := range cfg.Tasks {
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

		visited := map[string]bool{}
		var printNode func(name, prefix string, last, root bool)
		printNode = func(name, prefix string, last, root bool) {
			connector := "├── "
			if last {
				connector = "└── "
			}
			if visited[name] {
				if root {
					fmt.Printf("%s (already shown)\n", name)
				} else {
					fmt.Printf("%s%s%s (already shown)\n", prefix, connector, name)
				}
				return
			}
			if root {
				fmt.Println(name)
			} else {
				fmt.Printf("%s%s%s\n", prefix, connector, name)
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

	// validate and apply --task filter. A named task is run on every selected
	// server even when it is not part of that server's configured pipeline, so
	// one-off tasks work on any host.
	for _, t := range taskFlags {
		if _, ok := cfg.Tasks[t]; !ok {
			fatal(fmt.Errorf("unknown task %q", t))
		}
	}
	if len(taskFlags) > 0 {
		for i := range cfg.Servers {
			cfg.Servers[i].TaskNames = append([]string{}, taskFlags...)
		}
		cfg.SelectedTasks = []string(taskFlags)
	}

	// validate and apply --server filter
	if len(serverFlags) > 0 {
		known := map[string]bool{}
		for _, srv := range cfg.Servers {
			known[srv.Name] = true
		}
		for _, name := range serverFlags {
			if !known[name] {
				fatal(fmt.Errorf("unknown server %q", name))
			}
		}
		selected := map[string]bool{}
		for _, name := range serverFlags {
			selected[name] = true
		}
		filtered := make([]gorun.Server, 0, len(serverFlags))
		for _, srv := range cfg.Servers {
			if selected[srv.Name] {
				filtered = append(filtered, srv)
			}
		}
		cfg.Servers = filtered
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
			for i, t := range server.Tasks {
				connector := "├── "
				if i == len(server.Tasks)-1 {
					connector = "└── "
				}
				fmt.Printf("    %s%s\n", connector, t.Name)
			}
			fmt.Println()
		}
		return
	}

	if *selectServers {
		if !useTUI {
			fatal(errors.New("--select needs an interactive terminal"))
		}

		selected, err := runServerSelectionTUI(cfg.Title, cfg.Servers)
		if err != nil {
			fatal(err)
		}
		if len(selected) == 0 {
			fmt.Println("no servers selected")
			return
		}
		cfg.Servers = selected
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

Run the tasks defined in a Gorunfile across one or more servers.

Options:
  -v        verbose: show every task and its output
  -plain    plain log output instead of the TUI (default when stdout is not a terminal)
  -select   select servers to deploy to using the TUI
  -task     run only the named task on any selected server (repeatable; dependencies included)
  -server  run only the named server (repeatable)
  -parallel N  run at most N hosts at once (0 = all servers)
  -list-tasks  list available tasks and exit
  -list-servers  list servers and their task pipelines and exit
  -dump     dump an empty Gorunfile template to stdout
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

func emptyGorunfile() string {
	return `# Gorunfile

title = "My deployment"
log_level = "info"   # debug|info|warn|error (inherited by servers/tasks)
# max_parallel = 0     # 0 (default) = every selected host at once

# envvars = { FOO = "bar" }   # exported to every task (keys upper-cased)

# Global SSH defaults (per-server values win; config_file is inherited).
# [ssh]
# username = "root"
# port = 22
# identityfile = "~/.ssh/id_rsa"
# password = ""               # prefer identityfile
# config_file = "~/.ssh/config"  # fills gaps; defaults to ~/.ssh/config

# Task library: define once, reference by name.
# [tasks.example]
# cmd = "echo hello"            # or cmd_file / cmd_template / cmd_template_file (pick one)
# sudo = false
# depends_on = []
# shell = "sh -c"               # default
# log_level = "info"
# timeout = "30m"               # default; task fails with exit code 124 after this
# [tasks.example.envvars]
# KEY = "value"
# [[tasks.example.copy]]
# src = "./local/file"          # or src_template / content
# dst = "/remote/path"          # or dir = "/remote/dir/"
# mode = "0644"

# Servers.
[servers]
tasks = []                      # global list; a host's own list replaces it

# [servers.hosts.myserver]
# hostname = "example.com"      # empty => run locally
# tasks = ["example"]           # omit to inherit global
# log_level = "info"
#   [servers.hosts.myserver.ssh]
#   username = "root"
#   port = 22
#   identityfile = "~/.ssh/id_rsa"
#   password = ""               # prefer identityfile
#   config_file = "~/.ssh/config"  # fills gaps; defaults to ~/.ssh/config
`
}
