package gorun

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	sshconfig "github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
)

// SSH represents SSH creds
type SSH struct {
	User         string `toml:"username"`
	Password     string `toml:"password"`
	Port         int    `toml:"port"`
	Identityfile string `toml:"identityfile"`
	ConfigFile   string `toml:"config_file,omitempty"`
	Config       string `toml:"config,omitempty"`
}

// NewSSHClient creates a new SSH client connected to the provided server.
func NewSSHClient(ctx context.Context, server Server) (*ssh.Client, error) {
	// Resolve an SSH config file: use the one given, otherwise fall back to the
	// default ~/.ssh/config when it exists.
	configPath := server.SSH.ConfigFile
	if configPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".ssh", "config")
			if _, err := os.Stat(candidate); err == nil {
				configPath = candidate
			}
		}
	} else {
		if strings.HasPrefix(configPath, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				configPath = filepath.Join(home, configPath[2:])
			}
		}
	}

	// Apply settings from the SSH config, filling in any gaps (explicit values in
	// the Gorunfile always take precedence).
	host := server.Hostname
	if host == "" {
		host = server.Name
	}
	alias := host
	resolvedHost := host
	var configKeys []string
	if configPath != "" {
		if f, err := os.Open(configPath); err == nil {
			if cfg, err := sshconfig.Decode(f); err == nil {
				_ = f.Close()
				if v, err := cfg.Get(alias, "HostName"); err == nil && v != "" && v != alias {
					resolvedHost = v
				}
				if server.SSH.User == "" {
					if v, err := cfg.Get(alias, "User"); err == nil && v != "" {
						server.SSH.User = v
					}
				}
				if server.SSH.Port == 0 {
					if v, err := cfg.Get(alias, "Port"); err == nil && v != "" {
						if p, err := strconv.Atoi(v); err == nil {
							server.SSH.Port = p
						}
					}
				}
				if server.SSH.Identityfile == "" {
					// every match, not just the first: a global IdentityFile must not hide the host's own
					configKeys, _ = cfg.GetAll(alias, "IdentityFile")
				}
			} else {
				_ = f.Close()
			}
		}
	}
	host = resolvedHost

	port := 22
	if server.SSH.Port != 0 {
		port = server.SSH.Port
	}

	address := fmt.Sprintf("%s:%d", host, port)
	auth := []ssh.AuthMethod{}

	signers, err := identitySigners(server.SSH.Identityfile, configKeys)
	if err != nil {
		return nil, err
	}
	if len(signers) > 0 {
		auth = append(auth, ssh.PublicKeys(signers...))
	}

	// if we have a SSH password, add it to the list of auths
	if server.SSH.Password != "" {
		auth = append(auth, ssh.Password(server.SSH.Password))
	}

	config := &ssh.ClientConfig{
		Config:          ssh.Config{},
		User:            server.SSH.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return nil, fmt.Errorf("error creating SSH client: %w", err)
	}

	return client, nil
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

// scpSender drives the source side of the scp protocol against a remote
// `scp -t` sink. Every command is acknowledged by the sink before the next one.
type scpSender struct {
	w io.Writer
	r *bufio.Reader
	// wrap, when set, wraps each file's contents as they are sent, to count them.
	wrap func(io.Reader) io.Reader
}

// ack reads the sink's reply: 0 is OK, 1 (warning) and 2 (fatal) carry a message.
func (s *scpSender) ack() error {
	b, err := s.r.ReadByte()
	if err != nil {
		return fmt.Errorf("scp: reading reply: %w", err)
	}
	switch b {
	case 0:
		return nil
	case 1, 2:
		msg, _ := s.r.ReadString('\n')
		return fmt.Errorf("scp: %s", strings.TrimSpace(msg))
	default:
		return fmt.Errorf("scp: unexpected reply %q", b)
	}
}

func (s *scpSender) command(format string, args ...any) error {
	if _, err := fmt.Fprintf(s.w, format, args...); err != nil {
		return err
	}
	return s.ack()
}

// file sends one regular file named name into the sink's current directory.
func (s *scpSender) file(name string, mode os.FileMode, size int64, r io.Reader) error {
	if err := s.command("C%04o %d %s\n", mode.Perm(), size, name); err != nil {
		return err
	}
	if s.wrap != nil {
		r = s.wrap(r)
	}
	if _, err := io.CopyN(s.w, r, size); err != nil {
		return err
	}
	if _, err := s.w.Write([]byte{0}); err != nil {
		return err
	}
	return s.ack()
}

// dirContents sends every entry of localDir into the sink's current directory,
// descending into subdirectories. Symlinks are followed, as `scp -r` does.
func (s *scpSender) dirContents(localDir string) error {
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(localDir, e.Name())
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if err := s.command("D%04o 0 %s\n", info.Mode().Perm(), e.Name()); err != nil {
				return err
			}
			if err := s.dirContents(path); err != nil {
				return err
			}
			if err := s.command("E\n"); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := s.localFile(path, e.Name(), info); err != nil {
				return err
			}
		default:
			return fmt.Errorf("scp: %s is not a regular file or directory", path)
		}
	}
	return nil
}

