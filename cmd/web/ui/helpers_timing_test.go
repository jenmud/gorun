package ui

import (
	"testing"
	"time"

	"github.com/jenmud/gorun/internal/store"
)

func TestTaskTimingNamesCheckStates(t *testing.T) {
	for _, c := range []struct {
		task TaskInfo
		want string
	}{
		{TaskInfo{State: store.StateUnchanged, Duration: int64(45 * time.Millisecond)}, "unchanged · 45ms"},
		{TaskInfo{State: store.StateWouldRun, Duration: int64(12 * time.Millisecond)}, "would run · 12ms"},
		{TaskInfo{State: store.StateWouldRun, Duration: 300}, "would run"}, // no check: nothing ran
		{TaskInfo{State: store.StateRunning, Phase: "check"}, "checking"},
	} {
		if got := taskTiming(&c.task); got != c.want {
			t.Errorf("state %d: got %q, want %q", c.task.State, got, c.want)
		}
	}
}
