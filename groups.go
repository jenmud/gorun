package gorun

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
)

// BundlePrefix marks a task list entry as a reference to a bundle rather than a task,
// e.g. "@base" for [bundles.base].
const BundlePrefix = "@"

// Bundle is a named list of tasks, defined as [bundles.<name>]. Task lists refer to it
// as "@<name>", and it expands to its tasks in order. A bundle may include other bundles.
type Bundle struct {
	Name        string   `toml:"-"` // the <name> in [bundles.<name>]
	Description string   `toml:"description"`
	Tasks       []string `toml:"tasks"`
}

// ServerGroup holds defaults shared by the servers that join it, defined as
// [server_groups.<name>]. A server joins groups by listing them in its groups key.
type ServerGroup struct {
	Name     string            `toml:"-"` // the <name> in [server_groups.<name>]
	LogLevel string            `toml:"log_level"`
	Tags     []string          `toml:"tags"`
	Tasks    []string          `toml:"tasks"`
	EnvVars  map[string]string `toml:"envvars"`
	SSH      SSH               `toml:"ssh"`
}

// validateLibrary checks the task, bundle and group definitions, so mistakes in a
// bundle or group are reported even when no server uses it.
func (c *Config) validateLibrary() error {
	for _, name := range slices.Sorted(maps.Keys(c.Tasks)) {
		if strings.HasPrefix(name, BundlePrefix) {
			return fmt.Errorf("task %q: task names must not start with %q, which marks a bundle", name, BundlePrefix)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(c.Bundles)) {
		if strings.HasPrefix(name, BundlePrefix) {
			return fmt.Errorf("bundle %q: write the name without %q; task lists refer to it as %q", name, BundlePrefix, BundlePrefix+strings.TrimPrefix(name, BundlePrefix))
		}
		if _, err := c.ExpandTaskRefs(BundlePrefix + name); err != nil {
			return err
		}
	}

	for _, name := range slices.Sorted(maps.Keys(c.Groups)) {
		if _, err := c.ExpandTaskRefs(c.Groups[name].Tasks...); err != nil {
			return fmt.Errorf("server group %q: %w", name, err)
		}
	}

	return nil
}

// ExpandTaskRefs turns a task list into task names: "@<bundle>" entries are replaced by
// the bundle's tasks, and repeated names are dropped, keeping the first. Every name must
// be a task in the library.
func (c *Config) ExpandTaskRefs(refs ...string) ([]string, error) {
	return c.expandTaskRefs(true, refs...)
}

// expandTaskRefs is ExpandTaskRefs. When strict is false, unknown task names listed
// directly in refs are passed through rather than reported, so ServerPipeline can report
// them for the servers that actually run; bundles are always checked.
func (c *Config) expandTaskRefs(strict bool, refs ...string) ([]string, error) {
	names := []string{}
	seen := map[string]bool{}

	var expand func(refs []string, path []string) error
	expand = func(refs []string, path []string) error {
		for _, ref := range refs {
			bundleName, isBundle := strings.CutPrefix(ref, BundlePrefix)
			if !isBundle {
				if _, ok := c.Tasks[ref]; !ok && (strict || len(path) > 0) {
					err := fmt.Errorf("unknown task %q", ref)
					if len(path) > 0 {
						err = fmt.Errorf("bundle %q lists unknown task %q", path[len(path)-1], ref)
					}
					return withSuggestion(err, ref, slices.Collect(maps.Keys(c.Tasks)), "")
				}
				if !seen[ref] {
					seen[ref] = true
					names = append(names, ref)
				}
				continue
			}

			bundle, ok := c.Bundles[bundleName]
			if !ok {
				err := fmt.Errorf("unknown bundle %q", ref)
				if len(path) > 0 {
					err = fmt.Errorf("bundle %q includes unknown bundle %q", path[len(path)-1], ref)
				}
				return withSuggestion(err, bundleName, slices.Collect(maps.Keys(c.Bundles)), BundlePrefix)
			}
			if slices.Contains(path, bundleName) {
				cycle := []string{}
				for _, name := range append(path[slices.Index(path, bundleName):], bundleName) {
					cycle = append(cycle, BundlePrefix+name)
				}
				return fmt.Errorf("cyclic bundle detected: %s", strings.Join(cycle, " -> "))
			}

			if err := expand(bundle.Tasks, append(path, bundleName)); err != nil {
				return err
			}
		}
		return nil
	}

	if err := expand(refs, nil); err != nil {
		return nil, err
	}
	return names, nil
}

// ExpandTaskPatterns turns --task style arguments into task names. Each argument is a
// task name, a glob such as "deploy-*", or a bundle reference ("@base", or a glob like
// "@web-*"). A glob must match at least one task or bundle.
func (c *Config) ExpandTaskPatterns(patterns ...string) ([]string, error) {
	refs := []string{}
	for _, pattern := range patterns {
		name, isBundle := strings.CutPrefix(pattern, BundlePrefix)
		if !isGlob(name) {
			refs = append(refs, pattern)
			continue
		}

		candidates := slices.Collect(maps.Keys(c.Tasks))
		prefix := ""
		if isBundle {
			candidates = slices.Collect(maps.Keys(c.Bundles))
			prefix = BundlePrefix
		}

		matches, err := matchGlob(name, candidates)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			kind := "tasks"
			if isBundle {
				kind = "bundles"
			}
			return nil, fmt.Errorf("no %s match %q", kind, pattern)
		}
		for _, match := range matches {
			refs = append(refs, prefix+match)
		}
	}

	return c.ExpandTaskRefs(refs...)
}

