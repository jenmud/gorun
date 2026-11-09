package gorun_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

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

// jsonLines is a Recorder that appends each event to a writer as one line of JSON: the same JSON
// gorun publishes on NATS. The mutex keeps lines whole, since Record is called concurrently.
type jsonLines struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (j *jsonLines) Record(event any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	_ = j.enc.Encode(event)
}

// Keep every event of a run as JSON lines in a file, with a Recorder of your own.
func ExampleRecorder() {
	f, err := os.OpenFile("runs.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	var cfg gorun.Config
	if err := cfg.LoadFile("Gorunfile"); err != nil {
		log.Fatal(err)
	}
	cfg.Recorder = &jsonLines{enc: json.NewEncoder(f)}
	if err := cfg.Run(context.Background()); err != nil {
		log.Print(err)
	}
}

// Count finished runs and failures, with a function as the Recorder.
func ExampleRecorderFunc() {
	var cfg gorun.Config
	if err := cfg.LoadFile("Gorunfile"); err != nil {
		log.Fatal(err)
	}
	cfg.Recorder = gorun.RecorderFunc(func(event any) {
		switch ev := event.(type) {
		case gorun.ProcessFinishedEvent:
			log.Printf("run %s: success=%v failed=%d skipped=%d", ev.GorunUUID, ev.Success, ev.Failed, ev.Skipped)
		case gorun.ServerErrorEvent:
			log.Printf("%s could not run: %v", ev.Server, ev.Err)
		}
	})
	_ = cfg.Run(context.Background())
}
