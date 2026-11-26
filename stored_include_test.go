package gorun

import (
	"errors"
	"strings"
	"testing"
)

// storedFiles serves Gorunfile texts by name, as a host that keeps them in a database does.
func storedFiles(files map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		text, ok := files[name]
		if !ok {
			return "", errors.New("no such Gorunfile")
		}
		return text, nil
	}
}

func TestStoredIncludeSharesOnlyTasksAndBundles(t *testing.T) {
	files := map[string]string{
		"base": `title = "base"
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
`,
		"web": `title = "web"
include = ["stored::base"]
[tasks.nginx]
cmd = "apt-get install -y nginx"
depends_on = ["update"]
[servers.hosts.web-1]
hostname = "10.0.0.11"
tasks = ["@baseline", "nginx"]
`,
	}

	cfg := &Config{Stored: storedFiles(files)}
	if err := cfg.LoadFile(StoredPath("web")); err != nil {
		t.Fatal(err)
	}
	if cfg.Title != "web" || len(cfg.Tasks) != 2 || len(cfg.Bundles) != 1 {
		t.Errorf("config = %q, %d tasks, %d bundles; want web's title with base's task and bundle", cfg.Title, len(cfg.Tasks), len(cfg.Bundles))
	}
	if len(cfg.Servers) != 1 || len(cfg.Schedules) != 0 {
		t.Errorf("config has %d servers and %d schedules; base's own shouldn't come along", len(cfg.Servers), len(cfg.Schedules))
	}
	if cfg.Path != "stored::web" || strings.Join(cfg.Files, " ") != "stored::web stored::base" {
		t.Errorf("Path = %q, Files = %q", cfg.Path, cfg.Files)
	}
	if got := cfg.Tasks["update"].Source; got != "stored::base" {
		t.Errorf("update's source = %q, want stored::base", got)
	}

	if err := (&Config{}).LoadFile(StoredPath("web")); err == nil || !strings.Contains(err.Error(), "only loads there") {
		t.Errorf("LoadFile without Stored = %v", err)
	}
}

func TestStoredGorunfileIncludesOnlyStoredOnes(t *testing.T) {
	for _, include := range []string{"tasks/*.toml", "git::https://example.com/lib.git//Gorunfile?ref=v1"} {
		cfg := &Config{Stored: storedFiles(map[string]string{"web": "include = [\"" + include + "\"]\n"})}
		if err := cfg.LoadFile(StoredPath("web")); err == nil || !strings.Contains(err.Error(), "only others kept there") {
			t.Errorf("include %q: LoadFile() = %v", include, err)
		}
	}
	cfg := &Config{Stored: storedFiles(map[string]string{"web": "include = [\"stored::nope\"]\n"})}
	if err := cfg.LoadFile(StoredPath("web")); err == nil || !strings.Contains(err.Error(), "no such Gorunfile") {
		t.Errorf("a missing stored include: LoadFile() = %v", err)
	}
}

func TestEditText(t *testing.T) {
	text := "title = \"x\"\n\n[tasks.a]\ncmd = \"true\"\n"
	got, err := EditText(text, []Change{
		{Kind: "task", Name: "a", Remove: true},
		{Kind: "task", Name: "b", Body: map[string]any{"cmd": "echo b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "[tasks.a]") || !strings.Contains(got, "[tasks.b]") || !strings.HasPrefix(got, "title = \"x\"") {
		t.Errorf("EditText() = %q", got)
	}
}
