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
	"strings"

	"golang.org/x/crypto/ssh"
)

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

// command sends one protocol line and waits for the sink to acknowledge it.
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

// localFile sends the file at path, under name.
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

// runSCP creates the target directory (and its parents) on the remote, starts
// `scp <flags> -t target` there in the same session, and lets send stream into it. Cancelling
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

	scp := "scp -t "
	if flags != "" {
		scp = "scp " + flags + " -t "
	}
	cmd := "mkdir -p " + shellQuote(target) + " && " + scp + shellQuote(target)
	if err := session.Start(cmd); err != nil {
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

// writeFile writes r to path on this machine, creating its directory.
func writeFile(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// copyFile copies r (size bytes) to path on the server, creating its directory if needed. A nil
// client writes on this machine. A zero mode means 0644.
func copyFile(ctx context.Context, client *ssh.Client, r io.Reader, size int64, path string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0644
	}
	if client == nil {
		return writeFile(path, r, mode)
	}
	return runSCP(ctx, client, "", filepath.Dir(path), nil, func(s *scpSender) error {
		return s.file(filepath.Base(path), mode, size, r)
	})
}

// copyDir recursively copies the contents of localDir into dir on the server, creating dir (and
// its parents) if needed, overwriting files and following symlinks. A nil client copies on this
// machine. wrap, when not nil, wraps each file's contents as they are copied.
func copyDir(ctx context.Context, client *ssh.Client, localDir, dir string, wrap func(io.Reader) io.Reader) error {
	if client != nil {
		return runSCP(ctx, client, "-r -d", dir, wrap, func(s *scpSender) error {
			return s.dirContents(localDir)
		})
	}
	info, err := os.Stat(localDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from, to := filepath.Join(localDir, e.Name()), filepath.Join(dir, e.Name())
		info, err := os.Stat(from)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyDir(ctx, nil, from, to, wrap); err != nil {
				return err
			}
			continue
		}
		in, err := os.Open(from)
		if err != nil {
			return err
		}
		var r io.Reader = in
		if wrap != nil {
			r = wrap(in)
		}
		err = writeFile(to, r, info.Mode().Perm())
		_ = in.Close()
		if err != nil {
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
	for _, c := range task.copies() {
		if err := copyOne(ctx, client, server, task, c, c.target(), progress); err != nil {
			return err
		}
	}
	return nil
}

// copyOne makes one copy: inline content, a rendered template, a directory or a file.
func copyOne(ctx context.Context, client *ssh.Client, server any, task *Task, c Copy, dst string, progress CopyProgressFunc) error {
	text := func(s []byte) error {
		return copyFile(ctx, client, bytes.NewReader(s), int64(len(s)), dst, 0)
	}
	switch {
	case c.Content != "":
		return text([]byte(c.Content))
	case c.SrcTpl != "":
		data, err := renderCopyTemplate(task, server, c.SrcTpl)
		if err != nil {
			return err
		}
		return text(data)
	case c.Src == "":
		return fmt.Errorf("no source for copy")
	}

	info, err := os.Stat(c.Src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		total, err := dirSize(c.Src)
		if err != nil {
			return err
		}
		tr := newCopyTracker(ctx, progress, dst, total)
		if err := copyDir(ctx, client, c.Src, dst, tr.wrap); err != nil {
			return err
		}
		tr.done()
		return nil
	}
	f, err := os.Open(c.Src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	tr := newCopyTracker(ctx, progress, dst, info.Size())
	if err := copyFile(ctx, client, tr.wrap(f), info.Size(), dst, info.Mode().Perm()); err != nil {
		return err
	}
	tr.done()
	return nil
}
