package websrv

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"
	"testing/fstest"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
	"github.com/jenmud/gorun/internal/store"
)

// storedNamePattern is what a stored Gorunfile may be called: it names a folder, and appears in
// stored::<name> includes.
var storedNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// starterGorunfile is a new stored Gorunfile's first version.
func starterGorunfile(name string) string {
	return fmt.Sprintf(`# This Gorunfile is kept in gorun's history database. Add servers, tasks and bundles on the
# Config page, or have the assistant write tasks. To use the tasks and bundles of another
# Gorunfile kept here, include it: include = ["stored::<its name>"]
title = %q
`, name)
}

// pickStored returns the names of the Gorunfiles kept in st when the service should use them: it
// was given no Gorunfile and isn't live-only. A Gorunfile given to it is used as it is, and the
// database isn't touched for it.
func pickStored(ctx context.Context, configPath string, st *store.Store, live bool) ([]string, error) {
	if configPath != "" || st == nil || live {
		return nil, nil
	}
	return openStored(ctx, st)
}

// openStored saves a starter Gorunfile called default when the database keeps none, and returns
// the names of those it keeps, the most recently saved first. They are never written to files:
// runs, the editor and the schedulers read them from the database through gorun.StoredPath.
func openStored(ctx context.Context, st *store.Store) ([]string, error) {
	list, err := st.Gorunfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the stored Gorunfiles: %w", err)
	}
	if len(list) == 0 {
		if _, err := st.SaveGorunfile(ctx, store.DefaultGorunfile, starterGorunfile(store.DefaultGorunfile), time.Now()); err != nil {
			return nil, fmt.Errorf("save the starter Gorunfile: %w", err)
		}
		list = []store.StoredGorunfile{{Name: store.DefaultGorunfile}}
	}
	names := make([]string, 0, len(list))
	for _, g := range list {
		if storedNamePattern.MatchString(g.Name) {
			names = append(names, g.Name)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("the history database keeps no Gorunfile with a usable name")
	}
	return names, nil
}

// storedNameOf is the name of the stored Gorunfile path names, or "".
func (a *app) storedNameOf(path string) string {
	if !a.keepsStored {
		return ""
	}
	name, ok := gorun.StoredName(path)
	if !ok || !storedNamePattern.MatchString(name) {
		return ""
	}
	return name
}

// currentStored is the name of the stored Gorunfile that is open, or "" when the service runs a
// Gorunfile given to it.
// It doesn't take a.mu, so pages drawn while a.mu is held can call it.
func (a *app) currentStored() string {
	name, _ := a.openStored.Load().(string)
	return name
}

// usingStored reports whether the open Gorunfile is one kept in the history database.
func (a *app) usingStored() bool { return a.currentStored() != "" }

// storedText is the latest version of the stored Gorunfile called name.
func (a *app) storedText(name string) (string, error) {
	if !storedNamePattern.MatchString(name) {
		return "", fmt.Errorf("%q isn't a stored Gorunfile's name", name)
	}
	text, ok, err := a.store.Gorunfile(context.Background(), name)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("no Gorunfile called %s is kept in the history database", name)
	}
	return text, nil
}

// storedPage fills in the Config page's stored Gorunfile, and reports whether one is open.
func (a *app) storedPage(ctx context.Context, d *ui.ConfigPageData) bool {
	name := a.currentStored()
	if name == "" {
		return false
	}
	d.Stored, d.StoredName = true, name
	d.StoredVersions, _ = a.store.GorunfileVersions(ctx, name)
	return true
}

// newConfig is an empty config that reads stored Gorunfiles from the database when the service
// keeps them there.
func (a *app) newConfig() *gorun.Config {
	cfg := &gorun.Config{}
	if a.keepsStored {
		cfg.Stored = a.storedText
	}
	return cfg
}

// saveChanges applies changes to the Gorunfiles they name. Stored ones are edited in the
// database: the edits are checked to load first, so a bad one never becomes a version, then
// each edited Gorunfile is saved as a new version.
func (a *app) saveChanges(changes []gorun.Change) error {
	if !a.keepsStored {
		return gorun.SaveChanges(changes)
	}
	edited := map[string]string{}
	var order []string
	for _, ch := range changes {
		name := a.storedNameOf(ch.File)
		if name == "" {
			return fmt.Errorf("%s isn't a Gorunfile kept in the history database", ch.File)
		}
		text, ok := edited[name]
		if !ok {
			t, err := a.storedText(name)
			if err != nil {
				return err
			}
			text = t
			order = append(order, name)
		}
		text, err := gorun.EditText(text, []gorun.Change{ch})
		if err != nil {
			return fmt.Errorf("save %s: %w", name, err)
		}
		edited[name] = text
	}
	return a.saveStored(edited, order)
}

// saveStored saves the texts in edited, by name, as new versions of those stored Gorunfiles, in
// order, once the open Gorunfile is checked to load with them. A change reloads the schedulers of
// the others, as they may share its tasks.
func (a *app) saveStored(edited map[string]string, order []string) error {
	a.mu.Lock()
	path := a.cfgPath
	a.mu.Unlock()
	cfg := a.newConfig()
	cfg.Stored = func(name string) (string, error) {
		if text, ok := edited[name]; ok {
			return text, nil
		}
		return a.storedText(name)
	}
	if path != "" {
		if err := cfg.LoadFile(path); err != nil {
			return fmt.Errorf("it wouldn't load: %w", err)
		}
	}
	changed := false
	for _, name := range order {
		saved, err := a.store.SaveGorunfile(context.Background(), name, edited[name], time.Now())
		if err != nil {
			return fmt.Errorf("save %s: %w", name, err)
		}
		changed = changed || saved
	}
	if changed {
		a.refreshBackground()
	}
	return nil
}

