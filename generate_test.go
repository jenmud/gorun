package gorun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeModel answers chat completions with replies in turn, and keeps the requests it got.
func fakeModel(t *testing.T, replies ...string) (*httptest.Server, *[][]ChatMessage) {
	t.Helper()
	var got [][]ChatMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var req struct{ Messages []ChatMessage }
		_ = json.NewDecoder(r.Body).Decode(&req)
		got = append(got, req.Messages)
		reply := replies[min(len(got), len(replies))-1]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": ChatMessage{Role: "assistant", Content: reply}}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestGenerateTasksRetriesUntilValid(t *testing.T) {
	bad := "[tasks.redis]\ndescription = \"Install redis\"\nsudo = true\n"
	good := "```toml\n[tasks.redis]\ndescription = \"Install redis\"\ncmd = \"apt-get install -y redis\"\ndepends_on = [\"update\"]\nsudo = true\n```"
	srv, got := fakeModel(t, bad, good)

	cfg := &Config{Tasks: map[string]Task{"update": {Description: "Update packages", Cmd: "apt-get update"}}}
	a := Assistant{URL: srv.URL + "/v1", Model: "test"}
	text, err := a.GenerateTasks(context.Background(), "install redis", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "```") || !strings.Contains(text, `cmd = "apt-get install -y redis"`) {
		t.Errorf("GenerateTasks() = %q, want the fixed task without fences", text)
	}
	if len(*got) != 2 {
		t.Fatalf("asked %d times, want 2", len(*got))
	}
	if system := (*got)[0][0].Content; !strings.Contains(system, "- update: Update packages") {
		t.Errorf("system prompt doesn't list the existing tasks:\n%s", system)
	}
	if retry := (*got)[1]; !strings.Contains(retry[len(retry)-1].Content, "no command specified") {
		t.Errorf("retry doesn't say what was wrong: %q", retry[len(retry)-1].Content)
	}
}

func TestGenerateTasksGivesUp(t *testing.T) {
	srv, got := fakeModel(t, "I can't help with that")
	a := Assistant{URL: srv.URL + "/v1", Attempts: 2}
	if _, err := a.GenerateTasks(context.Background(), "anything", nil); err == nil {
		t.Fatal("GenerateTasks() succeeded on a reply that isn't TOML")
	}
	if len(*got) != 2 {
		t.Errorf("asked %d times, want 2", len(*got))
	}
}

func TestExistingTasksKeepsTheClosest(t *testing.T) {
	cfg := &Config{Tasks: map[string]Task{"restart-app": {Description: "Restart the app"}}}
	for i := range 2 * maxExistingTasks {
		cfg.Tasks[fmt.Sprintf("tool-%02d", i)] = Task{Description: "Install a tool"}
	}
	list := existingTasks(cfg, "restart the app")
	if !strings.Contains(list, "- restart-app:") {
		t.Errorf("the matching task isn't listed:\n%s", list)
	}
	if n := strings.Count(list, "\n- "); n != maxExistingTasks {
		t.Errorf("listed %d tasks, want %d", n, maxExistingTasks)
	}
}

func TestCheckTasks(t *testing.T) {
	cfg := &Config{Tasks: map[string]Task{"update": {Cmd: "apt-get update"}}}
	tests := []struct {
		name, text, wantErr string
	}{
		{"valid", "[tasks.a]\ncmd = \"true\"\ndepends_on = [\"update\"]", ""},
		{"depends on a new task", "[tasks.a]\ncmd = \"true\"\n[tasks.b]\ncmd = \"true\"\ndepends_on = [\"a\"]", ""},
		{"not toml", "sure, here you go", "invalid TOML"},
		{"no tasks", "name = \"x\"", "unknown key"},
		{"empty", "", "no [tasks.<name>]"},
		{"unknown key", "[tasks.a]\ncmd = \"true\"\ncommand = \"x\"", "unknown key tasks.a.command"},
		{"missing dependency", "[tasks.a]\ncmd = \"true\"\ndepends_on = [\"nope\"]", "nope, which doesn't exist"},
		{"redefines a task", "[tasks.update]\ncmd = \"true\"", "already exists"},
		{"bad rerun", "[tasks.a]\ncmd = \"true\"\nrerun = \"maybe\"", "rerun must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkTasks(tt.text, cfg, false)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("checkTasks() = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("checkTasks() = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestExtractTaskEditsReplacesExistingTasks(t *testing.T) {
	cfg := &Config{Tasks: map[string]Task{"update": {Cmd: "apt-get update"}}}
	reply := "```toml\n[tasks.update]\ncmd = \"apt-get update\"\ntimeout = \"10m\"\n```"
	if _, err := ExtractTasks(reply, cfg); err == nil {
		t.Error("ExtractTasks() accepted a task that exists")
	}
	if _, err := ExtractTaskEdits(reply, cfg); err != nil {
		t.Errorf("ExtractTaskEdits() = %v, want the replacement accepted", err)
	}
}

func TestExtractTasksSkipsShellBlocks(t *testing.T) {
	reply := "Stop it first:\n```bash\nsudo systemctl stop nginx\n```\nThen add:\n```toml\n[tasks.a]\ncmd = \"true\"\n```\n"
	got, err := ExtractTasks(reply, nil)
	if err != nil {
		t.Fatalf("ExtractTasks() error = %v", err)
	}
	if got != "[tasks.a]\ncmd = \"true\"\n" {
		t.Errorf("ExtractTasks() = %q", got)
	}
}

func TestGenerateTasksReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `model "x" not found`, http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := Assistant{URL: srv.URL}.GenerateTasks(context.Background(), "x", nil)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("GenerateTasks() = %v, want the server's error", err)
	}
}

func TestExtractTasksDropsABooleanCopy(t *testing.T) {
	reply := "```toml\n[tasks.page]\ncmd = \"mv /tmp/i.html /var/www/\"\ncopy = false\n[[tasks.page.copy]]\ndst = \"/tmp/i.html\"\ncontent = \"<p>hi</p>\"\n```"
	got, err := ExtractTasks(reply, nil)
	if err != nil {
		t.Fatalf("ExtractTasks() error = %v", err)
	}
	if strings.Contains(got, "copy = false") || !strings.Contains(got, "[[tasks.page.copy]]") {
		t.Errorf("ExtractTasks() = %q", got)
	}
}
