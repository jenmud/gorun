package websrv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
	"github.com/jenmud/gorun/internal/llm"
)

// modelDownload is the one download from Hugging Face that may run at a time, so a page with no
// login can't start many multi-gigabyte downloads at once.
type modelDownload struct {
	mu                      sync.Mutex
	repo, file, status, err string
	running, done           bool
	cancel                  context.CancelFunc
}

func (d *modelDownload) view() *ui.ModelDownload {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.repo == "" {
		return nil
	}
	return &ui.ModelDownload{Repo: d.repo, File: d.file, Status: d.status, Error: d.err, Running: d.running, Done: d.done}
}

// start begins downloading file from repo into dir, unless a download is running already.
func (d *modelDownload) start(dir, repo, file string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running {
		return fmt.Errorf("%s is downloading; wait for it, or cancel it, first", path.Base(d.file))
	}
	ctx, cancel := context.WithCancel(context.Background())
	// field by field: replacing the whole struct would reset the mutex held here
	d.repo, d.file, d.status, d.err = repo, file, "Starting the download", ""
	d.running, d.done, d.cancel = true, false, cancel
	trace := &gorun.ChatTrace{Status: func(s string) {
		d.mu.Lock()
		d.status = strings.Replace(s, ", the first time only", "", 1)
		d.mu.Unlock()
	}}
	go func() {
		_, err := llm.DownloadHub(gorun.WithChatTrace(ctx, trace), dir, repo, file)
		d.mu.Lock()
		defer d.mu.Unlock()
		d.running, d.cancel = false, nil
		switch {
		case errors.Is(err, context.Canceled):
			d.err = "Cancelled."
		case err != nil:
			d.err = err.Error()
			slog.Error("download a model", "repo", repo, "file", file, "error", err)
		default:
			d.done = true
		}
		cancel()
	}()
	return nil
}

// stop cancels the running download, if any.
func (d *modelDownload) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancel != nil {
		d.cancel()
	}
}

// engine is the built-in model, or nil with a model server or no assistant.
func (a *app) engine() *llm.Engine {
	if a.gen == nil {
		return nil
	}
	return a.gen.engine
}

// modelsData lists gorun's own model and the downloaded ones, marking the one in use.
func (a *app) modelsData(errText string) ui.ModelsData {
	e := a.engine()
	s := e.Stats()
	d := ui.ModelsData{Error: errText, Download: a.download.view()}
	builtin := path.Base(llm.DefaultModel.URL)
	files, err := llm.Models(e.Dir)
	if err != nil {
		d.Error = "Can't read the models folder: " + err.Error()
	}
	row := ui.ModelRow{Label: "gorun's Qwen3 4B (" + builtin + ")", Builtin: true, InUse: s.Model == builtin, Loaded: s.Model == builtin && s.Loaded}
	if i := slices.IndexFunc(files, func(f llm.LocalModel) bool { return f.Name == builtin }); i >= 0 {
		row.Size = bytesText(uint64(files[i].Size))
	}
	d.Rows = append(d.Rows, row)
	for _, f := range files {
		if f.Name == builtin {
			continue
		}
		d.Rows = append(d.Rows, ui.ModelRow{Name: f.Name, Label: f.Name, Size: bytesText(uint64(f.Size)), InUse: s.Model == f.Name, Loaded: s.Model == f.Name && s.Loaded})
	}
	return d
}

// modelsPanel draws the models list, for the page to show after an action or while downloading.
func (a *app) modelsPanel(w http.ResponseWriter, r *http.Request, errText string) {
	a.drawModels(w, r, a.modelsData(errText))
}

func (a *app) drawModels(w http.ResponseWriter, r *http.Request, d ui.ModelsData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = ui.ModelsList(d).Render(a.pageCtx(r), w)
}

// modelsHandler draws the models list.
func (a *app) modelsHandler(w http.ResponseWriter, r *http.Request) {
	if a.engine() == nil {
		http.NotFound(w, r)
		return
	}
	a.modelsPanel(w, r, "")
}

// modelActionHandler opens, closes, switches or deletes a model, or cancels the download. Opening
// and closing wait for the model to finish any answer it is writing.
func (a *app) modelActionHandler(w http.ResponseWriter, r *http.Request) {
	e := a.engine()
	if e == nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.modelsPanel(w, r, "Could not read the request.")
		return
	}
	name, action := r.PostFormValue("name"), r.PathValue("action")
	err := a.modelAction(r.Context(), e, action, name, r.PostFormValue("force") == "1")
	if inUse := (errModelInUse("")); errors.As(err, &inUse) {
		d := a.modelsData(err.Error())
		label := "Open it anyway"
		if action == "unload" {
			label = "Close it anyway"
		}
		d.Retry = &ui.ModelRetry{Action: action, Name: name, Label: label}
		a.drawModels(w, r, d)
		return
	}
	if err != nil {
		a.modelsPanel(w, r, err.Error())
		return
	}
	a.modelsPanel(w, r, "")
}

