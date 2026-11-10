package gorun

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// copyDigest is what one copy would put on the server: its target and the MD5 of each file,
// by path relative to the target ("" for a single file).
type copyDigest struct {
	dst   string
	dir   bool
	files map[string]string
}

// copyDigests works out the MD5 of everything the task's copies would write: inline content, a
// template rendered for server, a file, or every file of a directory.
func copyDigests(task *Task, server any) ([]copyDigest, error) {
	var out []copyDigest
	for _, c := range task.copies() {
		d := copyDigest{dst: c.target(), files: map[string]string{}}
		switch {
		case c.Content != "":
			d.files[""] = md5Hex([]byte(c.Content))
		case c.SrcTpl != "":
			data, err := renderCopyTemplate(task, server, c.SrcTpl)
			if err != nil {
				return nil, err
			}
			d.files[""] = md5Hex(data)
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
				d.files[""], err = fileMD5(c.Src)
			}
			if err != nil {
				return nil, err
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// dirDigests adds the MD5 of every regular file under dir to files, by path relative to the
// copy's source, following symlinks as the copy does.
func dirDigests(dir, rel string, files map[string]string) error {
	entries, err := os.ReadDir(filepath.Join(dir, rel))
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := filepath.ToSlash(filepath.Join(rel, e.Name()))
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
			if files[name], err = fileMD5(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// fileMD5 is the MD5 of the file at path, or "" when it can't be read.
func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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

// digestScript is a POSIX sh script that prints the MD5 of what is at each copy's target on the
// server: a "== <n>" line per copy, then "<md5> <path>" lines. It uses md5sum, or md5 -r on
// macOS, and prints nothing for a target that isn't there.
func digestScript(digests []copyDigest) string {
	var b strings.Builder
	// a missing target, or a server with neither tool, prints nothing: that copy counts as changed
	b.WriteString(`h() { md5sum "$1" 2>/dev/null || md5 -r "$1" 2>/dev/null || true; }` + "\n")
	for i, d := range digests {
		fmt.Fprintf(&b, "echo '== %d'\n", i)
		if d.dir {
			fmt.Fprintf(&b, "(cd %s 2>/dev/null && find . -type f | while IFS= read -r f; do h \"$f\"; done)\n", shellQuote(d.dst))
		} else {
			fmt.Fprintf(&b, "h %s\n", shellQuote(d.dst))
		}
	}
	b.WriteString("true\n")
	return "sh -c " + shellQuote(b.String())
}

// parseDigests reads digestScript's output: for each copy, the MD5 of each file by its path
// relative to the copy's target ("" for a single file).
func parseDigests(output string, n int) []map[string]string {
	out := make([]map[string]string, n)
	for i := range out {
		out[i] = map[string]string{}
	}
	current := -1
	for _, line := range strings.Split(output, "\n") {
		if rest, ok := strings.CutPrefix(line, "== "); ok {
			current, _ = strconv.Atoi(strings.TrimSpace(rest))
			continue
		}
		hash, path, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || current < 0 || current >= n {
			continue
		}
		path = strings.TrimPrefix(strings.TrimSpace(path), "./")
		if !strings.HasPrefix(line, hash) || len(hash) != 32 {
			continue
		}
		out[current][path] = hash
	}
	return out
}

// compareCopies checks whether the server already has what the task's copies would write, by
// comparing MD5s. It reports which copies differ, as a check result: exit 0 when none do.
func (t *TaskExec) compareCopies(ctx context.Context, run runFunc) (*StepResult, error) {
	started := time.Now()
	local, err := copyDigests(&t.task, t.target)
	if err != nil {
		return nil, fmt.Errorf("compare copies: %w", err)
	}
	script := digestScript(local)
	stdout, stderr, err := run(ctx, script, false)
	if err != nil {
		return nil, fmt.Errorf("compare copies: %w", err)
	}
	remote := parseDigests(string(stdout), len(local))

	res := &StepResult{Command: "compare copies (md5)", Stderr: string(stderr)}
	var report strings.Builder
	for i, d := range local {
		changed := []string{}
		for _, path := range slices.Sorted(maps.Keys(d.files)) {
			got := remote[i][path]
			if !d.dir {
				// a single file is listed under its target path, however md5sum prints it
				for _, hash := range remote[i] {
					got = hash
				}
			}
			if got != d.files[path] {
				changed = append(changed, path)
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
	res.Stdout = report.String()
	res.Duration = time.Since(started)
	return res, nil
}

// firstN returns up to n items of list, and "…" when there are more.
func firstN(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return append(slices.Clone(list[:n]), "…")
}
