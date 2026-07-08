package main

// Task is a task to execute on one or more servers.
type Task struct {
	Name      string   `toml:"name"`
	Cmd       string   `toml:"cmd"`
	DependsOn []string `toml:"depends_on"`
}

// ExecutionTask is a task to be executed on a server
type ExecutionTask struct {
	name      string
	server    Server
	cmd       string
	completed bool
	failed    bool
}
