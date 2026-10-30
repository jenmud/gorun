package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCompletionNames(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "Gorunfile")
	content := `[tasks.zeta]
cmd = "true"

[tasks.alpha]
cmd = "true"

[servers.hosts.web-1]
tasks = ["alpha"]

[servers.hosts.db-1]
tasks = ["zeta"]
`
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		kind string
		want []string
	}{
		{kind: "tasks", want: []string{"alpha", "zeta"}},
		{kind: "servers", want: []string{"db-1", "web-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got, err := completionNames(tt.kind, filename)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("completionNames(%q) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestCompletionNamesUnknownKind(t *testing.T) {
	if _, err := completionNames("flags", "Gorunfile"); err == nil {
		t.Fatal("expected an error for an unknown completion kind")
	}
}

func TestCompletionNamesMissingFile(t *testing.T) {
	if _, err := completionNames("tasks", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected an error for a missing Gorunfile")
	}
}
