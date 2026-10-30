package nats

import "testing"

func TestUIEventCarriesTaskMetadataAndAttempts(t *testing.T) {
	ev := Event{
		Type:      "task",
		GorunUUID: "run-1",
		Server:    "web-1",
		Task:      "deploy",
		State:     1,
		Attempt:   2,
		Attempts:  3,
		TaskMeta: []TaskMeta{{
			Name:        "deploy",
			Description: "Deploy the app",
			Details:     "Replaces the running binary.",
			Rerun:       "unsafe",
		}},
	}

	got := ev.UIEvent()
	if got.Attempt != 2 || got.Attempts != 3 {
		t.Errorf("attempt = %d/%d, want 2/3", got.Attempt, got.Attempts)
	}
	if len(got.TaskMeta) != 1 {
		t.Fatalf("task metadata = %v, want one entry", got.TaskMeta)
	}
	meta := got.TaskMeta[0]
	if meta.Name != "deploy" || meta.Description != "Deploy the app" || meta.Details != "Replaces the running binary." || meta.Rerun != "unsafe" {
		t.Errorf("task metadata = %+v, want the event's description, details and rerun value", meta)
	}
}
