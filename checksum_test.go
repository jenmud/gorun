package gorun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// putFile writes text to path, creating its directories.
func putFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shWith returns a runFunc that runs commands through this machine's sh with only tools on the
// PATH, as a server that has just those would.
func shWith(t *testing.T, tools ...string) runFunc {
	bin := t.TempDir()
	for _, tool := range tools {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not installed", tool)
		}
		if err := os.Symlink(real, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return func(ctx context.Context, command string, _ bool) ([]byte, []byte, error) {
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Env = []string{"PATH=" + bin}
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return []byte(stdout.String()), []byte(stderr.String()), err
	}
}

// remoteCopies sets up copies whose targets partly match, and a TaskExec that treats this
// machine as a server, so the comparison runs the server-side script.
func remoteCopies(t *testing.T) (*TaskExec, string) {
	src, remote := t.TempDir(), t.TempDir()
	putFile(t, filepath.Join(src, "app.conf"), "port = 8080\n")
	putFile(t, filepath.Join(src, "site", "index.html"), "<h1>v2</h1>")
	putFile(t, filepath.Join(src, "site", "css", "app.css"), "body{}")
	// the server has the config and the page, but an older stylesheet, and no note
	putFile(t, filepath.Join(remote, "app.conf"), "port = 8080\n")
	putFile(t, filepath.Join(remote, "site", "index.html"), "<h1>v2</h1>")
	putFile(t, filepath.Join(remote, "site", "css", "app.css"), "body{;")
	putFile(t, filepath.Join(remote, "site", "extra.txt"), "files the copy doesn't include are ignored")

	return &TaskExec{server: &ssh.Client{}, target: &Server{Name: "web-1"}, task: Task{Copy: []Copy{
		{Src: filepath.Join(src, "app.conf"), Dst: filepath.Join(remote, "app.conf")},
		{Src: filepath.Join(src, "site"), Dst: filepath.Join(remote, "site")},
		{Content: "new note", Dst: filepath.Join(remote, "missing", "note.txt")},
	}}}, remote
}

func TestCompareCopiesWithEachServerTool(t *testing.T) {
	// md5 -r on macOS and BSD prints "<md5> <name>", as openssl md5 -r does but without the '*'
	fakeMD5 := t.TempDir()
	putFile(t, filepath.Join(fakeMD5, "md5"), "#!/bin/sh\n[ \"$1\" = -r ] && shift\nset -- $(md5sum \"$1\") || exit 1\necho \"$1 $2\"\n")
	if err := os.Chmod(filepath.Join(fakeMD5, "md5"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeMD5+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, tools := range [][]string{{"md5sum", "find"}, {"md5", "md5sum", "find"}, {"openssl", "find"}} {
		t.Run(tools[0], func(t *testing.T) {
			run := shWith(t, append(tools, "sh")...)
			if tools[0] == "md5" {
				// md5sum is only there for the fake md5 to use: hide it from the script
				run = hideMD5Sum(run)
			}
			exec, remote := remoteCopies(t)
			res, err := exec.compareCopies(context.Background(), run)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
			if res.ExitCode != 1 || len(lines) != 3 ||
				!strings.HasPrefix(lines[0], "same") ||
				!strings.Contains(lines[1], "changed") || !strings.Contains(lines[1], "1 of 2 files: css/app.css") ||
				!strings.Contains(lines[2], "changed") {
				t.Fatalf("exit %d, report:\n%s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
			}

			putFile(t, filepath.Join(remote, "site", "css", "app.css"), "body{}")
			putFile(t, filepath.Join(remote, "missing", "note.txt"), "new note")
			if res, err := exec.compareCopies(context.Background(), run); err != nil || res.ExitCode != 0 {
				t.Fatalf("after syncing: exit %d, err %v, report:\n%s", res.ExitCode, err, res.Stdout)
			}
		})
	}
}

// hideMD5Sum makes the script's "command -v md5sum" fail, so it falls through to md5.
func hideMD5Sum(run runFunc) runFunc {
	return func(ctx context.Context, command string, copies bool) ([]byte, []byte, error) {
		return run(ctx, strings.Replace(command, "command -v md5sum", "false", 1), copies)
	}
}

func TestCompareCopiesCopiesAnywayWhenTheServerCantHash(t *testing.T) {
	for name, run := range map[string]runFunc{
		"no md5 tool": shWith(t, "sh", "find"),
		"no sh":       shWith(t),
	} {
		t.Run(name, func(t *testing.T) {
			exec, remote := remoteCopies(t)
			// even a copy the server already has counts as changed: it can't be compared
			putFile(t, filepath.Join(remote, "site", "css", "app.css"), "body{}")
			putFile(t, filepath.Join(remote, "missing", "note.txt"), "new note")
			res, err := exec.compareCopies(context.Background(), run)
			if err != nil {
				t.Fatal(err)
			}
			if res.ExitCode != 1 || strings.Count(res.Stdout, "not compared") != 3 {
				t.Fatalf("exit %d, report:\n%s", res.ExitCode, res.Stdout)
			}
		})
	}
}
