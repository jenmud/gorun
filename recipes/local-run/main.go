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

	// Show how tasks are filtered per server: local = true tasks only on the
	// local server, the rest only over SSH.
	pipelines, err := cfg.ServersPipeline()
	if err != nil {
		log.Fatal(err)
	}
	for _, name := range []string{"local", "remote"} {
		s := pipelines[name]
		fmt.Printf("%s: %v (local=%v)\n", s.Name, taskNames(s.Tasks), s.IsLocal())
	}

	// Print each task's outcome. A task whose check found nothing to do ends as unchanged; its
	// check's result is in ev.Check, and a verify's in ev.Verify.
	cfg.OnEvent = func(event any) {
		ev, ok := event.(gorun.TaskEvent)
		if !ok || ev.State == gorun.TaskRunning {
			return
		}
		fmt.Printf("%s: %s %s (exit %d)\n", ev.Server, ev.Task, ev.State, ev.ExitCode)
		if ev.Verify != nil {
			fmt.Printf("    verified: %s (exit %d)\n", ev.Verify.Command, ev.Verify.ExitCode)
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