func (s *scpSender) localFile(path, name string, info os.FileInfo) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return s.file(name, info.Mode(), info.Size(), f)
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runRemote runs cmd on the remote and returns its error together with stderr.
func runRemote(client *ssh.Client, cmd string) error {
	session, closeSession, err := newSession(context.Background(), client)
	if err != nil {
		return err
	}
	defer closeSession()
	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runSCP starts `scp <flags> -t target` on the remote and lets send stream into it. Cancelling
// ctx closes the session, which stops a copy even while it waits on a slow connection.
func runSCP(ctx context.Context, client *ssh.Client, flags, target string, wrap func(io.Reader) io.Reader, send func(*scpSender) error) error {
	session, closeSession, err := newSession(ctx, client)
	if err != nil {
		return err
	}
	defer closeSession()
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stop()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	session.Stderr = &stderr

	if err := session.Start("scp " + flags + " -t " + shellQuote(target)); err != nil {
		return err
	}
	s := &scpSender{w: stdin, r: bufio.NewReader(stdout), wrap: wrap}
	sendErr := s.ack() // the sink announces it is ready
	if sendErr == nil {
		sendErr = send(s)
	}
	_ = stdin.Close()
	waitErr := session.Wait()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scp to %s: %w", target, err)
	}
	if err := errors.Join(sendErr, waitErr); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("scp to %s: %w: %s", target, err, msg)
		}
		return fmt.Errorf("scp to %s: %w", target, err)
	}
	return nil
}

// scpCopy copies r (size bytes) to remotePath. A nil client writes locally.
func scpCopy(ctx context.Context, client *ssh.Client, r io.Reader, size int64, remotePath string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0644
	}
	if client == nil {
		if err := os.MkdirAll(filepath.Dir(remotePath), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(remotePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, r)
		return err
	}
	return runSCP(ctx, client, "", filepath.Dir(remotePath), nil, func(s *scpSender) error {
		return s.file(filepath.Base(remotePath), mode, size, r)
	})
}

// scpCopyDir recursively copies the contents of localDir into remoteDir,
// creating remoteDir (and its parents) if needed. A nil client copies locally.
// wrap, when not nil, wraps each file's contents as they are copied.
func scpCopyDir(ctx context.Context, client *ssh.Client, localDir, remoteDir string, wrap func(io.Reader) io.Reader) error {
	if client == nil {
		return copyDirLocal(localDir, remoteDir, wrap)
	}
	if err := runRemote(client, "mkdir -p "+shellQuote(remoteDir)); err != nil {
		return err
	}
	return runSCP(ctx, client, "-r -d", remoteDir, wrap, func(s *scpSender) error {
		return s.dirContents(localDir)
	})
}

// copyDirLocal recursively copies src into dst, overwriting existing files.
func copyDirLocal(src, dst string, wrap func(io.Reader) io.Reader) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		info, err := os.Stat(from)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyDirLocal(from, to, wrap); err != nil {
				return err
			}
			continue
		}
		in, err := os.Open(from)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			in.Close()
			return err
		}
		var r io.Reader = in
		if wrap != nil {
			r = wrap(in)
		}
		if _, err := io.Copy(out, r); err != nil {
			in.Close()
			out.Close()
			return err
		}
		if err := in.Close(); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}

// CopyFiles copies files for a task to a server. Templates are executed with
// server as their data, the same as cmd_template.
func CopyFiles(client *ssh.Client, server any, task *Task) error {
	return CopyFilesContext(context.Background(), client, server, task, nil)
}

// CopyFilesContext is CopyFiles with a context and a progress callback. Cancelling ctx stops
// a copy part way. progress, when not nil, is told how far each file and directory copy has
// got: once at the start, then at most every quarter of a second, and once more when the copy
// is done. Inline content and templates are not reported.
func CopyFilesContext(ctx context.Context, client *ssh.Client, server any, task *Task, progress CopyProgressFunc) error {
	if ctx == nil {
		ctx = context.Background()
	}
	copies := task.getCopies()
	for _, c := range copies {
		cc := c
		if cc.SrcFile != "" && cc.Src == "" {
			cc.Src = cc.SrcFile
		}
		if cc.Template != "" && cc.SrcTpl == "" {
			cc.SrcTpl = cc.Template
		}
		if cc.SrcTmpl != "" && cc.SrcTpl == "" {
			cc.SrcTpl = cc.SrcTmpl
		}
		if cc.DstPath != "" && cc.Dst == "" {
			cc.Dst = cc.DstPath
		}
		if cc.Dest != "" && cc.Dst == "" {
			cc.Dst = cc.Dest
		}
		dst := cc.Dst
		srcPath := cc.Src
		if dst == "" && cc.Dir != "" && srcPath != "" {
			dst = filepath.Join(cc.Dir, filepath.Base(srcPath))
		}

		if cc.Content != "" {
			data := []byte(cc.Content)
			if err := scpCopy(ctx, client, bytes.NewReader(data), int64(len(data)), dst, 0); err != nil {
				return err
			}
			continue
		}
		if cc.SrcTpl != "" {
			tmplData, err := os.ReadFile(cc.SrcTpl)
			if err != nil {
				return err
			}
			tmpl, err := template.New(filepath.Base(cc.SrcTpl)).Parse(string(tmplData))
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, task.templateData(server)); err != nil {
				return err
			}
			data := buf.Bytes()
			if err := scpCopy(ctx, client, bytes.NewReader(data), int64(len(data)), dst, 0); err != nil {
				return err
			}
			continue
		}
		if srcPath != "" {
			info, err := os.Stat(srcPath)
			if err != nil {
				return err
			}
			if info.IsDir() {
				total, err := dirSize(srcPath)
				if err != nil {
					return err
				}
				tr := newCopyTracker(ctx, progress, dst, total)
				if err := scpCopyDir(ctx, client, srcPath, dst, tr.wrap); err != nil {
					return err
				}
				tr.done()
				continue
			}
			f, err := os.Open(srcPath)
			if err != nil {
				return err
			}
			tr := newCopyTracker(ctx, progress, dst, info.Size())
			err = scpCopy(ctx, client, tr.wrap(f), info.Size(), dst, info.Mode().Perm())
			f.Close()
			if err != nil {
				return err
			}
			tr.done()
			continue
		}
		return fmt.Errorf("no source for copy")
	}
	return nil
}
