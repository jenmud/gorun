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
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/a-h/templ"
	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
	"github.com/jenmud/gorun/history"
	"github.com/jenmud/gorun/internal/envvars"
	inats "github.com/jenmud/gorun/internal/nats"
	"github.com/jenmud/gorun/internal/natsserver"
	"github.com/jenmud/gorun/internal/store"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics for the events the service receives, from NATS or from its own scheduled runs.
var (
	meter = otel.Meter("gorun")

	eventTypes, _ = meter.Int64Counter("gorun.nats.events.by_type",
		metric.WithDescription("Number of events by type"), metric.WithUnit("{event}"))
	eventDuration, _ = meter.Float64Histogram("gorun.nats.event.processing.duration",
		metric.WithDescription("Time taken to apply an event"), metric.WithUnit("s"))
	unmarshalErrors, _ = meter.Int64Counter("gorun.nats.message.unmarshal.errors",
		metric.WithDescription("Number of events that could not be read"), metric.WithUnit("{error}"))
)

// app holds the state the handlers share. Each piece is guarded by mu.
type app struct {
	saveMu sync.Mutex // one direct save from a page at a time, so edits can't overwrite each other
	store  *store.Store
	nc     *inats.Client
	topic  string
	hub    *hub

	// pushSoon is set while a coalesced dashboard push is waiting, so copy progress from many
	// servers at once costs one render per interval, not one per event.
	pushSoon atomic.Bool

	mu      sync.Mutex
	dash    ui.DashboardState
	cfgPath string                        // the saved Gorunfile runs and the editor use
	cfg     *gorun.Config                 // the loaded config, for the editor
	pending []gorun.Change                // unsaved editor changes
	runs    map[string]context.CancelFunc // runs started from this page, by UUID
	sched   *gorun.Scheduler              // fires the loaded Gorunfile's schedules, if any
	nats    natsState                     // how this service reaches NATS; set once at startup
}

// Run starts the web server on GORUN_ADDRESS (default :8080). configPath, when set, is the
// Gorunfile to load; it can also be loaded from the config page.
func Run(configPath string) {
	st := openStore()
	if st != nil {
		defer func() { _ = st.Close() }()
	}

	// a NATS server is started here when none is running, before anything connects to one
	emb, err := natsserver.Ensure(natsserver.Settings{
		ConfigPath: envvars.Get("GORUN_NATS_SERVER_CONFIG"),
		ProbeURL:   inats.DefaultURL,
		ClientURL:  envvars.Get("GORUN_NATS_CLIENT_URL"),
		Skip:       envvars.Get("GORUN_SKIP_NATS") != "",
	})
	if err != nil {
		log.Printf("gorun web: NATS server not started: %v", err)
	}
	if emb != nil {
		defer emb.Stop()
		if err := os.Setenv("GORUN_NATS_CLIENT_URL", emb.URL); err != nil {
			log.Printf("gorun web: %v", err)
		}
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
		nats:  natsStartState(emb, nc, err, envvars.Get("GORUN_SKIP_NATS") != ""),
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
	mux.HandleFunc("POST /schedules/pause", a.schedulePause)
	mux.HandleFunc("POST /schedules/resume", a.scheduleResume)
	mux.HandleFunc("POST /schedules/skip", a.scheduleSkip)
	mux.HandleFunc("POST /schedules/edit", a.editEntity(kindSchedule, "/schedules"))
	mux.HandleFunc("POST /schedules/add", a.addEntity(kindSchedule, "/schedules"))
	mux.HandleFunc("POST /tasks/add", a.addEntity(kindTask, "/tasks"))
	mux.HandleFunc("POST /tasks/edit", a.editEntity(kindTask, "/tasks"))
	mux.HandleFunc("POST /bundles/add", a.addEntity(kindBundle, "/bundles"))
	mux.HandleFunc("POST /bundles/edit", a.editEntity(kindBundle, "/bundles"))
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
	path := history.DefaultPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	st, err := store.Open(path)
	if err != nil {
		log.Printf("gorun web: history disabled: %v", err)
		return nil
	}
	log.Printf("gorun web: history at %s", path)
	return st
}

// onEvent applies one run event from NATS to the dashboard and the history, then pushes
// the change to every open page.
func (a *app) onEvent(m *nats.Msg) {
	a.applyEvent(m.Data)
}

// Record saves one run event to the history database and the live dashboard, as the NATS
// handler does for events it receives: the web service is a gorun.Recorder.
func (a *app) Record(event any) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	a.applyEvent(data)
}

var _ gorun.Recorder = (*app)(nil)

// applyEvent applies one run event, as JSON, to the dashboard and the history, then pushes the
// change to every open page. NATS delivers events this way; so do scheduled runs when NATS is
// down, so their history and live view don't depend on NATS.
func (a *app) applyEvent(data []byte) {
	start := time.Now()
	var ev store.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		unmarshalErrors.Add(context.Background(), 1)
		return
	}
	kind := metric.WithAttributes(attribute.String("type", ev.Type))
	eventTypes.Add(context.Background(), 1, kind)
	defer func() { eventDuration.Record(context.Background(), time.Since(start).Seconds(), kind) }()

	a.mu.Lock()
	a.dash.Apply(ev)
	a.mu.Unlock()

	if ev.Type == "copy_progress" {
		// progress isn't kept in the history, and it comes often: coalesce the pushes
		a.pushDashboardSoon()
		return
	}

	a.store.Apply(context.Background(), &ev)
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

// copyPushInterval is the least time between two dashboard pushes caused by copy progress.
const copyPushInterval = 250 * time.Millisecond

// pushDashboardSoon pushes the dashboard after copyPushInterval, unless a push is already waiting.
func (a *app) pushDashboardSoon() {
	if a.pushSoon.Swap(true) {
		return
	}
	time.AfterFunc(copyPushInterval, func() {
		a.pushSoon.Store(false)
		a.pushDashboard()
	})
}

// pushStats recomputes the stats from the history and sends them to every open page.
func (a *app) pushStats(ctx context.Context) {
	a.hub.broadcast(patch("#stats", render(ui.StatsPanel(a.stats(ctx)))))
}