// composeServer applies the server's groups and builds its final task list. Defaults are
// layered as global, then each group in the order the server lists them, then the server
// itself, with later values winning:
//
//   - log_level and each ssh field: the last value set wins.
//   - envvars: merged by key, the last value wins. Global envvars are applied at run time.
//   - tags: the union of the groups' and the server's tags.
//   - tasks: a server's own tasks replace the global and group lists; otherwise it runs
//     the global [servers] tasks followed by each group's tasks. Then add_tasks are
//     appended, skip_tasks removed, and repeats dropped.
//
// ownTasks reports whether the server sets its own tasks key.
func (c *Config) composeServer(s *Server, globalTasks []string, ownTasks bool) error {
	groups := []ServerGroup{}
	joined := map[string]bool{}
	for _, name := range s.Groups {
		group, ok := c.Groups[name]
		if !ok {
			return withSuggestion(fmt.Errorf("unknown server group %q", name), name, slices.Collect(maps.Keys(c.Groups)), "")
		}
		if !joined[name] {
			joined[name] = true
			groups = append(groups, group)
		}
	}

	if len(groups) > 0 {
		tags := []string{}
		sshLayers := []SSH{}
		env := map[string]string{}
		for _, group := range groups {
			tags = append(tags, group.Tags...)
			sshLayers = append(sshLayers, group.SSH)
			maps.Copy(env, group.EnvVars)
		}
		maps.Copy(env, s.EnvVars)

		if s.LogLevel == "" {
			for _, group := range slices.Backward(groups) {
				if group.LogLevel != "" {
					s.LogLevel = group.LogLevel
					break
				}
			}
		}

		s.Tags = append(tags, s.Tags...)
		s.SSH = mergeSSH(append(sshLayers, s.SSH)...)
		if len(env) > 0 {
			s.EnvVars = env
		}
	}
	if len(s.Tags) > 0 {
		s.Tags = unique(s.Tags)
	}

	refs := []string{}
	if ownTasks {
		refs = append(refs, s.TaskNames...)
	} else {
		refs = append(refs, globalTasks...)
		for _, group := range groups {
			refs = append(refs, group.Tasks...)
		}
	}

	// unknown task names are left for ServerPipeline, as before groups existed, so a typo
	// in one host's list doesn't stop runs on the others
	names, err := c.expandTaskRefs(false, refs...)
	if err != nil {
		return err
	}

	added, err := c.ExpandTaskRefs(s.AddTasks...)
	if err != nil {
		return fmt.Errorf("add_tasks: %w", err)
	}

	skipped, err := c.ExpandTaskRefs(s.SkipTasks...)
	if err != nil {
		return fmt.Errorf("skip_tasks: %w", err)
	}

	names = unique(append(names, added...))
	names = slices.DeleteFunc(names, func(name string) bool { return slices.Contains(skipped, name) })
	s.TaskNames = names

	// a skipped task that another task depends on would be pulled straight back in, so
	// say so rather than run it anyway
	if len(skipped) > 0 {
		// an unresolvable list is reported by ServerPipeline instead
		pipeline, _ := resolveTasks(c.Tasks, names...)
		for _, task := range pipeline {
			for _, dep := range task.DependsOn {
				if slices.Contains(skipped, dep) {
					return fmt.Errorf("skip_tasks: %q can't be skipped because task %q depends on it", dep, task.Name)
				}
			}
		}
	}

	return nil
}

// mergeSSH fills each SSH field from the last layer that sets it.
func mergeSSH(layers ...SSH) SSH {
	merged := SSH{}
	for _, layer := range layers {
		// config is an alias of config_file; normalise it so a later layer's alias
		// still overrides an earlier layer's config_file
		if layer.ConfigFile == "" {
			layer.ConfigFile = layer.Config
		}

		if layer.User != "" {
			merged.User = layer.User
		}
		if layer.Password != "" {
			merged.Password = layer.Password
		}
		if layer.Port != 0 {
			merged.Port = layer.Port
		}
		if layer.Identityfile != "" {
			merged.Identityfile = layer.Identityfile
		}
		if layer.ConfigFile != "" {
			merged.ConfigFile = layer.ConfigFile
		}
	}
	return merged
}

