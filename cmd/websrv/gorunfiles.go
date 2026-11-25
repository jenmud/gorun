package websrv

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// gorunfilesPage lists the Gorunfiles kept in the history database.
func (a *app) gorunfilesPage(w http.ResponseWriter, r *http.Request) {
	msg, bad := flashFrom(r)
	d := ui.GorunfilesPageData{Flash: msg, FlashBad: bad, Open: a.currentStored()}
	list, err := a.store.Gorunfiles(r.Context())
	if err != nil {
		d.Flash, d.FlashBad = "Could not list the Gorunfiles: "+err.Error(), true
	}
	shares := a.storedShares(r.Context())
	for _, g := range list {
		row := ui.StoredGorunfileRow{Name: g.Name, Versions: g.Versions, SavedAt: g.SavedAt.Local().Format("2006-01-02 15:04"), Shares: shares[g.Name]}
		for _, other := range list {
			if slices.Contains(shares[other.Name], g.Name) {
				row.SharedBy = append(row.SharedBy, other.Name)
			}
		}
		d.Items = append(d.Items, row)
	}
	a.page(w, "Gorunfiles", "gorunfiles", ui.GorunfilesPage(d))
}

// storedShares maps each stored Gorunfile to the stored ones it includes.
func (a *app) storedShares(ctx context.Context) map[string][]string {
	out := map[string][]string{}
	list, _ := a.store.Gorunfiles(ctx)
	for _, g := range list {
		content, _, err := a.store.Gorunfile(ctx, g.Name)
		if err != nil {
			continue
		}
		var doc struct {
			Include []string `toml:"include"`
		}
		_, _ = toml.Decode(content, &doc)
		for _, inc := range doc.Include {
			if name, ok := strings.CutPrefix(inc, "stored::"); ok {
				out[g.Name] = append(out[g.Name], name)
			}
		}
	}
	return out
}

// newGorunfile saves a starter Gorunfile under a new name and opens it.
func (a *app) newGorunfile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if !storedNamePattern.MatchString(name) {
		redirectWith(w, r, "/gorunfiles", "A Gorunfile's name is letters, digits, - and _, starting with a letter or digit.", true)
		return
	}
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if _, ok, err := a.store.Gorunfile(r.Context(), name); err != nil || ok {
		redirectWith(w, r, "/gorunfiles", fmt.Sprintf("A Gorunfile called %s is kept already.", name), true)
		return
	}
	content := starterGorunfile(name)
	if _, err := a.store.SaveGorunfile(r.Context(), name, content, time.Now()); err != nil {
		redirectWith(w, r, "/gorunfiles", "Could not save it: "+err.Error(), true)
		return
	}
	if err := writeWorkingCopy(a.storedDir, name, content); err != nil {
		redirectWith(w, r, "/gorunfiles", "Could not write it: "+err.Error(), true)
		return
	}
	if err := a.loadConfig(a.storedFile(name)); err != nil {
		redirectWith(w, r, "/gorunfiles", "Could not open it: "+err.Error(), true)
		return
	}
	redirectWith(w, r, "/config", fmt.Sprintf("Created and opened %s. Add servers and tasks here, or import a Gorunfile.", name), false)
}

// openGorunfile makes another stored Gorunfile the open one. Unsaved changes are dropped, and the
// schedules of the one open before keep running.
func (a *app) openGorunfile(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	path, err := a.resolveStored(name)
	if err != nil {
		redirectWith(w, r, "/gorunfiles", err.Error(), true)
		return
	}
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if err := a.loadConfig(path); err != nil {
		redirectWith(w, r, "/gorunfiles", fmt.Sprintf("%s doesn't load: %v", name, err), true)
		return
	}
	a.refreshBackground()
	redirectWith(w, r, "/gorunfiles", fmt.Sprintf("Opened %s.", name), false)
}

// deleteGorunfile removes a stored Gorunfile that isn't open, and no other includes, with every
// version of it.
func (a *app) deleteGorunfile(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if _, err := a.resolveStored(name); err != nil {
		redirectWith(w, r, "/gorunfiles", err.Error(), true)
		return
	}
	if name == a.currentStored() {
		redirectWith(w, r, "/gorunfiles", "Open another Gorunfile before deleting this one.", true)
		return
	}
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	for other, shared := range a.storedShares(r.Context()) {
		if slices.Contains(shared, name) {
			redirectWith(w, r, "/gorunfiles", fmt.Sprintf("%s includes %s, so it can't be deleted.", other, name), true)
			return
		}
	}
	if err := a.store.DeleteGorunfile(r.Context(), name); err != nil {
		redirectWith(w, r, "/gorunfiles", "Could not delete it: "+err.Error(), true)
		return
	}
	_ = os.RemoveAll(filepath.Dir(a.storedFile(name)))
	a.refreshBackground()
	redirectWith(w, r, "/gorunfiles", fmt.Sprintf("Deleted %s.", name), false)
}
