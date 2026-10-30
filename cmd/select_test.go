package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/store"
)

// pipelineServers returns servers with resolved pipelines, as main passes them in:
// packages <- timezone, packages <- docker <- deploy-user, and a standalone greet.
func pipelineServers() []gorun.Server {
	tasks := []gorun.Task{
		{Name: "greet"},
		{Name: "packages"},
		{Name: "timezone", DependsOn: []string{"packages"}},
		{Name: "docker", DependsOn: []string{"packages"}},
		{Name: "deploy-user", DependsOn: []string{"docker"}, Description: "Create the deploy user"},
	}
	names := []string{"greet", "timezone", "deploy-user"}
	return []gorun.Server{
		{Name: "web-1", TaskNames: names, Tasks: tasks},
		{Name: "web-2", TaskNames: names, Tasks: tasks},
	}
}

func press(m serverSelectionModel, keys ...string) serverSelectionModel {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight}
		case "left":
			msg = tea.KeyPressMsg{Code: tea.KeyLeft}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "ctrl+a":
			msg = tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
		}
		next, _ := m.Update(msg)
		m = next.(serverSelectionModel)
	}
	return m
}

func chosenTasks(m serverSelectionModel) map[string][]string {
	got := map[string][]string{}
	for _, s := range m.chosen() {
		got[s.Name] = s.TaskNames
	}
	return got
}

func TestServerSelectionModel_ExpandTasks(t *testing.T) {
	m := newServerSelectionModel("test", pipelineServers())

	m = press(m, "right")
	if len(m.rows) != 2+5 {
		t.Fatalf("expanding web-1 should list its 5 tasks, got %d rows", len(m.rows))
	}
	view := m.viewContent()
	for _, want := range []string{"▾", "deploy-user", "Create the deploy user", "(needs docker)"} {
		if !strings.Contains(view, want) {
			t.Errorf("view should contain %q, got:\n%s", want, view)
		}
	}

	// left on a task collapses the server and moves the cursor back to it
	m = press(m, "down", "down", "left")
	if len(m.rows) != 2 || m.cursor != 0 {
		t.Fatalf("expected collapsed list with the cursor on web-1, got %d rows, cursor %d", len(m.rows), m.cursor)
	}

	// right on a server without tasks does nothing
	empty := press(newServerSelectionModel("test", []gorun.Server{{Name: "bare"}}), "right")
	if len(empty.rows) != 1 {
		t.Errorf("a server without tasks can't expand, got %d rows", len(empty.rows))
	}
}

func TestServerSelectionModel_SelectTasks(t *testing.T) {
	m := newServerSelectionModel("test", pipelineServers())
	m = press(m, "right") // rows: web-1, greet, packages, timezone, docker, deploy-user, web-2

	// leaving out docker also leaves out deploy-user, which needs it, and selects web-1
	m = press(m, "down", "down", "down", "down", "space")
	if diff := cmp.Diff(map[string][]string{"web-1": {"greet", "packages", "timezone"}}, chosenTasks(m)); diff != "" {
		t.Errorf("chosen mismatch (-want +got):\n%s", diff)
	}
	if !strings.Contains(m.viewContent(), "3/5 tasks") {
		t.Errorf("the server row should show how many tasks are selected, got:\n%s", m.viewContent())
	}

	// adding deploy-user back also adds docker
	m = press(m, "down", "space")
	if diff := cmp.Diff(map[string][]string{"web-1": {"greet", "timezone", "deploy-user"}}, chosenTasks(m)); diff != "" {
		t.Errorf("all tasks back: the original task list should be used (-want +got):\n%s", diff)
	}

	// leaving out packages leaves out everything but greet
	m = press(m, "up", "up", "up", "space")
	if diff := cmp.Diff(map[string][]string{"web-1": {"greet"}}, chosenTasks(m)); diff != "" {
		t.Errorf("chosen mismatch (-want +got):\n%s", diff)
	}

	// leaving out the last task deselects the server, and nothing can run
	m = press(m, "up", "space")
	if m.selected["web-1"] || len(m.chosen()) != 0 {
		t.Errorf("with no tasks left web-1 should be deselected and nothing chosen, got %v", chosenTasks(m))
	}

	// with web-1 deselected and nothing else selected, the cursor on it doesn't run it
	if got := chosenTasks(m); len(got) != 0 {
		t.Errorf("nothing is selected, so nothing should run, got %v", got)
	}

	// selecting the server again brings all its tasks back
	m = press(m, "up", "space")
	if diff := cmp.Diff(map[string][]string{"web-1": {"greet", "timezone", "deploy-user"}}, chosenTasks(m)); diff != "" {
		t.Errorf("chosen mismatch (-want +got):\n%s", diff)
	}

	// task choices are per server
	m = press(m, "ctrl+a")
	if diff := cmp.Diff(map[string][]string{
		"web-1": {"greet", "timezone", "deploy-user"},
		"web-2": {"greet", "timezone", "deploy-user"},
	}, chosenTasks(m)); diff != "" {
		t.Errorf("chosen mismatch (-want +got):\n%s", diff)
	}
}

