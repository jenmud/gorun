package websrv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jenmud/gorun/internal/store"
)

// newTestApp returns an app over a temporary Gorunfile with one task and one server.
func newTestApp(t *testing.T) (*app, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Gorunfile")
	src := "[tasks.hello]\ncmd = \"echo hi\"\n\n[servers.hosts.local-1]\ntasks = [\"hello\"]\n"
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

func TestPagesRenderTheLoadedFile(t *testing.T) {
	a, _ := newTestApp(t)

	for path, want := range map[string]string{
		"/":        "local-1",
		"/config":  "hello",
		"/history": "No runs saved yet",
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

func TestRemoveThenSaveRewritesTheFile(t *testing.T) {
	a, path := newTestApp(t)

	rec := post(t, a, "/config/remove", url.Values{"kind": {kindTask}, "name": {"hello"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("remove: status %d", rec.Code)
	}
	if len(a.pending) != 1 {
		t.Fatalf("pending = %d changes, want 1", len(a.pending))
	}
	if body := get(t, a, "/config").Body.String(); !strings.Contains(body, "Remove task hello") {
		t.Error("config page does not list the pending removal")
	}

	if rec := post(t, a, "/config/save", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("save: status %d", rec.Code)
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

func TestAddRefusesAnExistingName(t *testing.T) {
	a, _ := newTestApp(t)

	rec := post(t, a, "/config/add", url.Values{
		"kind": {kindTask},
		"name": {"hello"},
		"cmd":  {"echo again"},
		"file": {a.cfg.Path},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add: status %d", rec.Code)
	}
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

func TestHistoryTaskDetailShowsEverythingSaved(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	st := a.store
	if err := st.CreateRun(ctx, "run-1", "deploy", "box", 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := st.AddServer(ctx, "run-1", "web-1", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertTask(ctx, "run-1", "web-1", "build", 2, "make build", "built ok", "warn: old", 0, 1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskEnvVars(ctx, "run-1", "web-1", "build", map[string]string{"STAGE": "prod"}, map[string]any{"release": "v1"}); err != nil {
		t.Fatal(err)
	}

	page := get(t, a, "/history/run-1").Body.String()
	if !strings.Contains(page, "task-0-0") || strings.Contains(page, "built ok") {
		t.Fatal("a passed task should load its detail on demand")
	}

	rec := get(t, a, "/history/run-1/task?server=web-1&name=build&el=task-0-0")
	if rec.Code != http.StatusOK {
		t.Fatalf("task detail: status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"selector #task-0-0", "$ make build", "built ok", "warn: old", "STAGE", "prod", "&#34;release&#34;", "1.5s", "10.0.0.1"} {
		if !strings.Contains(body, want) {
			t.Errorf("task detail does not contain %q", want)
		}
	}

	if rec := get(t, a, "/history/run-1/task?server=web-1&name=build&el=x%20y"); rec.Code != http.StatusNotFound {
		t.Errorf("bad element id: status %d, want 404", rec.Code)
	}
}

func TestConfigPickerListsFolder(t *testing.T) {
	a, path := newTestApp(t)
	dir := filepath.Dir(path)
	if err := os.Mkdir(filepath.Join(dir, "inventory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	body := get(t, a, "/config?dir="+url.QueryEscape(dir)).Body.String()
	for _, want := range []string{"inventory/", "Gorunfile", "gorun.db"} {
		if !strings.Contains(body, want) {
			t.Errorf("picker does not list %q", want)
		}
	}
	if strings.Contains(body, ".hidden") {
		t.Error("picker lists a hidden file")
	}
}

func TestHistoryExplainsAServerThatCouldNotRun(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	st := a.store
	_ = st.CreateRun(ctx, "run-x", "deploy", "box", 1, time.Now())
	_ = st.AddServer(ctx, "run-x", "web-1", "10.0.0.1")
	_ = st.PlanTask(ctx, "run-x", "web-1", "build")
	_ = st.PlanTask(ctx, "run-x", "web-1", "deploy")
	_ = st.SetServerError(ctx, "run-x", "web-1", "dial tcp 10.0.0.1:22: connection refused")
	_ = st.FinishRun(ctx, "run-x", false, 2, 2, 0, time.Second, time.Now())

	body := get(t, a, "/history/run-x").Body.String()
	for _, want := range []string{"connection refused", "Could not run on this server", "Its 2 tasks did not run.", "not run", "build", "deploy"} {
		if !strings.Contains(body, want) {
			t.Errorf("history detail does not contain %q", want)
		}
	}
	if strings.Contains(body, "no tasks") {
		t.Error("a server with planned tasks must not say it has none")
	}
}

func TestHistoryListsTasksSlowestFirst(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	st := a.store
	_ = st.CreateRun(ctx, "run-s", "t", "box", 1, time.Now())
	_ = st.AddServer(ctx, "run-s", "web-1", "")
	for name, d := range map[string]time.Duration{"quick": time.Millisecond, "slowest": 9 * time.Second, "medium": time.Second} {
		_ = st.UpsertTask(ctx, "run-s", "web-1", name, 2, "", "", "", 0, d)
	}
	body := get(t, a, "/history/run-s").Body.String()
	i, j, k := strings.Index(body, ">slowest<"), strings.Index(body, ">medium<"), strings.Index(body, ">quick<")
	if !(i > 0 && i < j && j < k) {
		t.Errorf("task order = slowest@%d medium@%d quick@%d, want slowest first", i, j, k)
	}
}

func TestRecentRunBarsNameTheRunAndLinkToIt(t *testing.T) {
	a, _ := newTestApp(t)
	ctx := context.Background()
	_ = a.store.CreateRun(ctx, "run-bar", "deploy", "box", 1, time.Now())
	_ = a.store.FinishRun(ctx, "run-bar", true, 1, 0, 0, time.Second, time.Now())

	body := get(t, a, "/").Body.String()
	// The stats stream in over SSE; the page itself carries them too.
	for _, want := range []string{`href="/history/run-bar"`, "run run-bar"} {
		if !strings.Contains(body, want) {
			t.Errorf("run page does not contain %q", want)
		}
	}
}

func TestTasksAndBundlesPages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Gorunfile")
	src := `[tasks.build]
cmd = "make build"
local = true
description = "Build it"
[tasks.ship]
cmd = "make ship"
sudo = true
depends_on = ["build"]
[bundles.release]
description = "Build and ship"
tasks = ["build", "ship"]
[bundles.all]
tasks = ["@release"]
[servers.hosts.local-1]
tasks = ["build"]
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp(t)
	if err := a.loadConfig(path); err != nil {
		t.Fatal(err)
	}

	tasks := get(t, a, "/tasks").Body.String()
	for _, want := range []string{"make ship", "Build it", "sudo", "depends on build", "@release", "local-1", "Timeout"} {
		if !strings.Contains(tasks, want) {
			t.Errorf("tasks page does not contain %q", want)
		}
	}
	bundles := get(t, a, "/bundles").Body.String()
	for _, want := range []string{"@release", "@all", "Build and ship", "Run this bundle", "/?bundle=release"} {
		if !strings.Contains(bundles, want) {
			t.Errorf("bundles page does not contain %q", want)
		}
	}
	if run := get(t, a, "/?bundle=release").Body.String(); !strings.Contains(run, `value="release" checked`) {
		t.Error("run page does not tick the bundle it was sent")
	}
	for _, p := range []string{"/", "/tasks", "/bundles", "/history", "/config"} {
		if body := get(t, a, p).Body.String(); !strings.Contains(body, `href="/bundles"`) || !strings.Contains(body, `href="/tasks"`) {
			t.Errorf("%s does not link the Tasks and Bundles tabs", p)
		}
	}
}

func TestRunRefusesTasksAndBundlesTogether(t *testing.T) {
	a, _ := newTestApp(t)
	rec := post(t, a, "/run", url.Values{"server": {"local-1"}, "task": {"hello"}, "bundle": {"x"}})
	// Without NATS the run is refused first; either way nothing may start.
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	if len(a.runs) != 0 {
		t.Errorf("a run started: %v", a.runs)
	}
}

func TestPlanRunUsesTickedTasksInPlaceOfTheServerList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Gorunfile")
	src := `[tasks.own]
local = true
cmd = "echo own"

[tasks.extra]
local = true
cmd = "echo extra"

[tasks.remote]
cmd = "uptime"

[servers.hosts.local]
hostname = "localhost"
tasks = ["own"]

[servers.hosts.web]
hostname = "web.example"
ssh.username = "deploy"
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	_, servers, err := planRun(path, []string{"local"}, []string{"extra"}, nil)
	if err != nil {
		t.Fatalf("a ticked task the server doesn't list should still run: %v", err)
	}
	if got := servers[0].Tasks; len(got) != 1 || got[0].Name != "extra" {
		t.Errorf("tasks = %v, want only extra", got)
	}

	if _, servers, err = planRun(path, []string{"local"}, nil, nil); err != nil || servers[0].Tasks[0].Name != "own" {
		t.Errorf("with nothing ticked the server's own list should run: %v %v", servers, err)
	}

	if _, _, err := planRun(path, []string{"local"}, []string{"remote"}, nil); err == nil || !strings.Contains(err.Error(), "Tick a server") {
		t.Errorf("an SSH-only task on a local server should be refused, got %v", err)
	}

	_, servers, err = planRun(path, nil, []string{"extra"}, nil)
	if err != nil || len(servers) != 1 || !servers[0].IsLocal() || servers[0].Tasks[0].Name != "extra" {
		t.Errorf("a local task should run here with no server ticked: %v %v", servers, err)
	}

	if _, _, err := planRun(path, nil, []string{"remote"}, nil); err == nil || !strings.Contains(err.Error(), "Tick a server") {
		t.Errorf("an SSH task with no server ticked should be refused, got %v", err)
	}

	_, servers, err = planRun(path, []string{"web"}, []string{"extra", "remote"}, nil)
	if err != nil || len(servers) != 2 {
		t.Fatalf("local and SSH tasks should run on this machine and the ticked server: %v %v", servers, err)
	}
	if servers[0].Name != "web" || servers[0].Tasks[0].Name != "remote" || !servers[1].IsLocal() || servers[1].Tasks[0].Name != "extra" {
		t.Errorf("servers = %+v", servers)
	}
}
