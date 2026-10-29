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
#
# This is a template produced by ` + "`gorun -dump`" + `. Every key is shown with its
# type, allowed values and default. Uncomment the lines you need and edit the values.
# Relative paths (identityfile, cmd_file, cmd_template_file, copy src/src_template)
# are resolved relative to THIS file, not your working directory, so gorun can be
# run from anywhere.
#
# Quick start:
#   gorun -dump > Gorunfile        # create this file
#   # edit it, then:
#   gorun Gorunfile                # run on every server
#   gorun Gorunfile --select       # pick servers interactively first
#   gorun Gorunfile -v             # verbose: list every task per server

# ---------------------------------------------------------------------------
# Top-level settings
# ---------------------------------------------------------------------------

# title: human-readable name for this run (shown in the TUI/web dashboard).
# Type: string. Default: "".
title = "My deployment"

# log_level: how chatty gorun's own logs are.
# Type: string. Allowed: "debug", "info", "warn"/"warning", "error". Default: "info".
# Inherited by servers and tasks unless they override it.
log_level = "info"

# envvars: environment variables exported to every task's process.
# Type: table of string -> string. Default: {} (none).
# Keys are upper-cased automatically when injected. Values are used verbatim
# (no ${VAR} expansion).
# [envvars]
# FOO = "bar"
# PATH_EXTRA = "/opt/bin:${PATH}"

# ssh: RESERVED global SSH block. SSH settings are per-server only
# ([servers.hosts.<name>.ssh]); these global values are NOT applied automatically
# except for config_file, which is inherited by servers that don't set their own.
# Prefer configuring SSH on each server. Shown for completeness only.
# [ssh]
# username = "root"
# port = 22
# identityfile = "~/.ssh/id_rsa"
# password = ""   # avoid committing passwords; prefer identityfile
# # config_file: path to an SSH config file to consult for host settings
# # (User, Port, IdentityFile, HostName). Empty/unset falls back to ~/.ssh/config
# # when that file exists. Alias: config.
# config_file = "~/.ssh/config"

# ---------------------------------------------------------------------------
# Task library: define each task once as [tasks.<name>], then reference by name.
# ---------------------------------------------------------------------------

# Minimal task: run an inline command.
# [tasks.hello]
# cmd = "echo 'hello world'"

# Task that reads its command from a local file (resolved relative to this file).
# [tasks.packages]
# cmd_file = "./scripts/packages.sh"
# sudo = true

# Task whose command is a Go text/template. Compiled at run time with a Server{}
# passed in, so you can use {{.Name}}, {{.Hostname}}, {{.SSH.Port}}, {{.SSH.User}}.
# [tasks.greet]
# cmd_template = "echo 'hello from {{.Name}} on {{.Hostname}}'"

# Task reading a template from a file.
# [tasks.greet-from-file]
# cmd_template_file = "./templates/greet.sh.tmpl"

