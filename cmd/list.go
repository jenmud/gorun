package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/jenmud/gorun"
)

// printTags writes every tag with the servers that have it, for --list-tags. Only the
// given servers count, so the server filters apply. Unless filtered, tags set on a group
// that no server joins are listed too.
func printTags(w io.Writer, cfg *gorun.Config, servers []gorun.Server, filtered bool) {
	members := tagMembers(cfg, servers, filtered)
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

// tagMembers maps every tag to the names of the servers that have it.
func tagMembers(cfg *gorun.Config, servers []gorun.Server, filtered bool) map[string][]string {
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

	return members
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

// taskDependents returns the tasks without dependencies and, for each task, the tasks
// that depend on it, both sorted. With shown set, only those tasks are included.
func taskDependents(cfg *gorun.Config, shown map[string]bool) ([]string, map[string][]string) {
	children := map[string][]string{}
	roots := []string{}
	for name := range cfg.Tasks {
		if shown != nil && !shown[name] {
			continue
		}
		if len(cfg.Tasks[name].DependsOn) == 0 {
			roots = append(roots, name)
		}
		for _, dep := range cfg.Tasks[name].DependsOn {
			children[dep] = append(children[dep], name)
		}
	}
	sort.Strings(roots)
	for _, kids := range children {
		sort.Strings(kids)
	}
	return roots, children
}

// taskLabel describes a task for --list-tasks.
func taskLabel(cfg *gorun.Config, name string) string {
	task := cfg.Tasks[name]
	s := name
	if task.Description != "" {
		s += " — " + task.Description
	}
	if task.Rerun == gorun.RerunUnsafe {
		s += " (⚠ not safe to rerun)"
	}
	if task.Retries > 0 {
		s += fmt.Sprintf(" (retries: %d)", task.Retries)
	}
	return s
}

// printTasks writes the tasks as a reverse dependency tree, for --list-tasks: a task's
// children are the tasks that depend on it, so dependencies appear above their
// dependents. A task reached a second time is marked rather than repeated.
func printTasks(w io.Writer, cfg *gorun.Config, shown map[string]bool) {
	roots, children := taskDependents(cfg, shown)

	visited := map[string]bool{}
	var printNode func(name, prefix string, last, root bool)
	printNode = func(name, prefix string, last, root bool) {
		connector := "├── "
		if last {
			connector = "└── "
		}
		if visited[name] {
			if root {
				fmt.Fprintf(w, "%s (already shown)\n", taskLabel(cfg, name))
			} else {
				fmt.Fprintf(w, "%s%s%s (already shown)\n", prefix, connector, taskLabel(cfg, name))
			}
			return
		}
		if root {
			fmt.Fprintln(w, taskLabel(cfg, name))
		} else {
			fmt.Fprintf(w, "%s%s%s\n", prefix, connector, taskLabel(cfg, name))
		}
		visited[name] = true
		kids := children[name]
		childPrefix := prefix
		if last {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
		for i, child := range kids {
			printNode(child, childPrefix, i == len(kids)-1, false)
		}
	}

	for _, root := range roots {
		printNode(root, "", true, true)
	}
}

// serverTaskLabel describes a task in a server's pipeline, for --list-servers.
func serverTaskLabel(t gorun.Task) string {
	label := t.Name
	if t.Retries > 0 {
		label += fmt.Sprintf(" (retries: %d)", t.Retries)
	}
	if t.Rerun == gorun.RerunUnsafe {
		label += " ⚠"
	}
	return label
}

// printServers writes every server with its groups, tags and task pipeline, for --list-servers.
func printServers(w io.Writer, servers map[string]gorun.Server) {
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		server := servers[name]
		fmt.Fprintf(w, "%s (%s)\n", server.Name, server.Address())
		if len(server.Groups) > 0 {
			fmt.Fprintf(w, "    groups: %s\n", strings.Join(server.Groups, ", "))
		}
		if len(server.Tags) > 0 {
			fmt.Fprintf(w, "    tags: %s\n", strings.Join(server.Tags, ", "))
		}
		for i, t := range server.Tasks {
			connector := "├── "
			if i == len(server.Tasks)-1 {
				connector = "└── "
			}
			fmt.Fprintf(w, "    %s%s\n", connector, serverTaskLabel(t))
		}
		fmt.Fprintln(w)
	}
}
