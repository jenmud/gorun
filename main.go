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
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	flag.Parse()
	filename := flag.Arg(0)

	slog.Info("parsing gorun file", slog.String("filename", filename))

	cfg := Config{}

	r, err := os.Open(filename)
	if err != nil {
		panic(err)
	}

	if err := cfg.Load(r); err != nil {
		panic(err)
	}

	slog.Info("parsed config file")

	servers, err := cfg.ServersPipeline()
	if err != nil {
		panic(err)
	}

	for _, server := range servers {
		names := make([]string, len(server.Tasks))

		for i, task := range server.Tasks {
			names[i] = task.Name
		}

		fmt.Printf("%s: %s\n", server.Hostname, strings.Join(names, " -> "))
	}

	if err := cfg.Run(ctx); err != nil {
		slog.Error("error executing tasks", slog.String("reason", err.Error()))
	}
}
