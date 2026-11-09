package history_test

import (
	"context"
	"fmt"
	"log"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/history"
)

// Record a run to the same history the gorun CLI and web UI use, then list the latest runs.
func Example() {
	h, err := history.Open(history.DefaultPath())
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	var cfg gorun.Config
	if err := cfg.LoadFile("Gorunfile"); err != nil {
		log.Fatal(err)
	}
	cfg.Recorder = h // OnEvent stays free for your own handler
	if err := cfg.Run(context.Background()); err != nil {
		log.Print(err)
	}

	runs, err := h.Runs(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range runs[:min(5, len(runs))] {
		fmt.Printf("%s  %s  ok=%v  %d/%d tasks failed\n", r.StartedAt.Format("2006-01-02 15:04"), r.Title, r.Success, r.Failed, r.TotalTasks)
	}
}
