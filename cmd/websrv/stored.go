package websrv

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jenmud/gorun/internal/store"
)

// starterGorunfile is the stored Gorunfile's first version, on a new history database.
const starterGorunfile = `# This Gorunfile is kept in gorun's history database: gorun -serve started without one.
# Add servers, tasks and bundles on the Config page, or have the assistant write tasks.
title = "gorun"
`

// storedPath is the working copy of the stored Gorunfile, next to the history database. Runs,
// the editor and the scheduler read it as they read any Gorunfile; the database is what lasts.
func storedPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "gorunfile", "Gorunfile")
}

// openStored writes the newest Gorunfile in st to path, saving the starter one first when the
// database has none.
func openStored(ctx context.Context, st *store.Store, path string) error {
	content, ok, err := st.Gorunfile(ctx)
	if err != nil {
		return fmt.Errorf("read the stored Gorunfile: %w", err)
	}
	if !ok {
		content = starterGorunfile
		if _, err := st.SaveGorunfile(ctx, content, time.Now()); err != nil {
			return fmt.Errorf("save the starter Gorunfile: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// keepStored saves the working copy into the database when the stored Gorunfile is the one
// loaded. It runs after every load, so each saved change becomes a version.
func (a *app) keepStored() {
	a.mu.Lock()
	path, stored := a.cfgPath, a.stored
	a.mu.Unlock()
	if stored == "" || path != stored || a.store == nil {
		return
	}
	data, err := os.ReadFile(path)
	if err == nil {
		_, err = a.store.SaveGorunfile(context.Background(), string(data), time.Now())
	}
	if err != nil {
		slog.Error("keep the stored Gorunfile", "path", path, "error", err)
	}
}
