package websrv

import (
	"context"
	"fmt"
	"log"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// page renders body inside the shared layout.
func (a *app) page(w http.ResponseWriter, title, active string, body templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ui.Layout(title, active, a.nc != nil, body).Render(context.Background(), w); err != nil {
		log.Printf("gorun web: render %s: %v", title, err)
	}
}

// flashFrom reads the message a redirect carried, and whether it reports an error.
func flashFrom(r *http.Request) (string, bool) {
	q := r.URL.Query()
	return q.Get("flash"), q.Get("bad") == "1"
}

// redirectWith sends the browser to to, carrying a message for the page it lands on.
func redirectWith(w http.ResponseWriter, r *http.Request, to, msg string, bad bool) {
	q := url.Values{}
	q.Set("flash", msg)
	if bad {
		q.Set("bad", "1")
	}
	http.Redirect(w, r, to+"?"+q.Encode(), http.StatusSeeOther)
}

// runPage is the run page: the picker, the stats and the live dashboard.
func (a *app) runPage(w http.ResponseWriter, r *http.Request) {
	msg, bad := flashFrom(r)
	a.mu.Lock()
	cfg, path, dash := a.cfg, a.cfgPath, a.dash
	a.mu.Unlock()

	d := ui.RunPageData{
		Dashboard: dash,
		Stats:     a.stats(r.Context()),
		NATSOK:    a.nc != nil,
		Path:      path,
		Flash:     msg,
		FlashBad:  bad,
	}
	switch {
	case cfg != nil:
		d.Servers = serverOptions(cfg)
		d.Tasks = taskOptions(cfg)
	case msg == "":
		d.Flash, d.FlashBad = "No Gorunfile loaded. Load one on the Config page to pick servers and tasks.", true
	}
	if d.Flash == "" && !d.NATSOK {
		d.Flash, d.FlashBad = "NATS is not running, so runs can't start from here.", true
	}
	a.page(w, "Run", "run", ui.RunPage(d))
}

// serverOptions lists the servers in the loaded file for the picker.
func serverOptions(cfg *gorun.Config) []ui.ServerOption {
	pipes, _ := cfg.ServersPipeline()
	out := make([]ui.ServerOption, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		out = append(out, ui.ServerOption{
			Name:    s.Name,
			Address: s.Address(),
			Tags:    strings.Join(s.Tags, ", "),
			Groups:  strings.Join(s.Groups, ", "),
			Local:   s.IsLocal(),
			Tasks:   len(pipes[s.Name].Tasks),
		})
	}
	return out
}

// taskOptions lists the task library in the loaded file for the picker.
func taskOptions(cfg *gorun.Config) []ui.TaskOption {
	names := slices.Sorted(maps.Keys(cfg.Tasks))
	out := make([]ui.TaskOption, 0, len(names))
	for _, n := range names {
		t := cfg.Tasks[n]
		out = append(out, ui.TaskOption{
			Name:        n,
			Description: t.Description,
			Details:     t.Details,
			Rerun:       t.Rerun,
			Local:       t.Local,
		})
	}
	return out
}

// startRunHandler starts the ticked servers and tasks in the background. Each run reads the
// Gorunfile on disk, so unsaved editor changes never run.
func (a *app) startRunHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/", "Could not read the form.", true)
		return
	}
	if a.nc == nil {
		redirectWith(w, r, "/", "NATS is not running, so runs can't start.", true)
		return
	}
	a.mu.Lock()
	path := a.cfgPath
	a.mu.Unlock()
	if path == "" {
		redirectWith(w, r, "/", "Load a Gorunfile on the Config page first.", true)
		return
	}
	names := r.Form["server"]
	if len(names) == 0 {
		redirectWith(w, r, "/", "Tick at least one server.", true)
		return
	}

	var cfg gorun.Config
	if err := cfg.LoadFile(path); err != nil {
		redirectWith(w, r, "/", err.Error(), true)
		return
	}
	cfg.SelectedTasks = r.Form["task"]
	pipes, err := cfg.ServersPipeline()
	if err != nil {
		redirectWith(w, r, "/", err.Error(), true)
		return
	}
	servers := make([]gorun.Server, 0, len(names))
	for _, n := range names {
		s, ok := pipes[n]
		if !ok {
			redirectWith(w, r, "/", fmt.Sprintf("Unknown server %q.", n), true)
			return
		}
		servers = append(servers, s)
	}

	uuid := fmt.Sprintf("web-%d-%d", os.Getpid(), time.Now().UnixNano())
	cfg.GorunUUID = uuid
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.runs[uuid] = cancel
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			delete(a.runs, uuid)
			a.mu.Unlock()
			cancel()
		}()
		if err := cfg.Run(ctx, servers...); err != nil {
			log.Printf("gorun web: run %s: %v", uuid, err)
		}
	}()
	redirectWith(w, r, "/", fmt.Sprintf("Started on %d server(s).", len(servers)), false)
}

// clearFinished removes the finished runs from the dashboard.
func (a *app) clearFinished(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	a.dash.ClearFinished()
	a.mu.Unlock()
	a.pushDashboard()
	w.WriteHeader(http.StatusNoContent)
}

// cancelRun stops a run this page started. Runs started elsewhere can't be stopped here.
func (a *app) cancelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.mu.Lock()
	cancel, ok := a.runs[id]
	a.mu.Unlock()
	if !ok {
		redirectWith(w, r, "/", "That run wasn't started from this page, so it can't be stopped here.", true)
		return
	}
	cancel()
	redirectWith(w, r, "/", "Stopping the run.", false)
}
