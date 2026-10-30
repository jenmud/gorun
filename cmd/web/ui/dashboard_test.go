package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDashboardPlanCarriesTaskMetadata(t *testing.T) {
	var state DashboardState
	state.Apply(Event{Type: "process_started", GorunUUID: "run-1", Title: "demo"})
	state.Apply(Event{
		Type:      "plan",
		GorunUUID: "run-1",
		Server:    "web-1",
		Tasks:     []string{"deploy"},
		TaskMeta: []TaskMeta{{
			Name:        "deploy",
			Description: "Deploy the app",
			Details:     "Replaces the running binary.",
			Rerun:       "unsafe",
		}},
	})

	process := state.Processes["run-1"]
	if process == nil {
		t.Fatal("expected the run in the dashboard")
	}
	server := process.Servers["web-1"]
	if server == nil {
		t.Fatal("expected the server in the dashboard")
	}
	task := server.Tasks["deploy"]
	if task == nil {
		t.Fatal("expected the task in the dashboard")
	}
	if task.Description != "Deploy the app" || task.Details != "Replaces the running binary." || task.Rerun != "unsafe" {
		t.Errorf("task metadata = %+v, want the plan's description, details and rerun value", task)
	}

	var buf bytes.Buffer
	if err := Dashboard(state).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Deploy the app", "Replaces the running binary.", "Not safe to rerun"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("dashboard output does not contain %q", want)
		}
	}
}
