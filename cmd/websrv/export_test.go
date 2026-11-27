package websrv

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jenmud/gorun"
)

// The export splits tasks and bundles into files of their own, folds in local includes, and
// loads back to the same config.
func TestExportZipLoadsBack(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Gorunfile", `# a comment
title = "fleet"
include = ["more/*.toml"]

[tasks.update]
cmd = "apt-get update"

[tasks.site]
cmd = "mv /tmp/index.html /var/www/index.html"
depends_on = ["update"]
[[tasks.site.copy]]
src = "./site/index.html"
dst = "/tmp/index.html"

[bundles.web]
tasks = ["update", "site"]

[server_groups.web]
tasks = ["@web"]

[servers.hosts.web-1]
hostname = "10.0.0.1"
groups = ["web"]

[[schedule]]
name = "nightly"
target = "update"
cron = "0 2 * * *"
`)
	write("more/db.toml", `[tasks.backup]
cmd = "pg_dump app > /srv/app.sql"

[servers.hosts.db-1]
hostname = "10.0.0.2"
tasks = ["backup"]

[[schedule]]
name = "backups"
target = "backup"
cron = "0 3 * * *"
`)
	want := &gorun.Config{}
	if err := want.LoadFile(filepath.Join(dir, "Gorunfile")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := exportZip(&buf, filepath.Join(dir, "Gorunfile"), nil, time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	names := unzip(t, buf.Bytes(), out)
	for _, n := range []string{"gorunfile/Gorunfile", "gorunfile/tasks/update.toml", "gorunfile/tasks/site.toml", "gorunfile/tasks/backup.toml", "gorunfile/bundles/web.toml"} {
		if !slices.Contains(names, n) {
			t.Errorf("the zip is missing %s; it has %v", n, names)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(out, "gorunfile/tasks/site.toml")); strings.Contains(string(data), "update]") || !strings.Contains(string(data), "[[tasks.site.copy]]") {
		t.Errorf("tasks/site.toml should hold only the site task, with its copy:\n%s", data)
	}

	got := &gorun.Config{}
	if err := got.LoadFile(filepath.Join(out, "gorunfile", "Gorunfile")); err != nil {
		t.Fatalf("the export doesn't load: %v", err)
	}
	if got.Title != want.Title || len(got.Tasks) != len(want.Tasks) || len(got.Bundles) != len(want.Bundles) ||
		len(got.Servers) != len(want.Servers) || len(got.Schedules) != len(want.Schedules) {
		t.Errorf("export = %d tasks, %d bundles, %d servers, %d schedules; want %d, %d, %d, %d",
			len(got.Tasks), len(got.Bundles), len(got.Servers), len(got.Schedules),
			len(want.Tasks), len(want.Bundles), len(want.Servers), len(want.Schedules))
	}
	gotP, _ := got.ServersPipeline()
	wantP, _ := want.ServersPipeline()
	for name, s := range wantP {
		if len(gotP[name].Tasks) != len(s.Tasks) {
			t.Errorf("%s runs %d tasks after the export, want %d", name, len(gotP[name].Tasks), len(s.Tasks))
		}
	}
}

func TestExportButtonDownloadsAZip(t *testing.T) {
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	if !strings.Contains(get(t, a, "/config").Body.String(), `href="/config/export"`) {
		t.Error("the Config page has no Export button")
	}
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config/export", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if names := unzip(t, rec.Body.Bytes(), t.TempDir()); !slices.Contains(names, "gorunfile/tasks/hello.toml") {
		t.Errorf("zip = %v", names)
	}
}

func unzip(t *testing.T, data []byte, dir string) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		p := filepath.Join(dir, f.Name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return names
}
