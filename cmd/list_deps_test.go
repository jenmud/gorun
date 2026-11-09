package main

import (
	"slices"
	"testing"

	"github.com/jenmud/gorun"
)

// A task whose dependency isn't listed starts its own branch of the tree instead of vanishing.
func TestTaskTreeKeepsTasksWhoseDependencyIsNotListed(t *testing.T) {
	cfg := &gorun.Config{Tasks: map[string]gorun.Task{
		"build":  {Name: "build"},
		"deploy": {Name: "deploy", DependsOn: []string{"build"}},
	}}
	roots, _ := taskDependents(cfg, map[string]bool{"deploy": true}) // build is filtered out
	if !slices.Contains(roots, "deploy") {
		t.Errorf("roots = %v, want deploy listed", roots)
	}
	roots, children := taskDependents(cfg, nil)
	if !slices.Equal(roots, []string{"build"}) || !slices.Equal(children["build"], []string{"deploy"}) {
		t.Errorf("unfiltered tree: roots %v, children %v", roots, children)
	}
}
