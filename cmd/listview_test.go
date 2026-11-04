package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
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

func TestTasksTreeShowsScrollableCommandAndVariables(t *testing.T) {
	lines := make([]string, 24)
	for i := range lines {
		lines[i] = fmt.Sprintf("echo build line %02d", i)
	}
	lines[0] = `echo build line ${BASE} ${SHARED} ${MISSING} \${ESCAPED} $${DOUBLE}`
	command := strings.Join(lines, "\n")
	retryDelay := 250 * time.Millisecond
	cfg := gorun.Config{
		EnvVars: map[string]string{"BASE": "global", "SHARED": "global"},
		Vars:    map[string]any{"region": "eu"},
		Tasks: map[string]gorun.Task{
			"alpha": {
				Name:        "alpha",
				Description: "Compile the application",
				Details:     "Builds all packages and generates a binary under dist/.",
				Cmd:         command,
				EnvVars:     map[string]string{"SHARED": "task"},
				Vars:        map[string]any{"port": 8080},
				Timeout:     45 * time.Second,
				Retries:     2,
				RetryDelay:  &retryDelay,
				Rerun:       gorun.RerunUnsafe,
				RetryUnsafe: true,
				Shell:       "bash -e",
				Sudo:        true,
				Local:       true,
				LogLevel:    "debug",
				Copy:        []gorun.Copy{{Src: "source", Dst: "destination"}},
			},
			"beta": {Name: "beta", Cmd: "echo beta"},
		},
	}
	m := newTreeModel("test", "tasks", taskTreeNodes(&cfg, nil))
	m.embedded = true
	m.width, m.height = 100, 12

	collapsed := strings.Join(m.taskDetailLines(80), "\n")
	for _, want := range []string{
		"Description", "Compile the application", "Details", "Builds all packages",
		"▸ Command · cmd [expand]", "▸ Execution settings [expand]", "▸ Env vars · config + task (2) [expand]",
		"▸ Config vars · server templates (1) [expand]", "▸ Task vars (1) [expand]",
	} {
		if !strings.Contains(collapsed, want) {
			t.Errorf("collapsed details are missing %q:\n%s", want, collapsed)
		}
	}
	for _, hidden := range []string{"echo build line", "BASE=global", `"region": "eu"`, `"port": 8080`, "Timeout: 45s"} {
		if strings.Contains(collapsed, hidden) {
			t.Errorf("collapsed section unexpectedly shows %q:\n%s", hidden, collapsed)
		}
	}

	m = pressTree(m, "tab")
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "▶ ▸ Command · cmd [expand]") {
		t.Fatalf("focused detail section should show a pointer and expand hint:\n%s", view)
	}
	m = pressTree(m, "enter", "down", "enter", "down", "enter", "down", "enter", "down", "enter")
	allDetails := strings.Join(m.taskDetailLines(80), "\n")
	for _, want := range []string{
		"echo build line global task ${MISSING}", `\${ESCAPED}`, "$${DOUBLE}", "┌", "┬", "SETTING", "Timeout", "45s", "Retries", "2 extra · 3 attempts", "Retry delay", "250ms",
		"Rerun", "unsafe", "Retry unsafe", "yes", "Shell", "bash -e", "Sudo", "Local only", "Log level", "debug",
		"Depends on", "File copies", "NAME", "VALUE", "BASE", "global", "SHARED", "task", "Config vars · server templates", `"region": "eu"`, "Task vars", `"port": 8080`,
	} {
		if !strings.Contains(allDetails, want) {
			t.Errorf("expanded details are missing %q:\n%s", want, allDetails)
		}
	}

	m = pressTree(m, "end")
	if !m.detailFocus || m.detailOffset == 0 {
		t.Fatalf("tab/end should focus and scroll the detail pane, focus=%v offset=%d", m.detailFocus, m.detailOffset)
	}
	for i, line := range strings.Split(m.View().Content, "\n") {
		if width := lipgloss.Width(line); width > m.width {
			t.Errorf("view line %d is %d cells wide, screen is %d: %q", i, width, m.width, ansi.Strip(line))
		}
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Task · alpha") || !strings.Contains(view, `"port": 8080`) {
		t.Errorf("scrolled details should show the task and its vars:\n%s", view)
	}

	m = pressTree(m, "tab", "down")
	if m.detailFocus || m.cursor != 1 || m.detailOffset != 0 {
		t.Errorf("returning to the tree and moving should reset detail scroll: focus=%v cursor=%d offset=%d", m.detailFocus, m.cursor, m.detailOffset)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Task · beta") {
		t.Errorf("moving the tree cursor should update the detail pane:\n%s", view)
	}
}

