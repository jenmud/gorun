package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jenmud/gorun"
)

func main() {
	f, err := os.Open("Gorunfile")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	var cfg gorun.Config
	if err := cfg.Load(f); err != nil {
		log.Fatal(err)
	}
	cfg.ResolvePaths(filepath.Dir(f.Name()))

	// Show how local-only tasks are filtered per server.
	pipelines, err := cfg.ServersPipeline()
	if err != nil {
		log.Fatal(err)
	}
	for _, name := range []string{"local", "remote"} {
		s := pipelines[name]
		fmt.Printf("%s: %v (local=%v)\n", s.Name, taskNames(s.Tasks), s.IsLocal())
	}

	cfg.OnEvent = func(event any) {
		if ev, ok := event.(gorun.TaskEvent); ok && ev.State != gorun.TaskRunning {
			fmt.Printf("%s: %s exit %d\n", ev.Server, ev.Task, ev.ExitCode)
		}
	}

	// Run the local server's tasks on this machine.
	if err := cfg.RunLocal(context.Background(), "show-env"); err != nil {
		log.Fatal(err)
	}
}

func taskNames(tasks []gorun.Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.Name
	}
	return out
}
