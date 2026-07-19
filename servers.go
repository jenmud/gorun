package main

// Server is a server to connect to and execute tasks.
type Server struct {
	Hostname string `toml:"hostname"`
	SSH      SSH    `toml:"ssh"`
	Tasks    []Task `toml:"tasks"`
}
