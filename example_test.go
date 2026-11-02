package gorun_test

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jenmud/gorun"
)

// Load a Gorunfile and run its tasks on this machine, printing each task as it finishes.
func ExampleConfig_RunLocal() {
	gorunfile := `
title = "Example"

[tasks.hello]
cmd = "echo hello"

[tasks.goodbye]
cmd = "echo goodbye"
depends_on = ["hello"]
`
	var cfg gorun.Config
	if err := cfg.Load(strings.NewReader(gorunfile)); err != nil {
		log.Fatal(err)
	}

	cfg.OnEvent = func(event any) {
		if ev, ok := event.(gorun.TaskEvent); ok && ev.State == gorun.TaskDone {
			fmt.Printf("%s: %s", ev.Task, ev.Stdout)
		}
	}

	if err := cfg.RunLocal(context.Background(), "goodbye"); err != nil {
		log.Fatal(err)
	}
	// Output:
	// hello: hello
	// goodbye: goodbye
}
