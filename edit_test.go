package gorun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveChangesRemoveKeepsOtherText(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "Gorunfile")
	writeFiles(t, dir, map[string]string{
		"Gorunfile": `# keep this banner
title = "x"

# the greet task
[tasks.greet]
cmd = "echo hi"

# about the other one
[tasks.other]
cmd = "true"
`,
	})

	if err := SaveChanges([]Change{{Kind: "task", Name: "greet", File: root, Remove: true}}); err != nil {
		t.Fatal(err)
	}

	got := read(t, root)
	for _, want := range []string{"# keep this banner", `title = "x"`, "# about the other one", "[tasks.other]"} {
		if !strings.Contains(got, want) {
			t.Errorf("saved file lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "greet") || strings.Contains(got, "# the greet task") {
		t.Errorf("greet should be gone, including its comment:\n%s", got)
	}
}

func TestSaveChangesAddThenLoad(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "Gorunfile")
	writeFiles(t, dir, map[string]string{"Gorunfile": `title = "x"` + "\n"})

	changes := []Change{
		{Kind: "task", Name: "greet", File: root, Body: map[string]any{"cmd": "echo hi", "description": "say hi"}},
		{Kind: "group", Name: "web", File: root, Body: map[string]any{"tags": []string{"web"}}},
		{Kind: "host", Name: "web-1", File: root, Body: map[string]any{"hostname": "web-1.example.com", "groups": []string{"web"}}},
	}
	if err := SaveChanges(changes); err != nil {
		t.Fatal(err)
	}

	var c Config
	if err := c.LoadFile(root); err != nil {
		t.Fatalf("saved file does not load: %v\n%s", err, read(t, root))
	}
	if c.Tasks["greet"].Cmd != "echo hi" || c.Tasks["greet"].Source != root {
		t.Errorf("task greet = %+v", c.Tasks["greet"])
	}
	if len(c.Servers) != 1 || c.Servers[0].Hostname != "web-1.example.com" {
		t.Errorf("servers = %+v", c.Servers)
	}
}

func TestSaveChangesAddRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "Gorunfile")
	writeFiles(t, dir, map[string]string{"Gorunfile": "[tasks.greet]\ncmd = \"a\"\n"})

	err := SaveChanges([]Change{{Kind: "task", Name: "greet", File: root, Body: map[string]any{"cmd": "b"}}})
	if err == nil || !strings.Contains(err.Error(), "already defined") {
		t.Fatalf("want already-defined error, got %v", err)
	}
}

func TestSaveChangesRemoveInlineHost(t *testing.T) {
	dir := t.TempDir()
	hosts := filepath.Join(dir, "hosts.toml")
	writeFiles(t, dir, map[string]string{
		"hosts.toml": `# inventory [web]
[servers.hosts]
web-01 = { hostname = "web-01.example.com" }
web-02 = { hostname = "web-02.example.com" }
`,
	})

	if err := SaveChanges([]Change{{Kind: "host", Name: "web-01", File: hosts, Remove: true}}); err != nil {
		t.Fatal(err)
	}

	got := read(t, hosts)
	if strings.Contains(got, "web-01") || !strings.Contains(got, "web-02") || !strings.Contains(got, "# inventory [web]") {
		t.Errorf("unexpected result:\n%s", got)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
