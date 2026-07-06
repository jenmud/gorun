package main

// Task is a task to execute on one or more servers.
type Task struct {
	Name    string   `toml:"name"`
	Cmd     string   `toml:"cmd"`
	Servers []Server `toml:"servers"`
	Tasks   []Task   `toml:"tasks"`
}
