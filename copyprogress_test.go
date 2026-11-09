package gorun

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// progressLog collects copy progress reports, safely from any goroutine.
type progressLog struct {
	mu   sync.Mutex
	seen []CopyProgress
}

func (l *progressLog) add(p CopyProgress) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, p)
}

func (l *progressLog) all() []CopyProgress {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]CopyProgress(nil), l.seen...)
}

// checkProgress checks a copy's reports: 0% first, never going backwards, and Done last with
// every byte counted.
func checkProgress(t *testing.T, seen []CopyProgress, file string, total int64) {
	t.Helper()
	if len(seen) < 2 {
		t.Fatalf("got %d progress reports, want a start and an end: %+v", len(seen), seen)
	}
	if first := seen[0]; first.Bytes != 0 || first.Done || first.Total != total {
		t.Errorf("first report = %+v, want 0 of %d", first, total)
	}
	last := seen[len(seen)-1]
	if !last.Done || last.Bytes != total || last.Total != total || last.Percent() != 100 {
		t.Errorf("last report = %+v (%.0f%%), want done with %d bytes", last, last.Percent(), total)
	}
	for i, p := range seen {
		if p.File != file {
			t.Errorf("report %d is for %q, want %q", i, p.File, file)
		}
		if i > 0 && p.Bytes < seen[i-1].Bytes {
			t.Errorf("report %d went backwards: %d after %d", i, p.Bytes, seen[i-1].Bytes)
		}
		if p.Done && i != len(seen)-1 {
			t.Errorf("report %d is done but is not the last", i)
		}
	}
}

// slowReader hands out at most chunk bytes per read, pausing between reads, so a copy takes
// long enough to report progress along the way.
type slowReader struct {
	r     io.Reader
	chunk int
	pause time.Duration
}

func (s *slowReader) Read(b []byte) (int, error) {
	time.Sleep(s.pause)
	if len(b) > s.chunk {
		b = b[:s.chunk]
	}
	return s.r.Read(b)
}

func TestCopyProgressLocalFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.bin")
	data := bytes.Repeat([]byte("x"), 3<<20)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "big.bin")

	var log progressLog
	task := &Task{Copy: []Copy{{Src: src, Dst: dst}}}
	if err := CopyFilesContext(context.Background(), nil, &Server{}, task, log.add); err != nil {
		t.Fatal(err)
	}
	checkProgress(t, log.all(), dst, int64(len(data)))
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data) {
		t.Fatal("the copy is not the same as the source")
	}
}

func TestCopyProgressDirectoryCountsEveryFile(t *testing.T) {
	src := t.TempDir()
	files := map[string]int{"a.txt": 1000, "sub/b.txt": 2500, "sub/deeper/c.txt": 40}
	var total int64
	for name, size := range files {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, bytes.Repeat([]byte("y"), size), 0o644); err != nil {
			t.Fatal(err)
		}
		total += int64(size)
	}
	dst := filepath.Join(t.TempDir(), "site")

	var log progressLog
	task := &Task{Copy: []Copy{{Src: src, Dst: dst}}}
	if err := CopyFilesContext(context.Background(), nil, &Server{}, task, log.add); err != nil {
		t.Fatal(err)
	}
	checkProgress(t, log.all(), dst, total)
}

func TestCopyProgressSkipsInlineContent(t *testing.T) {
	var log progressLog
	task := &Task{Copy: []Copy{{Content: "hello", Dst: filepath.Join(t.TempDir(), "hello.txt")}}}
	if err := CopyFilesContext(context.Background(), nil, &Server{}, task, log.add); err != nil {
		t.Fatal(err)
	}
	if seen := log.all(); len(seen) != 0 {
		t.Fatalf("inline content reported progress: %+v", seen)
	}
}

func TestCopyProgressReportsAlongTheWay(t *testing.T) {
	var log progressLog
	tr := newCopyTracker(context.Background(), log.add, "/srv/big.bin", 64<<10)
	r := tr.wrap(&slowReader{r: bytes.NewReader(make([]byte, 64<<10)), chunk: 4 << 10, pause: 60 * time.Millisecond})
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatal(err)
	}
	tr.done()

	seen := log.all()
	checkProgress(t, seen, "/srv/big.bin", 64<<10)
	// 16 reads of 60ms is about a second: a few reports in between, but not one per read
	if middle := len(seen) - 2; middle < 2 || middle > 8 {
		t.Errorf("got %d reports between start and end, want a few", middle)
	}
	if rate := seen[len(seen)-1].BytesPerSecond(); rate <= 0 {
		t.Errorf("rate = %v, want above zero", rate)
	}
}

func TestCopyStopsWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(src, make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	task := &Task{Copy: []Copy{{Src: src, Dst: filepath.Join(dir, "out.bin")}}}
	err := CopyFilesContext(ctx, nil, &Server{}, task, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// scpSinkServer is an in-process SSH server that answers `scp -t` as a real sink does, writing
// what it receives into dir. With stall set it stops reading after the first 64 KiB of a
// file, like a server whose connection has hung.
type scpSinkServer struct {
	dir   string
	stall bool
}

func (s *scpSinkServer) start(t *testing.T) *ssh.Client {
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
			go s.serve(ch, chReqs)
		}
	}()

	client, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func (s *scpSinkServer) serve(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		_ = req.Reply(true, nil)
		status := 0
		if err := s.sink(ch); err != nil {
			status = 1
		}
		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(status)}))
		return
	}
}

// sink speaks the receiving side of scp for single files: a C line, the bytes, then a zero.
func (s *scpSinkServer) sink(ch ssh.Channel) error {
	r := bufio.NewReader(ch)
	ok := func() { _, _ = ch.Write([]byte{0}) }
	ok()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil // the sender is done
		}
		fields := strings.SplitN(strings.TrimSpace(line), " ", 3)
		if len(fields) != 3 || !strings.HasPrefix(fields[0], "C") {
			return fmt.Errorf("unexpected scp command %q", line)
		}
		size, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return err
		}
		ok()
		if s.stall {
			_, _ = io.CopyN(io.Discard, r, 64<<10)
			select {} // never read again
		}
		f, err := os.Create(filepath.Join(s.dir, fields[2]))
		if err != nil {
			return err
		}
		_, err = io.CopyN(f, r, size)
		_ = f.Close()
		if err != nil {
			return err
		}
		if _, err := r.ReadByte(); err != nil {
			return err
		}
		ok()
	}
}

func TestCopyProgressOverSSH(t *testing.T) {
	sinkDir := t.TempDir()
	client := (&scpSinkServer{dir: sinkDir}).start(t)

	src := filepath.Join(t.TempDir(), "release.tar.gz")
	data := bytes.Repeat([]byte("gorun"), 1<<20) // 5 MiB
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}

	var log progressLog
	task := &Task{Copy: []Copy{{Src: src, Dst: "/srv/app/release.tar.gz"}}}
	if err := CopyFilesContext(context.Background(), client, &Server{}, task, log.add); err != nil {
		t.Fatal(err)
	}
	checkProgress(t, log.all(), "/srv/app/release.tar.gz", int64(len(data)))
	if got, _ := os.ReadFile(filepath.Join(sinkDir, "release.tar.gz")); !bytes.Equal(got, data) {
		t.Fatal("the server did not receive the whole file")
	}
}

func TestCopyOverSSHStopsWhenCancelled(t *testing.T) {
	client := (&scpSinkServer{dir: t.TempDir(), stall: true}).start(t)

	src := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(src, make([]byte, 32<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var log progressLog
	time.AfterFunc(300*time.Millisecond, cancel)

	errc := make(chan error, 1)
	go func() {
		task := &Task{Copy: []Copy{{Src: src, Dst: "/srv/big.bin"}}}
		errc <- CopyFilesContext(ctx, client, &Server{}, task, log.add)
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the copy kept going after the run was cancelled")
	}
	for _, p := range log.all() {
		if p.Done {
			t.Fatal("a cancelled copy reported done")
		}
	}
}

func TestRunEmitsCopyProgress(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "artifact.bin")
	if err := os.WriteFile(src, make([]byte, 2<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "deployed", "artifact.bin")

	var mu sync.Mutex
	var events []CopyProgressEvent
	cfg := &Config{
		Tasks: map[string]Task{"upload": {Name: "upload", Local: true, Copy: []Copy{{Src: src, Dst: dst}}}},
		OnEvent: func(event any) {
			if ev, ok := event.(CopyProgressEvent); ok {
				mu.Lock()
				events = append(events, ev)
				mu.Unlock()
			}
		},
	}
	cfg.Servers = []Server{{Name: "local", TaskNames: []string{"upload"}}}
	if err := cfg.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) < 2 {
		t.Fatalf("got %d copy_progress events, want at least 2", len(events))
	}
	last := events[len(events)-1]
	if last.Type != "copy_progress" || last.Server != "local" || last.Task != "upload" || last.File != dst ||
		!last.Done || last.Percent != 100 || last.Total != 2<<20 || last.GorunUUID == "" {
		t.Fatalf("last event = %+v", last)
	}
}
