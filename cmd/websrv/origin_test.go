package websrv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jenmud/gorun/internal/store"
)

// The history list and a run's page show who started the run and where; a run saved before
// gorun recorded that still shows its host.
func TestHistoryShowsWhoStartedARun(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	a.store.Apply(ctx, &store.Event{Type: "process_started", GorunUUID: "run-o", Title: "deploy", Hostname: "laptop",
		Origin: &store.Origin{User: "jen", Hostname: "laptop", OS: "linux", Arch: "amd64", Trigger: "tui", Version: "v0.21.0",
			Gorunfile: "/home/jen/infra/Gorunfile", Command: []string{"gorun", "Gorunfile", "-r"},
			CI: "github-actions", CIUser: "sam", CIURL: "https://github.com/o/r/actions/runs/7",
			Git:    &store.GitInfo{Commit: "abc1234def5678", Branch: "main", Dirty: true},
			Labels: map[string]string{"ticket": "CHG-42"}}})
	a.store.Apply(ctx, &store.Event{Type: "process_started", GorunUUID: "run-old", Title: "old", Hostname: "oldhost"})

	list := get(t, a, "/history").Body.String()
	for _, want := range []string{"jen@laptop", "tui", "github-actions · sam", "v0.21.0", "main@abc1234*", "oldhost"} {
		if !strings.Contains(list, want) {
			t.Errorf("history list does not show %q", want)
		}
	}
	page := get(t, a, "/history/run-o").Body.String()
	for _, want := range []string{"by jen@laptop", "Who and where", "/home/jen/infra/Gorunfile", "gorun Gorunfile -r",
		`href="https://github.com/o/r/actions/runs/7"`, "main @ abc1234def5678 (uncommitted changes)", "Label ticket", "CHG-42"} {
		if !strings.Contains(page, want) {
			t.Errorf("run page does not show %q", want)
		}
	}
	if old := get(t, a, "/history/run-old").Body.String(); !strings.Contains(old, "on oldhost") || strings.Contains(old, "Who and where") {
		t.Error("an old run should say which host it ran on, with no origin panel")
	}
}

// A run started from the web UI records the browser's address and the user an auth proxy
// says is signed in.
func TestWebRunsRecordTheBrowserAndProxyUser(t *testing.T) {
	Version = "v0.21.0"
	t.Cleanup(func() { Version = "" })
	r := httptest.NewRequest(http.MethodPost, "/run", nil)
	r.RemoteAddr = "192.0.2.7:51000"
	r.Header.Set("X-Forwarded-For", "203.0.113.5")
	r.Header.Set("X-Auth-Request-User", "jen")
	o := webOrigin(r)
	if o.Trigger != "web" || o.Version != "v0.21.0" || o.Client != "192.0.2.7 (forwarded for 203.0.113.5)" || o.WebUser != "jen" {
		t.Errorf("origin = %+v", o)
	}
}
