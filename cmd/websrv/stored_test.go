package websrv

import (
	"archive/zip"
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/store"
)

// newStoredApp is an app on the Gorunfiles kept in a new history database, with the default
// one open, set up as run does.
func newStoredApp(t *testing.T) (*app, *store.Store) {
	t.Helper()
	return newStoredAppAt(t, filepath.Join(t.TempDir(), "gorun.db"))
}

// newStoredAppAt is newStoredApp with its history database at dbPath.
func newStoredAppAt(t *testing.T, dbPath string) (*app, *store.Store) {
	t.Helper()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	names, err := openStored(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{store: st, hub: &hub{clients: map[chan string]struct{}{}}, runs: map[string]context.CancelFunc{}, keepsStored: true}
	t.Cleanup(a.stopScheduler)
	t.Cleanup(a.stopBackground)
	if err := a.loadConfig(gorun.StoredPath(names[0])); err != nil {
		t.Fatal(err)
	}
	return a, st
}

// A stored Gorunfile lives only in the database: each save is a version there, and no file is
// written for it.
func TestStoredGorunfileKeepsEachSaveInTheDatabaseOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	a, st := newStoredAppAt(t, "gorun.db") // relative, as GORUN_DB_PATH may be
	ctx := context.Background()
	if got := a.currentStored(); got != store.DefaultGorunfile || a.cfgPath != "stored::default" {
		t.Fatalf("open = %q at %q, want the default one", got, a.cfgPath)
	}

	add := gorun.Change{Kind: kindTask, Name: "hello", File: a.cfgPath, Body: map[string]any{"cmd": "echo hi", "local": true}}
	if err := a.saveDirect(a.cfgPath, []gorun.Change{add}); err != nil {
		t.Fatal(err)
	}
	content, _, err := st.Gorunfile(ctx, store.DefaultGorunfile)
	if err != nil || !strings.Contains(content, "[tasks.hello]") {
		t.Fatalf("stored Gorunfile = %q, %v; want the new task", content, err)
	}
	if n, _ := st.GorunfileVersions(ctx, store.DefaultGorunfile); n != 2 {
		t.Errorf("versions = %d, want 2 (the starter and the save)", n)
	}
	if _, ok := a.cfg.Tasks["hello"]; !ok {
		t.Error("the open config doesn't have the saved task")
	}

	// an edit that wouldn't load is refused, and isn't a version
	bad := gorun.Change{Kind: kindTask, Name: "broken", File: a.cfgPath, Body: map[string]any{"cmd": "true", "depends_on": []any{"nope"}}}
	if err := a.saveDirect(a.cfgPath, []gorun.Change{bad}); err == nil {
		t.Error("saved an edit that doesn't load")
	}
	if n, _ := st.GorunfileVersions(ctx, store.DefaultGorunfile); n != 2 {
		t.Errorf("versions after a refused edit = %d, want 2", n)
	}

	entries, _ := os.ReadDir(".")
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "gorun.db") {
			t.Errorf("the database's folder holds %s; a stored Gorunfile shouldn't write files", e.Name())
		}
	}
}

