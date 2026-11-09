package gorun

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jenmud/gorun/internal/envvars"
)

func init() {
	// Default the OTEL service name to GORUN_NAME when OTEL_SERVICE_NAME is
	// not explicitly provided, so traces/metrics are attributed correctly.
	if _, ok := os.LookupEnv("OTEL_SERVICE_NAME"); !ok {
		_ = os.Setenv("OTEL_SERVICE_NAME", cmp.Or(envvars.Get("GORUN_NAME"), "gorun"))
	}
}

// Config is a loaded Gorunfile: the task library, the servers and how to run them.
type Config struct {
	Title           string            `toml:"title"`
	GorunUUID       string            `toml:"-"`
	LogLevel        string            `toml:"log_level"`
	MaxParallel     int               `toml:"max_parallel"`      // 0 means run every selected host at once
	MaxTaskParallel int               `toml:"max_task_parallel"` // 0 (default) = one at a time; -1 = all at once
	EnvVars         map[string]string `toml:"envvars"`
	Vars            map[string]any    `toml:"vars,omitempty"`
	SSH             SSH               `toml:"ssh"`

	// Tasks is the task library, keyed by task name. Servers pick tasks from it by name.
	Tasks map[string]Task `toml:"tasks"`

	// Bundles are named task lists, referred to as "@<name>" wherever a task list is accepted.
	Bundles map[string]Bundle `toml:"bundles"`

	// Groups hold defaults shared by the servers that join them.
	Groups map[string]ServerGroup `toml:"server_groups"`

	// Schedules are the [[schedule]] tables, in file order; see docs/schedules.md.
	Schedules []Schedule `toml:"schedule"`

	// Servers are the [servers.hosts.<name>] tables, in the order they appear in the file.
	Servers []Server `toml:"-"`

	// Path is the root Gorunfile, and Files every file read to build the config (the root
	// first, then includes), all absolute. Set by LoadFile.
	Path  string   `toml:"-"`
	Files []string `toml:"-"`

	// OnEvent, when set, receives the events in events.go as tasks run.
	OnEvent func(event any) `toml:"-"`

	// Recorder, when set, also receives every event, to keep it as history or anywhere else.
	Recorder Recorder `toml:"-"`

	// UpdateIncludes makes LoadFile look up every git include's ref again, instead of using the
	// commit in the lock file, and save the new commits (the -update-includes flag).
	UpdateIncludes bool `toml:"-"`

	// Includes are the git includes LoadFile read, with the commit each one used.
	Includes []IncludeSource `toml:"-"`

	// Schedule is the name of the schedule that started this run, set by the Scheduler. It is
	// empty for a run started any other way, and is reported in ProcessStartedEvent.
	Schedule string `toml:"-"`

	// SelectedTasks, when non-empty, limits execution to these tasks (and their
	// dependencies). Bundle references ("@name") are expanded. Used by the --task CLI flag.
	SelectedTasks []string `toml:"-"`
}

// configFile is the layout of a Gorunfile on disk. Load flattens the [servers] section
// into Config.Servers.
type configFile struct {
	Config

	// Include lists files to merge into this one, as paths or globs relative to this file.
	Include []string `toml:"include"`

	Servers struct {
		// Tasks run on every host that doesn't set its own task list.
		Tasks []string          `toml:"tasks"`
		Hosts map[string]Server `toml:"hosts"`
	} `toml:"servers"`
}

// isVarsNestedKey reports whether key sits inside any free-form vars table
// ([...vars] or a nested key beneath it). BurntSushi/toml fills the Vars map but
// still lists the nested keys as undecoded, so they must not be reported as typos.
func isVarsNestedKey(key toml.Key) bool {
	for i, seg := range key {
		if seg == "vars" && i < len(key)-1 {
			return true
		}
	}
	return false
}

// Load reads a Gorunfile from r. A Gorunfile with an include list must use LoadFile, because
// include paths are relative to the file that names them.
func (c *Config) Load(r io.Reader) error {
	return newLoader(c).load(r, "")
}

