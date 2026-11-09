package websrv

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// addEntity handles an add form on a page: the new item is written to the chosen file at once,
// and the page it came from is shown again.
func (a *app) addEntity(kind, back string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			redirectWith(w, r, back, "Could not read the form.", true)
			return
		}
		a.saveMu.Lock()
		defer a.saveMu.Unlock()

		a.mu.Lock()
		cfg, pending := a.cfg, slices.Clone(a.pending)
		a.mu.Unlock()

		name := strings.TrimSpace(r.PostFormValue("name"))
		fail := func(msg string) { redirectWith(w, r, back, msg, true) }
		switch {
		case cfg == nil:
			fail("Load a Gorunfile first.")
			return
		case validName(name) != nil:
			fail(validName(name).Error())
			return
		case pendingConflict(pending, kind, name) != "":
			fail(pendingConflict(pending, kind, name))
			return
		case present(cfg, pending, kind, name):
			fail(fmt.Sprintf("A %s named %s already exists.", kindNames[kind], name))
			return
		}
		file := r.PostFormValue("file")
		if !slices.Contains(cfg.Files, file) {
			fail("Pick the file to save into.")
			return
		}
		change, err := newChange(kind, name, file, r.PostForm)
		if err == nil {
			err = checkChangeIn(cfg, change)
		}
		if err == nil {
			err = a.saveDirect(file, []gorun.Change{change})
		}
		if err != nil {
			fail(err.Error())
			return
		}
		redirectWith(w, r, back, fmt.Sprintf("Added %s %s.", kindNames[kind], name), false)
	}
}

// editEntity handles an edit form on a page. The item's table is rewritten in the file that
// defines it, keeping the settings the form doesn't show.
func (a *app) editEntity(kind, back string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			redirectWith(w, r, back, "Could not read the form.", true)
			return
		}
		a.saveMu.Lock()
		defer a.saveMu.Unlock()

		name := r.PostFormValue("name")
		editBack := back + "?" + url.Values{"edit": {name}}.Encode()
		fail := func(msg string) { redirectWith(w, r, editBack, msg, true) }

		a.mu.Lock()
		cfg, pending := a.cfg, slices.Clone(a.pending)
		a.mu.Unlock()
		if cfg == nil {
			redirectWith(w, r, back, "Load a Gorunfile first.", true)
			return
		}
		if msg := pendingConflict(pending, kind, name); msg != "" {
			fail(msg)
			return
		}
		body, err := currentBody(cfg, pending, kind, name)
		if err == nil {
			err = applyEdit(kind, body, r.PostForm)
		}
		if err == nil {
			err = checkChangeIn(cfg, gorun.Change{Kind: kind, Name: name, Body: body})
		}
		if err == nil {
			file := sourceOf(cfg, pending, kind, name)
			err = a.saveDirect(file, []gorun.Change{
				{Kind: kind, Name: name, File: file, Remove: true},
				{Kind: kind, Name: name, File: file, Body: body},
			})
		}
		if err != nil {
			fail(err.Error())
			return
		}
		redirectWith(w, r, back, fmt.Sprintf("Updated %s %s.", kindNames[kind], name), false)
	}
}

// saveDirect writes changes into file and reloads the Gorunfile. If the result no longer loads,
// the file is put back as it was, so a bad edit never leaves the Gorunfile broken. The caller
// holds a.saveMu.
func (a *app) saveDirect(file string, changes []gorun.Change) error {
	orig, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	if err := gorun.SaveChanges(changes); err != nil {
		return err
	}
	if err := a.reloadConfig(); err != nil {
		if werr := os.WriteFile(file, orig, info.Mode().Perm()); werr != nil {
			return fmt.Errorf("%w (and restoring %s failed: %v)", err, file, werr)
		}
		if rerr := a.reloadConfig(); rerr != nil {
			return fmt.Errorf("%w (and reloading the restored file failed: %v)", err, rerr)
		}
		return err
	}
	return nil
}

// fileOptions lists the files of the loaded Gorunfile that an add form can save into.
func fileOptions(cfg *gorun.Config) []ui.FileOption {
	out := make([]ui.FileOption, 0, len(cfg.Files))
	for _, f := range cfg.Files {
		out = append(out, ui.FileOption{Path: f, Label: rel(cfg.Path, f)})
	}
	return out
}

// editPanel is the edit state of a page's item: the form values, or the reason it can't be
// edited here.
func editPanel(cfg *gorun.Config, pending []gorun.Change, kind, name string) (ui.EditFields, string) {
	if msg := pendingConflict(pending, kind, name); msg != "" {
		return ui.EditFields{}, msg
	}
	body, err := currentBody(cfg, pending, kind, name)
	if err != nil {
		return ui.EditFields{}, err.Error()
	}
	return editFields(body), ""
}
