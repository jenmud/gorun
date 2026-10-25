package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	_ "github.com/joho/godotenv/autoload"
	"golang.org/x/term"

	"gorun"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	verbose := flag.Bool("v", false, "verbose: show every task and its output")
	plain := flag.Bool("plain", false, "plain log output instead of the TUI (default when stdout is not a terminal)")
	selectServers := flag.Bool("select", false, "select servers to deploy to using TUI")
	flag.Parse()
	filename := flag.Arg(0)

	// the flag package stops at the first positional argument, so parse again
	// to allow flags after the filename, e.g. `gorun Gorunfile --select -v`
	if flag.NArg() > 1 {
		_ = flag.CommandLine.Parse(flag.Args()[1:])
	}

	useTUI := !*plain && term.IsTerminal(int(os.Stdout.Fd()))
	if !useTUI {
		slog.Info("parsing gorun file", slog.String("filename", filename))
	}

	if filename == "" {
		fatal(errors.New("usage: gorun [-v] [-plain] [-select] <Gorunfile>"))
	}

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