// ServerFilter selects servers. Every set field must match:
//
//   - Names: the server name matches any of these names or globs.
//   - Tags: the server has every one of these tags.
//   - Groups: the server is in every one of these groups.
//   - Exclude: the server name matches none of these names or globs.
type ServerFilter struct {
	Names   []string
	Exclude []string
	Tags    []string
	Groups  []string
}

// IsZero reports whether the filter selects every server.
func (f ServerFilter) IsZero() bool {
	return len(f.Names) == 0 && len(f.Exclude) == 0 && len(f.Tags) == 0 && len(f.Groups) == 0
}

// SelectServers returns the servers matching the filter, in config order. Exact names,
// tags and groups that don't exist are errors (with a suggestion for likely typos), as
// is a name glob that matches nothing or a filter that leaves no servers.
func (c *Config) SelectServers(f ServerFilter) ([]Server, error) {
	if f.IsZero() {
		return slices.Clone(c.Servers), nil
	}

	serverNames := []string{}
	knownTags := map[string]bool{}
	for _, server := range c.Servers {
		serverNames = append(serverNames, server.Name)
		for _, tag := range server.Tags {
			knownTags[tag] = true
		}
	}
	for _, group := range c.Groups {
		for _, tag := range group.Tags {
			knownTags[tag] = true
		}
	}

	// matchNames returns the set of servers matching any of the patterns
	matchNames := func(patterns []string, requireMatch bool) (map[string]bool, error) {
		matched := map[string]bool{}
		for _, pattern := range patterns {
			if !isGlob(pattern) {
				if !slices.Contains(serverNames, pattern) {
					return nil, withSuggestion(fmt.Errorf("unknown server %q", pattern), pattern, serverNames, "")
				}
				matched[pattern] = true
				continue
			}

			matches, err := matchGlob(pattern, serverNames)
			if err != nil {
				return nil, err
			}
			if len(matches) == 0 && requireMatch {
				return nil, fmt.Errorf("no servers match %q", pattern)
			}
			for _, name := range matches {
				matched[name] = true
			}
		}
		return matched, nil
	}

	included, err := matchNames(f.Names, true)
	if err != nil {
		return nil, err
	}
	// excluding a glob that matches nothing is fine: there is simply nothing to exclude
	excluded, err := matchNames(f.Exclude, false)
	if err != nil {
		return nil, err
	}

	for _, tag := range f.Tags {
		if !knownTags[tag] {
			return nil, withSuggestion(fmt.Errorf("unknown tag %q", tag), tag, slices.Collect(maps.Keys(knownTags)), "")
		}
	}
	for _, group := range f.Groups {
		if _, ok := c.Groups[group]; !ok {
			return nil, withSuggestion(fmt.Errorf("unknown server group %q", group), group, slices.Collect(maps.Keys(c.Groups)), "")
		}
	}

	selected := []Server{}
	for _, server := range c.Servers {
		switch {
		case len(f.Names) > 0 && !included[server.Name]:
		case excluded[server.Name]:
		case !containsAll(server.Tags, f.Tags):
		case !containsAll(server.Groups, f.Groups):
		default:
			selected = append(selected, server)
		}
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no servers match the selection")
	}
	return selected, nil
}

// isGlob reports whether s uses glob syntax, as understood by path.Match.
func isGlob(s string) bool {
	return strings.ContainsAny(s, `*?[\`)
}

// matchGlob returns the candidates matching the glob pattern, sorted.
func matchGlob(pattern string, candidates []string) ([]string, error) {
	matches := []string{}
	for _, candidate := range slices.Sorted(slices.Values(candidates)) {
		ok, err := path.Match(pattern, candidate)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", pattern, err)
		}
		if ok {
			matches = append(matches, candidate)
		}
	}
	return matches, nil
}

// withSuggestion adds a "did you mean" hint to err when one of the candidates is a likely
// typo of name. prefix is prepended to the suggestion, e.g. "@" for bundles.
func withSuggestion(err error, name string, candidates []string, prefix string) error {
	if suggestion := closestName(name, candidates); suggestion != "" {
		return fmt.Errorf("%w (did you mean %q?)", err, prefix+suggestion)
	}
	return err
}

// unique returns list without repeats, keeping the first occurrence of each.
func unique(list []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(list))
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// containsAll reports whether list contains every one of want.
func containsAll(list, want []string) bool {
	for _, w := range want {
		if !slices.Contains(list, w) {
			return false
		}
	}
	return true
}
