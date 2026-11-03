package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/jenmud/gorun"
)

func pressTree(m treeModel, keys ...string) treeModel {
	for _, k := range keys {
		next, _ := m.Update(keyPress(k))
		m = next.(treeModel)
	}
	return m
}

// visible returns the rows on screen as plain text, without the cursor pointer.
func visible(m treeModel) []string {
	lines := []string{}
	for i := range m.rows {
		row := ansi.Strip(m.renderRow(i))
		if i == m.cursor {
			row = strings.TrimPrefix(row, "▶ ")
		} else {
			row = strings.TrimPrefix(row, "  ")
		}
		lines = append(lines, strings.TrimRight(row, " "))
	}
	return lines
}

func TestTreeStartsCollapsedAndExpands(t *testing.T) {
	cfg := loadList(t)
	m := newTreeModel("t", "bundles", bundleTreeNodes(&cfg))

	if diff := cmp.Diff([]string{"▸ @base — Baseline", "▸ @full"}, visible(m)); diff != "" {
		t.Fatalf("collapsed (-want +got):\n%s", diff)
	}

	m = pressTree(m, "down", "enter") // expand @full
	want := []string{
		"▸ @base — Baseline",
		"▾ @full",
		"  ├── ▸ @base — Baseline",
		"  ├──   verify",
		"  └──   → packages, timezone, verify",
	}
	if diff := cmp.Diff(want, visible(m)); diff != "" {
		t.Fatalf("expanded (-want +got):\n%s", diff)
	}

	m = pressTree(m, "right", "right") // step into @full, then expand the nested @base
	want = []string{
		"▸ @base — Baseline",
		"▾ @full",
		"  ├── ▾ @base — Baseline",
		"  │   ├──   packages — Install packages",
		"  │   └──   timezone",
		"  ├──   verify",
		"  └──   → packages, timezone, verify",
	}
	if diff := cmp.Diff(want, visible(m)); diff != "" {
		t.Fatalf("nested (-want +got):\n%s", diff)
	}

	// left on a leaf jumps to its parent, left on an expanded node collapses it
	m = pressTree(m, "down", "left")
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want the nested @base row", m.cursor)
	}
	m = pressTree(m, "left", "left", "left")
	if diff := cmp.Diff([]string{"▸ @base — Baseline", "▸ @full"}, visible(m)); diff != "" {
		t.Fatalf("collapsed again (-want +got):\n%s", diff)
	}

}

func TestTreeExpandAll(t *testing.T) {
	cfg := loadList(t)
	m := newTreeModel("t", "bundles", bundleTreeNodes(&cfg))

	// e expands everything under the cursor, nested bundles included, and nothing else
	m = pressTree(m, "down", "e")
	want := []string{
		"▸ @base — Baseline",
		"▾ @full",
		"  ├── ▾ @base — Baseline",
		"  │   ├──   packages — Install packages",
		"  │   └──   timezone",
		"  ├──   verify",
		"  └──   → packages, timezone, verify",
	}
	if diff := cmp.Diff(want, visible(m)); diff != "" {
		t.Fatalf("e (-want +got):\n%s", diff)
	}

	m = pressTree(m, "E")
	if len(m.rows) != 9 {
		t.Fatalf("E expanded to %d rows, want 9", len(m.rows))
	}

	// c collapses everything under the cursor, and nothing else
	m = pressTree(m, "end", "up", "up", "up", "up", "c") // the nested @base under @full
	want = []string{
		"▾ @base — Baseline",
		"  ├──   packages — Install packages",
		"  └──   timezone",
		"▾ @full",
		"  ├── ▸ @base — Baseline",
		"  ├──   verify",
		"  └──   → packages, timezone, verify",
	}
	if diff := cmp.Diff(want, visible(m)); diff != "" {
		t.Fatalf("c (-want +got):\n%s", diff)
	}

	// C collapses the whole tree, all the way down
	m = pressTree(m, "end", "C")
	if len(m.rows) != 2 || m.cursor != 1 {
		t.Fatalf("C left %d rows with cursor %d, want 2 rows with the cursor on @full", len(m.rows), m.cursor)
	}
	m = pressTree(m, "enter")
	if visible(m)[2] != "  ├── ▸ @base — Baseline" {
		t.Errorf("C left the nested @base expanded: %q", visible(m))
	}
}

