package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jenmud/gorun"
	inats "github.com/jenmud/gorun/internal/nats"
	"github.com/jenmud/gorun/internal/store"
)

// recordEvent saves an event to the history database, for tests that set up history directly.
// The CLI itself records through history.Store (see the history package). Writes use a context
// detached from the run context: quitting a run cancels that context, but the
// final task and process_finished events still need to be recorded so the
// history doesn't stay stuck on "running".
func recordEvent(ctx context.Context, st *store.Store, event any) {
	if st == nil {
		return
	}

	// a server error's message doesn't survive JSON (an error marshals as {}), so it
	// is stored directly
	if e, ok := event.(gorun.ServerErrorEvent); ok {
		if e.Err != nil {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = st.SetServerError(ctx, e.GorunUUID, e.Server, e.Err.Error())
		}
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	var ev inats.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	inats.UpdateDatabase(ctx, st, &ev)
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
