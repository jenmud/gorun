package websrv

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jenmud/gorun"
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

// storedDir holds the working copies of the stored Gorunfiles, next to the history database, one
// folder each. Runs, the editor and the schedulers read them as they read any Gorunfile; the
// database is what lasts. It is absolute, as the paths LoadFile reports are, so storedNameOf can
// tell a working copy when the database path is relative.
func storedDir(dbPath string) string {
	dir := filepath.Join(filepath.Dir(dbPath), "gorunfiles")
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// openStored writes a working copy of every Gorunfile in st into dir, saving a starter one called
// default first when there are none. It returns their names, the most recently saved first.
func openStored(ctx context.Context, st *store.Store, dir string) ([]string, error) {
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
		if !storedNamePattern.MatchString(g.Name) {
			continue
		}
		content, _, err := st.Gorunfile(ctx, g.Name)
		if err != nil {
			return nil, err
		}
		if err := writeWorkingCopy(dir, g.Name, content); err != nil {
			return nil, err
		}
		names = append(names, g.Name)
	}
	return names, nil
}

// writeWorkingCopy writes content as the working copy of the stored Gorunfile called name.
func writeWorkingCopy(dir, name, content string) error {
	path := filepath.Join(dir, name, "Gorunfile")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// storedFile is the working copy of the stored Gorunfile called name.
func (a *app) storedFile(name string) string {
	return filepath.Join(a.storedDir, name, "Gorunfile")
}

// storedNameOf is the name of the stored Gorunfile whose working copy is path, or "".
func (a *app) storedNameOf(path string) string {
	if a.storedDir == "" || path == "" {
		return ""
	}
	rel, err := filepath.Rel(a.storedDir, path)
	if err != nil || filepath.Base(rel) != "Gorunfile" {
		return ""
	}
	name := filepath.Dir(rel)
	if !storedNamePattern.MatchString(name) {
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

// resolveStored finds a stored::<name> include: the working copy of the Gorunfile called name.
func (a *app) resolveStored(name string) (string, error) {
	if !storedNamePattern.MatchString(name) {
		return "", fmt.Errorf("%q isn't a stored Gorunfile's name", name)
	}
	path := a.storedFile(name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("no Gorunfile called %s is kept in the history database", name)
	}
	return path, nil
}

// newConfig is an empty config that can resolve stored::<name> includes when the service keeps
// Gorunfiles in its database.
func (a *app) newConfig() *gorun.Config {
	cfg := &gorun.Config{}
	if a.storedDir != "" {
		cfg.StoredIncludes = a.resolveStored
	}
	return cfg
}

// keepStored saves into the database the working copies of the stored Gorunfiles the open one
// reads: itself, and those it includes, whose shared tasks can be edited from it. It runs after
// every load, so each saved change becomes a version. A change reloads the others' schedulers,
// as they may share its tasks.
func (a *app) keepStored() {
	if a.storedDir == "" || a.store == nil {
		return
	}
	a.mu.Lock()
	var files []string
	if a.cfg != nil {
		files = slices.Clone(a.cfg.Files)
	}
	a.mu.Unlock()
	changed := false
	for _, path := range files {
		name := a.storedNameOf(path)
		if name == "" {
			continue
		}
		data, err := os.ReadFile(path)
		saved := false
		if err == nil {
			saved, err = a.store.SaveGorunfile(context.Background(), name, string(data), time.Now())
		}
		if err != nil {
			slog.Error("keep the stored Gorunfile", "name", name, "error", err)
			continue
		}
		changed = changed || saved
	}
	if changed {
		a.refreshBackground()
	}
}

// refreshBackground (re)starts the schedulers of the stored Gorunfiles that aren't open, so every
// one's schedules fire, and with the tasks they share as they are now.
func (a *app) refreshBackground() {
	if a.storedDir == "" || a.store == nil {
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
	if err := cfg.LoadFile(a.storedFile(name)); err != nil {
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
	stored := a.storedFile(name)
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
	dir, err := os.MkdirTemp("", "gorun-import-")
	if err != nil {
		fail(err.Error())
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()
	root, err := unpackImport(data, dir)
	if err != nil {
		fail(err.Error())
		return
	}
	if err := a.newConfig().LoadFile(root); err != nil {
		fail("it doesn't load: " + err.Error())
		return
	}
	text, err := flatten(root)
	if err != nil {
		fail(err.Error())
		return
	}
	// relative paths resolve from the stored Gorunfile's folder, so check it loads there
	check := filepath.Join(filepath.Dir(stored), "Gorunfile.import")
	defer func() { _ = os.Remove(check) }()
	if err := os.WriteFile(check, text, 0o600); err != nil {
		fail(err.Error())
		return
	}
	if err := a.newConfig().LoadFile(check); err != nil {
		fail("it doesn't load as one file next to the database: " + err.Error())
		return
	}

	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if _, err := a.store.SaveGorunfile(r.Context(), name, string(text), time.Now()); err != nil {
		fail(err.Error())
		return
	}
	if err := os.WriteFile(stored, text, 0o600); err != nil {
		fail(err.Error())
		return
	}
	if err := a.loadConfig(stored); err != nil {
		fail(err.Error())
		return
	}
	a.mu.Lock()
	n := len(a.cfg.Tasks)
	a.mu.Unlock()
	a.refreshBackground()
	redirectWith(w, r, "/config", fmt.Sprintf("Imported a Gorunfile with %d tasks into %s. The one before is kept as an earlier version.", n, name), false)
}

// unpackImport writes an upload into dir and returns the Gorunfile to load: the upload itself,
// or the Gorunfile nearest the top of a zip.
func unpackImport(data []byte, dir string) (string, error) {
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		path := filepath.Join(dir, "Gorunfile")
		return path, os.WriteFile(path, data, 0o600)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("read the zip: %w", err)
	}
	root, total := "", int64(0)
	for _, f := range zr.File {
		name := filepath.FromSlash(f.Name)
		if f.FileInfo().IsDir() {
			continue
		}
		if !filepath.IsLocal(name) {
			return "", fmt.Errorf("the zip holds %s, outside its folder", f.Name)
		}
		total += int64(f.UncompressedSize64)
		if total > maxUnpacked {
			return "", errors.New("the zip unpacks to more than 50 MB")
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxUnpacked))
		_ = rc.Close()
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			return "", err
		}
		if filepath.Base(name) == "Gorunfile" && (root == "" || strings.Count(name, string(filepath.Separator)) < strings.Count(root, string(filepath.Separator))) {
			root = name
		}
	}
	if root == "" {
		return "", errors.New("the zip has no file named Gorunfile")
	}
	return filepath.Join(dir, root), nil
}
