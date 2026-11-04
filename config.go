package gorun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/crypto/ssh"

	"github.com/jenmud/gorun/internal/envvars"
)

func init() {
	// Default the OTEL service name to GORUN_NAME when OTEL_SERVICE_NAME is
	// not explicitly provided, so traces/metrics are attributed correctly.
	if _, ok := os.LookupEnv("OTEL_SERVICE_NAME"); !ok {
		if name := envvars.Get("GORUN_NAME"); name != "" {
			_ = os.Setenv("OTEL_SERVICE_NAME", name)
		} else {
			_ = os.Setenv("OTEL_SERVICE_NAME", "gorun")
		}
	}
}

var tracer = otel.Tracer("gorun")
var meter = otel.Meter("gorun")

// task metrics
var (
	taskExecutions, _ = meter.Int64Counter(
		"gorun.task.executions",
		metric.WithDescription("Number of task executions"),
		metric.WithUnit("{execution}"),
	)
	taskDuration, _ = meter.Float64Histogram(
		"gorun.task.duration",
		metric.WithDescription("Task execution duration"),
		metric.WithUnit("s"),
	)
	taskSkipped, _ = meter.Int64Counter(
		"gorun.task.skipped",
		metric.WithDescription("Number of tasks skipped due to a failed dependency"),
		metric.WithUnit("{task}"),
	)
)

// Config represents a config file.
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

	// Servers are the [servers.hosts.<name>] tables, in the order they appear in the file.
	Servers []Server `toml:"-"`

	// OnEvent, when set, receives the events in events.go as tasks run.
	OnEvent func(event any) `toml:"-"`

	// SelectedTasks, when non-empty, limits execution to these tasks (and their
	// dependencies). Bundle references ("@name") are expanded. Used by the --task CLI flag.
	SelectedTasks []string `toml:"-"`
}

// configFile is the layout of a Gorunfile on disk. Load flattens the [servers] section
// into Config.Servers.
type configFile struct {
	Config

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

// Load reads and updates the config.
func (c *Config) Load(r io.Reader) error {
	f := configFile{Config: *c}

	md, err := toml.NewDecoder(r).Decode(&f)
	if err != nil {
		return fmt.Errorf("error with loading config: %w", err)
	}

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
			return fmt.Errorf("error with loading config: unknown keys: %s", strings.Join(keys, ", "))
		}
	}

	*c = f.Config

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

	// map iteration order is random, so walk the keys to keep the servers in file order
	c.Servers = nil
	for _, key := range md.Keys() {
		if len(key) != 3 || key[0] != "servers" || key[1] != "hosts" {
			continue
		}

		server := f.Servers.Hosts[key[2]]
		server.Name = key[2]

		ownTasks := md.IsDefined("servers", "hosts", key[2], "tasks")
		if err := c.composeServer(&server, f.Servers.Tasks, ownTasks); err != nil {
			return fmt.Errorf("error with loading config: server %q: %w", server.Name, err)
		}

		c.Servers = append(c.Servers, server)
	}

	return nil
}

