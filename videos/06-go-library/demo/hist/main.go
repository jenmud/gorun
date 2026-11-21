package main

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/history"
)

func main() {
	gorun.SetLogOutput(io.Discard) // task logs go to stdout by default
	ctx := context.Background()
	h, err := history.Open(history.DefaultPath()) // the same database as the CLI and web UI
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	var cfg gorun.Config
	if err := cfg.LoadFile("Gorunfile"); err != nil {
		log.Fatal(err)
	}
	cfg.Recorder = h                          // save every event
	_ = cfg.RunTask(ctx, cfg.Tasks["deploy"]) // on every server

	runs, err := h.Runs(ctx) // newest first
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range runs {
		fmt.Printf("%s  success=%v  %d tasks, %d failed\n", r.Title, r.Success, r.TotalTasks, r.Failed)
	}
}
