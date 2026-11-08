package gorun

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// rootOnlyKeys can only be set in the root Gorunfile, so an included file can't change
// settings that apply to the whole run.
var rootOnlyKeys = [][]string{
	{"title"}, {"log_level"}, {"max_parallel"}, {"max_task_parallel"}, {"ssh"}, {"servers", "tasks"},
}

// pendingHost is a server read from a file, composed once every file has been merged.
type pendingHost struct {
	server   Server
	ownTasks bool
}

// loader reads the root Gorunfile and the files it includes into one Config. Named entries
// are merged across files and a name defined twice is an error, so file order never changes
// a value.
type loader struct {
	cfg          *Config
	dir          string            // directory of the root Gorunfile, for display paths
	defaultTasks []string          // [servers] tasks, from the root file only
	hosts        []pendingHost     // in read order: the root file first, then includes in order
	owners       map[string]string // "kind name" -> the file that defined it
	loaded       map[string]bool   // files already merged, so each is read once
	stack        []string          // files being read, to catch include cycles
}

func newLoader(c *Config) *loader {
	return &loader{cfg: c, owners: map[string]string{}, loaded: map[string]bool{}}
}

// load reads the root file from r and every file it includes, then validates the result.
// path is the root file's absolute path, or empty when r has no file behind it.
func (l *loader) load(r io.Reader, path string) error {
	if path != "" {
		l.dir = filepath.Dir(path)
		l.loaded[path] = true
		l.stack = append(l.stack, path)
		l.cfg.Path = path
		l.cfg.Files = []string{path}
	}
	if err := l.read(r, path, true); err != nil {
		return fmt.Errorf("error with loading config: %w", err)
	}
	return l.finish()
}

// read decodes one file, merges it into the config and then reads the files it includes.
func (l *loader) read(r io.Reader, path string, root bool) error {
	f := configFile{}
	if root {
		f.Config = *l.cfg
	}

	md, err := toml.NewDecoder(r).Decode(&f)
	if err != nil {
		return err
	}
	tag(&f, path)

	// catch typos, which would otherwise be silently ignored
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := []string{}
		for _, key := range undecoded {
			if isVarsNestedKey(key) {
				continue
			}
			keys = append(keys, key.String())
		}
		if len(keys) > 0 {
			return fmt.Errorf("unknown keys: %s", strings.Join(keys, ", "))
		}
	}

	if root {
		*l.cfg = f.Config
		l.defaultTasks = f.Servers.Tasks
	} else {
		for _, key := range rootOnlyKeys {
			if md.IsDefined(key...) {
				return fmt.Errorf("%s can only be set in the root Gorunfile", strings.Join(key, "."))
			}
		}
	}

	if err := l.claim(f, path); err != nil {
		return err
	}
	if !root {
		l.merge(f)
	}
	l.collectHosts(f, &md)

	if len(f.Include) > 0 && path == "" {
		return fmt.Errorf("include needs a file: use LoadFile, not Load")
	}
	for _, pattern := range f.Include {
		files, err := expandInclude(filepath.Dir(path), pattern)
		if err != nil {
			return err
		}
		for _, file := range files {
			if err := l.readFile(file); err != nil {
				return err
			}
		}
	}
	return nil
}

// readFile reads one included file, unless it is already loaded or would make a cycle.
func (l *loader) readFile(file string) error {
	real, err := filepath.EvalSymlinks(file)
	if err != nil {
		return fmt.Errorf("include %s: %w", l.rel(file), err)
	}
	if slices.Contains(l.stack, real) {
		chain := []string{}
		for _, p := range append(l.stack, real) {
			chain = append(chain, l.rel(p))
		}
		return fmt.Errorf("include cycle: %s", strings.Join(chain, " -> "))
	}
	if l.loaded[real] {
		return nil
	}
	l.loaded[real] = true

	fh, err := os.Open(real)
	if err != nil {
		return fmt.Errorf("include %s: %w", l.rel(file), err)
	}
	defer func() { _ = fh.Close() }()

	l.stack = append(l.stack, real)
	defer func() { l.stack = l.stack[:len(l.stack)-1] }()
	l.cfg.Files = append(l.cfg.Files, real)

	if err := l.read(fh, real, false); err != nil {
		return fmt.Errorf("%s: %w", l.rel(file), err)
	}
	return nil
}

