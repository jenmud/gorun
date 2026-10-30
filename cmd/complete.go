package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"

	"github.com/jenmud/gorun"
)

// completionNames returns the names a shell can complete for kind, which is
// "tasks" (including "@bundle" references), "servers", "tags" or "groups". It is
// used by the bash completion function documented in the README.
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
		names = make([]string, 0, len(cfg.Tasks)+len(cfg.Bundles))
		for name := range cfg.Tasks {
			names = append(names, name)
		}
		for name := range cfg.Bundles {
			names = append(names, gorun.BundlePrefix+name)
		}
	case "servers":
		names = make([]string, 0, len(cfg.Servers))
		for _, server := range cfg.Servers {
			if server.Name != "" {
				names = append(names, server.Name)
			}
		}
	case "tags":
		seen := map[string]bool{}
		for _, server := range cfg.Servers {
			for _, tag := range server.Tags {
				seen[tag] = true
			}
		}
		for _, group := range cfg.Groups {
			for _, tag := range group.Tags {
				seen[tag] = true
			}
		}
		names = slices.Collect(maps.Keys(seen))
	case "groups":
		names = slices.Collect(maps.Keys(cfg.Groups))
	default:
		return nil, fmt.Errorf("unknown completion kind %q (want tasks, servers, tags or groups)", kind)
	}

	sort.Strings(names)
	return names, nil
}
