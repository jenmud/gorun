package main

import (
	"errors"
	"fmt"
	"log/slog"
)

// TopologicalSort is a function which takes one of more tasks and will return the tasks topically sorted based on the Kahn's topological sorting
func TopologicalSort(t ...Task) ([]Task, error) {
	// keep track of tasks and the count of inbound edges.
	inDegree := map[string]int{}

	// fast lookup for pulling out task lists
	adj := map[string][]Task{}

	// fist build a unique lookup map of tasks
	tasks := map[string]Task{}
	for _, task := range t {
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
		slog.Error("tasks ->", "tasks", tasks)
		slog.Error("results ->", "results", result)
		return nil, errors.New("cyclic dependency detected")
	}

	return result, nil
}