// LoadFile reads the Gorunfile at path, and every file it includes, into the config.
func (c *Config) LoadFile(path string) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("error with loading config: %w", err)
	}
	abs, err := filepath.Abs(real)
	if err != nil {
		return fmt.Errorf("error with loading config: %w", err)
	}
	f, err := os.Open(abs)
	if err != nil {
		return fmt.Errorf("error with loading config: %w", err)
	}
	defer func() { _ = f.Close() }()
	return newLoader(c).load(f, abs)
}

// ResolvePaths makes the relative file paths in the config (SSH identity and config files, task
// cmd_files and copy sources) relative to dir, normally the directory holding the config file,
// so gorun can be run from anywhere. A leading ~/ is the home directory.
func (c *Config) ResolvePaths(dir string) {
	resolve := func(path string) string {
		if rest, ok := strings.CutPrefix(path, "~/"); ok {
			if home, err := os.UserHomeDir(); err == nil {
				return filepath.Join(home, rest)
			}
		}
		if path == "" || filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(dir, path)
	}
	resolveSSH := func(s *SSH) {
		s.Identityfile = resolve(s.Identityfile)
		s.ConfigFile = resolve(cmp.Or(s.ConfigFile, s.Config)) // config is another name for config_file
	}

	resolveSSH(&c.SSH)
	for name, task := range c.Tasks {
		task.CmdFile = resolve(task.CmdFile)
		task.CmdTemplateFile = resolve(task.CmdTemplateFile)
		for _, list := range [][]Copy{task.Copy, task.Copies, task.Upload, task.Files} {
			for i, cp := range list {
				cp = cp.normalized()
				cp.Src, cp.SrcTpl = resolve(cp.Src), resolve(cp.SrcTpl)
				list[i] = cp
			}
		}
		c.Tasks[name] = task
	}
	for i := range c.Servers {
		s := &c.Servers[i].SSH
		resolveSSH(s)
		// a server without its own SSH config file uses the global one
		s.ConfigFile = cmp.Or(s.ConfigFile, c.SSH.ConfigFile)
	}
}