func TestTreeNodesForEveryList(t *testing.T) {
	cfg := loadList(t)
	servers, err := cfg.ServersPipeline()
	if err != nil {
		t.Fatal(err)
	}

	tags := newTreeModel("t", "tags", tagTreeNodes(&cfg, cfg.Servers, false))
	if diff := cmp.Diff([]string{"▸ prod — 2 servers", "  staging — no servers", "▸ web — 2 servers"}, visible(tags)); diff != "" {
		t.Errorf("tags (-want +got):\n%s", diff)
	}

	srv := pressTree(newTreeModel("t", "servers", serverTreeNodes(servers)), "enter")
	if got := visible(srv); len(got) < 2 || !strings.HasPrefix(got[0], "▾ db-1 — local") || got[1] != "  ├──   groups: prod" {
		t.Errorf("servers: %q", got)
	}

	tasks := newTreeModel("t", "tasks", taskTreeNodes(&cfg, nil))
	if diff := cmp.Diff([]string{"  packages — Install packages", "  timezone", "  verify"}, visible(tasks)); diff != "" {
		t.Errorf("tasks (-want +got):\n%s", diff)
	}
}

// TestTreeLazyTaskDAG makes sure the dependency tree of the big dev-server
// recipe is only built as far as it is opened, and stays fast to browse.
func TestTreeLazyTaskDAG(t *testing.T) {
	f, err := os.Open("../recipes/04-dev-server/Gorunfile")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	var cfg gorun.Config
	if err := cfg.Load(f); err != nil {
		t.Fatal(err)
	}

	m := newTreeModel("t", "tasks", taskTreeNodes(&cfg, nil))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(treeModel)

	start := time.Now()
	// open bootstrap and keep drilling into the first child, all the way down
	for range 20 {
		m = pressTree(m, "right", "right")
	}
	m = pressTree(m, "E", "end", "pgup", "home", "pgdown")
	_ = m.View()
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("browsing the task tree took %s", d)
	}
	if lines := strings.Count(m.View().Content, "\n") + 1; lines != m.height {
		t.Errorf("view is %d lines, want the screen height %d", lines, m.height)
	}

	// e on a nested task opens its whole chain of dependents, and only that
	m = newTreeModel("t", "tasks", taskTreeNodes(&cfg, nil))
	m = pressTree(m, "right", "right") // bootstrap, then its first dependent
	start = time.Now()
	m = pressTree(m, "e")
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("e on a nested task took %s", d)
	}
	cursor := m.rows[m.cursor].node
	if cursor.label != "fetch-bash" || !cursor.expanded {
		t.Fatalf("cursor on %q (expanded %v), want an expanded fetch-bash", cursor.label, cursor.expanded)
	}
	for _, name := range []string{"install-bash", "configure-bash", "smoke-bash"} {
		found := false
		for _, row := range m.rows {
			if row.node.label == name {
				found = true
				if row.node.hasChildren() && !row.node.expanded {
					t.Errorf("%s is not expanded", name)
				}
			}
		}
		if !found {
			t.Errorf("%s is not shown under fetch-bash", name)
		}
	}
	for _, row := range m.rows {
		if row.node.label == "fetch-bat" && row.node.expanded {
			t.Error("e expanded a sibling of the cursor")
		}
	}
}

