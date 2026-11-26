package websrv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
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
	e := &recordingEngine{reply: "Here you go:\n```toml\n[tasks.nginx]\ncmd = \"apt-get install -y nginx\"\ncheck = \"false\"\nverify = \"true\"\ndepends_on = [\"hello\"]\n```"}
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

	// adding saves them straight into the Gorunfile, with nothing left to save on the Config page
	added := postJSON(t, a, "/assistant/add", map[string]string{"tasks": out.Tasks, "file": path})
	if added.Error != "" || !strings.Contains(added.Reply, "Saved nginx") || len(a.pending) != 0 {
		t.Fatalf("add = %+v, pending = %+v", added, a.pending)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "[tasks.nginx]") {
		t.Errorf("the Gorunfile doesn't have the task:\n%s", data)
	}
	if _, ok := a.cfg.Tasks["nginx"]; !ok {
		t.Error("the loaded config doesn't have the task")
	}
	// adding again replaces it where it is defined
	if again := postJSON(t, a, "/assistant/add", map[string]string{"tasks": out.Tasks, "file": path}); again.Error != "" {
		t.Errorf("adding twice = %+v", again)
	}
	if data, _ := os.ReadFile(path); strings.Count(string(data), "[tasks.nginx]") != 1 {
		t.Errorf("adding twice left:\n%s", data)
	}
	// a task with unsaved changes on the Config page is left alone
	a.pending = []gorun.Change{{Kind: kindTask, Name: "nginx", File: path, Remove: true}}
	if busy := postJSON(t, a, "/assistant/add", map[string]string{"tasks": out.Tasks, "file": path}); !strings.Contains(busy.Error, "unsaved changes") {
		t.Errorf("adding over unsaved changes = %+v", busy)
	}
}

// A reply that rewrites a task the Gorunfile has is offered as its replacement, and the model is
// shown the task as it is now when the question names it.
func TestChatChangesExistingTasks(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	e := &recordingEngine{reply: "```toml\n[tasks.hello]\ncmd = \"echo hello\"\ncheck = \"false\"\nverify = \"true\"\ntimeout = \"1m\"\n```"}
	a.gen = &generator{assistant: gorun.Assistant{Engine: e}}

	out := postJSON(t, a, "/assistant/chat", chatRequest{Messages: []gorun.ChatMessage{{Role: "user", Content: "Add a timeout to the hello task"}}})
	if out.Replaces != "hello" || out.TaskError != "" {
		t.Fatalf("reply = %+v, want it to replace hello", out)
	}
	if system := e.got[0].Content; !strings.Contains(system, "[tasks.hello]") || !strings.Contains(system, `cmd = "echo hi"`) {
		t.Errorf("the context doesn't show the hello task as it is:\n%s", e.got[0].Content)
	}
	postJSON(t, a, "/assistant/add", map[string]string{"tasks": out.Tasks, "file": path})
	if got := a.cfg.Tasks["hello"]; got.Timeout != time.Minute {
		t.Errorf("hello = %+v, want it saved with the timeout", got)
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

// streamingEngine reports a stage and writes its reply in pieces, as the built-in engine does.
type streamingEngine struct{}

func (streamingEngine) Chat(ctx context.Context, _ []gorun.ChatMessage) (string, error) {
	trace := gorun.ChatTraceFrom(ctx)
	trace.Statusf("Loading the model")
	for _, p := range []string{"Port 80 ", "is taken."} {
		trace.Piece(p)
	}
	return "Port 80 is taken.", nil
}

// A streamed chat sends what the assistant is doing, the reply as it is written, then the reply.
func TestChatStreamsProgress(t *testing.T) {
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	a.gen = &generator{assistant: gorun.Assistant{Engine: streamingEngine{}}}

	data, _ := json.Marshal(chatRequest{Messages: []gorun.ChatMessage{{Role: "user", Content: "why?"}}, Stream: true})
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/assistant/chat", bytes.NewReader(data)))
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var statuses, tokens []string
	var done *chatReply
	for _, line := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n") {
		var ev chatEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		if ev.Status != "" {
			statuses = append(statuses, ev.Status)
		}
		if ev.Token != "" {
			tokens = append(tokens, ev.Token)
		}
		if ev.Done != nil {
			done = ev.Done
		}
	}
	if !slices.Contains(statuses, "Loading the model") || strings.Join(tokens, "") != "Port 80 is taken." {
		t.Errorf("statuses = %q, tokens = %q", statuses, tokens)
	}
	if done == nil || done.Reply != "Port 80 is taken." {
		t.Errorf("done = %+v", done)
	}
}

func TestTaskBlockNeedsACodeBlock(t *testing.T) {
	for _, tc := range []struct {
		reply string
		want  bool
	}{
		{"```toml\n[tasks.a]\ncmd = \"true\"\n```", true},
		{"Stop it first:\n```bash\nsudo systemctl stop nginx\n```\nthen:\n```toml\n[tasks.a]\ncmd = \"x\"\n```", true},
		{"[tasks.site]\nDeploys the site's index page.", false},
		{"```bash\nls\n```\n[tasks.site] deploys it", false},
	} {
		if got := hasTaskBlock(tc.reply); got != tc.want {
			t.Errorf("hasTaskBlock(%q) = %v, want %v", tc.reply, got, tc.want)
		}
	}
}

// The stats panel says where the model stands before the first question, and for a server.
func TestAssistantStatsPanel(t *testing.T) {
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	a.gen = &generator{engine: &llm.Engine{Dir: t.TempDir()}}
	a.gen.assistant.Engine = a.gen.engine
	page := get(t, a, "/assistant").Body.String()
	for _, want := range []string{`id="assistant-stats"`, "Not downloaded yet", "Qwen3-4B-Instruct-2507-Q4_K_M.gguf"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Assistant page is missing %q", want)
		}
	}
	if body := get(t, a, "/assistant/stats").Body.String(); !strings.Contains(body, "Not downloaded yet") || strings.Contains(body, "<html") {
		t.Errorf("/assistant/stats = %q, want just the panel", body)
	}

	a.gen = &generator{assistant: gorun.Assistant{URL: "http://gpu-box:11434/v1", Model: "qwen3:4b"}}
	if body := get(t, a, "/assistant/stats").Body.String(); !strings.Contains(body, "Answered by a model server") || !strings.Contains(body, "questions are sent there") {
		t.Errorf("server stats = %q", body)
	}
}

