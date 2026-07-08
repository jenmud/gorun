package main

import (
	"fmt"
	"io"

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

func run(t Task, indent string) {
	fmt.Printf("%s -> %s\n", indent, t.Name)

	for _, subT := range t.Tasks {
		run(subT, indent+"\t")
	}
}

func (c Config) RunTaskPipeline() {
	for _, task := range c.Tasks {
		fmt.Println()
		run(task, "")
	}
}
