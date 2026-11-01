package gorun

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// limitedSSHServer is an in-process SSH server that, like sshd's MaxSessions,
// rejects sessions beyond maxSessions with "open failed". Every exec request
// takes a few milliseconds and exits 0.
type limitedSSHServer struct {
	maxSessions int
	open        atomic.Int32
	peak        atomic.Int32
	rejected    atomic.Int32
}

func (s *limitedSSHServer) start(t *testing.T) *ssh.Client {
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
			if int(s.open.Load()) >= s.maxSessions {
				s.rejected.Add(1)
				_ = nc.Reject(ssh.ConnectionFailed, "open failed")
				continue
			}
			n := s.open.Add(1)
			for {
				peak := s.peak.Load()
				if n <= peak || s.peak.CompareAndSwap(peak, n) {
					break
				}
			}
			ch, chReqs, err := nc.Accept()
			if err != nil {
				s.open.Add(-1)
				continue
			}
			go func() {
				defer s.open.Add(-1)
				defer ch.Close()
				for req := range chReqs {
					if req.Type != "exec" {
						_ = req.Reply(false, nil)
						continue
					}
					_ = req.Reply(true, nil)
					time.Sleep(5 * time.Millisecond)
					_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
					return
				}
			}()
		}
	}()

	client, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeSSHClient(client) })
	return client
}

func runSessions(t *testing.T, client *ssh.Client, n int) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			session, closeSession, err := newSession(context.Background(), client)
			if err != nil {
				errs <- err
				return
			}
			defer closeSession()
			if err := session.Run("true"); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestNewSessionRespectsMaxSessions runs far more tasks at once than sshd's
// default MaxSessions over a single connection: none may be rejected.
func TestNewSessionRespectsMaxSessions(t *testing.T) {
	srv := &limitedSSHServer{maxSessions: 10}
	client := srv.start(t)

	runSessions(t, client, 50)

	if peak := srv.peak.Load(); peak > maxSessions {
		t.Errorf("%d sessions were open at once, want at most %d", peak, maxSessions)
	}
}

// TestNewSessionRetriesOpenFailed covers servers with a lower MaxSessions:
// rejected opens are retried until a session frees up.
func TestNewSessionRetriesOpenFailed(t *testing.T) {
	srv := &limitedSSHServer{maxSessions: 3}
	client := srv.start(t)

	runSessions(t, client, 20)

	if srv.rejected.Load() == 0 {
		t.Error("expected the server to reject some sessions")
	}
}

// TestNewSessionWaitIsCancellable makes sure a task waiting for a free session
// gives up when the run is cancelled.
func TestNewSessionWaitIsCancellable(t *testing.T) {
	srv := &limitedSSHServer{maxSessions: maxSessions}
	client := srv.start(t)

	closers := []func(){}
	for range maxSessions {
		_, closeSession, err := newSession(context.Background(), client)
		if err != nil {
			t.Fatal(err)
		}
		closers = append(closers, closeSession)
	}
	defer func() {
		for _, c := range closers {
			c()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := newSession(ctx, client); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}
