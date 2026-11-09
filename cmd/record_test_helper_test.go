package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jenmud/gorun/internal/store"
)

// recordEvent saves an event to the history database, for tests that set up history directly.
// The CLI itself records through history.Store.
func recordEvent(ctx context.Context, st *store.Store, event any) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	var ev store.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}
	st.Apply(context.WithoutCancel(ctx), &ev)
}

// GORUN_DB_PATH is honoured before the file exists, so the CLI writes where the web UI reads.
func TestHistoryPathHonoursGORUN_DB_PATHBeforeItExists(t *testing.T) {
	want := filepath.Join(t.TempDir(), "sub", "history.db")
	t.Setenv("GORUN_DB_PATH", want)
	if got := getHistoryDBPath(); got != want {
		t.Errorf("getHistoryDBPath() = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Dir(want)); err != nil {
		t.Errorf("its folder was not created: %v", err)
	}
}
