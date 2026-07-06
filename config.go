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
