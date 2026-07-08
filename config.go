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

// TaskMap returns a map of all the loaded tasks for fast lookup.
func (c *Config) TaskMap() map[string]Task {
	m := make(map[string]Task)
	for _, t := range c.Tasks {
		m[t.Name] = t
	}
	return m
}

func (c *Config) Pipeline() [][]Task {
	//tasks := c.TaskMap()
	pipeline := []Task{}

	for _, t := range c.Tasks {
		pipeline = append(pipeline, t)
	}

	return [][]Task{pipeline}
}