// TestPickerListTabs checks the Tasks and Bundles tabs of the server picker:
// shift+tab cycles through every tab, the trees expand in place and esc goes
// back to the servers.
func TestPickerListTabs(t *testing.T) {
	cfg := loadList(t)
	m := newServerSelectionModel("t", pipelineServers()).withLists(&cfg).withHistory(nil, errors.New("none"))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = next.(serverSelectionModel)

	if plain := ansi.Strip(m.titleLine()); !strings.Contains(plain, "Servers │ Tasks │ Bundles │ Groups │ History") {
		t.Fatalf("title line %q is missing the tabs", plain)
	}

	order := []int{}
	for range 5 {
		m = press(m, "shift+tab")
		order = append(order, m.tab)
	}
	if diff := cmp.Diff([]int{tabTasks, tabBundles, tabGroups, tabHistory, tabServers}, order); diff != "" {
		t.Fatalf("shift+tab order (-want +got):\n%s", diff)
	}

	m = press(m, "shift+tab", "shift+tab") // Bundles, which starts fully expanded
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"▾ @full", "  └──   → packages, timezone, verify", "esc/q back to servers"} {
		if !strings.Contains(view, want) {
			t.Errorf("bundles tab is missing %q:\n%s", want, view)
		}
	}
	if lines := strings.Count(view, "\n") + 1; lines != 20 {
		t.Errorf("bundles tab is %d lines, want the screen height 20", lines)
	}

	// clicking a tab label switches to it
	plain := ansi.Strip(m.titleLine())
	x := lipgloss.Width(plain[:strings.Index(plain, "Tasks")])
	next, _ = m.Update(tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft})
	m = next.(serverSelectionModel)
	if m.tab != tabTasks || !strings.Contains(ansi.Strip(m.View().Content), "packages — Install packages") {
		t.Fatalf("clicking Tasks showed tab %d:\n%s", m.tab, ansi.Strip(m.View().Content))
	}

	// the Bundles tab kept its state, and esc goes back without quitting
	m = press(m, "shift+tab")
	if !strings.Contains(ansi.Strip(m.View().Content), "▾ @full") {
		t.Error("the bundles tree was collapsed by switching tabs")
	}
	m = press(m, "esc")
	if m.tab != tabServers || m.quitting {
		t.Fatalf("esc left tab %d (quitting %v), want the servers tab", m.tab, m.quitting)
	}
}

// TestPickerBundleSelection checks that checking a bundle on the Bundles tab
// replaces every server's pipeline with the bundle's tasks until it is unchecked.
func TestPickerBundleSelection(t *testing.T) {
	cfg := loadList(t)
	cfg.SelectedTasks = []string{"verify"} // as set by --task
	m := newServerSelectionModel("t", pipelineServers()).withLists(&cfg)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(serverSelectionModel)

	names := func(s gorun.Server) []string {
		out := []string{}
		for _, task := range s.Tasks {
			out = append(out, task.Name)
		}
		return out
	}

	m = press(m, "shift+tab", "shift+tab") // Bundles; the cursor starts on @base
	if m.tab != tabBundles {
		t.Fatalf("tab = %d, want Bundles", m.tab)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "go back to Servers and select hosts") {
		t.Errorf("the bundles tab should say where to pick servers, got:\n%s", view)
	}
	m = press(m, "space")
	if !m.selectedBundles["base"] {
		t.Fatal("space should check @base")
	}
	if len(cfg.SelectedTasks) != 0 {
		t.Errorf("checking a bundle should drop the --task filter, got %v", cfg.SelectedTasks)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "[x] ▾ @base") {
		t.Errorf("the bundle should show as checked, got:\n%s", view)
	}
	for _, s := range m.servers {
		if diff := cmp.Diff([]string{"packages", "timezone"}, names(s)); diff != "" {
			t.Errorf("%s should run @base's tasks (-want +got):\n%s", s.Name, diff)
		}
	}

	// pick a server and run; the bundle's tasks are what gets chosen
	m = press(m, "ctrl+s", "space")
	if diff := cmp.Diff(map[string][]string{"web-1": {"packages", "timezone"}}, chosenTasks(m)); diff != "" {
		t.Errorf("chosen mismatch (-want +got):\n%s", diff)
	}
	if view := ansi.Strip(m.viewContent()); !strings.Contains(view, "· @base") {
		t.Errorf("the server count line should name the checked bundle, got:\n%s", view)
	}

	// unchecking puts every server's own pipeline back
	m = press(m, "ctrl+b", "space")
	if m.selectedBundles["base"] {
		t.Fatal("space should uncheck @base")
	}
	if diff := cmp.Diff([]string{"greet", "timezone", "deploy-user"}, m.servers[0].TaskNames); diff != "" {
		t.Errorf("web-1 should have its own pipeline back (-want +got):\n%s", diff)
	}
}
