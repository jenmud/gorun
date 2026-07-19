package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"golang.org/x/crypto/ssh"
)

// Config represents a config file.
type Config struct {
	Title   string            `toml:"title"`
	EnvVars map[string]string `toml:"envvars"`
	SSH     SSH               `toml:"ssh"`
	Servers []Server          `toml:"servers"`
	Tasks   []Task            `toml:"tasks"`
}

// Load reads and updates the config.
func (c *Config) Load(r io.Reader) error {
	decoder := toml.NewDecoder(r)

	if _, err := decoder.Decode(c); err != nil {
		return fmt.Errorf("error with loading config: %w", err)
	}

	return nil
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
func (c *Config) ServersPipeline() (map[string][]Task, error) {
	sTasks := make(map[string][]Task)
	tasks := append([]Task{}, c.Tasks...)

	for _, server := range c.Servers {
		serverTasks := append(tasks, server.Tasks...)

		pipeline, err := topological(serverTasks...)
		if err != nil {
			err = fmt.Errorf("error building server task pipeline: %w", err)
			return nil, err
		}

		sTasks[server.Hostname] = pipeline
	}

	return sTasks, nil
}

// RunTask runs a single task one or more servers.
func (c *Config) RunTask(ctx context.Context, t Task, server ...Server) error {
	return errors.New("not implemented")
}

// Run runs all the tasks as returned by `Pipeline` on one or more servers.
func (c *Config) Run(ctx context.Context, server ...Server) error {
	pipeline, err := c.Pipeline()
	if err != nil {
		return fmt.Errorf("error building task execution pipeline: %w", err)
	}

	var wg sync.WaitGroup

	for _, server := range c.Servers {
		wg.Go(func() {
			logger := slog.With(
				slog.Group(
					"server",
					slog.String("address", server.Hostname),
				),
			)

			var session *ssh.Session

			if strings.ToLower(server.Hostname) != "localhost" {
				logger.Debug("connecting to server via SSH")
				sshClient, err := NewSSHClient(ctx, server)
				if err != nil {
					// TODO: need to catch this in a error group
					err = fmt.Errorf("error creating SSH connection: %w", err)
					logger.Error("error with running tasks", slog.String("reason", err.Error()))
					panic(err)
				}

				defer sshClient.Close()

				sshSession, err := sshClient.NewSession()
				if err != nil {
					// TODO: need to catch this in a error group
					err = fmt.Errorf("error creating SSH session: %w", err)
					logger.Error("error with running tasks", slog.String("reason", err.Error()))
					panic(err)
				}

				defer sshSession.Close()

				session = sshSession
			}

			for _, task := range NewTaskExec(session, pipeline...) {
				if err := task.Execute(ctx); err != nil {
					// TODO: need to catch this in a error group
					err = fmt.Errorf("error executing task via the SSH session: %w", err)
					logger.Error("error with running tasks", slog.String("reason", err.Error()))
					panic(err)
				}
			}
		})
	}

	wg.Wait()
	return nil
}
