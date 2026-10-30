package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/jenmud/gorun"
)

// printTags writes every tag with the servers that have it, for --list-tags. Only the
// given servers count, so the server filters apply. Unless filtered, tags set on a group
// that no server joins are listed too.
func printTags(w io.Writer, cfg *gorun.Config, servers []gorun.Server, filtered bool) {
	members := map[string][]string{}
	for _, server := range servers {
		for _, tag := range server.Tags {
			members[tag] = append(members[tag], server.Name)
		}
	}

	if !filtered {
		for _, group := range cfg.Groups {
			for _, tag := range group.Tags {
				if _, ok := members[tag]; !ok {
					members[tag] = nil
				}
			}
		}
	}

	if len(members) == 0 {
		fmt.Fprintln(w, "no tags")
		return
	}

	for _, tag := range slices.Sorted(maps.Keys(members)) {
		names := members[tag]
		if len(names) == 0 {
			fmt.Fprintf(w, "%s (no servers)\n", tag)
			continue
		}
		fmt.Fprintf(w, "%s (%d): %s\n", tag, len(names), strings.Join(names, ", "))
	}
}

// printBundles writes every bundle as a tree, for --list-bundles: its tasks in order, with
// included bundles expanded underneath, then the task names it expands to when it
// includes other bundles.
func printBundles(w io.Writer, cfg *gorun.Config) {
	if len(cfg.Bundles) == 0 {
		fmt.Fprintln(w, "no bundles")
		return
	}

	var printEntries func(refs []string, prefix string)
	printEntries = func(refs []string, prefix string) {
		for i, ref := range refs {
			connector, childPrefix := "├── ", prefix+"│   "
			if i == len(refs)-1 {
				connector, childPrefix = "└── ", prefix+"    "
			}

			label := ref
			if task, ok := cfg.Tasks[ref]; ok && task.Description != "" {
				label += " — " + task.Description
			}
			fmt.Fprintf(w, "%s%s%s\n", prefix, connector, label)

			// Load rejects cyclic and unknown bundles, so this terminates
			if name, ok := strings.CutPrefix(ref, gorun.BundlePrefix); ok {
				printEntries(cfg.Bundles[name].Tasks, childPrefix)
			}
		}
	}

	for i, name := range slices.Sorted(maps.Keys(cfg.Bundles)) {
		if i > 0 {
			fmt.Fprintln(w)
		}

		bundle := cfg.Bundles[name]
		label := gorun.BundlePrefix + name
		if bundle.Description != "" {
			label += " — " + bundle.Description
		}
		fmt.Fprintln(w, label)
		printEntries(bundle.Tasks, "    ")

		nested := slices.ContainsFunc(bundle.Tasks, func(ref string) bool {
			return strings.HasPrefix(ref, gorun.BundlePrefix)
		})
		if nested {
			if names, err := cfg.ExpandTaskRefs(gorun.BundlePrefix + name); err == nil {
				fmt.Fprintf(w, "    = %s\n", strings.Join(names, ", "))
			}
		}
	}
}