# Full task showing every available field:
# [tasks.example]
# # log_level: override the global level for this task only.
# # Type: string. Allowed: "debug"/"info"/"warn"/"warning"/"error". Default: inherited.
# log_level = "info"
#
# # sudo: wrap the command in 'sudo -n' (fails instead of prompting for a password).
# # Type: bool. Default: false.
# sudo = false
#
# # Command source: pick EXACTLY ONE of cmd / cmd_file / cmd_template / cmd_template_file.
# # (A task may also have no command if it only performs file copies.)
# cmd = "echo hello"
# # cmd_file = "./scripts/example.sh"
# # cmd_template = "echo 'hi {{.Name}}'"
# # cmd_template_file = "./templates/example.sh.tmpl"
#
# # shell: run the command through a different shell/interpreter.
# # Type: string. Default: "sh -c" (i.e. multi-word commands work out of the box).
# # Format: "<binary> [arg ...]" (space separated). The command is appended as the
# # last argument.
# shell = "bash -c"
#
# # envvars: per-task environment variables (merged with the global ones).
# # Type: table of string -> string. Keys are upper-cased automatically.
# [tasks.example.envvars]
# FOO = "bar"
#
# # depends_on: tasks that must run (and succeed) before this one.
# # Their dependencies are pulled in transitively, so list only what you care about.
# # Type: array of task names. Default: [].
# depends_on = ["packages"]
#
# # copy / copies / upload / files: file copy operations (all four are equivalent
# # aliases and are merged). Each entry is a [[tasks.<name>.copy]] array-of-tables.
# [[tasks.example.copy]]
# # src: local file to copy (resolved relative to this file). Mutually exclusive
# #      with src_template/template/content.
# src = "./config/app.conf"
# # dst: remote (or local) destination path. Required unless 'dir' is set.
# dst = "/etc/app/app.conf"
# # dir: destination directory; the file keeps its base name. Used instead of dst.
# # dir = "/etc/app/"
# # mode / perm: file mode for the copy. Type: string (octal like "0644"). Default: "0644".
# mode = "0644"
#
# # Template copy: compile a local template file as a Go text/template with a
# # Server{} and push the result. Use src_template (aliases: template, src_tmpl).
# # [[tasks.example.copy]]
# # src_template = "./templates/nginx.conf.tmpl"
# # dst = "/etc/nginx/nginx.conf"
#
# # Inline content copy (no source file).
# # [[tasks.example.copy]]
# # content = "hello {{.Name}}"
# # dst = "/tmp/greeting.txt"

# ---------------------------------------------------------------------------
# Servers
# ---------------------------------------------------------------------------

# [servers]: global task list applied to every host that does NOT set its own
# 'tasks'. A host's own list REPLACES (does not merge with) the global one.
# Dependencies are still pulled in either way.
[servers]
tasks = []

# [servers.hosts.<name>]: one entry per target server. <name> is a short label.
# [servers.hosts.myserver]
# # hostname: host to connect to. Default: "" (treated as localhost / local run).
# hostname = "example.com"
#
# # tasks: override the global list for this host. Omit to inherit [servers].tasks.
# tasks = ["example"]
#
# # log_level: override for this server. Default: inherited from top level.
# log_level = "info"
#
# # ssh: connection details. Omit entirely to run tasks LOCALLY on this machine
# # (no SSH). See example/Gorunfile-local for a local-only setup.
# [servers.hosts.myserver.ssh]
# # username: SSH user. Default: "" (uses the current OS user for local runs).
# username = "root"
# # port: SSH port. Type: int. Default: 22.
# port = 22
# # identityfile: path to a private key (resolved relative to this file).
# # Default: "" (no key auth; password or agent may still apply).
# identityfile = "~/.ssh/id_rsa"
# # password: SSH password. Default: "". Prefer identityfile; never commit secrets.
# password = ""
# # config_file: path to an SSH config file to consult for this host (User, Port,
# # IdentityFile, HostName). Empty/unset falls back to ~/.ssh/config when present.
# # Explicit fields above always take precedence over the config.
# config_file = "~/.ssh/config"

# ---------------------------------------------------------------------------
# Examples
# ---------------------------------------------------------------------------

# Example 1: a single local task (no SSH).
# [tasks.say-hi]
# cmd = "echo 'hi from local'"
#
# [servers]
# tasks = ["say-hi"]
#
# [servers.hosts.local]
# # no hostname/ssh => runs locally

# Example 2: two remote servers sharing a global task list.
# [tasks.uptime]
# cmd = "uptime"
#
# [servers]
# tasks = ["uptime"]
#
# [servers.hosts.web1]
# hostname = "web1.example.com"
#     [servers.hosts.web1.ssh]
#     username = "deploy"
#     identityfile = "~/.ssh/id_ed25519"
#
# [servers.hosts.web2]
# hostname = "web2.example.com"
#     [servers.hosts.web2.ssh]
#     username = "deploy"
#     identityfile = "~/.ssh/id_ed25519"

# Example 3: dependency chain.
# [tasks.packages]
# cmd_file = "./scripts/packages.sh"
# sudo = true
#
# [tasks.timezone]
# cmd = "cp /usr/share/zoneinfo/UTC /etc/localtime"
# sudo = true
# depends_on = ["packages"]   # packages runs first
#
# [servers]
# tasks = ["timezone"]        # pulls in packages automatically
`
}
