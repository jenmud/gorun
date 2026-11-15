package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/jenmud/gorun"
)

// filterTree is a tree like the Tasks tab's: each task with its description, and bundles that
// list tasks as children.
func filterTree() treeModel {
	base := &treeNode{label: "@base", detail: "What every server gets", children: []*treeNode{
		{label: "hello", detail: "Say hello"},
		{label: "show-env", detail: "Print the environment"},
	}}
	deploy := &treeNode{label: "@deploy", detail: "Ship the release", children: []*treeNode{
		{label: "copy-files", detail: "Upload the release"},
	}}
	return newTreeModel("test", "bundles", []*treeNode{base, deploy})
}

func typeText(t *testing.T, m treeModel, text string) treeModel {
	t.Helper()
	for _, r := range text {
		next, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(treeModel)
	}
	return m
}

func labels(m treeModel) []string {
	var out []string
	for _, r := range m.rows {
		out = append(out, r.node.label)
	}
	return out
}

// Typing after / filters the tree to the matches and their ancestors; enter keeps the filter, and
// esc clears it.
func TestTreeFilterShowsMatchesAndTheirParents(t *testing.T) {
	m := filterTree()
	next, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = next.(treeModel)
	if !m.filtering {
		t.Fatal("/ should start the filter")
	}
	m = typeText(t, m, "show")
	if got := labels(m); strings.Join(got, ",") != "@base,show-env" {
		t.Fatalf("rows for show = %v", got)
	}
	if shown, total := m.matchCount(); shown != 1 || total != 2 {
		t.Errorf("matchCount = %d of %d, want 1 of 2", shown, total)
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(treeModel)
	if m.filtering || m.query != "show" {
		t.Fatalf("enter should keep the query %q and stop typing: filtering=%v query=%q", "show", m.filtering, m.query)
	}
	if got := labels(m); strings.Join(got, ",") != "@base,show-env" {
		t.Errorf("rows after enter = %v", got)
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(treeModel)
	if m.query != "" || m.keep != nil || len(m.rows) != 2 {
		t.Errorf("esc should clear the filter: query=%q rows=%v", m.query, labels(m))
	}
}

// The filter matches a node's description, not just its name, ignores case, and backspace
// shortens the query.
func TestTreeFilterMatchesDescriptionsAndBackspace(t *testing.T) {
	m := filterTree()
	next, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = typeText(t, next.(treeModel), "RELEASE")
	if got := labels(m); strings.Join(got, ",") != "@deploy,copy-files" {
		t.Fatalf("rows for RELEASE = %v", got)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = next.(treeModel)
	if m.query != "RELEAS" || !m.filtering {
		t.Errorf("backspace: query=%q filtering=%v", m.query, m.filtering)
	}
}

// With no match, the tree is empty and the filter line says so.
func TestTreeFilterWithNoMatch(t *testing.T) {
	m := typeText(t, func() treeModel {
		n, _ := filterTree().Update(tea.KeyPressMsg{Code: '/', Text: "/"})
		return n.(treeModel)
	}(), "zzz")
	if len(m.rows) != 0 {
		t.Errorf("rows = %v, want none", labels(m))
	}
	if line := m.filterLine(); !strings.Contains(line, "0 of 2") {
		t.Errorf("filter line = %q", line)
	}
}

// While the query is being typed, q and the other keys that move around the tree are text.
func TestTreeFilterTakesLettersAsText(t *testing.T) {
	m := typeText(t, func() treeModel {
		n, _ := filterTree().Update(tea.KeyPressMsg{Code: '/', Text: "/"})
		return n.(treeModel)
	}(), "q")
	if m.query != "q" || !m.filtering {
		t.Fatalf("q should be typed into the filter: query=%q filtering=%v", m.query, m.filtering)
	}
}

// In the picker, the Bundles tab's query takes the keys first: esc clears it and stays on the tab,
// and a second esc leaves the tab as before.
func TestPickerEscClearsTheTasksFilterFirst(t *testing.T) {
	cfg := gorun.Config{
		Tasks:   map[string]gorun.Task{"hello": {Name: "hello"}, "show-env": {Name: "show-env"}},
		Bundles: map[string]gorun.Bundle{"base": {Tasks: []string{"hello", "show-env"}}},
	}
	m := newServerSelectionModel("test", nil).withLists(&cfg)
	m.tab = tabTasks
	next, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = next.(serverSelectionModel)
	next, _ = m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	m = next.(serverSelectionModel)
	if m.tab != tabTasks || !m.tasks.filtering || m.tasks.query != "h" {
		t.Fatalf("typing a filter should stay on Tasks: tab=%d query=%q", m.tab, m.tasks.query)
	}
	// q is text while typing, so it doesn't leave the tab
	next, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(serverSelectionModel)
	if m.tab != tabTasks || m.tasks.query != "hq" {
		t.Fatalf("q while typing: tab=%d query=%q", m.tab, m.tasks.query)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(serverSelectionModel)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(serverSelectionModel)
	if m.tab != tabTasks || m.tasks.query != "" {
		t.Errorf("esc with a kept filter: tab=%d query=%q, want the filter cleared on Tasks", m.tab, m.tasks.query)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(serverSelectionModel)
	if m.tab != tabServers {
		t.Errorf("a second esc should go back to the servers, tab=%d", m.tab)
	}
}

// A query finds a task by its command and its copies, not only its name.
func TestTreeFilterSearchesTaskCommands(t *testing.T) {
	deploy := &treeNode{label: "deploy", task: &taskDetail{command: "apt-get install -y nginx", copies: []string{"site.conf → /etc/nginx/site.conf"}}}
	tree := newTreeModel("test", "tasks", []*treeNode{{label: "hello", detail: "Say hello"}, deploy})
	m := typeText(t, func() treeModel { n, _ := tree.Update(tea.KeyPressMsg{Code: '/', Text: "/"}); return n.(treeModel) }(), "nginx")
	if got := labels(m); strings.Join(got, ",") != "deploy" {
		t.Errorf("rows for nginx = %v, want deploy", got)
	}
	m.setQuery("site.conf")
	if got := labels(m); strings.Join(got, ",") != "deploy" {
		t.Errorf("rows for a copy = %v, want deploy", got)
	}
}
