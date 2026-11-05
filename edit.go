package gorun

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Change is one edit to a Gorunfile. An add writes Body as a new table in File. A remove
// deletes the entity from File, where it is defined.
type Change struct {
	Kind   string // "task", "bundle", "group" or "host"
	Name   string
	File   string
	Remove bool
	Body   map[string]any
}

// kindTable is the table path each kind is written under.
var kindTable = map[string][]string{
	"task":   {"tasks"},
	"bundle": {"bundles"},
	"group":  {"server_groups"},
	"host":   {"servers", "hosts"},
}

// section is one table in a file: its header line and the lines that belong to it.
type section struct {
	start  int      // first line, including comment lines directly above the header
	header int      // the header line
	end    int      // first line after the section
	path   []string // the dotted header, split into keys
}

var headerLine = regexp.MustCompile(`^\s*\[[^\[\]]+\]\s*$`)

// SaveChanges applies changes to the files they name. Only the tables being changed are
// rewritten, so comments and layout elsewhere in each file are kept.
func SaveChanges(changes []Change) error {
	var files []string
	byFile := map[string][]Change{}
	for _, ch := range changes {
		if _, ok := byFile[ch.File]; !ok {
			files = append(files, ch.File)
		}
		byFile[ch.File] = append(byFile[ch.File], ch)
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("save %s: %w", file, err)
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		for _, ch := range byFile[file] {
			if ch.Remove {
				lines = removeEntity(lines, ch)
				continue
			}
			if lines, err = addEntity(lines, ch); err != nil {
				return fmt.Errorf("save %s: %w", file, err)
			}
		}
		if err := writeFileAtomic(file, []byte(strings.Join(lines, "\n")+"\n")); err != nil {
			return fmt.Errorf("save %s: %w", file, err)
		}
	}
	return nil
}

// writeFileAtomic replaces path with data, so a failed write never leaves a half file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gorun-*.toml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmp.Name(), info.Mode().Perm())
	}
	return os.Rename(tmp.Name(), path)
}

// removeEntity deletes the entity's tables from the lines. A host can also be an inline
// key in a shared [servers.hosts] table, which is removed line by line.
func removeEntity(lines []string, ch Change) []string {
	base := kindTable[ch.Kind]
	want := append(append([]string{}, base...), ch.Name)

	secs := parseSections(lines)
	drop := make([]bool, len(lines))
	for _, s := range secs {
		if hasPrefix(s.path, want) {
			for i := s.start; i < s.end; i++ {
				drop[i] = true
			}
		}
		if ch.Kind == "host" && slicesEqual(s.path, base) {
			key := regexp.MustCompile(`^\s*("?)` + regexp.QuoteMeta(ch.Name) + `("?)\s*=`)
			for i := s.header + 1; i < s.end; i++ {
				if key.MatchString(lines[i]) {
					drop[i] = true
				}
			}
		}
	}
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if !drop[i] {
			out = append(out, line)
		}
	}
	return out
}

// addEntity appends the entity as its own table, after a blank line.
func addEntity(lines []string, ch Change) ([]string, error) {
	base := kindTable[ch.Kind]
	want := append(append([]string{}, base...), ch.Name)
	for _, s := range parseSections(lines) {
		if hasPrefix(s.path, want) {
			return nil, fmt.Errorf("%s %q is already defined in this file", ch.Kind, ch.Name)
		}
	}

	block, err := encodeTable(base, ch.Name, ch.Body)
	if err != nil {
		return nil, err
	}
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	return append(lines, block...), nil
}

// encodeTable renders body as the TOML tables base.name, base.name.<sub-table>..., with the
// parent headers that the encoder adds left out.
func encodeTable(base []string, name string, body map[string]any) ([]string, error) {
	var nest any = map[string]any{name: body}
	for i := len(base) - 1; i >= 0; i-- {
		nest = map[string]any{base[i]: nest}
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(nest); err != nil {
		return nil, fmt.Errorf("encode %s: %w", name, err)
	}

	var out []string
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if s, ok := headerPath(line); ok && len(s) <= len(base) {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

// parseSections finds every table header and the lines that belong to each table. Comment
// lines directly above a header belong to that table.
func parseSections(lines []string) []section {
	var secs []section
	for i, line := range lines {
		path, ok := headerPath(line)
		if !ok {
			continue
		}
		start := i
		prevEnd := 0
		if len(secs) > 0 {
			prevEnd = secs[len(secs)-1].header + 1
		}
		for start > prevEnd && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "#") {
			start--
		}
		if len(secs) > 0 {
			secs[len(secs)-1].end = start
		}
		secs = append(secs, section{start: start, end: len(lines), path: path, header: i})
	}
	return secs
}

// headerPath returns the keys of a [table] header line.
func headerPath(line string) ([]string, bool) {
	if !headerLine.MatchString(line) {
		return nil, false
	}
	inner := strings.TrimSpace(line)
	inner = strings.TrimSuffix(strings.TrimPrefix(inner, "["), "]")

	var keys []string
	var cur strings.Builder
	quoted := false
	for _, r := range inner {
		switch {
		case r == '"':
			quoted = !quoted
		case r == '.' && !quoted:
			keys = append(keys, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(keys, strings.TrimSpace(cur.String())), true
}

// hasPrefix reports whether path starts with prefix.
func hasPrefix(path, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	return slicesEqual(path[:len(prefix)], prefix)
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
