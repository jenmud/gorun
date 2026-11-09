// Package history is gorun's built-in SQLite implementation of gorun.Recorder: it saves runs to a
// SQLite database and reads them back. It is the same history the gorun CLI and web UI keep, so a
// program built on the library can record its runs (set Config.Recorder to a Store), show them,
// and have them appear in `gorun -history` and `gorun -web` too. Using it is optional: to keep
// runs anywhere else, implement gorun.Recorder yourself.
package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/envvars"
	inats "github.com/jenmud/gorun/internal/nats"
	"github.com/jenmud/gorun/internal/store"
)

// Run is one saved run: when it started and finished, and how its tasks went.
type Run = store.RunSummary

// RunDetail is a saved run with every server and task it ran.
type RunDetail = store.RunDetail

// Task is one task on one server in a saved run. State holds a gorun.TaskState value.
type Task = store.TaskDetail

var _ gorun.Recorder = (*Store)(nil)

// ErrNotFound is returned by Run and Task when nothing was saved under that name.
var ErrNotFound = errors.New("history: not found")

// writeTimeout bounds each save, so a stuck database never holds up a run.
const writeTimeout = 5 * time.Second

// Store is an open history database. It is a gorun.Recorder, so it can be set as
// Config.Recorder to save runs, and it is safe for concurrent use.
type Store struct {
	mu sync.Mutex // keeps a run's events in the order they arrived
	st *store.Store
}

// DefaultPath is the database the gorun CLI and web UI use: GORUN_DB_PATH, or
// ~/.gorun/gorun.db. Use it to share history with them.
func DefaultPath() string {
	if p := envvars.Get("GORUN_DB_PATH"); p != "" {
		return expandHome(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "gorun.db"
	}
	return filepath.Join(home, ".gorun", "gorun.db")
}

// Open opens the history database at path, creating it and its folder if needed.
func Open(path string) (*Store, error) {
	path = expandHome(path)
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("history: %w", err)
		}
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("history: open %s: %w", path, err)
	}
	return &Store{st: st}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.st.Close()
}

// Record saves one run event, as Config.Recorder receives it. Values that aren't gorun events
// are ignored. Saves use their own short deadline, so the end of a cancelled run is still recorded.
func (s *Store) Record(event any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	// events marshal to the same JSON as NATS carries (errors as their message), which the
	// store already knows how to save
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	var ev inats.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}
	inats.UpdateDatabase(ctx, s.st, &ev)
}

// Runs lists every saved run, newest first.
func (s *Store) Runs(ctx context.Context) ([]Run, error) {
	runs, err := s.st.ListRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("history: list runs: %w", err)
	}
	slices.SortStableFunc(runs, func(a, b Run) int { return b.StartedAt.Compare(a.StartedAt) })
	return runs, nil
}

// Run returns one saved run with its servers and tasks.
func (s *Store) Run(ctx context.Context, id string) (*RunDetail, error) {
	r, err := s.st.GetRun(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("run %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("history: run %s: %w", id, err)
	}
	return r, nil
}

// Task returns one task of a saved run, with its command, output and exit code.
func (s *Store) Task(ctx context.Context, runID, server, name string) (Task, error) {
	t, err := s.st.GetTask(ctx, runID, server, name)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("task %s on %s in run %s: %w", name, server, runID, ErrNotFound)
	}
	if err != nil {
		return Task{}, fmt.Errorf("history: task %s on %s in run %s: %w", name, server, runID, err)
	}
	return t, nil
}

// Delete removes one saved run and everything recorded for it.
func (s *Store) Delete(ctx context.Context, id string) error {
	if err := s.st.DeleteRun(ctx, id); err != nil {
		return fmt.Errorf("history: delete run %s: %w", id, err)
	}
	return nil
}

// Keep deletes all but the newest n runs.
func (s *Store) Keep(ctx context.Context, n int) error {
	if n < 0 {
		return fmt.Errorf("history: keep %d: must not be negative", n)
	}
	if err := s.st.TruncateRuns(ctx, n); err != nil {
		return fmt.Errorf("history: keep %d: %w", n, err)
	}
	return nil
}

// expandHome replaces a leading ~/ with the home folder.
func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}