// resolveTasks looks up the named tasks in the library and returns them along with every
// task they depend on, ordered so each task runs after its dependencies. Otherwise tasks
// keep the order they are listed in.
func resolveTasks(library map[string]Task, names ...string) ([]Task, error) {
	const (
		visiting = iota + 1
		visited
	)

	state := map[string]int{}
	result := []Task{}

	// depth first: add a task's dependencies, then the task itself
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		path = append(path, name)

		switch state[name] {
		case visited:
			return nil
		case visiting:
			return fmt.Errorf("cyclic dependency detected: %s", strings.Join(path, " -> "))
		}

		task, ok := library[name]
		if !ok {
			err := fmt.Errorf("unknown task %q", name)
			if len(path) > 1 {
				err = fmt.Errorf("task %q depends on unknown task %q", path[len(path)-2], name)
			}
			if suggestion := closestName(name, slices.Collect(maps.Keys(library))); suggestion != "" {
				err = fmt.Errorf("%w (did you mean %q?)", err, suggestion)
			}
			return err
		}

		state[name] = visiting
		for _, dep := range task.DependsOn {
			if err := visit(dep, path); err != nil {
				return err
			}
		}
		state[name] = visited

		result = append(result, task)
		return nil
	}

	for _, name := range names {
		if err := visit(name, nil); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// closestName returns the candidate most likely to be a typo of name, or "" if none is close.
func closestName(name string, candidates []string) string {
	best, bestDistance := "", max(2, len(name)/3)+1

	slices.Sort(candidates) // stable result on ties
	for _, candidate := range candidates {
		if d := editDistance(name, candidate); d < bestDistance {
			best, bestDistance = candidate, d
		}
	}

	return best
}

// editDistance is the Damerau-Levenshtein (optimal string alignment) distance between a
// and b: the number of single character insertions, deletions, substitutions and adjacent
// swaps needed to turn one into the other.
func editDistance(a, b string) int {
	s, t := []rune(a), []rune(b)

	d := make([][]int, len(s)+1)
	for i := range d {
		d[i] = make([]int, len(t)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}

	for i := 1; i <= len(s); i++ {
		for j := 1; j <= len(t); j++ {
			cost := 1
			if s[i-1] == t[j-1] {
				cost = 0
			}

			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)

			if i > 1 && j > 1 && s[i-1] == t[j-2] && s[i-2] == t[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}

	return d[len(s)][len(t)]
}

// ServersPipeline returns every server, by name, with its tasks resolved into run order.
func (c *Config) ServersPipeline() (map[string]Server, error) {
	servers := make(map[string]Server, len(c.Servers))
	for _, server := range c.Servers {
		server, err := c.ServerPipeline(server)
		if err != nil {
			return nil, err
		}
		servers[server.Name] = server
	}
	return servers, nil
}

// ErrNoTasks is returned by Run when no selected server has a task to run. Local
// servers only run tasks marked local = true, and other tasks only run over SSH.
var ErrNoTasks = errors.New("no tasks to run: local servers only run tasks marked local = true, and other tasks only run over SSH")

// CanRun reports whether at least one of the named tasks has a server it can run on: a local
// server for a task marked local = true, an SSH server for any other. When none has, the error
// wraps ErrNoTasks and says how to fix the Gorunfile.
func (c *Config) CanRun(names []string) error {
	hasLocal, hasRemote := false, false
	for _, s := range c.Servers {
		if s.IsLocal() {
			hasLocal = true
		} else {
			hasRemote = true
		}
	}
	var local, remote []string
	for _, name := range names {
		if c.Tasks[name].Local {
			if hasLocal {
				return nil
			}
			local = append(local, name)
		} else {
			if hasRemote {
				return nil
			}
			remote = append(remote, name)
		}
	}
	switch {
	case len(local) > 0:
		return noTasksErr(taskList(local) + " marked local = true, but no server in the Gorunfile is local. " +
			"Add a server with no ssh table to run it on this machine, such as [servers.hosts.localhost]")
	case len(remote) > 0:
		return noTasksErr(taskList(remote) + " not marked local = true, so it runs over SSH, but every server in the Gorunfile is local. " +
			"Add a server with an ssh table, or mark the task local = true to run it on this machine")
	}
	return ErrNoTasks
}

// noTasksErr is ErrNoTasks with the specific reason in place of the general one.
type noTasksErr string

func (e noTasksErr) Error() string        { return "no tasks to run: " + string(e) }
func (e noTasksErr) Is(target error) bool { return target == ErrNoTasks }

// taskList names tasks in a sentence: `task "a" is` or `tasks "a" and "b" are`.
func taskList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strconv.Quote(n)
	}
	if len(quoted) == 1 {
		return "task " + quoted[0] + " is"
	}
	return "tasks " + strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1] + " are"
}

// noTasksError explains why a run has nothing to do, naming the tasks that were asked for.
func (c *Config) noTasksError() error {
	wanted := c.SelectedTasks
	if len(wanted) == 0 {
		for _, s := range c.Servers {
			wanted = append(wanted, s.TaskNames...)
		}
	}
	if err := c.CanRun(wanted); err != nil {
		return err
	}
	return ErrNoTasks
}

// ServerPipeline returns the server with its tasks, and their dependencies, resolved into run order.
func (c *Config) ServerPipeline(server Server) (Server, error) {
	names, err := c.ExpandTaskRefs(server.TaskNames...)
	if err == nil {
		server.Tasks, err = resolveTasks(c.Tasks, names...)
	}
	if err != nil {
		return Server{}, fmt.Errorf("error building task pipeline for server %q: %w", server.Name, err)
	}

	// A task runs on one kind of server: local-only tasks never run over SSH, and
	// other tasks never run on the local machine. Anything that depends on a
	// dropped task is dropped too.
	local := server.IsLocal()
	server.Tasks = dropTasks(server.Tasks, func(t Task) bool { return t.Local != local })
	return server, nil
}

