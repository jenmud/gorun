package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	_ "github.com/joho/godotenv/autoload"
	"golang.org/x/term"

	"gorun"
	web "gorun/cmd/websrv"
	inats "gorun/internal/nats"
	"gorun/internal/store"
)

var version = "dev"

func init() {
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

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	verbose := flag.Bool("v", false, "verbose: show every task and its output")
	plain := flag.Bool("plain", false, "plain log output instead of the TUI (default when stdout is not a terminal)")
	selectServers := flag.Bool("select", false, "select servers to deploy to using TUI")
	dump := flag.Bool("dump", false, "dump an empty Gorunfile template to stdout")
	versionFlag := flag.Bool("version", false, "print version and exit")
	webFlag := flag.Bool("web", false, "start the web server")
	historyFlag := flag.Bool("history", false, "view historical runs")

	flag.Usage = printUsage
	flag.Parse()

	if *versionFlag {
		fmt.Println(version)
		return
	}

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

	if filename == "" {
		printUsage()
		os.Exit(1)
	}

	if !useTUI {
		slog.Info("parsing gorun file", slog.String("filename", filename))
	}

	// hide all log output unless running in plain mode
	if useTUI {
		log.SetOutput(io.Discard)
		gorun.SetLogOutput(io.Discard)
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

	// check every server's task list up front, so config mistakes such as an unknown task
	// are reported before the TUI starts rather than lost inside it
	servers, err := cfg.ServersPipeline()
	if err != nil {
		fatal(fmt.Errorf("%s: %w", filename, err))
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

	cfg.OnEvent = func(event any) {
		if st != nil {
			if data, err := json.Marshal(event); err == nil {
				var ev inats.Event
				if json.Unmarshal(data, &ev) == nil {
					inats.UpdateDatabase(ctx, st, &ev)
				}
			}
		}
	}

	if err := cfg.Run(ctx); err != nil {
		slog.Error("error executing tasks", slog.String("reason", err.Error()))
		os.Exit(1)
	}
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
  GORUN_EVENT_TOPIC                   default: gorun.events
  GORUN_SKIP_NATS                     default: unset (set to skip NATS publishing)
  GORUN_NATS_CLIENT_URL               default: nats://127.0.0.1:4222
  GORUN_NATS_CLIENT_NAME              default: GORUN_NAME (or gorun)
  GORUN_NATS_CLIENT_ALLOW_RECONNECT   default: true
  GORUN_NATS_CLIENT_RECONNECT_WAIT    default: nats default (e.g. 2s)
  GORUN_NATS_CLIENT_TIMEOUT           default: nats default (e.g. 5s)

OpenTelemetry SDK variables (OTEL_*) are also honoured by the instrumented build.
`)
}

func emptyGorunfile() string {
	return `# Gorunfile

title = "My deployment"
log_level = "info"   # debug|info|warn|error (inherited by servers/tasks)

# envvars = { FOO = "bar" }   # exported to every task (keys upper-cased)

# Task library: define once, reference by name.
# [tasks.example]
# cmd = "echo hello"            # or cmd_file / cmd_template / cmd_template_file (pick one)
# sudo = false
# depends_on = []
# shell = "sh -c"               # default
# log_level = "info"
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
