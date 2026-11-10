package gorun

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sftpServer starts an in-process SSH server that offers only the sftp subsystem, serving this
// machine's files, and returns a client connected to it.
func sftpServer(t *testing.T) *ssh.Client {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
		if err != nil {
			return
		}
		go ssh.DiscardRequests(reqs)
		for nc := range chans {
			ch, chReqs, err := nc.Accept()
			if err != nil {
				continue
			}
			go func() {
				for req := range chReqs {
					ok := req.Type == "subsystem" && string(req.Payload[4:]) == "sftp"
					_ = req.Reply(ok, nil)
					if ok {
						if server, err := sftp.NewServer(ch); err == nil {
							_ = server.Serve()
						}
						_ = ch.Close()
						return
					}
				}
			}()
		}
	}()

	client, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestCompareCopiesOverSFTP(t *testing.T) {
	client := sftpServer(t)
	src, remote := t.TempDir(), t.TempDir()
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(src, "app.conf"), "port = 8080\n")
	write(filepath.Join(src, "site", "index.html"), "<h1>v2</h1>")
	write(filepath.Join(src, "site", "css", "app.css"), "body{}")
	// the server already has the config and the page, but an older stylesheet of the same size
	write(filepath.Join(remote, "app.conf"), "port = 8080\n")
	write(filepath.Join(remote, "site", "index.html"), "<h1>v2</h1>")
	write(filepath.Join(remote, "site", "css", "app.css"), "body{;")
	write(filepath.Join(remote, "site", "extra.txt"), "files the copy doesn't include are ignored")

	exec := &TaskExec{server: client, target: &Server{Name: "web-1"}, task: Task{Copy: []Copy{
		{Src: filepath.Join(src, "app.conf"), Dst: filepath.Join(remote, "app.conf")},
		{Src: filepath.Join(src, "site"), Dst: filepath.Join(remote, "site")},
		{Content: "new note", Dst: filepath.Join(remote, "missing", "note.txt")},
	}}}
	res, err := exec.compareCopies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	if res.ExitCode != 1 || len(lines) != 3 ||
		!strings.HasPrefix(lines[0], "same") ||
		!strings.Contains(lines[1], "changed") || !strings.Contains(lines[1], "1 of 2 files: css/app.css") ||
		!strings.Contains(lines[2], "changed") {
		t.Fatalf("exit %d, report:\n%s", res.ExitCode, res.Stdout)
	}

	write(filepath.Join(remote, "site", "css", "app.css"), "body{}")
	write(filepath.Join(remote, "missing", "note.txt"), "new note")
	if res, err := exec.compareCopies(context.Background()); err != nil || res.ExitCode != 0 {
		t.Fatalf("after syncing: exit %d, err %v, report:\n%s", res.ExitCode, err, res.Stdout)
	}
}