func TestCtrlEEditsFileBackedTaskAndQuits(t *testing.T) {
	const file = "scripts/deploy.sh"
	editorCmd := taskEditorCommand("my-editor --wait", file)
	if diff := cmp.Diff([]string{"my-editor", "--wait", file}, editorCmd.Args); diff != "" {
		t.Fatalf("editor args mismatch (-want +got):\n%s", diff)
	}
	if got := taskEditorCommand("", file).Args; !slices.Equal(got, []string{"vi", file}) {
		t.Fatalf("default editor args = %v, want [vi %s]", got, file)
	}

	cfg := gorun.Config{Tasks: map[string]gorun.Task{"deploy": {Name: "deploy", CmdFile: file}}}
	m := newServerSelectionModel("test", nil).withLists(&cfg)
	m.tab = tabTasks
	next, teaCmd := m.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m = next.(serverSelectionModel)
	if teaCmd == nil || !m.editorOpened {
		t.Fatal("ctrl+e should launch the selected task's command file editor")
	}

	next, teaCmd = m.Update(editorFinishedMsg{})
	m = next.(serverSelectionModel)
	if !m.quitting || !m.editorOpened || teaCmd == nil {
		t.Fatal("the picker should quit after the editor exits")
	}
	if _, ok := teaCmd().(tea.QuitMsg); !ok {
		t.Error("editor completion should return a quit command")
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
	servers := pipelineServers()
	for i := range servers {
		servers[i].SSH = gorun.SSH{User: "deploy"} // @base's tasks only run over SSH
	}
	m := newServerSelectionModel("t", servers).withLists(&cfg)
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

// Checking a bundle replaces every selected server's tasks, but a local server must still
// skip remote-only tasks, and an SSH server local-only ones.
func TestPickerBundleSelectionKeepsLocalAndRemoteApart(t *testing.T) {
	var cfg gorun.Config
	if err := cfg.Load(strings.NewReader(`
[tasks.docker]
cmd = "true"

[tasks.caddy]
cmd = "true"

[tasks.lint]
cmd = "true"
local = true

[bundles.validate]
tasks = ["docker", "caddy"]

[bundles.check]
tasks = ["lint"]

[servers.hosts.web-1]
hostname = "web-1.example.com"
tasks = ["@validate"]
  [servers.hosts.web-1.ssh]
  username = "deploy"

[servers.hosts.laptop]
tasks = ["@check"]
`)); err != nil {
		t.Fatal(err)
	}

	servers := []gorun.Server{}
	for _, s := range cfg.Servers {
		pipeline, err := cfg.ServerPipeline(s)
		if err != nil {
			t.Fatal(err)
		}
		servers = append(servers, pipeline)
	}
	m := newServerSelectionModel("t", servers).withLists(&cfg)

	tasksOf := func() map[string][]string {
		got := map[string][]string{}
		for _, s := range m.servers {
			names := []string{}
			for _, task := range s.Tasks {
				names = append(names, task.Name)
			}
			got[s.Name] = names
		}
		return got
	}

	m.selectedBundles = map[string]bool{"validate": true}
	m.applyBundles()
	if diff := cmp.Diff(map[string][]string{"web-1": {"docker", "caddy"}, "laptop": {}}, tasksOf()); diff != "" {
		t.Errorf("@validate should only run on the SSH server (-want +got):\n%s", diff)
	}

	m.selectedBundles = map[string]bool{"check": true}
	m.applyBundles()
	if diff := cmp.Diff(map[string][]string{"web-1": {}, "laptop": {"lint"}}, tasksOf()); diff != "" {
		t.Errorf("@check should only run on the local server (-want +got):\n%s", diff)
	}
}
