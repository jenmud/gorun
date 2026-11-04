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
