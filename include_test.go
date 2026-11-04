package gorun

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeFiles writes each file under dir, creating folders as needed.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// loadTree writes files under a fresh directory and loads its Gorunfile with LoadFile.
func loadTree(t *testing.T, files map[string]string) (*Config, error) {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, files)
	var c Config
	return &c, c.LoadFile(filepath.Join(dir, "Gorunfile"))
}

func serverNames(c *Config) []string {
	names := []string{}
	for _, s := range c.Servers {
		names = append(names, s.Name)
	}
	return names
}

func TestLoadFileIncludesGlobsInOrder(t *testing.T) {
	c, err := loadTree(t, map[string]string{
		"Gorunfile": `
include = ["tasks/*.toml", "hosts/*/*.toml"]

[servers]
tasks = ["greet"]

[servers.hosts.root-1]
hostname = "root-1.example.com"
`,
		"tasks/greet.toml": `
[tasks.greet]
cmd = "echo hi"
`,
		"hosts/web/web.toml": `
[servers.hosts.web-1]
hostname = "web-1.example.com"

[servers.hosts.web-2]
hostname = "web-2.example.com"
`,
		"hosts/db/db.toml": `
[servers.hosts.db-1]
hostname = "db-1.example.com"
`,
	})
	if err != nil {
		t.Fatal(err)
	}

	// the root file's hosts come first, then each include in sorted order
	if got, want := serverNames(c), []string{"root-1", "db-1", "web-1", "web-2"}; !slices.Equal(got, want) {
		t.Fatalf("servers = %v, want %v", got, want)
	}
	if _, ok := c.Tasks["greet"]; !ok {
		t.Fatal("task greet from included file is missing")
	}
	for _, s := range c.Servers {
		if !slices.Equal(s.TaskNames, []string{"greet"}) {
			t.Errorf("server %s tasks = %v, want [greet]", s.Name, s.TaskNames)
		}
	}
}

func TestLoadFileIncludePathsRelativeToIncludingFile(t *testing.T) {
	c, err := loadTree(t, map[string]string{
		"Gorunfile":          `include = ["hosts/web/web.toml"]`,
		"shared/tasks.toml":  `[tasks.shared]` + "\n" + `cmd = "true"`,
		"hosts/web/web.toml": `include = ["../../shared/*.toml"]` + "\n" + `[servers.hosts.web-1]` + "\n" + `hostname = "web-1"` + "\n" + `tasks = ["shared"]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Tasks["shared"]; !ok {
		t.Fatal("task from ../../shared was not included")
	}
}

func TestLoadFileDuplicateNameIsError(t *testing.T) {
	_, err := loadTree(t, map[string]string{
		"Gorunfile":    `include = ["tasks/*.toml"]`,
		"tasks/a.toml": `[tasks.greet]` + "\n" + `cmd = "a"`,
		"tasks/b.toml": `[tasks.greet]` + "\n" + `cmd = "b"`,
	})
	if err == nil {
		t.Fatal("want an error for a task defined in two files")
	}
	for _, want := range []string{`duplicate task "greet"`, "tasks/a.toml", "tasks/b.toml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestLoadFileDuplicateAcrossRootAndIncludeIsError(t *testing.T) {
	_, err := loadTree(t, map[string]string{
		"Gorunfile": `
include = ["hosts/*.toml"]

[servers.hosts.web-1]
hostname = "web-1"
`,
		"hosts/more.toml": `
[servers.hosts.web-1]
hostname = "other"
`,
	})
	if err == nil || !strings.Contains(err.Error(), `duplicate host "web-1"`) {
		t.Fatalf("want duplicate host error, got %v", err)
	}
}

func TestLoadFileRootOnlySettingInIncludeIsError(t *testing.T) {
	_, err := loadTree(t, map[string]string{
		"Gorunfile":  `include = ["extra.toml"]`,
		"extra.toml": `title = "nope"`,
	})
	if err == nil || !strings.Contains(err.Error(), "title can only be set in the root Gorunfile") {
		t.Fatalf("want root-only error, got %v", err)
	}
}

func TestLoadFileUnknownKeyNamesIncludedFile(t *testing.T) {
	_, err := loadTree(t, map[string]string{
		"Gorunfile":  `include = ["extra.toml"]`,
		"extra.toml": `[tasks.x]` + "\n" + `comand = "typo"`,
	})
	if err == nil || !strings.Contains(err.Error(), "extra.toml") || !strings.Contains(err.Error(), "unknown keys") {
		t.Fatalf("want unknown-key error naming extra.toml, got %v", err)
	}
}

func TestLoadFileIncludeCycleIsError(t *testing.T) {
	_, err := loadTree(t, map[string]string{
		"Gorunfile": `include = ["a.toml"]`,
		"a.toml":    `include = ["b.toml"]`,
		"b.toml":    `include = ["a.toml"]`,
	})
	if err == nil || !strings.Contains(err.Error(), "include cycle") {
		t.Fatalf("want include cycle error, got %v", err)
	}
}

func TestLoadFileIncludeOfRootIsCycle(t *testing.T) {
	_, err := loadTree(t, map[string]string{
		"Gorunfile": `include = ["Gorunfile"]`,
	})
	if err == nil || !strings.Contains(err.Error(), "include cycle") {
		t.Fatalf("want include cycle error, got %v", err)
	}
}

func TestLoadFileSameFileIncludedTwiceIsLoadedOnce(t *testing.T) {
	c, err := loadTree(t, map[string]string{
		"Gorunfile":      `include = ["hosts/*.toml", "hosts/web.toml"]`,
		"hosts/web.toml": `[servers.hosts.web-1]` + "\n" + `hostname = "web-1"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := serverNames(c); !slices.Equal(got, []string{"web-1"}) {
		t.Fatalf("servers = %v, want [web-1] once", got)
	}
}

func TestLoadFileEmptyGlobIsError(t *testing.T) {
	_, err := loadTree(t, map[string]string{
		"Gorunfile": `include = ["hosts/*.toml"]`,
	})
	if err == nil || !strings.Contains(err.Error(), "matches no files") {
		t.Fatalf("want empty-glob error, got %v", err)
	}
}

func TestLoadReaderRejectsInclude(t *testing.T) {
	var c Config
	err := c.Load(strings.NewReader(`include = ["x.toml"]`))
	if err == nil || !strings.Contains(err.Error(), "LoadFile") {
		t.Fatalf("want error asking for LoadFile, got %v", err)
	}
}