// dropTasks removes the tasks drop reports, and tasks that transitively depend on them. tasks
// must be in run order, dependencies first.
func dropTasks(tasks []Task, drop func(Task) bool) []Task {
	removed := map[string]bool{}
	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if drop(t) || slices.ContainsFunc(t.DependsOn, func(dep string) bool { return removed[dep] }) {
			removed[t.Name] = true
			continue
		}
		out = append(out, t)
	}
	return out
}

// AllServers returns a copy of every server in the config, in config order. Use it to
// browse or filter the full inventory (e.g. for a picker or a "run on local" toggle).
func (c *Config) AllServers() []Server {
	return slices.Clone(c.Servers)
}

// LocalServer returns a server that runs on this machine. If the config already
// defines a server with no SSH configured, that server is returned; otherwise a
// new "local" server is returned.
func (c *Config) LocalServer() Server {
	for _, name := range []string{"local", "localhost"} {
		for _, s := range c.Servers {
			if s.Name == name && s.IsLocal() {
				return s
			}
		}
	}
	for _, s := range c.Servers {
		if s.IsLocal() {
			return s
		}
	}
	return Server{Name: "local"}
}

// RunLocal runs the named tasks (or every task when none are given) on the local
// machine. Only tasks marked local = true run there, and ErrNoTasks is returned
// when none of them are. Task names may be globs or bundle references ("@name"),
// just like --task.
func (c *Config) RunLocal(ctx context.Context, names ...string) error {
	taskNames := c.TaskNames()
	if len(names) > 0 {
		var err error
		if taskNames, err = c.ExpandTaskPatterns(names...); err != nil {
			return err
		}
	}
	if len(taskNames) == 0 {
		return errors.New("no tasks to run")
	}

	server := c.LocalServer()
	server.TaskNames = taskNames
	return c.Run(ctx, server)
}

// RunTask runs a single task (and its dependencies) on one or more servers. When
// no servers are given, every server in the config is used. The task does not
// need to be part of any server's configured task list.
func (c *Config) RunTask(ctx context.Context, t Task, server ...Server) error {
	if t.Name == "" {
		return errors.New("task must have a name")
	}

	if c.Tasks == nil {
		c.Tasks = map[string]Task{}
	}
	c.Tasks[t.Name] = t

	targets := slices.Clone(server)
	if len(targets) == 0 {
		targets = slices.Clone(c.Servers)
	}
	if len(targets) == 0 {
		return errors.New("no servers to run on")
	}

	// local-only tasks run only on local servers, and other tasks only over SSH
	targets = slices.DeleteFunc(targets, func(s Server) bool { return s.IsLocal() != t.Local })
	if len(targets) == 0 {
		if t.Local {
			return fmt.Errorf("task %q only runs locally", t.Name)
		}
		return fmt.Errorf("task %q only runs over SSH; set local = true to run it on this machine", t.Name)
	}

	for i := range targets {
		targets[i].TaskNames = []string{t.Name}
	}

	// a one-off run ignores any previously selected task filter
	c.SelectedTasks = nil

	return c.Run(ctx, targets...)
}

// TaskNames returns the task names in dependency order (dependencies first).
func (c *Config) TaskNames() []string {
	names := slices.Sorted(maps.Keys(c.Tasks))
	ordered, err := resolveTasks(c.Tasks, names...)
	if err != nil {
		return names // alphabetical, when the dependency graph can't be resolved
	}
	for i, t := range ordered {
		names[i] = t.Name
	}
	return names
}

// selectedTaskSet returns the set of task names that should run when a task filter
// is active, including transitive dependencies. Returns nil when no filter.
func (c *Config) selectedTaskSet() map[string]bool {
	if len(c.SelectedTasks) == 0 {
		return nil
	}
	allowed := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if allowed[name] {
			return
		}
		allowed[name] = true
		if t, ok := c.Tasks[name]; ok {
			for _, dep := range t.DependsOn {
				visit(dep)
			}
		}
	}
	names, err := c.ExpandTaskRefs(c.SelectedTasks...)
	if err != nil {
		// unknown names can't match a task; keep the plain ones
		names = c.SelectedTasks
	}
	for _, name := range names {
		visit(name)
	}
	return allowed
}
