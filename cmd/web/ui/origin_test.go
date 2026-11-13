package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jenmud/gorun/internal/store"
)

// The live dashboard shows who started each run and where, as soon as it starts.
func TestDashboardShowsWhoStartedARun(t *testing.T) {
	var state DashboardState
	state.Apply(store.Event{Type: "process_started", GorunUUID: "r1", Title: "deploy",
		Origin: &store.Origin{User: "jen", Hostname: "laptop", Trigger: "web", WebUser: "sam", Client: "192.0.2.7",
			Version: "v0.21.0", Git: &store.GitInfo{Commit: "abc1234def", Branch: "main"}}})
	var buf bytes.Buffer
	if err := Dashboard(state).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"jen@laptop", "web user sam", "web", "v0.21.0", "main@abc1234", "Who and where", "192.0.2.7"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("dashboard does not show %q", want)
		}
	}
}

// A run with no origin (from an older gorun) shows no origin line or panel.
func TestDashboardWithoutOrigin(t *testing.T) {
	var state DashboardState
	state.Apply(store.Event{Type: "process_started", GorunUUID: "r1", Title: "deploy"})
	var buf bytes.Buffer
	if err := Dashboard(state).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "data-origin") {
		t.Error("a run without an origin shows an origin")
	}
}
