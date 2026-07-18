package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
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

// Pipeline returns the tasks sorted using Kahn topological soring.
func (c *Config) Pipeline() ([]Task, error) {
	// keep track of tasks and the count of inbound edges.
	inDegree := map[string]int{}

	// fast lookup for pulling out task lists
	adj := map[string][]Task{}

	// fist build a unique lookup map of tasks
	tasks := map[string]Task{}
	for _, task := range c.Tasks {
		tasks[task.Name] = task
		inDegree[task.Name] = 0
	}

	edges := [][2]Task{}
	for _, task := range tasks {
		for _, dep := range task.DependsOn {
			d, ok := tasks[dep]
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
			queue = append(queue, tasks[task])
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
			sshClient, err := NewSSHClient(ctx, server)
			if err != nil {
				// TODO: need to catch this in a error group
				panic(err)
			}

			defer sshClient.Close()

			session, err := sshClient.NewSession()
			if err != nil {
				// TODO: need to catch this in a error group
				panic(err)
			}

			defer session.Close()

			for _, task := range pipeline {
				t := TaskExec{
					task:      task,
					server:    session,
					createdAt: time.Now(),
				}

				if err := t.Execute(ctx); err != nil {
					// TODO: need to catch this in a error group
					panic(err)
				}
			}
		})
	}

	wg.Wait()
	return nil
}
