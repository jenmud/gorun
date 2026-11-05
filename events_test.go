package gorun

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestServerErrorEventPublishesTheMessage(t *testing.T) {
	data, err := json.Marshal(ServerErrorEvent{Type: "server_error", GorunUUID: "r1", Server: "web-1", Err: errors.New("dial tcp: connection refused")})
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Error string }
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%s: %v", data, err)
	}
	if !strings.Contains(got.Error, "connection refused") {
		t.Errorf("published %s, want the error message", data)
	}
}

func TestTaskEventPublishesTheSkipReason(t *testing.T) {
	data, err := json.Marshal(TaskEvent{Type: "task", Task: "deploy", State: TaskSkipped, Err: errors.New("dependency build failed")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"error":"dependency build failed"`) || !strings.Contains(string(data), `"task":"deploy"`) {
		t.Errorf("published %s, want the reason and the task's other fields", data)
	}
}
