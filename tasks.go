package main

// Task is a task to execute on one or more servers.
type Task struct {
	Name    string   `toml:"name"`
	Cmd     string   `toml:"cmd"`
	Servers []Server `toml:"servers"`
	Tasks   []Task   `toml:"tasks"`
}

// UniqueServers returns all the known servers filtering out duplicates.
func (t Task) UniqueServers() []Server {
	servers := map[string]Server{}

	// first pull out the global servers
	for _, s := range t.Servers {
		servers[s.Hostname] = s
	}

	// run over all the other tasks recursively
	for _, task := range t.Tasks {
		for _, s := range task.UniqueServers() {
			servers[s.Hostname] = s
		}
	}

	i := 0
	found := make([]Server, len(servers))

	for _, server := range servers {
		found[i] = server
		i++
	}

	return found
}

// TaskPipeline returns the full task execution pipeline.
func (t Task) TaskPipeline() []Task {
	pipeline := []Task{}

	for _, task := range t.Tasks {
		pipeline = append(pipeline, task)

		for _, subTask := range task.Tasks {
			pipeline = append(pipeline, subTask)
		}
	}

	return pipeline
}
