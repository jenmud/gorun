package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/jenmud/gorun"
)

func main() {
	gorun.SetLogOutput(io.Discard) // task logs go to stdout by default
	var cfg gorun.Config
	if err := cfg.LoadFile("Gorunfile"); err != nil {
		log.Fatal(err)
	}

	var mu sync.Mutex // OnEvent runs on one goroutine per server
	cfg.OnEvent = func(event any) {
		mu.Lock()
		defer mu.Unlock()
		switch ev := event.(type) {
		case gorun.TaskEvent:
			if ev.State != gorun.TaskRunning {
				fmt.Printf("%-6s %-7s %-6s exit %d\n", ev.Server, ev.Task, ev.State, ev.ExitCode)
			}
		case gorun.ProcessFinishedEvent:
			fmt.Printf("%d tasks, %d failed\n", ev.TotalTasks, ev.Failed)
		}
	}

	if err := cfg.RunTask(context.Background(), cfg.Tasks["restart"]); err != nil { // and what it needs
		log.Print(err)
	}
}
