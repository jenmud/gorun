package main

// Task is a task to execute on one or more servers.
type Task struct {
	Name  string `toml:"name"`
	Cmd   string `toml:"cmd"`
	Tasks []Task `toml:"tasks"`
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

// ExecutionTask is a task to be executed on a server
type ExecutionTask struct {
	name      string
	server    Server
	cmd       string
	completed bool
	failed    bool
}
