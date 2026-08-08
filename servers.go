package main

// Server is a server to connect to and execute tasks.
type Server struct {
	LogLevel string `toml:"log_level"`
	Name     string `toml:"name"`
	Hostname string `toml:"hostname"`
	SSH      SSH    `toml:"ssh"`
	Tasks    []Task `toml:"tasks"`
}
