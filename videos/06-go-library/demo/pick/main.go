package main

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/jenmud/gorun"
)

func main() {
	gorun.SetLogOutput(io.Discard) // task logs go to stdout by default
	var cfg gorun.Config
	if err := cfg.LoadFile("Gorunfile"); err != nil {
		log.Fatal(err)
	}
	cfg.OnEvent = func(event any) {
		if ev, ok := event.(gorun.TaskEvent); ok && ev.State == gorun.TaskDone {
			fmt.Printf("%s: %s\n", ev.Server, ev.Task)
		}
	}

	// the same as --tag web --task @rollout
	servers, err := cfg.SelectServers(gorun.ServerFilter{Tags: []string{"web"}})
	if err != nil {
		log.Fatal(err)
	}
	tasks, err := cfg.ExpandTaskPatterns("@rollout")
	if err != nil {
		log.Fatal(err)
	}
	for i := range servers {
		servers[i].TaskNames = tasks
	}

	if err := cfg.Run(context.Background(), servers...); err != nil {
		log.Print(err)
	}
}
