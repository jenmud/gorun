package gorun

import "fmt"

// Server is a server to connect to and execute tasks.
type Server struct {
	LogLevel string `toml:"log_level"`
	Name     string `toml:"-"` // the <name> in [servers.hosts.<name>]
	Hostname string `toml:"hostname"`
	SSH      SSH    `toml:"ssh"`

	// TaskNames are the tasks to run, by name. Load replaces them with the server's
	// final task list: its own tasks (or the global and group lists), plus AddTasks,
	// minus SkipTasks, with bundles expanded.
	TaskNames []string `toml:"tasks"`

	// Groups are the [server_groups.<name>] this server joins, in order. Load applies
	// their defaults to the server.
	Groups []string `toml:"groups"`

	// Tags label the server for --tag selection. Load adds its groups' tags.
	Tags []string `toml:"tags"`

	// EnvVars are exported to every task on this server. Load merges in its groups'
	// envvars; a task's own envvars still win.
	EnvVars map[string]string `toml:"envvars"`

	// AddTasks are appended to the server's task list and SkipTasks removed from it.
	AddTasks  []string `toml:"add_tasks"`
	SkipTasks []string `toml:"skip_tasks"`

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
