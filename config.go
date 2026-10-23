package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
	"go.opentelemetry.io/otel"
	"golang.org/x/crypto/ssh"
)

var tracer = otel.Tracer("gorun")

// Config represents a config file.
type Config struct {
	Title    string            `toml:"title"`
	LogLevel string            `toml:"log_level"`
	EnvVars  map[string]string `toml:"envvars"`
	SSH      SSH               `toml:"ssh"`
	Servers  []Server          `toml:"servers"`
	Tasks    []Task            `toml:"tasks"`

	// OnEvent, when set, receives the events in events.go as tasks run.
	OnEvent func(event any) `toml:"-"`
}

// Load reads and updates the config.
func (c *Config) Load(r io.Reader) error {
	decoder := toml.NewDecoder(r)

	if _, err := decoder.Decode(c); err != nil {
		return fmt.Errorf("error with loading config: %w", err)
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

	for i := range c.Tasks {
		c.Tasks[i].CmdFile = resolve(c.Tasks[i].CmdFile)
	}

	for i := range c.Servers {
		server := &c.Servers[i]
		server.SSH.Identityfile = resolve(server.SSH.Identityfile)

		for j := range server.Tasks {
			server.Tasks[j].CmdFile = resolve(server.Tasks[j].CmdFile)
		}
	}
}

// TaskMap returns a map of all the loaded tasks for fast lookup.
func (c *Config) TaskMap() map[string]Task {
	m := make(map[string]Task)
	for _, t := range c.Tasks {
		m[t.Name] = t
	}
	return m
}

// topological returns all the tasks in a topological sort using the Kahn method.
func topological(tasks ...Task) ([]Task, error) {
	// keep track of tasks and the count of inbound edges.
	inDegree := map[string]int{}

	// fast lookup for pulling out task lists
	adj := map[string][]Task{}

	// fist build a unique lookup map of tasks
	mappedTasks := map[string]Task{}
	for _, task := range tasks {
		mappedTasks[task.Name] = task
		inDegree[task.Name] = 0
	}

	edges := [][2]Task{}
	for _, task := range tasks {
		for _, dep := range task.DependsOn {
			d, ok := mappedTasks[dep]
			if !ok {
				return nil, fmt.Errorf("tasks %q not found", dep)
			}
			edges = append(edges, [2]Task{d, task})
		}
	}

	// build the dependency graph
	for _, edge := range edges {
		u, v := edge[0], edge[1]
		adj[u.Name] = append(adj[u.Name], v)
		inDegree[v.Name]++
	}

	queue := []Task{}
	for task, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, mappedTasks[task])
		}
	}

	result := []Task{}

	for len(queue) > 0 {
		task := queue[0]
		queue = queue[1:]
		result = append(result, task)

		for _, d := range adj[task.Name] {
			inDegree[d.Name]--
			if inDegree[d.Name] == 0 {
				queue = append(queue, d)
			}
		}
	}

	if len(result) != len(tasks) {
		return nil, errors.New("cyclic dependency detected")
	}

	return result, nil
}

// Pipeline returns the tasks sorted using Kahn topological soring.
func (c *Config) Pipeline() ([]Task, error) {
	return topological(c.Tasks...)
}

// ServersPipeline returns all the servers tasks in topological sort.
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

// ServerPipeline returns a server's tasks in topological sort.
func (c *Config) ServerPipeline(server Server) (Server, error) {
	tasks := append([]Task{}, c.Tasks...)

	serverTasks := append(tasks, server.Tasks...)

	pipeline, err := topological(serverTasks...)
	if err != nil {
		err = fmt.Errorf("error building server task pipeline: %w", err)
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

		c.emit(PlanEvent{Server: server.Name, Address: serverAddress(server), Tasks: tasks})
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
				logger.Error("error with running tasks", slog.String("reason", err.Error()))
				failed.Store(true)
				c.emit(ServerErrorEvent{Server: server.Name, Err: err})
				return
			}

			for _, task := range tasks {
				tlogger := logger.With(slog.String("task", task.task.Name))

				if parent != nil && parent.exitCode != 0 {
					err = fmt.Errorf("dependent task (%s, exit code=%d) failed, aborting task (%s)", parent.task.Name, parent.exitCode, task.task.Name)
					tlogger.Error("error with running tasks", slog.String("reason", err.Error()))
					c.emit(TaskEvent{Server: server.Name, Task: task.task.Name, State: TaskSkipped, Err: err})
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