// ResolvePaths makes the relative file paths in the config (SSH identity files and task
// cmd_files) relative to dir, normally the directory holding the config file, so gorun
// can be run from anywhere.
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

	c.SSH.Identityfile = resolve(c.SSH.Identityfile)
	if c.SSH.ConfigFile == "" && c.SSH.Config != "" {
		c.SSH.ConfigFile = c.SSH.Config
	}
	c.SSH.ConfigFile = resolve(c.SSH.ConfigFile)

	for name, task := range c.Tasks {
		task.CmdFile = resolve(task.CmdFile)
		task.CmdTemplateFile = resolve(task.CmdTemplateFile)
		resolveCopy := func(c *Copy) {
			if c.SrcFile != "" && c.Src == "" {
				c.Src = c.SrcFile
			}
			if c.DstPath != "" && c.Dst == "" {
				c.Dst = c.DstPath
			}
			if c.Dest != "" && c.Dst == "" {
				c.Dst = c.Dest
			}
			if c.Template != "" && c.SrcTpl == "" {
				c.SrcTpl = c.Template
			}
			if c.SrcTmpl != "" && c.SrcTpl == "" {
				c.SrcTpl = c.SrcTmpl
			}
			if c.Dir != "" && c.Dst == "" && c.Src != "" {
				// leave for runtime resolution
			}
			if c.Src != "" {
				c.Src = resolve(c.Src)
			}
			if c.SrcTpl != "" {
				c.SrcTpl = resolve(c.SrcTpl)
			}
		}
		for i := range task.Copy {
			resolveCopy(&task.Copy[i])
		}
		for i := range task.Copies {
			resolveCopy(&task.Copies[i])
		}
		for i := range task.Upload {
			resolveCopy(&task.Upload[i])
		}
		for i := range task.Files {
			resolveCopy(&task.Files[i])
		}
		c.Tasks[name] = task
	}

	for i := range c.Servers {
		c.Servers[i].SSH.Identityfile = resolve(c.Servers[i].SSH.Identityfile)
		if c.Servers[i].SSH.ConfigFile == "" && c.Servers[i].SSH.Config != "" {
			c.Servers[i].SSH.ConfigFile = c.Servers[i].SSH.Config
		}
		c.Servers[i].SSH.ConfigFile = resolve(c.Servers[i].SSH.ConfigFile)
		// inherit global ssh config file when the server doesn't specify one
		if c.Servers[i].SSH.ConfigFile == "" && c.SSH.ConfigFile != "" {
			c.Servers[i].SSH.ConfigFile = c.SSH.ConfigFile
		}
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

// ServersPipeline returns every server with its tasks resolved into run order.
func (c *Config) ServersPipeline() (map[string]Server, error) {
	sTasks := make(map[string]Server)

	for _, server := range c.Servers {
		server, err := c.ServerPipeline(server)

		if err != nil {
			return nil, err
		}

		sTasks[server.Name] = server
	}

	return sTasks, nil
}

// ErrNoTasks is returned by Run when no selected server has a task to run. Local
// servers only run tasks marked local = true, and other tasks only run over SSH.
var ErrNoTasks = errors.New("no tasks to run: local servers only run tasks marked local = true, and other tasks only run over SSH")

// ServerPipeline returns the server with its tasks, and their dependencies, resolved into run order.
func (c *Config) ServerPipeline(server Server) (Server, error) {
	names, err := c.ExpandTaskRefs(server.TaskNames...)
	if err != nil {
		err = fmt.Errorf("error building task pipeline for server %q: %w", server.Name, err)
		return Server{}, err
	}

	pipeline, err := resolveTasks(c.Tasks, names...)
	if err != nil {
		err = fmt.Errorf("error building task pipeline for server %q: %w", server.Name, err)
		return Server{}, err
	}

	// A task runs on one kind of server: local-only tasks never run over SSH, and
	// other tasks never run on the local machine. Anything that depends on a
	// dropped task is dropped too.
	local := server.IsLocal()
	pipeline = dropTasks(pipeline, func(t Task) bool { return t.Local != local })

	server.Tasks = pipeline
	return server, nil
}

// dropTasks removes the tasks drop reports, and tasks that transitively depend on them.
func dropTasks(tasks []Task, drop func(Task) bool) []Task {
	removed := map[string]bool{}
	changed := true
	out := append([]Task(nil), tasks...)
	for changed {
		changed = false
		next := out[:0]
		for _, t := range out {
			if drop(t) {
				removed[t.Name] = true
				changed = true
				continue
			}
			depRemoved := false
			for _, dep := range t.DependsOn {
				if removed[dep] {
					depRemoved = true
					break
				}
			}
			if depRemoved {
				removed[t.Name] = true
				changed = true
				continue
			}
			next = append(next, t)
		}
		out = next
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
	var taskNames []string
	if len(names) > 0 {
		expanded, err := c.ExpandTaskPatterns(names...)
		if err != nil {
			return err
		}
		taskNames = expanded
	} else {
		taskNames = c.TaskNames()
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

	targets := make([]Server, len(server))
	copy(targets, server)
	if len(targets) == 0 {
		targets = make([]Server, len(c.Servers))
		copy(targets, c.Servers)
	}
	if len(targets) == 0 {
		return errors.New("no servers to run on")
	}

	// local-only tasks run only on local servers, and other tasks only over SSH
	filtered := targets[:0]
	for _, s := range targets {
		if s.IsLocal() == t.Local {
			filtered = append(filtered, s)
		}
	}
	targets = filtered
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
	names := make([]string, 0, len(c.Tasks))
	for name := range c.Tasks {
		names = append(names, name)
	}
	ordered, err := resolveTasks(c.Tasks, names...)
	if err != nil {
		// Fall back to alphabetical order if the dependency graph cannot be resolved.
		sort.Strings(names)
		return names
	}
	out := make([]string, 0, len(ordered))
	for _, t := range ordered {
		out = append(out, t.Name)
	}
	return out
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

// Run runs all the tasks as returned by `Pipeline` on one or more servers.
func (c *Config) Run(ctx context.Context, server ...Server) error {
	if c.MaxParallel < 0 {
		return errors.New("max_parallel must not be negative")
	}

	hostname, _ := os.Hostname()
	if c.GorunUUID == "" {
		c.GorunUUID = fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), time.Now().UnixNano())
	}

	started := time.Now()

	var wg sync.WaitGroup

	pipeline := make(map[string]Server)

	if len(server) > 0 {
		for _, s := range server {
			s, err := c.ServerPipeline(s)
			if err != nil {
				err = fmt.Errorf("error buiding servers task execution pipeline: %w", err)
				return err
			}
			pipeline[s.Name] = s
		}
	} else {
		pipelines, err := c.ServersPipeline()
		if err != nil {
			err = fmt.Errorf("error buiding servers task execution pipeline: %w", err)
			return err
		}
		pipeline = pipelines
	}

	// merge the global envvars, then the server's (including its groups'), then the
	// task's own into each task, later ones winning. Keys are compared upper-cased and
	// values expand $VAR from gorun's environment. Tasks share their library map
	// between servers, so each one gets a fresh map.
	for name, server := range pipeline {
		// global vars are the lowest layer for server vars, mirroring envvars
		if len(c.Vars) > 0 {
			if server.Vars == nil {
				server.Vars = map[string]any{}
			}
			for k, v := range c.Vars {
				if _, ok := server.Vars[k]; !ok {
					server.Vars[k] = v
				}
			}
		}
		// global envvars are the lowest layer for server envvars too, so templates
		// and dumps see them alongside the server's own.
		if len(c.EnvVars) > 0 {
			if server.EnvVars == nil {
				server.EnvVars = map[string]string{}
			}
			for k, v := range c.EnvVars {
				if _, ok := server.EnvVars[k]; !ok {
					server.EnvVars[k] = v
				}
			}
		}
		for i := range server.Tasks {
			env := map[string]string{}
			for _, layer := range []map[string]string{c.EnvVars, server.EnvVars, server.Tasks[i].EnvVars} {
				for k, v := range layer {
					env[strings.ToUpper(k)] = expandEnv(v)
				}
			}
			server.Tasks[i].EnvVars = env
		}
		pipeline[name] = server
	}

	// apply --task filter (plus dependencies) if requested
	if allowed := c.selectedTaskSet(); allowed != nil {
		for name, server := range pipeline {
			filtered := make([]Task, 0, len(server.Tasks))
			for _, t := range server.Tasks {
				if allowed[t.Name] {
					filtered = append(filtered, t)
				}
			}
			server.Tasks = filtered
			pipeline[name] = server
		}
	}

	// a server left with no tasks of its kind has nothing to do, so don't connect to it
	for name, server := range pipeline {
		if len(server.Tasks) == 0 {
			delete(pipeline, name)
		}
	}
	if len(pipeline) == 0 {
		return ErrNoTasks
	}

	serverNames := slices.Sorted(maps.Keys(pipeline))

	totalTasks := 0
	for _, s := range pipeline {
		totalTasks += len(s.Tasks)
	}

	maxParallel := c.MaxParallel
	if maxParallel == 0 || maxParallel > len(pipeline) {
		maxParallel = len(pipeline)
	}
	parallelSlots := make(chan struct{}, maxParallel)

	var failedCount, skippedCount atomic.Int64

	c.emit(ProcessStartedEvent{
		Type:      "process_started",
		GorunUUID: c.GorunUUID,
		Title:     c.Title,
		Servers:   serverNames,
		StartedAt: started,
		Hostname:  func() string { h, _ := os.Hostname(); return h }(),
		PID:       os.Getpid(),
		EnvVars:   c.EnvVars,
		Vars:      c.Vars,
	})

	// announce every server's plan up front, in a stable order
	names := slices.Sorted(maps.Keys(pipeline))
	for _, name := range names {
		server := pipeline[name]

		tasks := []string{}
		meta := make([]TaskMeta, 0, len(server.Tasks))
		for _, task := range server.Tasks {
			tasks = append(tasks, task.Name)
			meta = append(meta, TaskMeta{
				Name:        task.Name,
				Description: task.Description,
				Details:     task.Details,
				Rerun:       task.Rerun,
			})
		}

		c.emit(PlanEvent{Type: "plan", GorunUUID: c.GorunUUID, Server: server.Name, Address: server.Address(), Tasks: tasks, TaskMeta: meta, EnvVars: server.EnvVars, Vars: server.Vars})
	}

	var failed atomic.Bool

	for _, server := range pipeline {

		if server.LogLevel == "" {
			server.LogLevel = c.LogLevel
		}

		// update tasks logging level based on the server logging level
		for i, task := range server.Tasks {
			if task.LogLevel == "" {
				task.LogLevel = server.LogLevel
			}

			server.Tasks[i] = task
		}
		wg.Go(func() {
			select {
			case parallelSlots <- struct{}{}:
				defer func() { <-parallelSlots }()
			case <-ctx.Done():
				return
			}

			ctx, span := tracer.Start(ctx, "server.run")
			defer span.End()
			span.SetAttributes(
				attribute.String("server.name", server.Name),
				attribute.String("server.address", server.Hostname),
			)

			logger := NewLogger(server.LogLevel, logOutput, false)
			logger = logger.With(
				slog.String("gorun_uuid", c.GorunUUID),
				slog.Group(
					"server",
					slog.String("name", server.Name),
					slog.String("address", server.Hostname),
				),
			)

			var sshClient *ssh.Client

			if server.SSH != (SSH{}) {
				logger.Debug("connecting to server via SSH")

				client, err := NewSSHClient(ctx, server)
				if err != nil {
					// TODO: need to catch this in a error group
					err = fmt.Errorf("error creating SSH connection: %w", err)
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					logger.Error("error with running tasks", slog.String("reason", err.Error()))
					failed.Store(true)
					failedCount.Add(int64(len(server.Tasks)))
					c.emit(ServerErrorEvent{Type: "server_error", GorunUUID: c.GorunUUID, Server: server.Name, Err: err})
					return
				}

				logger.Debug("connected to server via SSH")
				defer func() { _ = closeSSHClient(client) }()
				sshClient = client
			}

			tasks, err := NewTaskExec(sshClient, server.Tasks...)
			if err != nil {
				err = fmt.Errorf("error loading tasks: %w", err)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				logger.Error("error with running tasks", slog.String("reason", err.Error()))
				failed.Store(true)
				failedCount.Add(int64(len(server.Tasks)))
				c.emit(ServerErrorEvent{Type: "server_error", GorunUUID: c.GorunUUID, Server: server.Name, Err: err})
				return
			}

			for _, task := range tasks {
				task.serverName = server.Name
				task.gorunServer = &server
				task.gorunUUID = c.GorunUUID
				task.logger = task.logger.With(slog.String("gorun_uuid", c.GorunUUID))
			}

			// completion signals: a task closes its channel when it finishes
			// (success, failure or skip), so dependents can wait on it.
			done := make(map[string]chan struct{}, len(tasks))
			for _, task := range tasks {
				done[task.task.Name] = make(chan struct{})
			}

			taskByName := make(map[string]*TaskExec, len(tasks))
			for _, task := range tasks {
				taskByName[task.task.Name] = task
			}

			taskLimit := server.MaxTaskParallel
			if taskLimit == 0 {
				taskLimit = c.MaxTaskParallel
			}
			if taskLimit == 0 {
				// default: run a host's tasks one at a time
				taskLimit = 1
			}
			if taskLimit < 0 {
				// -1 means as many as possible
				taskLimit = len(tasks)
			}
			if taskLimit < 1 && len(tasks) > 0 {
				taskLimit = 1
			}
			if taskLimit > len(tasks) {
				taskLimit = len(tasks)
			}
			taskSlots := make(chan struct{}, taskLimit)

			var resultsMu sync.Mutex
			results := make(map[string]error, len(tasks))

			var taskWg sync.WaitGroup
			runOne := func(task *TaskExec) {
				defer taskWg.Done()
				name := task.task.Name
				tlogger := logger.With(slog.String("task", name))

				// wait for every dependency to finish first
				for _, dep := range task.task.DependsOn {
					if ch, ok := done[dep]; ok {
						select {
						case <-ch:
						case <-ctx.Done():
						}
					}
				}

				finish := func(runErr error) {
					resultsMu.Lock()
					results[name] = runErr
					resultsMu.Unlock()
					close(done[name])
				}

				if ctx.Err() != nil {
					err := ctx.Err()
					tlogger.Warn("task skipped due to cancellation", slog.String("reason", err.Error()))
					skippedCmd, _ := task.task.CommandFor(&server)
					c.emit(TaskEvent{Type: "task", GorunUUID: c.GorunUUID, Server: server.Name, Task: name, State: TaskSkipped, Command: skippedCmd, Stderr: err.Error(), Err: err, EnvVars: server.EnvVars, Vars: server.Vars})
					skippedCount.Add(1)
					taskSkipped.Add(ctx, 1, metric.WithAttributes(
						attribute.String("task.name", name),
						attribute.String("server.name", server.Name),
					))
					finish(err)
					return
				}

				// a failed (or skipped) dependency, or any earlier failure,
				// means this task can't run
				resultsMu.Lock()
				depFailed := false
				var depName string
				for _, dep := range task.task.DependsOn {
					if r, ok := results[dep]; ok && r != nil {
						depFailed = true
						depName = dep
						break
					}
				}
				resultsMu.Unlock()

				if depFailed {
					var err error
					if depName != "" {
						if slices.Contains(task.task.DependsOn, depName) {
							err = fmt.Errorf("dependent task (%s) failed, aborting task (%s)", depName, name)
						} else {
							err = fmt.Errorf("task %s failed, aborting task (%s)", depName, name)
						}
					} else {
						err = fmt.Errorf("a previous task failed, aborting task (%s)", name)
					}
					tlogger.Error("error with running tasks", slog.String("reason", err.Error()))
					skippedCmd, _ := task.task.CommandFor(&server)
					c.emit(TaskEvent{Type: "task", GorunUUID: c.GorunUUID, Server: server.Name, Task: name, State: TaskSkipped, Command: skippedCmd, Stderr: err.Error(), Err: err, EnvVars: server.EnvVars, Vars: server.Vars})
					skippedCount.Add(1)
					taskSkipped.Add(ctx, 1, metric.WithAttributes(
						attribute.String("task.name", name),
						attribute.String("server.name", server.Name),
					))
					finish(err)
					return
				}

				// wait for a task slot so a server can opt out of
				// running its tasks in parallel (max_task_parallel = 1)
				select {
				case taskSlots <- struct{}{}:
				case <-ctx.Done():
					err := ctx.Err()
					tlogger.Warn("task skipped due to cancellation", slog.String("reason", err.Error()))
					skippedCmd, _ := task.task.CommandFor(&server)
					c.emit(TaskEvent{Type: "task", GorunUUID: c.GorunUUID, Server: server.Name, Task: name, State: TaskSkipped, Command: skippedCmd, Stderr: err.Error(), Err: err, EnvVars: server.EnvVars, Vars: server.Vars})
					skippedCount.Add(1)
					taskSkipped.Add(ctx, 1, metric.WithAttributes(
						attribute.String("task.name", name),
						attribute.String("server.name", server.Name),
					))
					finish(err)
					return
				}
				defer func() { <-taskSlots }()

				attempts := task.task.Attempts()
				var (
					attempt int
					runErr  error
					started = time.Now()
				)

				for attempt = 1; attempt <= attempts; attempt++ {
					// Retry events only carry attempt numbers when retries are configured.
					evAttempt, evAttempts := 0, 0
					if attempts > 1 {
						evAttempt, evAttempts = attempt, attempts
					}

					runningCmd, _ := task.task.CommandFor(&server)
					runningEvent := TaskEvent{
						Type:      "task",
						GorunUUID: c.GorunUUID,
						Server:    server.Name,
						Task:      name,
						State:     TaskRunning,
						Command:   runningCmd,
						Attempt:   evAttempt,
						Attempts:  evAttempts,
						EnvVars:   server.EnvVars,
						Vars:      server.Vars,
					}

					if attempt > 1 {
						// Carry the previous attempt's failure so consumers can
						// explain why the task is running again.
						runningEvent.ExitCode = task.exitCode
						runningEvent.Stderr = string(task.stderr)
						runningEvent.Err = runErr
						task.resetForRetry()
					}

					c.emit(runningEvent)

					runErr = task.Execute(ctx)
					if runErr == nil || task.stopped || ctx.Err() != nil || attempt == attempts {
						break
					}

					if !task.task.CanRetry() {
						task.appendStderr("not retried: task is marked unsafe to rerun (set retry_unsafe = true to override)")
						tlogger.Warn("task failed; not retrying because it is marked unsafe to rerun",
							slog.Int("attempt", attempt),
							slog.Int("attempts", attempts),
							slog.String("reason", runErr.Error()),
						)
						break
					}

					tlogger.Warn("task failed, retrying",
						slog.Int("attempt", attempt),
						slog.Int("attempts", attempts),
						slog.Duration("delay", task.task.RetryDelayDuration()),
						slog.String("reason", runErr.Error()),
					)

					if !task.waitForRetry(ctx) {
						break
					}
				}

				event := TaskEvent{Type: "task", GorunUUID: c.GorunUUID, Server: server.Name, Task: name, State: TaskDone}
				if attempts > 1 {
					event.Attempt = attempt
					event.Attempts = attempts
				}
				if runErr != nil {
					failed.Store(true)
					failedCount.Add(1)
					event.State = TaskFailed
					event.Err = runErr
				}

				event.Command = task.command
				if event.Command == "" {
					if ccmd, err := task.task.CommandFor(&server); err == nil {
						event.Command = ccmd
					}
				}
				event.Stdout = string(task.stdout)
				event.Stderr = string(task.stderr)
				event.ExitCode = task.exitCode
				event.Duration = time.Since(started)
				event.EnvVars = server.EnvVars
				event.Vars = server.Vars
				c.emit(event)

				finish(runErr)
			}

			if taskLimit == 1 {
				for _, task := range tasks {
					taskWg.Add(1)
					runOne(task)
				}
			} else {
				for _, task := range tasks {
					taskWg.Add(1)
					go runOne(task)
				}
				taskWg.Wait()
			}
			taskWg.Wait()
		})
	}

	wg.Wait()

	success := !failed.Load()

	c.emit(ProcessFinishedEvent{
		Type:       "process_finished",
		GorunUUID:  c.GorunUUID,
		Success:    success,
		Duration:   time.Since(started),
		TotalTasks: totalTasks,
		Failed:     int(failedCount.Load()),
		Skipped:    int(skippedCount.Load()),
		FinishedAt: time.Now(),
	})

	if failed.Load() {
		return errors.New("one or more tasks failed")
	}

	return nil
}

// emit sends an event to the OnEvent callback, if one is set, and publishes it
// to NATS when a publisher is available.
func (c *Config) emit(event any) {
	if c.OnEvent != nil {
		c.OnEvent(event)
	}

	if publisher != nil {
		topic := envvars.Get("GORUN_EVENT_TOPIC")
		if topic == "" {
			topic = "gorun.events"
		}
		if data, err := json.Marshal(event); err == nil {
			_ = publisher.Publish(topic, data)
			_ = publisher.Flush()
		}
	}
}
