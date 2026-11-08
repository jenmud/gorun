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
local = true

[tasks.goodbye]
cmd = "echo goodbye"
local = true
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

// Start the schedules in a Gorunfile, then pause one and skip the next run of another.
func ExampleNewScheduler() {
	var cfg gorun.Config
	if err := cfg.LoadFile("Gorunfile"); err != nil {
		log.Fatal(err)
	}

	s := gorun.NewScheduler(&cfg, func(ev gorun.ScheduleEvent) {
		log.Printf("%s %s %s", ev.Schedule, ev.Kind, ev.Reason)
	})
	if err := s.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
	defer s.Stop()

	// hold the nightly backup while the database is moved, and cancel tonight's check
	if err := s.Pause("nightly-backup"); err != nil {
		log.Fatal(err)
	}
	if err := s.Skip("hourly-check"); err != nil {
		log.Fatal(err)
	}
	for _, st := range s.Status() {
		fmt.Printf("%s paused=%v next=%v\n", st.Name, st.Paused, st.Next)
	}
}
