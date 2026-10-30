package main

import (
	"fmt"
	"os"
	"sort"

	"gorun"
)

// completionNames returns the names a shell can complete for kind, which is
// either "tasks" or "servers". It is used by the bash completion function
// documented in the README.
func completionNames(kind, filename string) ([]string, error) {
	if filename == "" {
		filename = "Gorunfile"
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	cfg := gorun.Config{}
	if err := cfg.Load(f); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}

	var names []string
	switch kind {
	case "tasks":
		names = make([]string, 0, len(cfg.Tasks))
		for name := range cfg.Tasks {
			names = append(names, name)
		}
	case "servers":
		names = make([]string, 0, len(cfg.Servers))
		for _, server := range cfg.Servers {
			if server.Name != "" {
				names = append(names, server.Name)
			}
		}
	default:
		return nil, fmt.Errorf("unknown completion kind %q (want tasks or servers)", kind)
	}

	sort.Strings(names)
	return names, nil
}
