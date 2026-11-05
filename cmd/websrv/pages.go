package websrv

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
		d.Bundles = bundleOptions(cfg, r.URL.Query().Get("bundle"))
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
	cfg, servers, err := planRun(path, r.Form["server"], r.Form["task"], r.Form["bundle"])
	if err != nil {
		redirectWith(w, r, "/", err.Error(), true)
		return
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

// planRun loads the saved Gorunfile and works out what each ticked server runs. Ticked tasks or
// bundles replace each server's own list, as --task and --bundles do; with neither, each server
// runs its own list. Ticked local tasks run on this machine without a server being ticked, but
// other tasks need a ticked SSH server. It fails when nothing would run.
func planRun(path string, names, tasks, bundles []string) (*gorun.Config, []gorun.Server, error) {
	if len(tasks) > 0 && len(bundles) > 0 {
		return nil, nil, errors.New("Pick tasks or bundles, not both.")
	}
	if len(names) == 0 && len(tasks) == 0 && len(bundles) == 0 {
		return nil, nil, errors.New("Tick a server, or a task marked local = true.")
	}
	cfg := &gorun.Config{}
	if err := cfg.LoadFile(path); err != nil {
		return nil, nil, err
	}
	cfg.ResolvePaths(filepath.Dir(cfg.Path))

	selected := tasks
	if len(bundles) > 0 {
		refs := make([]string, len(bundles))
		for i, b := range bundles {
			refs[i] = gorun.BundlePrefix + b
		}
		expanded, err := cfg.ExpandTaskRefs(refs...)
		if err != nil {
			return nil, nil, err
		}
		if len(expanded) == 0 {
			return nil, nil, errors.New("The chosen bundles expand to no tasks.")
		}
		selected = expanded
	}
	if len(selected) > 0 {
		for i := range cfg.Servers {
			cfg.Servers[i].TaskNames = slices.Clone(selected)
		}
		cfg.SelectedTasks = selected
	}

	pipes, err := cfg.ServersPipeline()
	if err != nil {
		return nil, nil, err
	}
	servers := make([]gorun.Server, 0, len(names))
	for _, n := range names {
		s, ok := pipes[n]
		if !ok {
			return nil, nil, fmt.Errorf("Unknown server %q.", n)
		}
		servers = append(servers, s)
	}
	if len(selected) > 0 {
		var err error
		if servers, err = addLocal(cfg, servers, selected); err != nil {
			return nil, nil, err
		}
	}
	if !slices.ContainsFunc(servers, func(s gorun.Server) bool { return len(s.Tasks) > 0 }) {
		return nil, nil, errors.New("Nothing to run on the ticked servers. Local servers only run tasks marked local = true, and other tasks only run over SSH.")
	}
	return cfg, servers, nil
}

// addLocal adds this machine to servers when selected has local tasks and no ticked server is
// local, and fails when selected has other tasks but no ticked server reaches them over SSH.
func addLocal(cfg *gorun.Config, servers []gorun.Server, selected []string) ([]gorun.Server, error) {
	var local, remote []string
	for _, n := range selected {
		if cfg.Tasks[n].Local {
			local = append(local, n)
		} else {
			remote = append(remote, n)
		}
	}
	if len(remote) > 0 && !slices.ContainsFunc(servers, func(s gorun.Server) bool { return !s.IsLocal() }) {
		return nil, fmt.Errorf("Tick a server to run %s on. Only tasks marked local = true run without one.", strings.Join(remote, ", "))
	}
	if len(local) == 0 || slices.ContainsFunc(servers, gorun.Server.IsLocal) {
		return servers, nil
	}
	s := cfg.LocalServer()
	s.TaskNames = slices.Clone(selected)
	s, err := cfg.ServerPipeline(s)
	if err != nil {
		return nil, err
	}
	return append(servers, s), nil
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

// viewFile shows a file a task in the loaded Gorunfile references, as plain text. Only those
// files are served, so the page can't be used to read anything else on the machine.
func (a *app) viewFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg == nil || !referenced(cfg, path) {
		http.Error(w, "No task in the loaded Gorunfile references that file.", http.StatusNotFound)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "Could not read the file: "+err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

// referenced reports whether some task in cfg references path.
func referenced(cfg *gorun.Config, path string) bool {
	for _, t := range cfg.Tasks {
		for _, f := range taskFiles(t, filepath.Dir(cfg.Path)) {
			if f.Path == path {
				return true
			}
		}
	}
	return false
}
