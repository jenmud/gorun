package websrv

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/jenmud/gorun"
)

// replyEngine is a model that always gives the same reply.
type replyEngine string

func (e replyEngine) Chat(context.Context, []gorun.ChatMessage) (string, error) {
	return string(e), nil
}

func withModel(a *app, reply string) {
	a.gen = &generator{assistant: gorun.Assistant{Engine: replyEngine(reply)}}
}

// Generated tasks are queued as unsaved changes, and the form only shows when there is a model.
func TestGenerateQueuesTasks(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	if strings.Contains(get(t, a, "/config").Body.String(), "/config/generate") {
		t.Error("the generate form shows without a model")
	}

	withModel(a, "[tasks.nginx]\ndescription = \"Install nginx\"\ncmd = \"apt-get install -y nginx\"\ncheck = \"false\"\nverify = \"true\"\nsudo = true\ndepends_on = [\"hello\"]\n")
	if !strings.Contains(get(t, a, "/config").Body.String(), "/config/generate") {
		t.Fatal("the generate form is missing")
	}
	rec := post(t, a, "/config/generate", url.Values{"request": {"install nginx"}, "file": {path}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad || !strings.Contains(msg, "nginx") {
		t.Fatalf("generate: %q (bad %v)", msg, bad)
	}
	if len(a.pending) != 1 || a.pending[0].Name != "nginx" || a.pending[0].Body["sudo"] != true {
		t.Fatalf("pending = %+v", a.pending)
	}

	// a second time, the name is taken by the unsaved task
	rec = post(t, a, "/config/generate", url.Values{"request": {"install nginx"}, "file": {path}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); !bad || !strings.Contains(msg, "already exists") {
		t.Errorf("generating a taken name: %q (bad %v)", msg, bad)
	}
	if len(a.pending) != 1 {
		t.Errorf("pending = %+v", a.pending)
	}
}

// A reply that never loads is reported, and nothing is queued.
func TestGenerateReportsBadReplies(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	withModel(a, "I can't help with that.")
	rec := post(t, a, "/config/generate", url.Values{"request": {"install nginx"}, "file": {path}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); !bad || !strings.Contains(msg, "Could not generate") {
		t.Errorf("flash = %q (bad %v)", msg, bad)
	}
	if len(a.pending) != 0 {
		t.Errorf("pending = %+v", a.pending)
	}
}