// tag records in each named entity the file that defines it.
func tag(f *configFile, path string) {
	for name, t := range f.Tasks {
		t.Source = path
		f.Tasks[name] = t
	}
	for name, b := range f.Bundles {
		b.Source = path
		f.Bundles[name] = b
	}
	for name, g := range f.Groups {
		g.Source = path
		f.Groups[name] = g
	}
	for name, s := range f.Servers.Hosts {
		s.Source = path
		f.Servers.Hosts[name] = s
	}
}

// claim records which file defines each name, and rejects a name a second file defines.
func (l *loader) claim(f configFile, path string) error {
	source := l.rel(path)
	if path == "" {
		source = "Gorunfile"
	}
	groups := []struct {
		kind  string
		names []string
	}{
		{"task", keysOf(f.Tasks)},
		{"bundle", keysOf(f.Bundles)},
		{"server_group", keysOf(f.Groups)},
		{"host", keysOf(f.Servers.Hosts)},
		{"envvar", keysOf(f.EnvVars)},
		{"var", keysOf(f.Vars)},
	}
	for _, g := range groups {
		for _, name := range g.names {
			id := g.kind + " " + name
			if owner, ok := l.owners[id]; ok {
				return fmt.Errorf("duplicate %s %q: defined in %s and %s", g.kind, name, owner, source)
			}
			l.owners[id] = source
		}
	}
	return nil
}

// merge adds an included file's named entries to the config. claim has already checked
// that none of them is defined elsewhere.
func (l *loader) merge(f configFile) {
	c := l.cfg
	putAll(&c.Tasks, f.Tasks)
	putAll(&c.Bundles, f.Bundles)
	putAll(&c.Groups, f.Groups)
	putAll(&c.EnvVars, f.EnvVars)
	putAll(&c.Vars, f.Vars)
}

// collectHosts keeps the file's servers in the order the file lists them.
func (l *loader) collectHosts(f configFile, md *toml.MetaData) {
	for _, key := range md.Keys() {
		if len(key) != 3 || key[0] != "servers" || key[1] != "hosts" {
			continue
		}
		server := f.Servers.Hosts[key[2]]
		server.Name = key[2]
		l.hosts = append(l.hosts, pendingHost{
			server:   server,
			ownTasks: md.IsDefined("servers", "hosts", key[2], "tasks"),
		})
	}
}

// finish names and validates the merged library, then composes every server.
func (l *loader) finish() error {
	c := l.cfg

	if c.MaxParallel < 0 {
		return fmt.Errorf("error with loading config: max_parallel must not be negative")
	}

	// tasks, bundles, groups and servers are named by their table keys
	for name, task := range c.Tasks {
		task.Name = name
		c.Tasks[name] = task
	}
	for name, bundle := range c.Bundles {
		bundle.Name = name
		c.Bundles[name] = bundle
	}
	for name, group := range c.Groups {
		group.Name = name
		c.Groups[name] = group
	}

	if err := c.validateLibrary(); err != nil {
		return fmt.Errorf("error with loading config: %w", err)
	}

	c.Servers = nil
	for _, h := range l.hosts {
		server := h.server
		if err := c.composeServer(&server, l.defaultTasks, h.ownTasks); err != nil {
			return fmt.Errorf("error with loading config: server %q: %w", server.Name, err)
		}
		c.Servers = append(c.Servers, server)
	}
	return nil
}

// rel shortens path to be relative to the root Gorunfile's directory, for error messages.
func (l *loader) rel(path string) string {
	if l.dir == "" || path == "" {
		return path
	}
	if r, err := filepath.Rel(l.dir, path); err == nil {
		return r
	}
	return path
}

// expandInclude returns the files an include pattern names, sorted. A relative pattern is
// relative to dir, the directory of the file that includes it. A pattern that matches nothing
// is an error, so a typo can't silently drop servers.
func expandInclude(dir, pattern string) ([]string, error) {
	p := pattern
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	matches, err := filepath.Glob(p)
	if err != nil {
		return nil, fmt.Errorf("include %q: %w", pattern, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("include %q matches no files", pattern)
	}
	sort.Strings(matches)
	return matches, nil
}

// keysOf returns the keys of m, in no particular order.
func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// putAll copies src into *dst, creating the map if needed.
func putAll[V any](dst *map[string]V, src map[string]V) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]V, len(src))
	}
	for k, v := range src {
		(*dst)[k] = v
	}
}
