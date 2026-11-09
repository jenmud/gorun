// Package websrv serves the gorun web UI: the run page with a live dashboard, the history of
// saved runs, and the config editor. Runs and live updates go through NATS, and the page
// updates over server-sent events (SSE) using Datastar.
package websrv

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/a-h/templ"
	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
	"github.com/jenmud/gorun/internal/envvars"
	inats "github.com/jenmud/gorun/internal/nats"
	"github.com/jenmud/gorun/internal/store"
	"github.com/nats-io/nats.go"
)

// app holds the state the handlers share. Each piece is guarded by mu.
type app struct {
	store *store.Store
	nc    *inats.Client
	topic string
	hub   *hub

	mu      sync.Mutex
	dash    ui.DashboardState
	cfgPath string                        // the saved Gorunfile runs and the editor use
	cfg     *gorun.Config                 // the loaded config, for the editor
	pending []gorun.Change                // unsaved editor changes
	runs    map[string]context.CancelFunc // runs started from this page, by UUID
	sched   *gorun.Scheduler              // fires the loaded Gorunfile's schedules, if any
}

// Run starts the web server on GORUN_ADDRESS (default :8080). configPath, when set, is the
// Gorunfile to load; it can also be loaded from the config page.
func Run(configPath string) {
	st := openStore()
	if st != nil {
		defer func() { _ = st.Close() }()
	}

	gorun.StartNATS()
	nc, err := inats.NewClient()
	if err != nil {
		log.Printf("gorun web: NATS disabled: %v", err)
	}

	a := &app{
		store: st,
		nc:    nc,
		topic: topicName(),
		hub:   &hub{clients: map[chan string]struct{}{}},
		runs:  map[string]context.CancelFunc{},
	}
	if nc != nil {
		defer nc.Close()
		if _, err := nc.Subscribe(a.topic, a.onEvent); err != nil {
			log.Printf("gorun web: subscribe failed: %v", err)
		} else {
			log.Printf("gorun web: subscribed to %s", a.topic)
		}
	}
	if configPath != "" {
		if err := a.loadConfig(configPath); err != nil {
			log.Printf("gorun web: %v", err)
		}
	}

	addr := envvars.Get("GORUN_ADDRESS")
	if addr == "" {
		addr = ":8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer a.stopScheduler()

	srv := &http.Server{Addr: addr, Handler: a.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("gorun web listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// routes maps every page and action to its handler.
func (a *app) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.runPage)
	mux.HandleFunc("POST /run", a.startRunHandler)
	mux.HandleFunc("GET /events", a.events)
	mux.HandleFunc("POST /clear", a.clearFinished)
	mux.HandleFunc("POST /runs/{id}/cancel", a.cancelRun)

	mux.HandleFunc("GET /tasks", a.tasksPage)
	mux.HandleFunc("GET /bundles", a.bundlesPage)
	mux.HandleFunc("GET /schedules", a.schedulesPage)
	mux.HandleFunc("GET /history", a.historyPage)
	mux.HandleFunc("GET /history/{id}", a.historyDetail)
	mux.HandleFunc("GET /history/{id}/task", a.taskDetail)
	mux.HandleFunc("POST /runs/{id}/delete", a.deleteRun)
	mux.HandleFunc("POST /runs/truncate", a.truncateRuns)

	mux.HandleFunc("GET /config", a.configPage)
	mux.HandleFunc("GET /config/edit", a.editPage)
	mux.HandleFunc("GET /file", a.viewFile)
	mux.HandleFunc("POST /config/load", a.loadHandler)
	mux.HandleFunc("POST /config/add", a.addHandler)
	mux.HandleFunc("POST /config/edit", a.editHandler)
	mux.HandleFunc("POST /config/remove", a.removeHandler)
	mux.HandleFunc("POST /config/save", a.saveHandler)
	mux.HandleFunc("POST /config/discard", a.discardHandler)
	return mux
}

// topicName is the NATS subject events are published on.
func topicName() string {
	if t := envvars.Get("GORUN_EVENT_TOPIC"); t != "" {
		return t
	}
	return "gorun.events"
}

// openStore opens the history database, or returns nil so the UI still works without it.
func openStore() *store.Store {
	dbPath := envvars.Get("GORUN_DB_PATH")
	if dbPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir := home + "/.gorun"
			_ = os.MkdirAll(dir, 0o755)
			dbPath = dir + "/gorun.db"
		} else {
			dbPath = "gorun.db"
		}
	} else if strings.HasPrefix(dbPath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dbPath = home + dbPath[1:]
		}
	}
	st, err := store.Open(dbPath)
	if err != nil {
		log.Printf("gorun web: history disabled: %v", err)
		return nil
	}
	log.Printf("gorun web: history at %s", dbPath)
	return st
}

// onEvent applies one run event from NATS to the dashboard and the history, then pushes
// the change to every open page.
func (a *app) onEvent(m *nats.Msg) {
	var ev inats.Event
	if err := json.Unmarshal(m.Data, &ev); err != nil {
		return
	}

	a.mu.Lock()
	a.dash.Apply(ev.UIEvent())
	a.mu.Unlock()

	if a.store != nil {
		inats.UpdateDatabase(context.Background(), a.store, &ev)
	}
	if ev.Type == "process_finished" {
		a.pushStats(context.Background())
	}
	a.pushDashboard()
}

// render turns a templ component into an HTML string.
func render(c templ.Component) string {
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		log.Printf("gorun web: render: %v", err)
		return ""
	}
	return buf.String()
}

// pushDashboard sends the current runs to every open page.
func (a *app) pushDashboard() {
	a.mu.Lock()
	html := render(ui.Dashboard(a.dash))
	a.mu.Unlock()
	a.hub.broadcast(patch("#dashboard", html))
}

// pushStats recomputes the stats from the history and sends them to every open page.
func (a *app) pushStats(ctx context.Context) {
	a.hub.broadcast(patch("#stats", render(ui.StatsPanel(a.stats(ctx)))))
}
