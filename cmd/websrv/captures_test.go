package websrv

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/store"
)

// With capture off, nothing is kept; with it on, an answer is kept with its prompt, rated,
// marked saved, and exported with addresses and keys replaced and without who asked.
func TestCaptureKeepsAnswersForTraining(t *testing.T) {
	t.Setenv("GORUN_ASSISTANT_CAPTURE", "")
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	ctx := context.Background()
	e := &recordingEngine{reply: "Here you go:\n```toml\n[tasks.nginx]\ncmd = \"curl http://10.1.2.3/ && echo AKIAABCDEFGHIJKLMNOP\"\ncheck = \"false\"\nverify = \"true\"\n```"}
	a.gen = &generator{assistant: gorun.Assistant{Engine: e}}
	ask := chatRequest{Messages: []gorun.ChatMessage{{Role: "user", Content: "install nginx; mail ops@example.org"}}}

	if out := postJSON(t, a, "/assistant/chat", ask); out.Capture != "" {
		t.Fatalf("kept an answer with capture off: %+v", out)
	}
	if n, _ := a.store.CountCaptures(ctx); n != 0 {
		t.Fatalf("%d kept with capture off", n)
	}

	form(t, a, "/assistant/captures/toggle", url.Values{"on": {"on"}})
	out := postJSON(t, a, "/assistant/chat", ask)
	if out.Capture == "" || out.TaskNames != "nginx" {
		t.Fatalf("reply = %+v, want a kept answer", out)
	}
	postJSON(t, a, "/assistant/feedback", map[string]any{"capture": out.Capture, "value": 1})
	if added := postJSON(t, a, "/assistant/add", map[string]string{"tasks": out.Tasks, "file": path, "capture": out.Capture}); added.Error != "" {
		t.Fatalf("add = %+v", added)
	}
	caps, err := a.store.Captures(ctx, 0)
	if err != nil || len(caps) != 1 {
		t.Fatalf("captures = %+v, %v", caps, err)
	}
	c := caps[0]
	if c.Kind != "chat" || c.Feedback != 1 || c.SavedAt.IsZero() || c.Tasks == "" || !strings.Contains(c.Messages, `"role":"system"`) {
		t.Errorf("kept = %+v", c)
	}

	// the export: one line, in the training data's shape, redacted
	rec := get(t, a, "/assistant/captures/export")
	var ex trainingExample
	lines := 0
	sc := bufio.NewScanner(rec.Body)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		lines++
		if err := json.Unmarshal(sc.Bytes(), &ex); err != nil {
			t.Fatal(err)
		}
	}
	if lines != 1 || ex.Kind != "captured-chat" || ex.Meta.Feedback != 1 || !ex.Meta.Saved || !ex.Meta.Loaded {
		t.Fatalf("export = %d lines, %+v", lines, ex.Meta)
	}
	all, _ := json.Marshal(ex)
	for _, leak := range []string{"10.1.2.3", "AKIAABCDEFGHIJKLMNOP", "ops@example.org"} {
		if strings.Contains(string(all), leak) {
			t.Errorf("the export holds %q", leak)
		}
	}
	if last := ex.Messages[len(ex.Messages)-1]; last.Role != "assistant" || !strings.Contains(last.Content, "198.51.100.1") {
		t.Errorf("the reply isn't last, redacted: %+v", last)
	}
	if raw := get(t, a, "/assistant/captures/export?redact=0").Body.String(); !strings.Contains(raw, "10.1.2.3") {
		t.Error("redact=0 still redacted")
	}

	form(t, a, "/assistant/captures/toggle", url.Values{})
	if out := postJSON(t, a, "/assistant/chat", ask); out.Capture != "" {
		t.Error("kept an answer after capture was turned off")
	}
	form(t, a, "/assistant/captures/delete", url.Values{"all": {"yes"}})
	if n, _ := a.store.CountCaptures(ctx); n != 0 {
		t.Errorf("%d left after deleting all", n)
	}
}

// The env var turns capture on whatever the page's setting says.
func TestCaptureEnvTurnsItOn(t *testing.T) {
	t.Setenv("GORUN_ASSISTANT_CAPTURE", "1")
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	if on, byEnv := a.capturing(context.Background()); !on || !byEnv {
		t.Errorf("capturing = %v, %v", on, byEnv)
	}
	if on, _ := (&app{}).capturing(context.Background()); on {
		t.Error("capturing without a history database")
	}
}

func TestOutcomeOfSavedTasks(t *testing.T) {
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	ctx := context.Background()
	saved := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	_ = a.store.CreateRun(ctx, "r1", "x", "h", 1, saved.Add(time.Hour))
	_ = a.store.UpsertTask(ctx, "r1", "web-1", "nginx", store.StateDone, "", "", "", 0, 0)
	_ = a.store.UpsertTask(ctx, "r1", "web-2", "nginx", store.StateFailed, "", "", "", 1, 0)
	row := a.captureRow(ctx, store.Capture{TaskNames: "nginx, schedule nightly", SavedAt: saved})
	if row.Passed != 1 || row.Failed != 1 {
		t.Errorf("outcome = %d passed, %d failed", row.Passed, row.Failed)
	}
}

// form posts a form through the app's routes, as a page would.
func form(t *testing.T, a *app, path string, v url.Values) {
	t.Helper()
	rec := post(t, a, path, v)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d: %s", path, rec.Code, rec.Body)
	}
}
