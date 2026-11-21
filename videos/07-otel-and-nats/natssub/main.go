// natssub prints every event gorun publishes on NATS, one line each.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/nats-io/nats.go"
)

// state names, in the order of gorun's TaskState numbers
var states = []string{"pending", "running", "done", "failed", "skipped", "unchanged", "would run"}

func main() {
	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()

	_, err = nc.Subscribe("gorun.events", func(m *nats.Msg) {
		var ev struct {
			Type, Server, Task string
			State              int
			ExitCode           int `json:"exit_code"`
		}
		if json.Unmarshal(m.Data, &ev) != nil {
			return
		}
		if ev.Type == "task" {
			fmt.Printf("%-16s %-6s %-8s %s (%d) exit %d\n", ev.Type, ev.Server, ev.Task, states[ev.State], ev.State, ev.ExitCode)
			return
		}
		fmt.Printf("%-16s %s\n", ev.Type, ev.Server)
	})
	if err != nil {
		log.Fatal(err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
}
