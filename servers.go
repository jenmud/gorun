package main

// Server is a server to connect to and execute tasks.
type Server struct {
	LogLevel string `toml:"log_level"`
	Name     string `toml:"-"` // the <name> in [servers.hosts.<name>]
	Hostname string `toml:"hostname"`
	SSH      SSH    `toml:"ssh"`

	// TaskNames are the tasks to run, by name. Load fills in the global servers.tasks
	// list when the server doesn't set its own.
	TaskNames []string `toml:"tasks"`

	// Tasks is the resolved pipeline, including dependencies, set by ServerPipeline.
	Tasks []Task `toml:"-"`
}
