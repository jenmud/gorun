package main

import (
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/jenmud/gorun"
)

// completionNames returns the names a shell can complete for kind, which is
// "tasks" (including "@bundle" references), "bundles", "servers", "tags" or "groups". It is
// used by the bash completion function documented in docs/integrations.md.
func completionNames(kind, filename string) ([]string, error) {
	if filename == "" {
		filename = "Gorunfile"
	}

	cfg := gorun.Config{}
	if err := cfg.LoadFile(filename); err != nil {
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
	case "bundles":
		names = slices.Collect(maps.Keys(cfg.Bundles))
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
		return nil, fmt.Errorf("unknown completion kind %q (want tasks, bundles, servers, tags or groups)", kind)
	}

	sort.Strings(names)
	return names, nil
}
