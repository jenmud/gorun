package gorun

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogFilePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "default", env: "", want: filepath.Join(home, ".gorun", "gorun.log")},
		{name: "absolute", env: "/var/log/gorun.log", want: "/var/log/gorun.log"},
		{name: "home expanded", env: "~/logs/gorun.log", want: filepath.Join(home, "logs", "gorun.log")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GORUN_LOG_FILE", tt.env)
			if got := LogFilePath(); got != tt.want {
				t.Errorf("LogFilePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpenLogFile_CreatesDirAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "gorun.log")

	for _, line := range []string{"first\n", "second\n"} {
		f, err := OpenLogFile(path)
		if err != nil {
			t.Fatalf("OpenLogFile: %v", err)
		}
		if _, err := f.WriteString(line); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "first\nsecond\n"; string(got) != want {
		t.Errorf("log file = %q, want %q", got, want)
	}
}