// sequenceEngine answers with each of its replies in turn, keeping what it was asked.
type sequenceEngine struct {
	replies []string
	asked   [][]gorun.ChatMessage
}

func (e *sequenceEngine) Chat(_ context.Context, m []gorun.ChatMessage) (string, error) {
	e.asked = append(e.asked, m)
	r := e.replies[0]
	if len(e.replies) > 1 {
		e.replies = e.replies[1:]
	}
	return r, nil
}

// Tasks that don't load are sent back to the model with the error, and the fixed ones offered.
func TestChatRepairsTasksThatDontLoad(t *testing.T) {
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	broken := "Here they are:\n```toml\n[tasks.page]\ncmd = '''python3 -c \"print('''hi''')\"'''\n```\nRun page first."
	fixed := "```toml\n[tasks.page]\ncmd = \"echo hi\"\ncheck = \"false\"\nverify = \"true\"\n```"
	e := &sequenceEngine{replies: []string{broken, fixed}}
	a.gen = &generator{assistant: gorun.Assistant{Engine: e}}

	out := postJSON(t, a, "/assistant/chat", chatRequest{Messages: []gorun.ChatMessage{{Role: "user", Content: "a page"}}})
	if out.TaskError != "" || out.TaskNames != "page" {
		t.Fatalf("reply = %+v, want the fixed task offered", out)
	}
	if !strings.Contains(out.Reply, "Here they are:") || !strings.Contains(out.Reply, `cmd = "echo hi"`) || !strings.Contains(out.Reply, "Run page first.") {
		t.Errorf("the reply should keep its words around the fixed tasks:\n%s", out.Reply)
	}
	if len(e.asked) != 2 || !strings.Contains(e.asked[1][len(e.asked[1])-1].Content, "don't load") {
		t.Errorf("the model wasn't asked to fix the tasks: %d calls", len(e.asked))
	}

	// a model that never gets it right is asked twice more, then the error is shown
	e = &sequenceEngine{replies: []string{broken}}
	a.gen = &generator{assistant: gorun.Assistant{Engine: e}}
	out = postJSON(t, a, "/assistant/chat", chatRequest{Messages: []gorun.ChatMessage{{Role: "user", Content: "a page"}}})
	if out.TaskError == "" || len(e.asked) != 1+chatRepairs {
		t.Errorf("after %d calls, taskError = %q", len(e.asked), out.TaskError)
	}
}