// Gorunfiles share tasks with stored:: includes; each can be opened, and the schedules of the
// ones that aren't open keep running.
func TestStoredGorunfilesShareTasksAndAllSchedule(t *testing.T) {
	a, st := newStoredApp(t)
	ctx := context.Background()
	post(t, a, "/gorunfiles/new", url.Values{"name": {"base"}})
	if a.currentStored() != "base" {
		t.Fatalf("open = %q after creating base", a.currentStored())
	}
	base := "title = \"base\"\n[tasks.update]\ncmd = \"apt-get update\"\nlocal = true\n[bundles.baseline]\ntasks = [\"update\"]\n[[schedule]]\nname = \"base-nightly\"\ntarget = \"update\"\ncron = \"0 2 * * *\"\n"
	if msg := upload(t, a, []byte(base)); !strings.Contains(msg, "Imported") {
		t.Fatalf("import base = %q", msg)
	}

	post(t, a, "/gorunfiles/new", url.Values{"name": {"web"}})
	web := "title = \"web\"\ninclude = [\"stored::base\"]\n[tasks.nginx]\ncmd = \"true\"\nlocal = true\ndepends_on = [\"update\"]\n[[schedule]]\nname = \"web-hourly\"\ntarget = \"@baseline\"\ncron = \"0 * * * *\"\n"
	if msg := upload(t, a, []byte(web)); !strings.Contains(msg, "Imported") {
		t.Fatalf("import web = %q", msg)
	}
	if _, ok := a.cfg.Tasks["update"]; !ok || len(a.cfg.Schedules) != 1 {
		t.Errorf("web = %d tasks (want base's update too), %d schedules (want only its own)", len(a.cfg.Tasks), len(a.cfg.Schedules))
	}
	// a shared task edited while web is open is saved into base, where it is defined
	edit := []gorun.Change{
		{Kind: kindTask, Name: "update", File: gorun.StoredPath("base"), Remove: true},
		{Kind: kindTask, Name: "update", File: gorun.StoredPath("base"), Body: map[string]any{"cmd": "apt-get update -q", "local": true}},
	}
	if err := a.saveDirect(gorun.StoredPath("base"), edit); err != nil {
		t.Fatal(err)
	}
	if content, _, _ := st.Gorunfile(ctx, "base"); !strings.Contains(content, "apt-get update -q") {
		t.Errorf("editing base's task from web didn't reach base in the database:\n%s", content)
	}
	a.mu.Lock()
	baseSched := a.bg["base"]
	a.mu.Unlock()
	if baseSched == nil || len(baseSched.Status()) != 1 {
		t.Errorf("base's schedule isn't running while web is open")
	}

	// the page lists them, with who shares with whom
	page := get(t, a, "/gorunfiles").Body.String()
	for _, want := range []string{"uses the tasks of base", "its tasks are used by web", `action="/gorunfiles/open"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the Gorunfiles page is missing %q", want)
		}
	}
	// base can't go while web includes it; default can
	if msg := redirectFlash(post(t, a, "/gorunfiles/delete", url.Values{"name": {"base"}})); !strings.Contains(msg, "web includes base") {
		t.Errorf("delete base = %q", msg)
	}
	if msg := redirectFlash(post(t, a, "/gorunfiles/delete", url.Values{"name": {store.DefaultGorunfile}})); !strings.Contains(msg, "Deleted") {
		t.Errorf("delete default = %q", msg)
	}

	// opening base puts web's schedule in the background
	post(t, a, "/gorunfiles/open", url.Values{"name": {"base"}})
	a.mu.Lock()
	webSched, open := a.bg["web"], a.storedNameOf(a.cfgPath)
	a.mu.Unlock()
	if open != "base" || webSched == nil {
		t.Errorf("after opening base: open = %q, web's scheduler running = %v", open, webSched != nil)
	}
	if list, _ := st.Gorunfiles(ctx); len(list) != 2 {
		t.Errorf("kept Gorunfiles = %+v", list)
	}

	// the export adds base's tasks, so it loads anywhere
	post(t, a, "/gorunfiles/open", url.Values{"name": {"web"}})
	rec := get(t, a, "/config/export")
	out := t.TempDir()
	unzip(t, rec.Body.Bytes(), out)
	cfg := &gorun.Config{}
	if err := cfg.LoadFile(filepath.Join(out, "gorunfile", "Gorunfile")); err != nil || len(cfg.Tasks) != 2 {
		t.Errorf("exported web = %d tasks, %v; want nginx and update", len(cfg.Tasks), err)
	}
}

func redirectFlash(rec *httptest.ResponseRecorder) string {
	loc, _ := url.Parse(rec.Header().Get("Location"))
	return loc.Query().Get("flash")
}

// upload posts data to /config/import as the file field and returns the flash message.
func upload(t *testing.T, a *app, data []byte) string {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "upload")
	_, _ = fw.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/config/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	return loc.Query().Get("flash")
}

func TestImportReplacesTheStoredGorunfile(t *testing.T) {
	a, st := newStoredApp(t)
	ctx := context.Background()
	if !strings.Contains(get(t, a, "/config").Body.String(), `action="/config/import"`) {
		t.Error("the Config page has no Import form")
	}

	// an export zip: a Gorunfile that includes a file per task
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "Gorunfile"), []byte("title = \"imported\"\ninclude = [\"more.toml\"]\n[tasks.a]\ncmd = \"true\"\n[servers.hosts.box]\nhostname = \"10.0.0.9\"\ntasks = [\"a\", \"b\"]\n"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "more.toml"), []byte("[tasks.b]\ncmd = \"true\"\ndepends_on = [\"a\"]\n"), 0o644)
	var zipped bytes.Buffer
	if err := exportZip(&zipped, filepath.Join(src, "Gorunfile"), nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if msg := upload(t, a, zipped.Bytes()); !strings.Contains(msg, "Imported a Gorunfile with 2 tasks into default") {
		t.Fatalf("import = %q", msg)
	}
	content, _, _ := st.Gorunfile(ctx, store.DefaultGorunfile)
	if !strings.Contains(content, "[tasks.a]") || !strings.Contains(content, "[tasks.b]") || strings.Contains(content, "include") {
		t.Errorf("the stored Gorunfile should hold both tasks in one file:\n%s", content)
	}
	if a.cfg.Title != "imported" || len(a.cfg.Servers) != 1 {
		t.Errorf("loaded config = %q with %d servers", a.cfg.Title, len(a.cfg.Servers))
	}

	// a plain Gorunfile works too, and a broken one changes nothing
	if msg := upload(t, a, []byte("[tasks.c]\ncmd = \"true\"\n")); !strings.Contains(msg, "1 tasks") {
		t.Errorf("plain import = %q", msg)
	}
	before, _ := st.GorunfileVersions(ctx, store.DefaultGorunfile)
	if msg := upload(t, a, []byte("[tasks.d]\ncommand = \"oops\"\n")); !strings.Contains(msg, "wouldn't load") {
		t.Errorf("broken import = %q", msg)
	}
	if after, _ := st.GorunfileVersions(ctx, store.DefaultGorunfile); after != before || a.cfg.Tasks["c"].Cmd == "" {
		t.Errorf("a failed import changed the stored Gorunfile: %d -> %d versions", before, after)
	}
}

// The Config page names the stored Gorunfile and offers import, not a file to load.
func TestConfigPageShowsTheStoredGorunfile(t *testing.T) {
	a, _ := newStoredApp(t)
	page := get(t, a, "/config").Body.String()
	for _, want := range []string{"history database, not in a file", `href="/gorunfiles"`, `action="/config/import"`, "1 saved version"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Config page is missing %q", want)
		}
	}
	for _, unwanted := range []string{`id="load"`, ">stored::default<", "Load a Gorunfile"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("the Config page shows %q", unwanted)
		}
	}
	msg := redirectFlash(post(t, a, "/config/load", url.Values{"path": {"/etc/hosts"}}))
	if !strings.Contains(msg, "Import a Gorunfile") || a.currentStored() != store.DefaultGorunfile {
		t.Errorf("loading a file = %q, open = %q; want it refused", msg, a.currentStored())
	}
}

// A Gorunfile given to gorun -serve is used as it is: the database keeps none for it, and saves
// go to the file.
func TestAGivenGorunfileSkipsTheDatabase(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for _, tt := range []struct {
		path string
		live bool
	}{{"Gorunfile", false}, {"", true}} {
		if names, err := pickStored(ctx, tt.path, st, tt.live); names != nil || err != nil {
			t.Errorf("pickStored(%q, live=%v) = %v, %v; want the database left alone", tt.path, tt.live, names, err)
		}
	}
	if list, _ := st.Gorunfiles(ctx); len(list) != 0 {
		t.Errorf("the database keeps %d Gorunfiles, want none", len(list))
	}
	if names, _ := pickStored(ctx, "", st, false); len(names) != 1 {
		t.Errorf("without a Gorunfile: names = %v, want the default one", names)
	}

	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	add := gorun.Change{Kind: kindTask, Name: "more", File: path, Body: map[string]any{"cmd": "true"}}
	if err := a.saveDirect(path, []gorun.Change{add}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "[tasks.more]") {
		t.Errorf("the save didn't reach the given Gorunfile:\n%s", data)
	}
}

func TestImportNeedsTheStoredGorunfile(t *testing.T) {
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	if strings.Contains(get(t, a, "/config").Body.String(), `action="/config/import"`) {
		t.Error("the Import form shows for a Gorunfile given on the command line")
	}
	if msg := upload(t, a, []byte("[tasks.c]\ncmd = \"true\"\n")); !strings.Contains(msg, "Import is for the Gorunfiles kept in the history database") {
		t.Errorf("import = %q", msg)
	}
}

func TestUnpackImportRefusesPathsOutsideTheZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("../evil/Gorunfile")
	_, _ = f.Write([]byte("title = \"x\"\n"))
	_ = zw.Close()
	if _, _, err := unpackImport(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "outside its folder") {
		t.Errorf("unpackImport() = %v, want it refused", err)
	}
}

// The Live page draws its dashboard while holding a.mu, and the menu asks which stored
// Gorunfile is open, so that must not take a.mu too.
func TestLiveDashboardRendersWithAStoredGorunfile(t *testing.T) {
	a, _ := newStoredApp(t)
	done := make(chan struct{})
	go func() {
		a.pushDashboard()
		get(t, a, "/assistant")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("drawing the Live dashboard with a stored Gorunfile open deadlocked")
	}
	if a.currentStored() != store.DefaultGorunfile {
		t.Errorf("currentStored() = %q", a.currentStored())
	}
}
