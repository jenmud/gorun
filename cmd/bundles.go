package main

import (
	"errors"
	"strings"

	"github.com/jenmud/gorun"
)

// selectBundles expands bundle names or globs and replaces each server's task list.
func selectBundles(cfg *gorun.Config, patterns ...string) ([]string, error) {
	refs := make([]string, len(patterns))
	for i, pattern := range patterns {
		if !strings.HasPrefix(pattern, gorun.BundlePrefix) {
			pattern = gorun.BundlePrefix + pattern
		}
		refs[i] = pattern
	}

	tasks, err := cfg.ExpandTaskPatterns(refs...)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, errors.New("selected bundles expand to no tasks")
	}

	for i := range cfg.Servers {
		cfg.Servers[i].TaskNames = append([]string(nil), tasks...)
	}
	cfg.SelectedTasks = nil
	return tasks, nil
}
