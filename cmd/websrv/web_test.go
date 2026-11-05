package websrv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jenmud/gorun/internal/store"
)

const oneTask = "[tasks.hello]\ncmd = \"echo hi\"\n\n[servers.hosts.local-1]\ntasks = [\"hello\"]\n"

const withDeps = `[tasks.build]
cmd = "make"

[tasks.test]
cmd = "make test"
description = "run tests"
depends_on = ["build"]

[server_groups.web]
tags = ["w"]

[servers.hosts.local-1]
tasks = ["test"]

[servers.hosts.web-1]
groups = ["web"]
tasks = ["build"]
`

// newTestApp returns an app over a temporary Gorunfile holding src.
func newTestApp(t *testing.T, src string) (*app, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Gorunfile")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	a := &app{
		store: st,
		hub:   &hub{clients: map[chan string]struct{}{}},
		runs:  map[string]context.CancelFunc{},
	}
	if err := a.loadConfig(path); err != nil {
		t.Fatal(err)
	}
	return a, path
}

// get and post run one request against the app's routes and return the recorded response.
func get(t *testing.T, a *app, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func post(t *testing.T, a *app, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	return rec
}

// refused reports whether a redirect carries an error message.
func refused(rec *httptest.ResponseRecorder) bool {
	return strings.Contains(rec.Header().Get("Location"), "bad=1")
}

func TestPagesRenderTheLoadedFile(t *testing.T) {
	a, _ := newTestApp(t, oneTask)

	for path, want := range map[string]string{
		"/":                      "local-1",
		"/config":                "hello",
		"/config?modal=add-task": "Depends on",
		"/history":               "No runs saved yet",
	} {
		rec := get(t, a, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("GET %s: body does not contain %q", path, want)
		}
	}
}

func TestEditDialogTicksTheDependencies(t *testing.T) {
	a, _ := newTestApp(t, withDeps)

	body := get(t, a, "/config?modal=edit-task&name=test").Body.String()
	if !strings.Contains(body, `value="build" checked`) {
		t.Error("edit dialog does not tick the task's dependency")
	}
	if strings.Contains(body, `value="test" checked`) {
		t.Error("a task can be picked as its own dependency")
	}
}

func TestRemoveRefusesWhenUsed(t *testing.T) {
	a, _ := newTestApp(t, oneTask)

	rec := post(t, a, "/config/remove", url.Values{"kind": {kindTask}, "name": {"hello"}})
	if !refused(rec) {
		t.Fatal("removing a task a server uses was not refused")
	}
	if len(a.pending) != 0 {
		t.Errorf("pending = %d changes, want none", len(a.pending))
	}
}

func TestRemoveThenSaveRewritesTheFile(t *testing.T) {
	a, path := newTestApp(t, oneTask)

	// The server uses the task, so the server goes first.
	post(t, a, "/config/remove", url.Values{"kind": {kindHost}, "name": {"local-1"}})
	post(t, a, "/config/remove", url.Values{"kind": {kindTask}, "name": {"hello"}})
	if len(a.pending) != 2 {
		t.Fatalf("pending = %d changes, want 2", len(a.pending))
	}
	if rec := post(t, a, "/config/save", nil); refused(rec) {
		t.Fatalf("save refused: %s", rec.Header().Get("Location"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "[tasks.hello]") {
		t.Errorf("saved file still defines the task:\n%s", data)
	}
	if len(a.pending) != 0 {
		t.Errorf("pending = %d changes after save, want 0", len(a.pending))
	}
}

func TestEditDropsADependency(t *testing.T) {
	a, path := newTestApp(t, withDeps)

	// The form has no depends_on ticked, so the edit deselects it.
	rec := post(t, a, "/config/update", url.Values{
		"kind":        {kindTask},
		"name":        {"test"},
		"cmd":         {"make test"},
		"description": {"run tests"},
		"file":        {path},
	})
	if refused(rec) {
		t.Fatalf("edit refused: %s", rec.Header().Get("Location"))
	}
	if labels := pendingLabels(a.pending); !slices.Equal(labels, []string{"Edit task test"}) {
		t.Errorf("pending = %v, want one edit", labels)
	}
	if rec := post(t, a, "/config/save", nil); refused(rec) {
		t.Fatalf("save refused: %s", rec.Header().Get("Location"))
	}
	if deps := a.cfg.Tasks["test"].DependsOn; len(deps) != 0 {
		t.Errorf("depends_on after edit = %v, want none", deps)
	}
	if _, ok := a.cfg.Tasks["build"]; !ok {
		t.Error("the dependency itself was lost")
	}
}

func TestGroupPickerSetsMembers(t *testing.T) {
	a, path := newTestApp(t, withDeps)

	// web-1 leaves the web group and local-1 joins it.
	rec := post(t, a, "/config/update", url.Values{
		"kind":    {kindGroup},
		"name":    {"web"},
		"tags":    {"w"},
		"servers": {"local-1"},
		"file":    {path},
	})
	if refused(rec) {
		t.Fatalf("update refused: %s", rec.Header().Get("Location"))
	}
	if rec := post(t, a, "/config/save", nil); refused(rec) {
		t.Fatalf("save refused: %s", rec.Header().Get("Location"))
	}
	members := map[string][]string{}
	for _, s := range a.cfg.Servers {
		members[s.Name] = s.Groups
	}
	if !slices.Contains(members["local-1"], "web") {
		t.Errorf("local-1 groups = %v, want web", members["local-1"])
	}
	if slices.Contains(members["web-1"], "web") {
		t.Errorf("web-1 groups = %v, want no web", members["web-1"])
	}
}

func TestRemoveRefusesAGroupWithMembers(t *testing.T) {
	a, _ := newTestApp(t, withDeps)

	if rec := post(t, a, "/config/remove", url.Values{"kind": {kindGroup}, "name": {"web"}}); !refused(rec) {
		t.Error("removing a group with members was not refused")
	}
}

func TestSaveRestoresWhenTheResultWillNotLoad(t *testing.T) {
	a, path := newTestApp(t, oneTask)

	// A dependency on a task that doesn't exist makes the file fail to load.
	post(t, a, "/config/add", url.Values{
		"kind":       {kindTask},
		"name":       {"broken"},
		"cmd":        {"x"},
		"depends_on": {"missing"},
		"file":       {path},
	})
	if len(a.pending) != 1 {
		t.Fatalf("pending = %d changes, want 1", len(a.pending))
	}
	rec := post(t, a, "/config/save", nil)
	if !refused(rec) {
		t.Fatal("a save that won't load was not refused")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != oneTask {
		t.Errorf("file changed after a refused save:\n%s", data)
	}
	if len(a.pending) != 1 {
		t.Errorf("pending = %d changes after a refused save, want 1 kept", len(a.pending))
	}
}

func TestAddRefusesAnExistingName(t *testing.T) {
	a, path := newTestApp(t, oneTask)

	post(t, a, "/config/add", url.Values{
		"kind": {kindTask},
		"name": {"hello"},
		"cmd":  {"echo again"},
		"file": {path},
	})
	if len(a.pending) != 0 {
		t.Errorf("a duplicate task was queued: %v", a.pending)
	}
}

func TestPatchFramesEachLine(t *testing.T) {
	got := patch("#dashboard", "<p>one</p>\n<p>two</p>")
	want := "event: datastar-patch-elements\n" +
		"data: mode inner\n" +
		"data: selector #dashboard\n" +
		"data: elements <p>one</p>\n" +
		"data: elements <p>two</p>\n\n"
	if got != want {
		t.Errorf("patch =\n%q\nwant\n%q", got, want)
	}
}
