package gorun

import "fmt"

// Server is a server to connect to and execute tasks.
type Server struct {
	LogLevel        string `toml:"log_level,omitempty"`
	MaxTaskParallel int    `toml:"max_task_parallel,omitzero"` // 0 (default) = one at a time; -1 = all at once
	Name            string `toml:"-"`                          // the <name> in [servers.hosts.<name>]
	Source          string `toml:"-"`                          // the file that defines it
	Hostname        string `toml:"hostname,omitempty"`
	SSH             SSH    `toml:"ssh,omitempty"`

	// TaskNames are the tasks to run, by name. Load replaces them with the server's
	// final task list: its own tasks (or the global and group lists), plus AddTasks,
	// minus SkipTasks, with bundles expanded.
	TaskNames []string `toml:"tasks"`

	// Groups are the [server_groups.<name>] this server joins, in order. Load applies
	// their defaults to the server.
	Groups []string `toml:"groups,omitempty"`

	// Tags label the server for --tag selection. Load adds its groups' tags.
	Tags []string `toml:"tags,omitempty"`

	// EnvVars are exported to every task on this server. Load merges in its groups'
	// envvars; a task's own envvars still win.
	EnvVars map[string]string `toml:"envvars,omitempty"`

	// Vars are template variables for this server (and its tasks). Load merges in its
	// groups' vars; a task's own vars still win in its templates.
	Vars map[string]any `toml:"vars,omitempty"`

	// Raw keeps the server's own vars and envvars exactly as written, with no templating.
	Raw bool `toml:"raw,omitempty"`

	// AddTasks are appended to the server's task list and SkipTasks removed from it.
	AddTasks  []string `toml:"add_tasks,omitempty"`
	SkipTasks []string `toml:"skip_tasks,omitempty"`

	// Tasks is the resolved pipeline, including dependencies, set by ServerPipeline.
	Tasks []Task `toml:"-"`
}

// IsLocal reports whether the server runs on the local machine (no SSH configured).
func (s Server) IsLocal() bool {
	return s.SSH == (SSH{})
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

// LocalServer is a ready-to-use Server that runs tasks on the local machine.
var LocalServer = Server{Name: "local"}
