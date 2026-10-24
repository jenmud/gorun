package gorun

import "fmt"

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

// Address formats a server's connection address for display ("local" when no SSH is configured).
func (s Server) Address() string {
	if s.SSH == (SSH{}) {
		return "local"
	}

	port := s.SSH.Port
	if port == 0 {
		port = 22
	}

	return fmt.Sprintf("%s:%d", s.Hostname, port)
}