func TestServerSelectionModel_ClickToggle(t *testing.T) {
	m := newServerSelectionModel("test", pipelineServers())
	l := m.layout()

	next, _ := m.Update(tea.MouseClickMsg{X: toggleColumn, Y: l.listTop, Button: tea.MouseLeft})
	m = next.(serverSelectionModel)
	if !m.expanded["web-1"] || m.selected["web-1"] {
		t.Fatalf("clicking ▸ should expand web-1 without selecting it, expanded=%v selected=%v", m.expanded, m.selected)
	}

	// clicking a task row toggles the task
	next, _ = m.Update(tea.MouseClickMsg{X: 10, Y: l.listTop + 1, Button: tea.MouseLeft})
	m = next.(serverSelectionModel)
	if m.taskSelected("web-1", "greet") || !m.selected["web-1"] {
		t.Errorf("clicking greet should leave it out and select web-1")
	}
}

// sectionModel returns a picker over a config with groups and tags.
func sectionModel(t *testing.T) (serverSelectionModel, *gorun.Config) {
	t.Helper()
	cfg := &gorun.Config{}
	err := cfg.Load(strings.NewReader(`
[tasks.packages]
cmd = "true"
[tasks.timezone]
cmd = "true"
depends_on = ["packages"]
[tasks.deploy]
cmd = "true"
[tasks.verify]
cmd = "true"

[server_groups.prod]
tags = ["prod"]

[servers]
tasks = ["timezone", "verify"]

[servers.hosts.web-1]
groups = ["prod"]
tags = ["web"]
[servers.hosts.web-2]
tags = ["web"]
[servers.hosts.db-1]
groups = ["prod"]
`))
	if err != nil {
		t.Fatal(err)
	}
	pipelines, err := cfg.ServersPipeline()
	if err != nil {
		t.Fatal(err)
	}
	servers := []gorun.Server{}
	for _, s := range cfg.Servers {
		servers = append(servers, pipelines[s.Name])
	}
	return newServerSelectionModel("test", servers), cfg
}

