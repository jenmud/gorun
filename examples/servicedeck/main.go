// Service Deck is a small web app that manages services on a fleet of servers with the
// gorun library. Each button runs a gorun task (with its dependencies) on the servers you
// pick, and every progress event gorun emits is streamed to the browser as it happens.
//
//	cd examples/servicedeck && go run .        # then open http://localhost:8090
//
// The page uses Datastar (https://data-star.dev): one long-lived server-sent event stream
// patches the board whenever gorun reports progress.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jenmud/gorun"
	"github.com/starfederation/datastar-go/datastar"
)

//go:embed page.html static
var assets embed.FS

var page = template.Must(template.New("page.html").Funcs(template.FuncMap{
	"pct":   func(n, of int) int { return pctOf(n, of) },
	"lower": strings.ToLower,
}).ParseFS(assets, "page.html"))

// An action is a button: a gorun task to run, with everything it depends on.
type action struct {
	ID, Label, Task, Hint string
}

var actions = []action{
	{"health", "Health check", "health", "GET /health on every server"},
	{"nginx", "Restart nginx", "restart-nginx", "config test, then a graceful reload"},
	{"deploy", "Deploy API 2.4", "deploy-api", "pull, migrate, restart, verify"},
	{"logs", "Rotate logs", "rotate-logs", "logrotate and compress"},
}

var targets = []string{"all", "web", "api", "worker"}

