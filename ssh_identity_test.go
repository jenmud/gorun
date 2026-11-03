package gorun

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// writeKey writes a fresh unencrypted OpenSSH private key to dir/name and returns its public key.
func writeKey(t *testing.T, dir, name string) ssh.PublicKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return sshPub
}

// keyOnlySSHServer accepts one connection that authenticates with allowed and returns its address.
func keyOnlySSHServer(t *testing.T, allowed ssh.PublicKey) string {
	t.Helper()
	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), allowed.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("key not allowed")
		},
	}
	cfg.AddHostKey(hostSigner)

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
			_ = nc.Reject(ssh.Prohibited, "no channels")
		}
	}()
	return ln.Addr().String()
}

// TestNewSSHClientTriesEveryConfigIdentityFile mirrors OpenSSH: a global IdentityFile listed before the
// host's own must not hide it, and a listed key that doesn't exist is skipped.
func TestNewSSHClientTriesEveryConfigIdentityFile(t *testing.T) {
	dir := t.TempDir()
	writeKey(t, dir, "global")
	hostKey := writeKey(t, dir, "host")
	addr := keyOnlySSHServer(t, hostKey)
	host, port, _ := net.SplitHostPort(addr)

	config := fmt.Sprintf(`IdentityFile %s
IdentityFile %s

Host %s
    Port %s
    User test
    IdentityFile %s
`, filepath.Join(dir, "global"), filepath.Join(dir, "missing"), host, port, filepath.Join(dir, "host"))
	configPath := filepath.Join(dir, "config")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	client, err := NewSSHClient(context.Background(), Server{Name: "s", Hostname: host, SSH: SSH{ConfigFile: configPath}})
	if err != nil {
		t.Fatalf("NewSSHClient: %v", err)
	}
	_ = client.Close()
}

// TestNewSSHClientExplicitIdentityfileWins checks a Gorunfile identityfile is the only key offered.
func TestNewSSHClientExplicitIdentityfileWins(t *testing.T) {
	dir := t.TempDir()
	writeKey(t, dir, "explicit")
	hostKey := writeKey(t, dir, "host")
	addr := keyOnlySSHServer(t, hostKey)
	host, port, _ := net.SplitHostPort(addr)

	config := fmt.Sprintf("Host %s\n    Port %s\n    User test\n    IdentityFile %s\n", host, port, filepath.Join(dir, "host"))
	configPath := filepath.Join(dir, "config")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	server := Server{Name: "s", Hostname: host, SSH: SSH{ConfigFile: configPath, Identityfile: filepath.Join(dir, "explicit")}}
	if client, err := NewSSHClient(context.Background(), server); err == nil {
		_ = client.Close()
		t.Fatal("NewSSHClient connected with a config key, want only the explicit identityfile offered")
	}
}
