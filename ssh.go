package gorun

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	sshconfig "github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
)

// SSH is how gorun connects to a server. A server with none of it set runs on this machine.
type SSH struct {
	User         string `toml:"username"`
	Password     string `toml:"password"`
	Port         int    `toml:"port"`
	Identityfile string `toml:"identityfile"`
	// ConfigFile is an OpenSSH config file to fill the gaps from; Config is another name for it.
	ConfigFile string `toml:"config_file,omitempty"`
	Config     string `toml:"config,omitempty"`
}

// NewSSHClient connects to the server over SSH. What the Gorunfile doesn't set (the host's real
// name, user, port and keys) is taken from the server's SSH config file, or ~/.ssh/config.
func NewSSHClient(ctx context.Context, server Server) (*ssh.Client, error) {
	creds := server.SSH
	host := cmp.Or(server.Hostname, server.Name)
	var configKeys []string
	if cfg := readSSHConfig(creds.ConfigFile); cfg != nil {
		alias := host
		get := func(key string) string { v, _ := cfg.Get(alias, key); return v }
		host = cmp.Or(get("HostName"), alias)
		creds.User = cmp.Or(creds.User, get("User"))
		if port, err := strconv.Atoi(get("Port")); err == nil && creds.Port == 0 {
			creds.Port = port
		}
		if creds.Identityfile == "" {
			// every match, not just the first: a global IdentityFile must not hide the host's own
			configKeys, _ = cfg.GetAll(alias, "IdentityFile")
		}
	}

	var auth []ssh.AuthMethod
	signers, err := identitySigners(creds.Identityfile, configKeys)
	if err != nil {
		return nil, err
	}
	if len(signers) > 0 {
		auth = append(auth, ssh.PublicKeys(signers...))
	}
	if creds.Password != "" {
		auth = append(auth, ssh.Password(creds.Password))
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", host, cmp.Or(creds.Port, 22)), &ssh.ClientConfig{
		User:            creds.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating SSH client: %w", err)
	}
	return client, nil
}

// readSSHConfig reads the OpenSSH config at path, or ~/.ssh/config when path is empty. It
// returns nil when there is none to read.
func readSSHConfig(path string) *sshconfig.Config {
	path = expandHome(cmp.Or(path, "~/.ssh/config"))
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	cfg, err := sshconfig.Decode(f)
	if err != nil {
		return nil
	}
	return cfg
}

// identitySigners returns the keys to offer, in order. An explicit Gorunfile identityfile is the only key
// and must load. Keys from the SSH config are all offered, like OpenSSH, skipping any that are missing or
// can't be parsed without a passphrase.
func identitySigners(explicit string, fromConfig []string) ([]ssh.Signer, error) {
	if explicit != "" {
		signer, err := readSigner(explicit)
		if err != nil {
			return nil, err
		}
		return []ssh.Signer{signer}, nil
	}

	signers := []ssh.Signer{}
	seen := map[string]bool{}
	for _, path := range fromConfig {
		path = expandHome(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		signer, err := readSigner(path)
		if err != nil {
			slog.Debug("skipping SSH IdentityFile", "path", path, "error", err)
			continue
		}
		signers = append(signers, signer)
	}
	return signers, nil
}

func readSigner(path string) (ssh.Signer, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading SSH IdentityFile %q: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("error parsing SSH IdentityFile %q: %w", path, err)
	}
	return signer, nil
}

// expandHome expands the leading ~/ that ssh_config values may have.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// maxSessions is how many sessions gorun opens at once on one SSH connection.
// OpenSSH's sshd allows 10 by default (MaxSessions) and rejects any more with
// "open failed", so tasks beyond that wait for a free session instead.
const maxSessions = 10

// sessionSlots holds a semaphore of maxSessions slots per SSH connection.
var sessionSlots sync.Map // *ssh.Client -> chan struct{}

func slotsFor(client *ssh.Client) chan struct{} {
	v, _ := sessionSlots.LoadOrStore(client, make(chan struct{}, maxSessions))
	return v.(chan struct{})
}

// closeSSHClient closes client and forgets its session slots.
func closeSSHClient(client *ssh.Client) error {
	sessionSlots.Delete(client)
	return client.Close()
}

// newSession opens a session on client once one of its maxSessions slots is
// free. The returned close func closes the session and frees the slot.
//
// A server configured with a lower MaxSessions, or one that hasn't finished
// tearing down a just-closed session, rejects the open with "open failed";
// that is retried with backoff until ctx is done.
func newSession(ctx context.Context, client *ssh.Client) (*ssh.Session, func(), error) {
	slots := slotsFor(client)
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	backoff := 20 * time.Millisecond
	for attempt := 1; ; attempt++ {
		session, err := client.NewSession()
		if err == nil {
			var once sync.Once
			return session, func() {
				once.Do(func() {
					_ = session.Close()
					<-slots
				})
			}, nil
		}

		var openErr *ssh.OpenChannelError
		if !errors.As(err, &openErr) || openErr.Reason != ssh.ConnectionFailed || attempt >= 30 {
			<-slots
			return nil, nil, err
		}
		select {
		case <-time.After(backoff):
			backoff = min(2*backoff, time.Second)
		case <-ctx.Done():
			<-slots
			return nil, nil, ctx.Err()
		}
	}
}
