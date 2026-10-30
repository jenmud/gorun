package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"gorun"
)

// plainPrinter prints a line per finished task for -plain runs, now that the
// logs go to the log file. Failed tasks get the tail of their stderr; verbose
// prints every task's output.
type plainPrinter struct {
	mu      sync.Mutex
	w       io.Writer
	verbose bool
}

func (p *plainPrinter) event(event any) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch ev := event.(type) {
	case gorun.TaskEvent:
		attempt := ""
		if ev.Attempts > 1 {
			attempt = fmt.Sprintf(", attempt %d/%d", ev.Attempt, ev.Attempts)
		}
		switch ev.State {
		case gorun.TaskRunning:
			if ev.Attempt > 1 {
				fmt.Fprintf(p.w, "%s: %s retrying (attempt %d/%d)\n", ev.Server, ev.Task, ev.Attempt, ev.Attempts)
				if ev.Err != nil {
					fmt.Fprintf(p.w, "    previous attempt: %v\n", ev.Err)
				}
			}
		case gorun.TaskDone:
			fmt.Fprintf(p.w, "%s: %s ok (%s%s)\n", ev.Server, ev.Task, ev.Duration.Round(time.Millisecond), attempt)
			if p.verbose {
				p.output("stdout", ev.Stdout, outputTailLines)
				p.output("stderr", ev.Stderr, outputTailLines)
			}
		case gorun.TaskFailed:
			fmt.Fprintf(p.w, "%s: %s FAILED (exit code %d, %s%s)\n", ev.Server, ev.Task, ev.ExitCode, ev.Duration.Round(time.Millisecond), attempt)
			if ev.Err != nil {
				fmt.Fprintf(p.w, "    reason: %v\n", ev.Err)
			}
			if p.verbose {
				p.output("stdout", ev.Stdout, outputTailLines)
			}
			if ev.Err == nil || strings.TrimSpace(ev.Stderr) != ev.Err.Error() {
				p.output("stderr", ev.Stderr, failTailLines)
			}
		case gorun.TaskSkipped:
			fmt.Fprintf(p.w, "%s: %s skipped\n", ev.Server, ev.Task)
		}
	case gorun.ServerErrorEvent:
		fmt.Fprintf(p.w, "%s: FAILED: %v\n", ev.Server, ev.Err)
	}
}

// output prints the last n lines of a task's output, indented under the task.
func (p *plainPrinter) output(name, s string, n int) {
	out := tail(s, n)
	if out == "" {
		return
	}

	fmt.Fprintf(p.w, "    %s:\n", name)
	for line := range strings.SplitSeq(out, "\n") {
		fmt.Fprintf(p.w, "      %s\n", line)
	}
}
