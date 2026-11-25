package websrv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/store"
)

func TestStoredGorunfileKeepsEachSave(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gorun.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	path := storedPath(dbPath)
	if err := openStored(ctx, st, path); err != nil {
		t.Fatal(err)
	}
	a := &app{store: st, hub: &hub{clients: map[chan string]struct{}{}}, runs: map[string]context.CancelFunc{}}
	if err := a.loadConfig(path); err != nil {
		t.Fatalf("the starter Gorunfile doesn't load: %v", err)
	}
	a.stored = a.cfgPath

	add := gorun.Change{Kind: kindTask, Name: "hello", File: a.cfgPath, Body: map[string]any{"cmd": "echo hi", "local": true}}
	if err := a.saveDirect(a.cfgPath, []gorun.Change{add}); err != nil {
		t.Fatal(err)
	}
	content, _, err := st.Gorunfile(ctx)
	if err != nil || !strings.Contains(content, "[tasks.hello]") {
		t.Fatalf("stored Gorunfile = %q, %v; want the new task", content, err)
	}
	if n, _ := st.GorunfileVersions(ctx); n != 2 {
		t.Errorf("versions = %d, want 2 (the starter and the save)", n)
	}

	// a restart writes the working copy again from the database
	if err := os.WriteFile(path, []byte("broken ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := openStored(ctx, st, path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != content {
		t.Errorf("working copy after a restart = %q, want the stored one", data)
	}
}
