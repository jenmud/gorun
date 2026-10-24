package gorun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
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

	"gorun/internal/envvars"
)

func init() {
	// Default the OTEL service name to GORUN_NAME when OTEL_SERVICE_NAME is
	// not explicitly provided, so traces/metrics are attributed correctly.
	if _, ok := os.LookupEnv("OTEL_SERVICE_NAME"); !ok {
		if name := envvars.Get("GORUN_NAME"); name != "" {
			os.Setenv("OTEL_SERVICE_NAME", name)
		} else {
			os.Setenv("OTEL_SERVICE_NAME", "gorun")
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
	Title    string            `toml:"title"`
	LogLevel string            `toml:"log_level"`
	EnvVars  map[string]string `toml:"envvars"`
	SSH      SSH               `toml:"ssh"`

	// Tasks is the task library, keyed by task name. Servers pick tasks from it by name.
	Tasks map[string]Task `toml:"tasks"`

	// Servers are the [servers.hosts.<name>] tables, in the order they appear in the file.
	Servers []Server `toml:"-"`

	// OnEvent, when set, receives the events in events.go as tasks run.
	OnEvent func(event any) `toml:"-"`
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
			keys = append(keys, key.String())
		}
		return fmt.Errorf("error with loading config: unknown keys: %s", strings.Join(keys, ", "))
	}

	*c = f.Config

	// tasks and servers are named by their table keys
	for name, task := range c.Tasks {
		task.Name = name
		c.Tasks[name] = task
	}

	// map iteration order is random, so walk the keys to keep the servers in file order
	c.Servers = nil
	for _, key := range md.Keys() {
		if len(key) != 3 || key[0] != "servers" || key[1] != "hosts" {
			continue
		}

		server := f.Servers.Hosts[key[2]]
		server.Name = key[2]

		// a server's own task list replaces the global one
		if !md.IsDefined(append(key, "tasks")...) {
			server.TaskNames = f.Servers.Tasks
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
		if path == "" || filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(dir, path)
	}

	c.SSH.Identityfile = resolve(c.SSH.Identityfile)

	for name, task := range c.Tasks {
		task.CmdFile = resolve(task.CmdFile)
		c.Tasks[name] = task
	}

	for i := range c.Servers {
		c.Servers[i].SSH.Identityfile = resolve(c.Servers[i].SSH.Identityfile)
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

// ServerPipeline returns the server with its tasks, and their dependencies, resolved into run order.
func (c *Config) ServerPipeline(server Server) (Server, error) {
	pipeline, err := resolveTasks(c.Tasks, server.TaskNames...)
	if err != nil {
		err = fmt.Errorf("error building task pipeline for server %q: %w", server.Name, err)
		return Server{}, err
	}

	server.Tasks = pipeline
	return server, nil
}

// RunTask runs a single task one or more servers.
func (c *Config) RunTask(ctx context.Context, t Task, server ...Server) error {
	return errors.New("not implemented")
}

// Run runs all the tasks as returned by `Pipeline` on one or more servers.
func (c *Config) Run(ctx context.Context, server ...Server) error {
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

	// announce every server's plan up front, in a stable order
	names := slices.Sorted(maps.Keys(pipeline))
	for _, name := range names {
		server := pipeline[name]

		tasks := []string{}
		for _, task := range server.Tasks {
			tasks = append(tasks, task.Name)
		}

		c.emit(PlanEvent{Server: server.Name, Address: server.Address(), Tasks: tasks})
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
			ctx, span := tracer.Start(ctx, "server.run")
			defer span.End()
			span.SetAttributes(
				attribute.String("server.name", server.Name),
				attribute.String("server.address", server.Hostname),
			)

			logger := NewLogger(server.LogLevel, logOutput, false)
			logger = logger.With(
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
					c.emit(ServerErrorEvent{Server: server.Name, Err: err})
					return
				}

				logger.Debug("connected to server via SSH")
				defer client.Close()
				sshClient = client
			}

			var parent *TaskExec

			tasks, err := NewTaskExec(sshClient, server.Tasks...)
			if err != nil {
				err = fmt.Errorf("error loading tasks: %w", err)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				logger.Error("error with running tasks", slog.String("reason", err.Error()))
				failed.Store(true)
				c.emit(ServerErrorEvent{Server: server.Name, Err: err})
				return
			}

			for _, task := range tasks {
				task.serverName = server.Name
				tlogger := logger.With(slog.String("task", task.task.Name))

				if parent != nil && parent.exitCode != 0 {
					err = fmt.Errorf("dependent task (%s, exit code=%d) failed, aborting task (%s)", parent.task.Name, parent.exitCode, task.task.Name)
					tlogger.Error("error with running tasks", slog.String("reason", err.Error()))
					c.emit(TaskEvent{Server: server.Name, Task: task.task.Name, State: TaskSkipped, Err: err})
					taskSkipped.Add(ctx, 1, metric.WithAttributes(
						attribute.String("task.name", task.task.Name),
						attribute.String("server.name", server.Name),
					))
					continue
				}

				parent = task

				c.emit(TaskEvent{Server: server.Name, Task: task.task.Name, State: TaskRunning})
				started := time.Now()

				event := TaskEvent{Server: server.Name, Task: task.task.Name, State: TaskDone}
				if err := task.Execute(ctx); err != nil {
					failed.Store(true)
					event.State = TaskFailed
					event.Err = err
				}

				event.Stdout = string(task.stdout)
				event.Stderr = string(task.stderr)
				event.ExitCode = task.exitCode
				event.Duration = time.Since(started)
				c.emit(event)
			}
		})
	}

	wg.Wait()

	if failed.Load() {
		return errors.New("one or more tasks failed")
	}

	return nil
}

// emit sends an event to the OnEvent callback, if one is set.
func (c *Config) emit(event any) {
	if c.OnEvent != nil {
		c.OnEvent(event)
	}
}
