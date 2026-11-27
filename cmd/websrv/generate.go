package websrv

import (
	"cmp"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/envvars"
	"github.com/jenmud/gorun/internal/llm"
)

// generator writes tasks for the Config page. Only the web service has one, so the CLI and
// TUI never download llama.cpp or a model. One request runs at a time.
type generator struct {
	assistant gorun.Assistant
	engine    *llm.Engine // the built-in model, loaded on first use; nil with GORUN_LLM_URL
}

// newGenerator uses the model server at GORUN_LLM_URL, or else the built-in model.
func newGenerator() *generator {
	a := gorun.Assistant{
		URL:    envvars.Get("GORUN_LLM_URL"),
		Model:  envvars.Get("GORUN_LLM_MODEL"),
		APIKey: envvars.Get("GORUN_LLM_API_KEY"),
	}
	g := &generator{assistant: a}
	if a.URL == "" {
		g.engine = &llm.Engine{Dir: llm.Dir(), Model: a.Model, Progress: os.Stderr}
		g.assistant.Engine = g.engine
	}
	return g
}

// describe names the model for the Config page, or is empty when there is no generator.
func (g *generator) describe() string {
	switch {
	case g == nil:
		return ""
	case g.engine == nil && g.private():
		return cmp.Or(g.assistant.Model, gorun.DefaultAssistantModel) + " at " + g.assistant.URL
	case g.engine == nil:
		return cmp.Or(g.assistant.Model, gorun.DefaultAssistantModel) + " at " + g.assistant.URL + ". Not private: your request and task list are sent there"
	case g.assistant.Model != "":
		return filepath.Base(g.assistant.Model) + ", built in"
	}
	if !llm.Downloaded(g.engine.Dir) {
		return "gorun's Qwen3 4B, built in; the first answer downloads it, 2.5 GB, so it takes a few minutes"
	}
	return "gorun's Qwen3 4B, built in"
}

// close frees the built-in model, if it was loaded.
func (g *generator) close() {
	if g == nil || g.engine == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		g.engine.Close()
		close(done)
	}()
	// the model may be partway through a step it can't stop; the process exiting frees it anyway
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		log.Printf("gorun web: the model is still busy; quitting without unloading it")
	}
}

// generateHandler asks the model for tasks that do what the form describes and queues them as
// unsaved changes, to be reviewed and saved like any other.
func (a *app) generateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/config", "Could not read the form.", true)
		return
	}
	request := strings.TrimSpace(r.PostFormValue("request"))
	file := r.PostFormValue("file")
	if request == "" {
		redirectWith(w, r, "/config", "Describe the tasks you want.", true)
		return
	}

	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	if cfg == nil {
		redirectWith(w, r, "/config", "Load a Gorunfile first.", true)
		return
	}
	if !slices.Contains(cfg.Files, file) {
		redirectWith(w, r, "/config", "Pick the file to save into.", true)
		return
	}

	if a.gen == nil {
		redirectWith(w, r, "/config", "Task generation is off.", true)
		return
	}
	// the model is slow and its state global, so requests wait their turn
	release, err := a.genQ.acquire(r.Context(), nil)
	if err != nil {
		redirectWith(w, r, "/config", "Could not generate tasks: "+err.Error(), true)
		return
	}
	text, err := a.gen.assistant.GenerateTasks(r.Context(), request, cfg)
	release()
	if err != nil {
		slog.ErrorContext(r.Context(), "generate tasks", "error", err)
		redirectWith(w, r, "/config", "Could not generate tasks: "+err.Error(), true)
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg != cfg {
		redirectWith(w, r, "/config", "Another Gorunfile was loaded meanwhile; generate again.", true)
		return
	}
	names, err := a.queueTasks(text, file, false)
	if err != nil {
		redirectWith(w, r, "/config", "The model's tasks can't be added: "+err.Error()+". Try again.", true)
		return
	}
	msg := fmt.Sprintf("Generated %d task(s): %s. Review them, edit what's wrong, then save.", len(names), strings.Join(names, ", "))
	redirectWith(w, r, "/config", msg, false)
}