// refreshBackground (re)starts the schedulers of the stored Gorunfiles that aren't open, so every
// one's schedules fire, and with the tasks they share as they are now.
func (a *app) refreshBackground() {
	if !a.keepsStored || a.store == nil {
		return
	}
	list, err := a.store.Gorunfiles(context.Background())
	if err != nil {
		slog.Error("list the stored Gorunfiles", "error", err)
		return
	}
	open := a.currentStored()
	keep := map[string]bool{}
	for _, g := range list {
		if g.Name == open || !storedNamePattern.MatchString(g.Name) {
			continue
		}
		keep[g.Name] = true
		a.startBackground(g.Name)
	}
	a.mu.Lock()
	var stale []*gorun.Scheduler
	for name, s := range a.bg {
		if !keep[name] {
			stale = append(stale, s)
			delete(a.bg, name)
		}
	}
	a.mu.Unlock()
	for _, s := range stale {
		s.Stop()
	}
}

// startBackground replaces the scheduler of the stored Gorunfile called name, which isn't open.
func (a *app) startBackground(name string) {
	cfg := a.newConfig()
	if err := cfg.LoadFile(gorun.StoredPath(name)); err != nil {
		slog.Error("stored Gorunfile doesn't load; its schedules don't fire", "name", name, "error", err)
		return
	}
	a.mu.Lock()
	old := a.bg[name]
	delete(a.bg, name)
	a.mu.Unlock()
	var status []gorun.ScheduleStatus
	if old != nil {
		status = old.Status()
		old.Stop()
	}
	s := a.newScheduler(cfg, status)
	if s == nil {
		return
	}
	a.mu.Lock()
	if a.bg == nil {
		a.bg = map[string]*gorun.Scheduler{}
	}
	a.bg[name] = s
	a.mu.Unlock()
}

// stopBackground stops the schedulers of the stored Gorunfiles that aren't open.
func (a *app) stopBackground() {
	a.mu.Lock()
	bg := a.bg
	a.bg = nil
	a.mu.Unlock()
	for _, s := range bg {
		s.Stop()
	}
}

// maxImport caps an uploaded Gorunfile or zip, and maxUnpacked what a zip unpacks to.
const (
	maxImport   = 10 << 20
	maxUnpacked = 50 << 20
)

// importHandler replaces the open stored Gorunfile with an uploaded one: a Gorunfile, or a zip
// holding one and the files it includes, such as an export. Its local includes are folded into
// one file. The old Gorunfile stays in the database as an earlier version.
func (a *app) importHandler(w http.ResponseWriter, r *http.Request) {
	name := a.currentStored()
	if name == "" {
		redirectWith(w, r, "/config", "Import is for the Gorunfiles kept in the history database: start gorun -serve without a Gorunfile.", true)
		return
	}
	fail := func(msg string) { redirectWith(w, r, "/config", "Could not import: "+msg, true) }

	r.Body = http.MaxBytesReader(w, r.Body, maxImport)
	f, _, err := r.FormFile("file")
	if err != nil {
		fail("pick a Gorunfile or a zip, at most 10 MB")
		return
	}
	data, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		fail(err.Error())
		return
	}
	files, root, err := unpackImport(data)
	if err != nil {
		fail(err.Error())
		return
	}
	text, err := flatten(files, root)
	if err != nil {
		fail(err.Error())
		return
	}

	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if err := a.saveStored(map[string]string{name: string(text)}, []string{name}); err != nil {
		fail(err.Error())
		return
	}
	if err := a.loadConfig(gorun.StoredPath(name)); err != nil {
		fail(err.Error())
		return
	}
	a.mu.Lock()
	n := len(a.cfg.Tasks)
	a.mu.Unlock()
	redirectWith(w, r, "/config", fmt.Sprintf("Imported a Gorunfile with %d tasks into %s. The one before is kept as an earlier version.", n, name), false)
}

// unpackImport reads an upload into memory and returns its files and the Gorunfile to load: the
// upload itself, or the Gorunfile nearest the top of a zip.
func unpackImport(data []byte) (fs.FS, string, error) {
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return fstest.MapFS{"Gorunfile": {Data: data, Mode: 0o600}}, "Gorunfile", nil
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", fmt.Errorf("read the zip: %w", err)
	}
	files, root, total := fstest.MapFS{}, "", 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(f.Name)
		if !fs.ValidPath(name) {
			return nil, "", fmt.Errorf("the zip holds %s, outside its folder", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, "", err
		}
		b, err := io.ReadAll(io.LimitReader(rc, int64(maxUnpacked-total)+1))
		_ = rc.Close()
		if err != nil {
			return nil, "", err
		}
		if total += len(b); total > maxUnpacked {
			return nil, "", errors.New("the zip unpacks to more than 50 MB")
		}
		files[name] = &fstest.MapFile{Data: b, Mode: 0o600}
		if path.Base(name) == "Gorunfile" && (root == "" || strings.Count(name, "/") < strings.Count(root, "/")) {
			root = name
		}
	}
	if root == "" {
		return nil, "", errors.New("the zip has no file named Gorunfile")
	}
	return files, root, nil
}
