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
