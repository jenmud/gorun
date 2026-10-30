package gorun

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
					if v, err := cfg.Get(alias, "IdentityFile"); err == nil && v != "" {
						// ssh_config may return a path with a leading ~
						if strings.HasPrefix(v, "~/") {
							if home, err := os.UserHomeDir(); err == nil {
								v = filepath.Join(home, v[2:])
							}
						}
						server.SSH.Identityfile = v
					}
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

	// Create the Signer for this private key.
	if server.SSH.Identityfile != "" {
		key, err := os.ReadFile(server.SSH.Identityfile)
		if err != nil {
			err = fmt.Errorf("error reading SSH IdentityFile %q: %w", server.SSH.Identityfile, err)
			return nil, err
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			err = fmt.Errorf("error parsing SSH IdentityFile %q: %w", server.SSH.Identityfile, err)
			return nil, err
		}

		auth = append(auth, ssh.PublicKeys(signer))
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

// scpSender drives the source side of the scp protocol against a remote
// `scp -t` sink. Every command is acknowledged by the sink before the next one.
type scpSender struct {
	w io.Writer
	r *bufio.Reader
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
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runSCP starts `scp <flags> -t target` on the remote and lets send stream into it.
func runSCP(client *ssh.Client, flags, target string, send func(*scpSender) error) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

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
	s := &scpSender{w: stdin, r: bufio.NewReader(stdout)}
	sendErr := s.ack() // the sink announces it is ready
	if sendErr == nil {
		sendErr = send(s)
	}
	_ = stdin.Close()
	waitErr := session.Wait()

	if err := errors.Join(sendErr, waitErr); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("scp to %s: %w: %s", target, err, msg)
		}
		return fmt.Errorf("scp to %s: %w", target, err)
	}
	return nil
}

// scpCopy copies data to remotePath. A nil client writes locally.
func scpCopy(client *ssh.Client, data []byte, remotePath string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0644
	}
	if client == nil {
		return os.WriteFile(remotePath, data, mode)
	}
	return runSCP(client, "", filepath.Dir(remotePath), func(s *scpSender) error {
		return s.file(filepath.Base(remotePath), mode, int64(len(data)), bytes.NewReader(data))
	})
}

// scpCopyDir recursively copies the contents of localDir into remoteDir,
// creating remoteDir (and its parents) if needed. A nil client copies locally.
func scpCopyDir(client *ssh.Client, localDir, remoteDir string) error {
	if client == nil {
		return copyDirLocal(localDir, remoteDir)
	}
	if err := runRemote(client, "mkdir -p "+shellQuote(remoteDir)); err != nil {
		return err
	}
	return runSCP(client, "-r -d", remoteDir, func(s *scpSender) error {
		return s.dirContents(localDir)
	})
}

// copyDirLocal recursively copies src into dst, overwriting existing files.
func copyDirLocal(src, dst string) error {
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
			if err := copyDirLocal(from, to); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// CopyFiles copies files for a task to a server. Templates are executed with
// server as their data, the same as cmd_template.
func CopyFiles(client *ssh.Client, server any, task *Task) error {
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

		var data []byte
		if cc.Content != "" {
			data = []byte(cc.Content)
		} else if cc.SrcTpl != "" {
			tmplData, err := os.ReadFile(cc.SrcTpl)
			if err != nil {
				return err
			}
			tmpl, err := template.New(filepath.Base(cc.SrcTpl)).Parse(string(tmplData))
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, server); err != nil {
				return err
			}
			data = buf.Bytes()
		} else if srcPath != "" {
			info, err := os.Stat(srcPath)
			if err != nil {
				return err
			}
			if info.IsDir() {
				if err := scpCopyDir(client, srcPath, dst); err != nil {
					return err
				}
				continue
			}
			data, err = os.ReadFile(srcPath)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("no source for copy")
		}

		if err := scpCopy(client, data, dst, 0); err != nil {
			return err
		}
	}
	return nil
}