func main() {
	addr := flag.String("addr", "localhost:8090", "address to listen on")
	file := flag.String("f", "Gorunfile", "the Gorunfile with the fleet and its tasks")
	flag.Parse()

	deck := &deck{file: *file}
	if _, err := deck.load(); err != nil { // fail fast on a broken Gorunfile
		log.Fatal(err)
	}

	static, _ := fs.Sub(assets, "static")
	http.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	http.HandleFunc("GET /{$}", deck.index)
	http.HandleFunc("GET /updates", deck.updates)
	http.HandleFunc("POST /run/{action}", deck.run)

	log.Printf("Service Deck on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

// deck holds the state of the current (or last) run, built from gorun's events.
type deck struct {
	file string

	mu      sync.Mutex
	version int // bumped on every change, so streams know when to redraw
	running bool
	board   board
}

// board is what the page shows.
type board struct {
	Title   string
	Action  string
	Target  string
	Servers []*serverState
	Events  []eventLine
	Summary string
	Failed  bool
	Started time.Time
}

type serverState struct {
	Name     string
	Tags     []string
	State    string // idle, queued, running, done, failed
	Tasks    []string
	Done     int
	Current  string
	Attempt  string
	LastLine string
	Elapsed  string
}

type eventLine struct {
	At, Kind, Text string
}

// load reads the Gorunfile. A fresh Config per run keeps runs independent.
func (d *deck) load() (*gorun.Config, error) {
	var cfg gorun.Config
	if err := cfg.LoadFile(d.file); err != nil {
		return nil, err
	}
	cfg.ResolvePaths(filepath.Dir(d.file))
	return &cfg, nil
}

func (d *deck) index(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	if d.board.Servers == nil {
		d.resetBoardLocked()
	}
	b := d.board
	d.mu.Unlock()
	if err := page.Execute(w, map[string]any{"Board": b, "Actions": actions, "Targets": targets}); err != nil {
		log.Print(err)
	}
}

// resetBoardLocked shows every server as idle.
func (d *deck) resetBoardLocked() {
	cfg, err := d.load()
	if err != nil {
		return
	}
	d.board = board{Title: cfg.Title}
	for _, s := range cfg.Servers {
		d.board.Servers = append(d.board.Servers, &serverState{Name: s.Name, Tags: s.Tags, State: "idle"})
	}
}

// run starts an action on the chosen servers and returns at once; progress arrives
// through OnEvent and the /updates stream.
func (d *deck) run(w http.ResponseWriter, r *http.Request) {
	var signals struct {
		Target string `json:"target"`
	}
	_ = datastar.ReadSignals(r, &signals)
	sse := datastar.NewSSE(w, r)

	i := slices.IndexFunc(actions, func(a action) bool { return a.ID == r.PathValue("action") })
	if i < 0 {
		_ = sse.ConsoleError(fmt.Errorf("unknown action"))
		return
	}
	act := actions[i]

	// docs:begin run
	cfg, err := d.load()
	if err != nil {
		_ = sse.ConsoleError(err)
		return
	}

	// pick servers the way gorun's --tag flag does
	filter := gorun.ServerFilter{}
	if signals.Target != "" && signals.Target != "all" {
		filter.Tags = []string{signals.Target}
	}
	servers, err := cfg.SelectServers(filter)
	if err != nil {
		_ = sse.ConsoleError(err)
		return
	}

	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return // one run at a time
	}
	d.running = true
	d.board = board{Title: cfg.Title, Action: act.Label, Target: signals.Target, Started: time.Now()}
	for _, s := range servers {
		d.board.Servers = append(d.board.Servers, &serverState{Name: s.Name, Tags: s.Tags, State: "queued"})
	}
	d.version++
	d.mu.Unlock()

	// Every progress event lands here, from many goroutines at once: one per server.
	cfg.OnEvent = d.onEvent

	go func() {
		err := cfg.RunTask(context.Background(), cfg.Tasks[act.Task], servers...)
		d.mu.Lock()
		defer d.mu.Unlock()
		d.running = false
		if err != nil && d.board.Summary == "" {
			d.board.Summary = err.Error()
			d.board.Failed = true
		}
		d.version++
	}()
	// docs:end run
}

// docs:begin events
// onEvent turns gorun's events into board state.
func (d *deck) onEvent(event any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() { d.version++ }()

	switch ev := event.(type) {
	case gorun.ProcessStartedEvent:
		d.logLocked("start", fmt.Sprintf("%s on %d servers", d.board.Action, len(ev.Servers)))

	case gorun.PlanEvent:
		if s := d.server(ev.Server); s != nil {
			s.Tasks = ev.Tasks
			d.logLocked("plan", fmt.Sprintf("%s: %s", ev.Server, strings.Join(ev.Tasks, " → ")))
		}

	case gorun.TaskEvent:
		s := d.server(ev.Server)
		if s == nil {
			return
		}
		switch ev.State {
		case gorun.TaskRunning:
			s.State, s.Current, s.Attempt = "running", ev.Task, ""
			if ev.Attempt > 1 {
				s.Attempt = fmt.Sprintf("retry %d/%d", ev.Attempt-1, ev.Attempts-1)
			}
			d.logLocked("running", fmt.Sprintf("%s › %s", ev.Server, ev.Task))
		case gorun.TaskDone, gorun.TaskUnchanged, gorun.TaskWouldRun: // unchanged: its check found nothing to do
			s.Done++
			s.LastLine = lastLine(ev.Stdout)
			s.Elapsed = ev.Duration.Round(10 * time.Millisecond).String()
			if s.Done == len(s.Tasks) {
				s.State, s.Current = "done", ""
			}
			kind := "done"
			if ev.State == gorun.TaskUnchanged {
				kind = "unchanged"
			}
			d.logLocked(kind, fmt.Sprintf("%s › %s in %s", ev.Server, ev.Task, s.Elapsed))
		case gorun.TaskFailed:
			s.State, s.Current = "failed", ev.Task
			s.LastLine = lastLine(ev.Stderr)
			d.logLocked("failed", fmt.Sprintf("%s › %s exit %d: %s", ev.Server, ev.Task, ev.ExitCode, s.LastLine))
		case gorun.TaskSkipped:
			d.logLocked("skipped", fmt.Sprintf("%s › %s", ev.Server, ev.Task))
		}

	case gorun.ServerErrorEvent:
		if s := d.server(ev.Server); s != nil {
			s.State, s.LastLine = "failed", fmt.Sprint(ev.Err)
		}
		d.logLocked("failed", fmt.Sprintf("%s: %v", ev.Server, ev.Err))

	case gorun.ProcessFinishedEvent:
		d.board.Failed = !ev.Success
		d.board.Summary = fmt.Sprintf("%d tasks, %d failed, %d skipped in %s",
			ev.TotalTasks, ev.Failed, ev.Skipped, ev.Duration.Round(100*time.Millisecond))
		d.logLocked("finish", d.board.Summary)
	}
}
// docs:end events

func (d *deck) server(name string) *serverState {
	for _, s := range d.board.Servers {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (d *deck) logLocked(kind, text string) {
	at := time.Since(d.board.Started).Seconds()
	d.board.Events = append(d.board.Events, eventLine{At: fmt.Sprintf("+%5.2fs", at), Kind: kind, Text: text})
	if n := len(d.board.Events); n > 14 {
		d.board.Events = d.board.Events[n-14:]
	}
}

// updates is a long-lived stream: it redraws the board whenever the state changes.
func (d *deck) updates(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	seen := -1
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		d.mu.Lock()
		changed := d.version != seen
		seen = d.version
		var buf strings.Builder
		if changed {
			err := page.ExecuteTemplate(&buf, "board", map[string]any{"Board": d.board, "Running": d.running})
			if err != nil {
				log.Print(err)
			}
		}
		running := d.running
		d.mu.Unlock()

		if changed {
			if err := sse.PatchElements(buf.String()); err != nil {
				return
			}
			if err := sse.MarshalAndPatchSignals(map[string]any{"running": running}); err != nil {
				return
			}
		}
		select {
		case <-sse.Context().Done():
			return
		case <-tick.C:
		}
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func pctOf(n, of int) int {
	if of == 0 {
		return 0
	}
	return n * 100 / of
}
