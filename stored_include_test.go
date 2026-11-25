package gorun

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoredIncludeSharesOnlyTasksAndBundles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	base := write("base/Gorunfile", `title = "base"
[tasks.update]
cmd = "apt-get update"
[bundles.baseline]
tasks = ["update"]
[servers.hosts.base-box]
hostname = "10.0.0.1"
[[schedule]]
name = "base-nightly"
target = "update"
cron = "0 2 * * *"
`)
	web := write("web/Gorunfile", `title = "web"
include = ["stored::base"]
[tasks.nginx]
cmd = "apt-get install -y nginx"
depends_on = ["update"]
[servers.hosts.web-1]
hostname = "10.0.0.11"
tasks = ["@baseline", "nginx"]
`)

	cfg := &Config{StoredIncludes: func(name string) (string, error) {
		if name != "base" {
			return "", errors.New("no such Gorunfile")
		}
		return base, nil
	}}
	if err := cfg.LoadFile(web); err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "web" || len(cfg.Tasks) != 2 || len(cfg.Bundles) != 1 {
		t.Errorf("config = %q, %d tasks, %d bundles; want web's title with base's task and bundle", cfg.Title, len(cfg.Tasks), len(cfg.Bundles))
	}
	if len(cfg.Servers) != 1 || len(cfg.Schedules) != 0 {
		t.Errorf("config has %d servers and %d schedules; base's own shouldn't come along", len(cfg.Servers), len(cfg.Schedules))
	}

	if err := (&Config{}).LoadFile(web); err == nil || !strings.Contains(err.Error(), "only loads there") {
		t.Errorf("LoadFile without StoredIncludes = %v", err)
	}
}