// cursorTo moves the cursor to the first row of kind named name.
func cursorTo(t *testing.T, m serverSelectionModel, kind int, name string) serverSelectionModel {
	t.Helper()
	for i, row := range m.rows {
		if row.kind != kind {
			continue
		}
		if s, ok := m.serverOfRow(row); (ok && s.Name == name) || row.item.name == name {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("no row %q of kind %d", name, kind)
	return m
}

func TestServerSelectionModel_Sections(t *testing.T) {
	m, _ := sectionModel(t)

	// rows: Groups, prod, Tags, prod, web, Servers, web-1, web-2, db-1
	got := []string{}
	for _, row := range m.rows {
		switch row.kind {
		case rowHeader:
			got = append(got, "="+row.item.name)
		case rowServer:
			s, _ := m.serverOfRow(row)
			got = append(got, s.Name)
		default:
			got = append(got, row.item.name)
		}
	}
	want := []string{"=Groups", "prod", "=Tags", "prod", "web", "=Servers", "web-1", "web-2", "db-1"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("rows mismatch (-want +got):\n%s", diff)
	}

	// the cursor starts on the first selectable row and skips titles
	if m.rows[m.cursor].kind != rowGroup {
		t.Errorf("cursor should start on the prod group, got row %+v", m.rows[m.cursor])
	}
	m = press(m, "down")
	if row := m.rows[m.cursor]; row.kind != rowTag || row.item.name != "prod" {
		t.Errorf("down should skip the Tags title, got row %+v", row)
	}
	m = press(m, "up", "up")
	if m.rows[m.cursor].kind != rowGroup {
		t.Errorf("up at the top should stay on the first group, got row %+v", m.rows[m.cursor])
	}

	// typing filters every section; titles of empty sections go away
	m.query = "web"
	m.filter()
	got = got[:0]
	for _, row := range m.rows {
		if row.kind == rowHeader {
			got = append(got, "="+row.item.name)
		}
	}
	if diff := cmp.Diff([]string{"=Tags", "=Servers"}, got); diff != "" {
		t.Errorf("titles mismatch (-want +got):\n%s", diff)
	}
}

func TestServerSelectionModel_GroupsAndTags(t *testing.T) {
	m, _ := sectionModel(t)

	// the cursor on a group doesn't choose it: nothing is selected, so nothing runs
	m = cursorTo(t, m, rowGroup, "prod")
	if got := chosenTasks(m); len(got) != 0 {
		t.Errorf("nothing is selected, so nothing should run, got %v", got)
	}
	if !strings.Contains(m.runButton(), "select servers to run") {
		t.Errorf("run button should ask for a selection, got %q", m.runButton())
	}

	// selecting the web tag selects both web servers
	m = cursorTo(t, m, rowTag, "web")
	m = press(m, "space")
	if !m.selected["web-1"] || !m.selected["web-2"] || m.selected["db-1"] {
		t.Fatalf("expected web-1 and web-2 selected, got %v", m.selected)
	}

	// the prod group is now partly selected; selecting it adds db-1
	m = cursorTo(t, m, rowGroup, "prod")
	if !strings.Contains(m.viewContent(), "[-]") || !strings.Contains(m.viewContent(), "1/2 servers") {
		t.Errorf("prod should show as partly selected, got:\n%s", m.viewContent())
	}
	m = press(m, "space")
	if !m.selected["db-1"] {
		t.Fatalf("selecting prod should select db-1, got %v", m.selected)
	}

	// with all its servers selected, toggling the group deselects them
	m = press(m, "space")
	if m.selected["web-1"] || m.selected["db-1"] || !m.selected["web-2"] {
		t.Errorf("deselecting prod should leave only web-2, got %v", m.selected)
	}
}

func TestServerSelectionModel_SectionHints(t *testing.T) {
	m, _ := sectionModel(t)
	view := m.viewContent()
	for title, hint := range sectionHints {
		if !strings.Contains(view, title) || !strings.Contains(view, hint) {
			t.Errorf("section %q should show its hint %q, got:\n%s", title, hint, view)
		}
	}
}

func TestServerSelectionModel_CursorKeepsItsRow(t *testing.T) {
	m, _ := sectionModel(t)

	// expanding web-1 adds rows above web-2; the cursor must stay on web-2
	m = cursorTo(t, m, rowServer, "web-2")
	m.expanded["web-1"] = true
	m.buildRows()
	if s, ok := m.current(); !ok || s.Name != "web-2" {
		t.Errorf("cursor should stay on web-2, got %+v", m.rows[m.cursor])
	}

	// collapsing web-1 while on one of its tasks moves the cursor to web-1
	m = cursorTo(t, m, rowTask, "web-1")
	m.expanded["web-1"] = false
	m.buildRows()
	if row := m.rows[m.cursor]; row.kind != rowServer {
		t.Errorf("cursor should move to web-1, got %+v", row)
	} else if s, _ := m.serverOfRow(row); s.Name != "web-1" {
		t.Errorf("cursor should move to web-1, got %s", s.Name)
	}
}

func TestServerSelectionModel_SearchBoxHeight(t *testing.T) {
	m, _ := sectionModel(t)
	for _, width := range []int{20, 40, 58, 60, 62, 80, 200} {
		m.width = width
		for _, query := range []string{"", strings.Repeat("x", 100)} {
			m.query = query
			if h := lipgloss.Height(m.searchBox()); h != 3 {
				t.Errorf("width %d, query %q: search box is %d lines, layout() expects 3", width, query, h)
			}
		}
	}
}

func TestServerSelectionModel_AllTasks(t *testing.T) {
	m := newServerSelectionModel("test", pipelineServers())
	ctrl := func(m serverSelectionModel, r rune) serverSelectionModel {
		next, _ := m.Update(tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl})
		return next.(serverSelectionModel)
	}

	// select both servers, leave a task out on web-2
	m = press(m, "ctrl+a")
	m.toggleTask(m.servers[1], "greet")

	// ctrl+x on one of web-1's task rows clears only web-1's tasks
	m = press(m, "right", "down", "down")
	m = ctrl(m, 'x')
	if m.countTasks(m.servers[0]) != 0 || m.selected["web-1"] {
		t.Errorf("web-1 should have no tasks and be deselected, got %d tasks, selected=%v", m.countTasks(m.servers[0]), m.selected["web-1"])
	}
	if diff := cmp.Diff(map[string][]string{"web-2": {"packages", "timezone", "docker", "deploy-user"}}, chosenTasks(m)); diff != "" {
		t.Errorf("web-2 should be untouched (-want +got):\n%s", diff)
	}
	if !strings.Contains(m.viewContent(), "0/5 tasks") {
		t.Errorf("web-1 should show 0/5 tasks, got:\n%s", m.viewContent())
	}

	// ctrl+t ticks them all again and selects web-1
	m = ctrl(m, 't')
	if m.countTasks(m.servers[0]) != 5 || !m.selected["web-1"] {
		t.Errorf("web-1 should have all 5 tasks and be selected, got %d tasks, selected=%v", m.countTasks(m.servers[0]), m.selected["web-1"])
	}

	// on web-2's row, ctrl+t brings back the task left out there
	m = cursorTo(t, m, rowServer, "web-2")
	m = ctrl(m, 't')
	if diff := cmp.Diff(map[string][]string{
		"web-1": {"greet", "timezone", "deploy-user"},
		"web-2": {"greet", "timezone", "deploy-user"},
	}, chosenTasks(m)); diff != "" {
		t.Errorf("chosen mismatch (-want +got):\n%s", diff)
	}
}

// historyPicker returns a picker with a History tab over a store holding one run.
func historyPicker(t *testing.T) serverSelectionModel {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	recordEvent(context.Background(), st, gorun.ProcessStartedEvent{
		Type:      "process_started",
		GorunUUID: "run-1",
		Title:     "nightly deploy",
		Servers:   []string{"web-1"},
		StartedAt: time.Now(),
	})

	m := newServerSelectionModel("test", pipelineServers()).withHistory(st, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return next.(serverSelectionModel)
}

// send delivers msg and runs any command it returns, feeding the result back in, the
// way bubbletea would (one level deep is enough for loading the history).
func send(m serverSelectionModel, msg tea.Msg) (serverSelectionModel, tea.Cmd) {
	next, cmd := m.Update(msg)
	m = next.(serverSelectionModel)
	if cmd == nil {
		return m, nil
	}
	if result := cmd(); result != nil {
		if _, quit := result.(tea.QuitMsg); quit {
			return m, cmd
		}
		next, _ = m.Update(result)
		m = next.(serverSelectionModel)
	}
	return m, cmd
}

func TestServerSelectionModel_HistoryTab(t *testing.T) {
	m := historyPicker(t)
	m = press(m, "space") // select web-1, which must survive the tab switches

	if view := ansi.Strip(m.viewContent()); !strings.Contains(view, "Servers") || !strings.Contains(view, "History") {
		t.Fatalf("the title line should show the tabs, got:\n%s", view)
	}

	// shift+tab shows the history, loading the runs
	m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.tab != tabHistory {
		t.Fatal("shift+tab should show the History tab")
	}
	view := ansi.Strip(m.historyContent())
	if !strings.Contains(view, "nightly deploy") {
		t.Errorf("the History tab should list the run, got:\n%s", view)
	}
	if strings.Contains(view, "gorun history") {
		t.Errorf("the history's own title should be replaced by the tabs, got:\n%s", view)
	}

	// esc on the runs list goes back to the servers rather than quitting
	m, cmd := send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.tab != tabServers || m.quitting || cmd != nil {
		t.Fatalf("esc should go back to the servers, got tab=%d quitting=%v", m.tab, m.quitting)
	}
	if !m.selected["web-1"] {
		t.Error("the selection should survive switching tabs")
	}

	// q on the runs list does the same
	m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m, _ = send(m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if m.tab != tabServers || m.quitting {
		t.Fatalf("q should go back to the servers, got tab=%d quitting=%v", m.tab, m.quitting)
	}
	if m.query != "" {
		t.Errorf("q in the history must not reach the server filter, got query %q", m.query)
	}

	// enter opens a run's details; esc there goes back to the runs list, not the servers
	m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.tab != tabHistory || m.history.state != historyDetail || m.confirmed {
		t.Fatalf("enter should open the run in the history, got tab=%d state=%d confirmed=%v", m.tab, m.history.state, m.confirmed)
	}
	m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.tab != tabHistory || m.history.state != historyList {
		t.Fatalf("esc in a run's details should go back to the runs, got tab=%d state=%d", m.tab, m.history.state)
	}

	// clicking the Servers tab label switches back
	x := lipgloss.Width(titleStyle.Render("gorun") + " " + mutedStyle.Render("test") + "   ")
	m, _ = send(m, tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft})
	if m.tab != tabServers {
		t.Fatal("clicking the Servers tab should show the servers")
	}
	m, _ = send(m, tea.MouseClickMsg{X: x + lipgloss.Width("Servers │ "), Y: 0, Button: tea.MouseLeft})
	if m.tab != tabHistory {
		t.Fatal("clicking the History tab should show the history")
	}

	// ctrl+c quits from the history
	m, cmd = send(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.quitting || cmd == nil || m.confirmed {
		t.Error("ctrl+c should quit without running anything")
	}
}

func TestServerSelectionModel_HistoryUnavailable(t *testing.T) {
	m := newServerSelectionModel("test", pipelineServers()).withHistory(nil, errors.New("disk full"))
	m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if view := ansi.Strip(m.historyContent()); !strings.Contains(view, "history unavailable: disk full") {
		t.Errorf("the History tab should explain the error, got:\n%s", view)
	}
	// keys don't reach the missing store
	m, _ = send(m, tea.KeyPressMsg{Code: 'd', Text: "d"})
	m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.tab != tabServers {
		t.Error("esc should still go back to the servers")
	}
}

func TestServerSelectionModel_NoHistoryTab(t *testing.T) {
	m := newServerSelectionModel("test", pipelineServers())
	if strings.Contains(ansi.Strip(m.viewContent()), "History") {
		t.Error("without history there should be no tabs")
	}
	m, _ = send(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.tab != tabServers {
		t.Error("shift+tab should do nothing without history")
	}
}

func TestHistoryListFitsTheScreen(t *testing.T) {
	m := newHistoryModel(nil)
	m.embedded = true
	for i := range 20 {
		m.runs = append(m.runs, store.RunSummary{ID: fmt.Sprintf("run-%02d-xxxxxxxx", i), Title: fmt.Sprintf("run %d", i), Finished: true, Success: true})
	}

	for _, height := range []int{12, 24, 30, 50} {
		m.height = height
		m.cursor = len(m.runs) - 1 // the newest run, where the list starts
		view := ansi.Strip(m.View().Content)
		lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
		if len(lines) > height {
			t.Errorf("height %d: the runs list is %d lines", height, len(lines))
		}
		if !strings.Contains(view, "> ") || !strings.Contains(view, "run 19") {
			t.Errorf("height %d: the selected run should be on screen, got:\n%s", height, view)
		}
		if !strings.Contains(lines[len(lines)-1], "esc/q back to servers") {
			t.Errorf("height %d: the help line should be the last line, got %q", height, lines[len(lines)-1])
		}
	}
}

func TestServerSelectionModel_LongTitleKeepsTabs(t *testing.T) {
	m := newServerSelectionModel("A rather long title for testing tab click mapping in the picker", pipelineServers()).withHistory(nil, errors.New("none"))
	m.width = 70

	line := m.titleLine()
	if w := lipgloss.Width(line); w > 70 {
		t.Errorf("the title line is %d wide, want at most 70", w)
	}
	if plain := ansi.Strip(line); !strings.Contains(plain, "Servers │ History") || !strings.Contains(plain, "…") {
		t.Errorf("the title should be shortened to keep the tabs, got %q", plain)
	}

	// clicks still find the labels where they are drawn
	plain := ansi.Strip(line)
	x := lipgloss.Width(plain[:strings.Index(plain, "History")])
	if tab, ok := m.tabAt(x, 0); !ok || tab != tabHistory {
		t.Errorf("clicking History at x=%d should find the History tab, got %d %v", x, tab, ok)
	}
}