// quietFor is how long since the last question before the model may be switched or closed
// without asking again, so it doesn't change in the middle of someone's chat.
const quietFor = 5 * time.Minute

// modelAction does one model action. Switching and closing are refused while someone is using
// the assistant, unless force, which still waits for the answer being written.
func (a *app) modelAction(ctx context.Context, e *llm.Engine, action, name string, force bool) error {
	// name is a file in the models folder, or "" for gorun's own model
	spec := ""
	if name != "" {
		p, err := llm.ModelPath(e.Dir, name)
		if err != nil {
			return err
		}
		spec = p
	}
	switch action {
	case "cancel":
		a.download.stop()
		return nil
	case "delete":
		if name == "" || e.Stats().Model == name {
			return errors.New("open another model before deleting this one")
		}
		return os.Remove(spec)
	case "use", "load", "unload":
	default:
		return fmt.Errorf("unknown action %q", action)
	}

	var release func()
	var err error
	if action == "load" || force {
		// opening the model in use changes nothing for anyone: it waits its turn like a question
		release, err = a.genQ.acquire(ctx, nil)
	} else {
		release, err = a.genQ.claim(quietFor)
	}
	if err != nil {
		return err
	}
	defer release()
	switch action {
	case "unload":
		e.Unload()
		return nil
	case "use":
		e.Use(spec)
		if err := llm.SaveModel(e.Dir, name); err != nil {
			slog.Warn("remember the model in use", "error", err)
		}
		if !e.Stats().Downloaded {
			return nil // gorun's own model downloads on the first question, which says so
		}
	}
	if err := e.Load(ctx); err != nil {
		return fmt.Errorf("couldn't open it: %w", err)
	}
	return nil
}

// hubSearchHandler finds GGUF models on Hugging Face.
func (a *app) hubSearchHandler(w http.ResponseWriter, r *http.Request) {
	if a.engine() == nil {
		http.NotFound(w, r)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	res := ui.HubResults{Query: q}
	models, err := llm.SearchHub(r.Context(), q)
	if err != nil {
		res.Error = err.Error()
	}
	for _, m := range models {
		res.Models = append(res.Models, hubRow(m))
	}
	a.renderHub(w, r, res)
}

// hubFilesHandler lists a Hugging Face repository's GGUF files.
func (a *app) hubFilesHandler(w http.ResponseWriter, r *http.Request) {
	e := a.engine()
	if e == nil {
		http.NotFound(w, r)
		return
	}
	res := ui.HubResults{Query: r.URL.Query().Get("q")}
	info, files, err := llm.HubFiles(r.Context(), r.URL.Query().Get("repo"))
	if err != nil {
		res.Error = err.Error()
		a.renderHub(w, r, res)
		return
	}
	row := hubRow(info)
	res.Repo = &row
	for _, f := range files {
		_, have := os.Stat(filepath.Join(e.Dir, "models", path.Base(f.Name)))
		res.Files = append(res.Files, ui.HubFileRow{Name: f.Name, Size: bytesText(uint64(f.Size)), Have: have == nil})
	}
	a.renderHub(w, r, res)
}

// hubDownloadHandler starts downloading a model from Hugging Face, and draws the models list.
func (a *app) hubDownloadHandler(w http.ResponseWriter, r *http.Request) {
	e := a.engine()
	if e == nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.modelsPanel(w, r, "Could not read the request.")
		return
	}
	if err := a.download.start(e.Dir, r.PostFormValue("repo"), r.PostFormValue("file")); err != nil {
		a.modelsPanel(w, r, err.Error())
		return
	}
	a.modelsPanel(w, r, "")
}

func (a *app) renderHub(w http.ResponseWriter, r *http.Request, res ui.HubResults) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = ui.HubResultsList(res).Render(a.pageCtx(r), w)
}

func hubRow(m llm.HubModel) ui.HubModelRow {
	return ui.HubModelRow{
		ID: m.ID, License: m.License, Gated: m.Gated,
		Downloads:  countText(m.Downloads),
		Permissive: m.License == "apache-2.0" || m.License == "mit",
	}
}

// countText is a count as people say it: 950, 12k, 1.2M.
func countText(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprint(n)
}
