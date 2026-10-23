package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"

	_ "github.com/joho/godotenv/autoload"
	"golang.org/x/term"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	verbose := flag.Bool("v", false, "verbose: show every task and its output")
	plain := flag.Bool("plain", false, "plain log output instead of the TUI (default when stdout is not a terminal)")
	selectServers := flag.Bool("select-servers", false, "select servers to deploy to using TUI")
	flag.Parse()
	filename := flag.Arg(0)

	useTUI := !*plain && term.IsTerminal(int(os.Stdout.Fd()))
	if !useTUI {
		slog.Info("parsing gorun file", slog.String("filename", filename))
	}

	cfg := Config{}

	r, err := os.Open(filename)
	if err != nil {
		panic(err)
	}

	if err := cfg.Load(r); err != nil {
		panic(err)
	}

	if *selectServers {
		if err := runServerSelectionTUI(ctx, cancel, &cfg); err != nil {
			os.Exit(1)
		}
		return
	}

	if useTUI {
		if err := runTUI(ctx, cancel, &cfg, *verbose); err != nil {
			os.Exit(1)
		}
		return
	}

	slog.Info("parsed config file")

	servers, err := cfg.ServersPipeline()
	if err != nil {
		panic(err)
	}

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
