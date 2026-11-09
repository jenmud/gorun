// rollout is a tiny command-line tool built on the gorun library: it runs a Gorunfile and
// draws its own live progress, one bar per server, from gorun's progress events.
//
//	cd examples/rollout && go run .              # every server
//	go run . -tag edge                           # only servers tagged edge
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jenmud/gorun"
)

func main() {
	file := flag.String("f", "Gorunfile", "the Gorunfile to run")
	tag := flag.String("tag", "", "only run servers with this tag")
	flag.Parse()

	// docs:begin main
	// gorun logs every task to stdout unless told otherwise. This tool draws on the
	// terminal itself, so send the logs to gorun's log file (~/.gorun/gorun.log).
	if logFile, err := gorun.OpenLogFile(gorun.LogFilePath()); err == nil {
		gorun.SetLogOutput(logFile)
		defer logFile.Close()
	}

	var cfg gorun.Config
	if err := cfg.LoadFile(*file); err != nil {
		fail(err)
	}
	cfg.ResolvePaths(filepath.Dir(*file))

	filter := gorun.ServerFilter{}
	if *tag != "" {
		filter.Tags = []string{*tag}
	}
	servers, err := cfg.SelectServers(filter)
	if err != nil {
		fail(err)
	}

	p := newProgress(cfg.Title, servers)
	cfg.OnEvent = p.onEvent // every progress event, from every server, as it happens

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	done := make(chan struct{})
	go p.draw(done) // redraw ten times a second
	err = cfg.Run(ctx, servers...)
	close(done)
	p.render() // the final frame

	if err != nil || p.failed > 0 {
		os.Exit(1)
	}
	// docs:end main
}

// progress is the state the bars are drawn from, updated only by gorun's events.
type progress struct {
	mu      sync.Mutex
	title   string
	order   []string
	servers map[string]*row
	start   time.Time
	summary string
	failed  int
	lines   int // lines drawn last time, to redraw in place
}

type row struct {
	total, done int
	task        string
	started     time.Time
	note        string
	failed      bool
	finished    bool
}

func newProgress(title string, servers []gorun.Server) *progress {
	p := &progress{title: title, servers: map[string]*row{}, start: time.Now()}
	for _, s := range servers {
		p.order = append(p.order, s.Name)
		p.servers[s.Name] = &row{}
	}
	return p
}

// docs:begin events
// onEvent is called by gorun from one goroutine per server, so it locks.
func (p *progress) onEvent(event any) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch ev := event.(type) {
	case gorun.PlanEvent: // the server's tasks, in the order they will run
		p.servers[ev.Server].total = len(ev.Tasks)

	case gorun.TaskEvent: // a task changed state
		r := p.servers[ev.Server]
		switch ev.State {
		case gorun.TaskRunning:
			r.task, r.started, r.note = ev.Task, time.Now(), ""
			if ev.Attempt > 1 {
				r.note = fmt.Sprintf("retry %d of %d", ev.Attempt-1, ev.Attempts-1)
			}
		case gorun.TaskDone:
			r.done++
			r.finished = r.done == r.total
		case gorun.TaskFailed:
			r.done++
			r.task = ev.Task
			r.note = fmt.Sprintf("exit %d: %s", ev.ExitCode, clip(lastLine(ev.Stderr), 32))
			r.failed, r.finished = true, true
		case gorun.TaskSkipped:
			r.done++
		}

	case gorun.CopyProgressEvent: // a task is uploading a file or directory
		r := p.servers[ev.Server]
		r.note = ""
		if !ev.Done {
			r.note = fmt.Sprintf("↑ %s %.0f%% of %.0f MB", filepath.Base(ev.File), ev.Percent, float64(ev.Total)/(1<<20))
		}

	case gorun.ServerErrorEvent: // the server couldn't run at all (e.g. SSH refused)
		r := p.servers[ev.Server]
		r.failed, r.finished, r.note = true, true, clip(fmt.Sprint(ev.Err), 32)

	case gorun.ProcessFinishedEvent: // the whole run is over
		p.failed = ev.Failed
		p.summary = fmt.Sprintf("%d tasks · %d failed · %d skipped · %s",
			ev.TotalTasks, ev.Failed, ev.Skipped, ev.Duration.Round(100*time.Millisecond))
	}
}
// docs:end events

func (p *progress) draw(done <-chan struct{}) {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			p.render()
		}
	}
}

const (
	reset  = "\033[0m"
	dim    = "\033[2m"
	bold   = "\033[1m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	blue   = "\033[34m"
	violet = "\033[35m"
	cyan   = "\033[36m"
)

var spin = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (p *progress) render() {
	p.mu.Lock()
	defer p.mu.Unlock()

	var b strings.Builder
	if p.lines > 0 {
		fmt.Fprintf(&b, "\033[%dA", p.lines) // back to the top of the last frame
	}
	elapsed := time.Since(p.start)
	frame := spin[int(elapsed/(100*time.Millisecond))%len(spin)]

	fmt.Fprintf(&b, "\033[2K%s%s rollout %s %s%s%s\n\033[2K\n", bold, violet, reset, bold, p.title, reset)
	for _, name := range p.order {
		r := p.servers[name]
		colour, status := blue, ""
		switch {
		case r.failed:
			colour, status = red, fmt.Sprintf("%s✗ %s%s %s%s%s", red, r.task, reset, dim, r.note, reset)
		case r.finished:
			colour, status = green, green+"✓ done"+reset
		case r.task != "":
			status = fmt.Sprintf("%s%s%s %s %s%.1fs%s %s%s%s", cyan, frame, reset, r.task,
				dim, time.Since(r.started).Seconds(), reset, yellow, r.note, reset)
		default:
			status = dim + frame + " connecting" + reset
		}
		fmt.Fprintf(&b, "\033[2K  %-8s %s %s%d/%d%s  %s\n", name, bar(r.done, r.total, 24, colour), dim, r.done, r.total, reset, status)
	}
	fmt.Fprintf(&b, "\033[2K\n")
	if p.summary != "" {
		mark := green + "✓"
		if p.failed > 0 {
			mark = red + "✗"
		}
		fmt.Fprintf(&b, "\033[2K%s %s%s\n", mark, p.summary, reset)
	} else {
		fmt.Fprintf(&b, "\033[2K%s%.1fs%s\n", dim, elapsed.Seconds(), reset)
	}
	p.lines = len(p.order) + 4
	fmt.Print(b.String())
}

func bar(n, of, width int, colour string) string {
	fill := 0
	if of > 0 {
		fill = n * width / of
	}
	return colour + strings.Repeat("━", fill) + reset + dim + strings.Repeat("━", width-fill) + reset
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// clip keeps a note to one terminal line, so redrawing in place stays aligned.
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "rollout:", err)
	os.Exit(1)
}
