package gorun

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFiles_TemplateGetsServer(t *testing.T) {
	dir := t.TempDir()
	tmpl := filepath.Join(dir, "Caddyfile.tmpl")
	if err := os.WriteFile(tmpl, []byte("{{ .Name }} {{ .Hostname }}:{{ .SSH.Port }}"), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "Caddyfile")
	task := &Task{Name: "copy", Copy: []Copy{{SrcTpl: tmpl, Dst: dst}}}
	server := &Server{Name: "otp", Hostname: "otp.example.com", SSH: SSH{Port: 2222}}

	// a nil client writes locally, which is enough to check the rendered template
	if err := CopyFiles(nil, server, task); err != nil {
		t.Fatalf("CopyFiles: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if want := "otp otp.example.com:2222"; string(got) != want {
		t.Errorf("rendered template = %q, want %q", got, want)
	}
}

func TestCopyFiles_Directory(t *testing.T) {
	src := t.TempDir()
	files := map[string]string{
		"a.txt":           "a",
		"sub/b.txt":       "b",
		"sub/deep/c.conf": "c",
	}
	for name, content := range files {
		path := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := t.TempDir()
	tests := []struct {
		name string
		copy Copy
		root string
	}{
		{"dst receives contents", Copy{Src: src, Dst: filepath.Join(out, "app")}, filepath.Join(out, "app")},
		{"dir receives the directory", Copy{Src: src, Dir: filepath.Join(out, "parent")}, filepath.Join(out, "parent", filepath.Base(src))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{Name: "copy-dir", Copy: []Copy{tt.copy}}
			if err := CopyFiles(nil, &Server{}, task); err != nil {
				t.Fatalf("CopyFiles: %v", err)
			}
			for name, want := range files {
				got, err := os.ReadFile(filepath.Join(tt.root, name))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}
