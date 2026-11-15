package gorun

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// Encode writes the config to w as a Gorunfile, the inverse of Load: the settings, vars,
// envvars, ssh, tasks, bundles, server groups and schedules, then one [servers.hosts.<name>]
// table per entry in Servers, in order. Empty values are left out and map keys are sorted, so
// the same config always gives the same bytes. Every value is written exactly as it is: nothing
// is filled in as a template or expanded. Fields a Gorunfile has no key for, such as Path,
// OnEvent or a server's resolved Tasks, are not written.
//
// A config from LoadFile has its servers composed already: their groups' and the global vars,
// envvars, tags and ssh settings are merged in and filled in, and their task lists expanded.
// Encode writes each server as it is, so those values appear on the server too, and loading the
// output gives the same config again. The exception is a raw layer whose filled-in value still
// holds "{{": the server's own vars are templates, so a second load would fill it in.
func (c *Config) Encode(w io.Writer) error {
	data, err := c.Marshal()
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// Marshal returns the config as Gorunfile text; see Encode.
func (c *Config) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(c); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	top := tidyTables(buf.String())
	buf.Reset()
	buf.WriteString(top)

	seen := make(map[string]bool, len(c.Servers))
	for i, s := range c.Servers {
		switch {
		case s.Name == "":
			return nil, fmt.Errorf("encode config: server %d has no name", i)
		case seen[s.Name]:
			return nil, fmt.Errorf("encode config: duplicate server %q", s.Name)
		}
		seen[s.Name] = true

		// Without a tasks key a server runs its groups' tasks, so an empty list is written only
		// when it has groups that would otherwise fill it.
		if len(s.TaskNames) == 0 && (s.TaskNames == nil || len(s.Groups) == 0) {
			s.TaskNames = nil
		}
		lines, err := encodeTable([]string{"servers", "hosts"}, s.Name, s)
		if err != nil {
			return nil, fmt.Errorf("encode config: server %q: %w", s.Name, err)
		}
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(strings.Join(lines, "\n") + "\n")
	}

	if !utf8.Valid(buf.Bytes()) {
		return nil, errors.New("encode config: a value is not valid UTF-8, which TOML can't hold")
	}
	return buf.Bytes(), nil
}

// tidyTables drops the empty parent headers the encoder writes, such as [tasks] directly above
// [tasks.build], and puts a blank line above each task, bundle and server group. Every line of
// the encoder's output is a key or a header, so a line starting with "[" is a header.
func tidyTables(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var out []string
	for i, line := range lines {
		path, isHeader := headerPath(line)
		if isHeader && len(path) == 1 && !strings.HasPrefix(line, "[[") && nextIsHeader(lines[i+1:]) {
			continue
		}
		entity := isHeader && len(path) == 2 && slices.Contains([]string{"tasks", "bundles", "server_groups"}, path[0])
		if line == "" && len(out) == 0 {
			continue
		}
		if entity && len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// nextIsHeader reports whether the first line in lines that isn't blank is a header.
func nextIsHeader(lines []string) bool {
	for _, line := range lines {
		if line != "" {
			return strings.HasPrefix(line, "[")
		}
	}
	return false
}
