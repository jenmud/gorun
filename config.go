package main

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/BurntSushi/toml"
)

// Config represents a config file.
type Config struct {
	Title   string            `toml:"title"`
	EnvVars map[string]string `toml:"envvars"`
	SSH     SSH               `toml:"ssh"`
	Servers []Server          `toml:"servers"`
	Tasks   []Task            `toml:"tasks"`
}

// Load reads and updates the config.
func (c *Config) Load(r io.Reader) error {
	decoder := toml.NewDecoder(r)

	if _, err := decoder.Decode(c); err != nil {
		return fmt.Errorf("error with loading config: %w", err)
	}

	return nil
}

// UniqueServers returns all the known servers in the config filtering out duplicates.
func (c Config) UniqueServers() []Server {
	servers := map[string]Server{}

	// first pull out the global servers
	for _, s := range c.Servers {
		servers[s.Hostname] = s
	}

	// now run over all the tasks adding in the tasks servers.
	for _, task := range c.Tasks {
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
func (c Config) TaskPipeline() []Task {
	pipeline := []Task{}

	for _, t := range c.Tasks {
		pipeline = append(pipeline, t)

		for _, subT := range t.Tasks {
			subTasks := subT.TaskPipeline()
			pipeline = append(pipeline, subTasks...)
		}
	}

	return pipeline
}

func run(t Task, indent int) {
	indentStr := ""
	for range indent {
		indentStr += "\t"
	}
	slog.Info(fmt.Sprintf("%s -> %s\n", indentStr, t.Name))
	for _, subT := range t.Tasks {
		indent++
		run(subT, indent)
	}
}

func (c Config) RunTaskPipeline() {
	slog.Info("running task pipeline")

	for _, task := range c.Tasks {
		run(task, 0)
	}
}
