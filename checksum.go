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
		case c.renders(task):
			data, err := renderCopyTemplate(task, server, c.Src)
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

// dirDigests adds the MD5 of every regular file under dir to files, by path relative to dir,
// following symlinks as the copy does.
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

// md5Hex is the MD5 of data, in hex.
func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// fileMD5 is the MD5 of the file at path, in hex.
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

// localDigests hashes what is at each copy's target on this machine, in Go. A target that is
// missing or can't be read has no entries, so it counts as changed.
func localDigests(digests []copyDigest) []map[string]string {
	out := make([]map[string]string, len(digests))
	for i, d := range digests {
		out[i] = map[string]string{}
		if d.dir {
			_ = dirDigests(d.dst, "", out[i])
		} else if sum, err := fileMD5(d.dst); err == nil {
			out[i][""] = sum
		}
	}
	return out
}

// noMD5Tool is what digestScript prints when the server has no tool to hash with.
const noMD5Tool = "!! no md5 tool"

// digestScript is a POSIX sh script that prints the MD5 of what is at each copy's target on the
// server: a "== <n>" line per copy, then "<md5> <path>" lines. It hashes with md5sum (Linux,
// BusyBox), md5 -r (macOS, BSD) or openssl, whichever is there, and prints noMD5Tool when none
// is. A target that isn't there prints nothing.
func digestScript(digests []copyDigest) string {
	var b strings.Builder
	b.WriteString(`if command -v md5sum >/dev/null 2>&1; then h() { md5sum "$1"; }
elif command -v md5 >/dev/null 2>&1; then h() { md5 -r "$1"; }
elif command -v openssl >/dev/null 2>&1; then h() { openssl md5 -r "$1"; }
else echo '` + noMD5Tool + `'; exit 0; fi
`)
	for i, d := range digests {
		fmt.Fprintf(&b, "echo '== %d'\n", i)
		if d.dir {
			fmt.Fprintf(&b, "(cd %s 2>/dev/null && find . -type f | while IFS= read -r f; do h \"$f\" 2>/dev/null; done)\n", shellQuote(d.dst))
		} else {
			fmt.Fprintf(&b, "h %s 2>/dev/null\n", shellQuote(d.dst))
		}
	}
	b.WriteString("true\n")
	return "sh -c " + shellQuote(b.String())
}

// parseDigests reads digestScript's output: for each copy, the MD5 of each file by its path
// relative to the copy's target ("" for a single file). It reports false when the server
// couldn't hash: no tool, or output that isn't the script's.
func parseDigests(output string, n int) ([]map[string]string, bool) {
	out := make([]map[string]string, n)
	for i := range out {
		out[i] = map[string]string{}
	}
	current, seen := -1, 0
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == noMD5Tool {
			return nil, false
		}
		if rest, ok := strings.CutPrefix(line, "== "); ok {
			current, _ = strconv.Atoi(strings.TrimSpace(rest))
			seen++
			continue
		}
		// md5sum marks a name it had to escape with a leading backslash
		line = strings.TrimPrefix(line, `\`)
		if current < 0 || current >= n || len(line) < 33 || !isHex(line[:32]) {
			continue
		}
		name := strings.TrimLeft(line[32:], " *") // "  name", " *name" (binary mode) or " name"
		out[current][strings.TrimPrefix(name, "./")] = line[:32]
	}
	return out, seen == n
}

// isHex reports whether s is only lowercase hex digits.
func isHex(s string) bool {
	return strings.Trim(s, "0123456789abcdef") == ""
}

// compareCopies checks whether the server already has what the task's copies would write, by
// comparing MD5s: this machine's files are hashed in Go, a server's with its own md5sum, md5 or
// openssl. When the server can't hash, every copy counts as changed, so the task copies anyway.
// It reports each copy as same or changed, as a check result: exit 0 when nothing changed.
func (t *TaskExec) compareCopies(ctx context.Context, run runFunc) (*StepResult, error) {
	started := time.Now()
	local, err := copyDigests(&t.task, t.target)
	if err != nil {
		return nil, fmt.Errorf("compare copies: %w", err)
	}
	res := &StepResult{Command: "compare copies (md5)"}

	remote, reason := localDigests(local), ""
	if t.server != nil {
		stdout, stderr, err := run(ctx, digestScript(local), false)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		res.Stderr = string(stderr)
		var ok bool
		remote, ok = parseDigests(string(stdout), len(local))
		switch {
		case err != nil:
			reason = "couldn't hash on the server: " + err.Error()
		case !ok:
			reason = "no md5sum, md5 or openssl on the server"
		}
	}

	var report strings.Builder
	for i, d := range local {
		changed := []string{}
		for _, path := range slices.Sorted(maps.Keys(d.files)) {
			if reason != "" {
				changed = append(changed, path)
				continue
			}
			got := remote[i][path]
			if !d.dir {
				// a single file is listed under its target path, however the tool prints it
				for _, hash := range remote[i] {
					got = hash
				}
			}
			if got != d.files[path] {
				changed = append(changed, path)
			}
		}
		switch {
		case reason != "":
			fmt.Fprintf(&report, "changed  %s (not compared: %s, so it is copied)\n", d.dst, reason)
		case len(changed) == 0:
			fmt.Fprintf(&report, "same     %s\n", d.dst)
		case !d.dir:
			fmt.Fprintf(&report, "changed  %s\n", d.dst)
		default:
			fmt.Fprintf(&report, "changed  %s (%d of %d files: %s)\n", d.dst, len(changed), len(d.files), strings.Join(firstN(changed, 5), ", "))
		}
		if len(changed) > 0 || reason != "" {
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
