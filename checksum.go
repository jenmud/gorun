package gorun

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/pkg/sftp"
)

// fileDigest is one file a copy would write: its size and MD5.
type fileDigest struct {
	size int64
	md5  string
}

// copyDigest is what one copy would put on the server: its target and each file, by path
// relative to the target ("" for a single file).
type copyDigest struct {
	dst   string
	dir   bool
	files map[string]fileDigest
}

// copyDigests works out what the task's copies would write: inline content, a template rendered
// for server, a file, or every file of a directory.
func copyDigests(task *Task, server any) ([]copyDigest, error) {
	var out []copyDigest
	for _, c := range task.copies() {
		d := copyDigest{dst: c.target(), files: map[string]fileDigest{}}
		switch {
		case c.Content != "":
			d.files[""] = digestOf([]byte(c.Content))
		case c.SrcTpl != "":
			data, err := renderCopyTemplate(task, server, c.SrcTpl)
			if err != nil {
				return nil, err
			}
			d.files[""] = digestOf(data)
		case c.Src == "":
			return nil, fmt.Errorf("no source for copy")
		default:
			info, err := os.Stat(c.Src)
			if err != nil {
				return nil, err
			}
			if d.dir = info.IsDir(); d.dir {
				err = dirDigests(c.Src, "", d.files)
			} else {
				d.files[""], err = fileDigestOf(osFiles{}, c.Src, info.Size())
			}
			if err != nil {
				return nil, err
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// dirDigests adds every regular file under dir to files, by path relative to the copy's source,
// following symlinks as the copy does.
func dirDigests(dir, rel string, files map[string]fileDigest) error {
	entries, err := os.ReadDir(filepath.Join(dir, rel))
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := path.Join(filepath.ToSlash(rel), e.Name())
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if err := dirDigests(dir, name, files); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if files[name], err = fileDigestOf(osFiles{}, filepath.Join(dir, name), info.Size()); err != nil {
				return err
			}
		}
	}
	return nil
}

// digestOf is the size and MD5 of data.
func digestOf(data []byte) fileDigest {
	sum := md5.Sum(data)
	return fileDigest{size: int64(len(data)), md5: hex.EncodeToString(sum[:])}
}

// fileDigestOf reads the file at name from files and returns its size and MD5.
func fileDigestOf(files fileSystem, name string, size int64) (fileDigest, error) {
	f, err := files.Open(name)
	if err != nil {
		return fileDigest{}, err
	}
	defer func() { _ = f.Close() }()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return fileDigest{}, err
	}
	return fileDigest{size: size, md5: hex.EncodeToString(h.Sum(nil))}, nil
}

// renderCopyTemplate renders a copy's template for server, as the copy itself does.
func renderCopyTemplate(task *Task, server any, path string) ([]byte, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(filepath.Base(path)).Parse(string(source))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, task.templateData(server)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fileSystem is where the copied files are read back from: this machine, or a server over SFTP.
type fileSystem interface {
	Stat(name string) (fs.FileInfo, error)
	Open(name string) (io.ReadCloser, error)
}

// osFiles reads files on this machine.
type osFiles struct{}

func (osFiles) Stat(name string) (fs.FileInfo, error)   { return os.Stat(name) }
func (osFiles) Open(name string) (io.ReadCloser, error) { return os.Open(name) }

// sftpFiles reads files on a server over SFTP.
type sftpFiles struct{ c *sftp.Client }

func (s sftpFiles) Stat(name string) (fs.FileInfo, error)   { return s.c.Stat(name) }
func (s sftpFiles) Open(name string) (io.ReadCloser, error) { return s.c.Open(name) }

// serverFiles opens the files of the task's server: an SFTP session over its connection (using
// one of its session slots), or this machine's files for a local server. Cancelling ctx closes
// the session. The returned func releases it.
func (t *TaskExec) serverFiles(ctx context.Context) (fileSystem, func(), error) {
	if t.server == nil {
		return osFiles{}, func() {}, nil
	}
	session, closeSession, err := newSession(ctx, t.server)
	if err != nil {
		return nil, nil, err
	}
	stdin, err1 := session.StdinPipe()
	stdout, err2 := session.StdoutPipe()
	if err := errors.Join(err1, err2, session.RequestSubsystem("sftp")); err != nil {
		closeSession()
		return nil, nil, fmt.Errorf("the server doesn't offer SFTP: %w", err)
	}
	client, err := sftp.NewClientPipe(stdout, stdin)
	if err != nil {
		closeSession()
		return nil, nil, fmt.Errorf("start SFTP: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	return sftpFiles{client}, func() {
		stop()
		_ = client.Close()
		closeSession()
	}, nil
}

// compareCopies checks whether the server already has what the task's copies would write. Both
// sides are hashed with MD5 in Go, so it works whatever the server runs: the server's files are
// read back over SFTP, but only when their size matches, as a different size is already a change.
// It reports each copy as same or changed, as a check result: exit 0 when nothing changed.
func (t *TaskExec) compareCopies(ctx context.Context) (*StepResult, error) {
	started := time.Now()
	local, err := copyDigests(&t.task, t.target)
	if err != nil {
		return nil, fmt.Errorf("compare copies: %w", err)
	}
	files, release, err := t.serverFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("compare copies: %w", err)
	}
	defer release()

	res := &StepResult{Command: "compare copies (md5)"}
	var report strings.Builder
	for _, d := range local {
		changed := []string{}
		for _, rel := range slices.Sorted(maps.Keys(d.files)) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			want := d.files[rel]
			name := d.dst
			if rel != "" {
				name = path.Join(filepath.ToSlash(d.dst), rel)
			}
			if !sameFile(files, name, want) {
				changed = append(changed, rel)
			}
		}
		switch {
		case len(changed) == 0:
			fmt.Fprintf(&report, "same     %s\n", d.dst)
		case !d.dir:
			fmt.Fprintf(&report, "changed  %s\n", d.dst)
		default:
			fmt.Fprintf(&report, "changed  %s (%d of %d files: %s)\n", d.dst, len(changed), len(d.files), strings.Join(firstN(changed, 5), ", "))
		}
		if len(changed) > 0 {
			res.ExitCode = 1
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	res.Stdout = report.String()
	res.Duration = time.Since(started)
	return res, nil
}

// sameFile reports whether the file at name has want's size and MD5. A file that is missing or
// can't be read counts as changed.
func sameFile(files fileSystem, name string, want fileDigest) bool {
	info, err := files.Stat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() != want.size {
		return false
	}
	got, err := fileDigestOf(files, name, info.Size())
	return err == nil && got.md5 == want.md5
}

// firstN returns up to n items of list, and "…" when there are more.
func firstN(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return append(slices.Clone(list[:n]), "…")
}
