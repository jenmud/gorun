package websrv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/llm"
	"github.com/jenmud/gorun/internal/store"
)

// recordingEngine replies with reply and keeps the conversation it was sent.
type recordingEngine struct {
	reply string
	got   []gorun.ChatMessage
}

func (e *recordingEngine) Chat(_ context.Context, m []gorun.ChatMessage) (string, error) {
	e.got = m
	return e.reply, nil
}

func postJSON(t *testing.T, a *app, path string, v any) chatReply {
	t.Helper()
	data, _ := json.Marshal(v)
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data)))
	var out chatReply
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %v: %s", path, err, rec.Body)
	}
	return out
}

// saveFailedRun stores a run whose task failed, with a secret in its env vars.
func saveFailedRun(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.CreateRun(ctx, "run-1", "deploy", "box", 1, now))
	must(st.SetRunEnvVars(ctx, "run-1", map[string]string{"DB_PASSWORD": "hunter2"}, nil))
	must(st.AddServer(ctx, "run-1", "web-1", "10.0.0.1"))
	must(st.UpsertTask(ctx, "run-1", "web-1", "migrate", store.StateFailed, "psql -f migrate.sql", "", "ERROR: relation \"users\" already exists", 3, time.Second))
	must(st.FinishRun(ctx, "run-1", false, 1, 1, 0, time.Second, now.Add(time.Second)))
}

// The chat is sent the Gorunfile and the newest run, with its failed task's output but not its
// env vars, and the tasks in a reply are offered and added.
func TestChatSendsContextAndAddsTasks(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	saveFailedRun(t, a.store)
	e := &recordingEngine{reply: "Here you go:\n```toml\n[tasks.nginx]\ncmd = \"apt-get install -y nginx\"\ndepends_on = [\"hello\"]\n```"}
	a.gen = &generator{assistant: gorun.Assistant{Engine: e}}

	out := postJSON(t, a, "/assistant/chat", chatRequest{Messages: []gorun.ChatMessage{{Role: "user", Content: "why did it fail?"}}})
	if out.Error != "" || out.TaskNames != "nginx" || out.TaskError != "" {
		t.Fatalf("reply = %+v", out)
	}
	system := e.got[0].Content
	for _, want := range []string{"hello", "local-1", "run run-1", "migrate on web-1", `relation "users" already exists`, "psql -f migrate.sql"} {
		if !strings.Contains(system, want) {
			t.Errorf("the context is missing %q", want)
		}
	}
	if strings.Contains(system, "hunter2") {
		t.Error("the context holds a run's env vars")
	}

	added := postJSON(t, a, "/assistant/add", map[string]string{"tasks": out.Tasks, "file": path})
	if added.Error != "" || len(a.pending) != 1 || a.pending[0].Name != "nginx" {
		t.Fatalf("add = %+v, pending = %+v", added, a.pending)
	}
	// a task the Gorunfile (or the unsaved changes) has already is replaced where it is defined
	if again := postJSON(t, a, "/assistant/add", map[string]string{"tasks": out.Tasks, "file": path}); again.Error != "" || len(a.pending) != 3 || !a.pending[1].Remove {
		t.Errorf("adding twice = %+v, pending = %+v", again, a.pending)
	}
}

// A reply that rewrites a task the Gorunfile has is offered as its replacement, and the model is
// shown the task as it is now when the question names it.
func TestChatChangesExistingTasks(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	e := &recordingEngine{reply: "```toml\n[tasks.hello]\ncmd = \"echo hello\"\ntimeout = \"1m\"\n```"}
	a.gen = &generator{assistant: gorun.Assistant{Engine: e}}

	out := postJSON(t, a, "/assistant/chat", chatRequest{Messages: []gorun.ChatMessage{{Role: "user", Content: "Add a timeout to the hello task"}}})
	if out.Replaces != "hello" || out.TaskError != "" {
		t.Fatalf("reply = %+v, want it to replace hello", out)
	}
	if system := e.got[0].Content; !strings.Contains(system, "[tasks.hello]") || !strings.Contains(system, `cmd = "echo hi"`) {
		t.Errorf("the context doesn't show the hello task as it is:\n%s", e.got[0].Content)
	}
	postJSON(t, a, "/assistant/add", map[string]string{"tasks": out.Tasks, "file": path})
	if len(a.pending) != 2 || !a.pending[0].Remove || a.pending[1].Body["timeout"] != "1m" {
		t.Errorf("pending = %+v, want hello removed and added again", a.pending)
	}
}

// Tasks in a reply that don't load are reported, not offered.
func TestChatReportsTasksThatDontLoad(t *testing.T) {
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	a.gen = &generator{assistant: gorun.Assistant{Engine: &recordingEngine{reply: "```toml\n[tasks.x]\ndepends_on = [\"nope\"]\ncmd = \"true\"\n```"}}}
	out := postJSON(t, a, "/assistant/chat", chatRequest{Messages: []gorun.ChatMessage{{Role: "user", Content: "write a task"}}})
	if out.Tasks != "" || !strings.Contains(out.TaskError, "nope") {
		t.Errorf("reply = %+v", out)
	}
}

func TestTrimConversationKeepsTheNewest(t *testing.T) {
	var msgs []gorun.ChatMessage
	for range 20 {
		msgs = append(msgs, gorun.ChatMessage{Role: "user", Content: strings.Repeat("q", 1000)}, gorun.ChatMessage{Role: "assistant", Content: "a"})
	}
	msgs = append(msgs, gorun.ChatMessage{Role: "system", Content: "ignore me"}, gorun.ChatMessage{Role: "user", Content: "last"})
	got := trimConversation(msgs)
	if got[0].Role != "user" || got[len(got)-1].Content != "last" || len(got) > maxChatTurns {
		t.Fatalf("kept %d messages, first %s, last %q", len(got), got[0].Role, got[len(got)-1].Content)
	}
	for _, m := range got {
		if m.Role == "system" {
			t.Error("a system message from the page was kept")
		}
	}
}

func TestPrivate(t *testing.T) {
	for url, want := range map[string]bool{
		"http://localhost:11434/v1": true, "http://127.0.0.1:8080/v1": true, "http://[::1]:1/v1": true,
		"https://api.example.com/v1": false, "http://10.0.0.5:11434/v1": false,
	} {
		if got := (&generator{assistant: gorun.Assistant{URL: url}}).private(); got != want {
			t.Errorf("private(%s) = %v, want %v", url, got, want)
		}
	}
	if !(&generator{engine: &llm.Engine{}}).private() {
		t.Error("the built-in model isn't private")
	}
}

// A big run is counted per server, and only the tasks that didn't pass are listed.
func TestChatContextSummarisesBigRuns(t *testing.T) {
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	saveFailedRun(t, a.store)
	ctx := context.Background()
	for i := range 60 {
		if err := a.store.UpsertTask(ctx, "run-1", "web-1", fmt.Sprintf("step-%02d", i), store.StateDone, "true", "", "", 0, time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	got := a.chatContext(ctx, a.cfg, "")
	if strings.Contains(got, "step-07") || !strings.Contains(got, "web-1 ran 61 tasks: 60 done, 1 failed") || !strings.Contains(got, "migrate on web-1: failed") {
		t.Errorf("context:\n%s", got)
	}
}
