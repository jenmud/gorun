package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/store"
)

var (
	matchStyle        = lipgloss.NewStyle().Foreground(colorSkip).Bold(true)
	searchBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(0, 1)
	searchLabelStyle  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	runButtonStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colorOK).Padding(0, 2)
	runAllButtonStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colorAccent).Padding(0, 2)
	offButtonStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Background(lipgloss.Color("#374151")).Padding(0, 2)
	headerStyle       = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	activeTabStyle    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
)

// serverMatch is a server that matches the current filter.
type serverMatch struct {
	index     int   // into serverSelectionModel.servers
	score     int   // fuzzy match score, higher is better
	field     int   // what matched: matchName, matchHost or matchTag
	tag       int   // index into the server's tags, when field is matchTag
	positions []int // rune indexes of the matched characters, for highlighting
}

// the server fields the filter matches, best first
const (
	matchName = iota
	matchHost
	matchTag
)

// the kinds of row in the list
const (
	rowServer = iota
	rowDetail // a line of an expanded server's details
	rowHeader // a section title
	rowGroup
	rowTag
	rowEnv     // the env vars toggle of an expanded server
	rowEnvVar  // a line of an expanded server's env vars table
	rowVars    // the vars toggle of an expanded server
	rowVarLine // a line of an expanded server's vars table
)

// itemMatch is a group or tag that matches the current filter.
type itemMatch struct {
	name      string
	score     int
	positions []int
}

// selectionRow is one line of the list.
type selectionRow struct {
	kind  int
	match int       // rowServer, rowDetail, rowEnv, rowEnvVar: index into serverSelectionModel.matches
	item  itemMatch // rowGroup, rowTag: the item; rowHeader: the title
	label string    // rowDetail: the field name
	value string    // rowDetail: the field value; rowEnvVar: the rendered table line
}

// serverSelectionModel is the bubbletea model for picking which servers to deploy to.
// Typing filters the list with a fuzzy match on the server name (or hostname, or tags).
// The servers are on the left, where expanding one shows its details; the tasks of the
// server under the cursor (or of every server) are on the right, where they can be left
// out. Groups and tags select all their servers at once.
type serverSelectionModel struct {
	title      string
	servers    []gorun.Server // with Tasks resolved, so they can be listed
	groupNames []string
	tags       []string

	groups *treeModel

	selected   map[string]bool
	expanded   map[string]bool // server name -> details shown
	envOpen    map[string]bool // server name -> env vars table shown
	varsOpen   map[string]bool // server name -> vars table shown
	groupsOpen bool
	tagsOpen   bool
	helpOpen   bool
	skipped    map[string]map[string]bool // server name -> task names left out of the run

	query        string
	matches      []serverMatch
	groupMatches []itemMatch
	tagMatches   []itemMatch
	rows         []selectionRow
	cursor       int // index into rows
	focus        int // paneServers or paneTasks
	allTasks     bool
	taskCursor   int // index into taskRows()
	width        int
	height       int

	// the History tab, shown when history is set up with withHistory
	tab        int
	hasHistory bool
	history    historyModel
	historyErr error // why the history database couldn't be opened

	// the Tasks and Bundles tabs, shown when set up with withLists
	tasks   *treeModel
	bundles *treeModel

	// the Bundles tab's checked bundles, and what they replace the servers' own
	// pipelines with while any are checked
	cfg             *gorun.Config
	baseServers     []gorun.Server
	selectedBundles map[string]bool

	confirmed    bool
	quitting     bool
	editorOpened bool
	editorErr    error
}

// the picker's tabs
const (
	tabServers = iota
	tabTasks
	tabBundles
	tabGroups
	tabHistory
)

var tabNames = map[int]string{tabServers: "Servers", tabTasks: "Tasks", tabBundles: "Bundles", tabGroups: "Groups", tabHistory: "History"}

var errTaskCommandEditorClosed = errors.New("task command editor closed")

// withLists adds Tasks and Bundles tabs browsing cfg's tasks and bundles, like
// --list-tasks and --list-bundles do.
func (m serverSelectionModel) withLists(cfg *gorun.Config) serverSelectionModel {
	m.cfg = cfg
	m.baseServers = slices.Clone(m.servers)
	if m.selectedBundles == nil {
		m.selectedBundles = map[string]bool{}
	}
	if nodes := taskTreeNodes(cfg, nil); len(nodes) > 0 {
		expandAll(nodes)
		t := newTreeModel(m.title, "tasks", nodes)
		t.embedded = true
		m.tasks = &t
	}
	if nodes := bundleTreeNodes(cfg); len(nodes) > 0 {
		expandAll(nodes)
		t := newTreeModel(m.title, "bundles", nodes)
		t.embedded = true
		t.hint = "space checks a bundle · go back to Servers and select hosts · the tasks pane lists only that bundle's tasks"
		t.checkbox = func(n *treeNode) (bool, bool) {
			name, ok := strings.CutPrefix(n.label, gorun.BundlePrefix)
			if !ok {
				return false, false
			}
			return m.selectedBundles[name], true
		}
		m.bundles = &t
	}
	if nodes := groupTreeNodes(cfg, cfg.Servers, false); len(nodes) > 0 {
		t := newTreeModel(m.title, "groups", nodes)
		t.embedded = true
		m.groups = &t
	}
	m.resizeTabs()
	return m
}

// tabs lists the picker's tabs in the order they are shown.
func (m serverSelectionModel) tabs() []int {
	tabs := []int{tabServers}
	if m.tasks != nil {
		tabs = append(tabs, tabTasks)
	}
	if m.bundles != nil {
		tabs = append(tabs, tabBundles)
	}
	if m.groups != nil {
		tabs = append(tabs, tabGroups)
	}
	if m.hasHistory {
		tabs = append(tabs, tabHistory)
	}
	return tabs
}

// resizeTabs gives the embedded views the picker's size.
func (m *serverSelectionModel) resizeTabs() {
	m.history.width, m.history.height = m.width, m.height
	for _, t := range []*treeModel{m.tasks, m.bundles, m.groups} {
		if t != nil {
			next, _ := t.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			*t = next.(treeModel)
		}
	}
}

// withHistory adds a History tab showing the runs in st, or err when the history
// database couldn't be opened.
func (m serverSelectionModel) withHistory(st *store.Store, err error) serverSelectionModel {
	m.hasHistory = true
	m.historyErr = err
	m.history = newHistoryModel(st)
	m.history.embedded = true
	m.history.width, m.history.height = m.width, m.height
	return m
}

// switchTab shows the next tab, wrapping around to the first.
func (m serverSelectionModel) switchTab() (tea.Model, tea.Cmd) {
	tabs := m.tabs()
	i := slices.Index(tabs, m.tab)
	return m.showTab(tabs[(i+1)%len(tabs)])
}

// showTab shows tab. The history is reloaded each time it is shown, so it includes
// runs that finished since.
func (m serverSelectionModel) showTab(tab int) (tea.Model, tea.Cmd) {
	m.tab = tab
	if tab != tabHistory || m.historyErr != nil {
		return m, nil
	}
	return m, m.history.Init()
}

// treeTab returns the tree shown on the current tab, if it is a Tasks or Bundles tab.
func (m serverSelectionModel) treeTab() *treeModel {
	switch m.tab {
	case tabTasks:
		return m.tasks
	case tabBundles:
		return m.bundles
	case tabGroups:
		return m.groups
	}
	return nil
}

// updateTree handles input while the Tasks or Bundles tab is shown.
func (m serverSelectionModel) updateTree(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.helpOpen {
		if kmsg, ok := msg.(tea.KeyPressMsg); ok {
			switch kmsg.String() {
			case "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			default:
				m.helpOpen = false
				return m, nil
			}
		}
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+e" && m.tab == tabTasks {
			if cmd, ok := m.treeTab().editCurrentCommandFile(); ok {
				m.editorOpened = true
				return m, cmd
			}
		}
		if m.tab == tabBundles && msg.String() == "space" {
			t := m.treeTab()
			if t.cursor >= 0 && t.cursor < len(t.rows) && t.rows[t.cursor].depth == 0 {
				if name, ok := strings.CutPrefix(t.rows[t.cursor].node.label, gorun.BundlePrefix); ok {
					m.selectedBundles[name] = !m.selectedBundles[name]
					m.applyBundles()
					return m, nil
				}
			}
		}
		switch msg.String() {
		case "ctrl+s":
			return m.showTab(tabServers)
		case "ctrl+t":
			if slices.Contains(m.tabs(), tabTasks) {
				return m.showTab(tabTasks)
			}
		case "ctrl+b":
			if slices.Contains(m.tabs(), tabBundles) {
				return m.showTab(tabBundles)
			}
		case "ctrl+g":
			if slices.Contains(m.tabs(), tabGroups) {
				return m.showTab(tabGroups)
			}
		case "ctrl+h":
			if slices.Contains(m.tabs(), tabHistory) {
				return m.showTab(tabHistory)
			}
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "ctrl+k":
			m.helpOpen = !m.helpOpen
			return m, nil
		case "shift+tab":
			return m.switchTab()
		case "esc", "q":
			return m.showTab(tabServers)
		}
	case tea.MouseClickMsg:
		if tab, ok := m.tabAt(msg.X, msg.Y); ok && tab != m.tab {
			return m.showTab(tab)
		}
		return m, nil
	}

	// the tree is shared by pointer, so copy it before updating to keep models immutable
	t := *m.treeTab()
	next, _ := t.Update(msg)
	t = next.(treeModel)
	if m.tab == tabTasks {
		m.tasks = &t
	} else if m.tab == tabBundles {
		m.bundles = &t
	} else if m.tab == tabGroups {
		m.groups = &t
	}
	return m, nil
}

// applyBundles replaces each server's pipeline with the checked bundles' tasks, or restores
// the server's own pipeline when none are checked.
func (m *serverSelectionModel) applyBundles() {
	refs := []string{}
	for _, name := range slices.Sorted(maps.Keys(m.selectedBundles)) {
		if m.selectedBundles[name] {
			refs = append(refs, gorun.BundlePrefix+name)
		}
	}
	if m.cfg == nil || len(refs) == 0 {
		m.servers = slices.Clone(m.baseServers)
		m.skipped = map[string]map[string]bool{}
		m.followServer()
		return
	}

	tasks, err := m.cfg.ExpandTaskRefs(refs...)
	if err != nil {
		return
	}

	// the checked bundles replace any --task filter the run started with
	m.cfg.SelectedTasks = nil

	servers := make([]gorun.Server, 0, len(m.baseServers))
	for _, base := range m.baseServers {
		server := base
		server.TaskNames = slices.Clone(tasks)
		if pipeline, err := m.cfg.ServerPipeline(server); err == nil {
			server = pipeline
		}
		servers = append(servers, server)
	}
	m.servers = servers
	m.skipped = map[string]map[string]bool{}
	m.followServer()
}

// bundleSummary names the checked bundles for the server count line.
func (m serverSelectionModel) bundleSummary() string {
	names := []string{}
	for _, name := range slices.Sorted(maps.Keys(m.selectedBundles)) {
		if m.selectedBundles[name] {
			names = append(names, gorun.BundlePrefix+name)
		}
	}
	return strings.Join(names, ", ")
}

// treeContent renders the Tasks or Bundles tab, under the picker's title line.
func (m serverSelectionModel) treeContent() string {
	lines := strings.Split(m.treeTab().View().Content, "\n")
	lines[0] = m.titleLine()
	return strings.Join(lines, "\n")
}

// updateHistory handles input while the History tab is shown.
func (m serverSelectionModel) updateHistory(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.helpOpen {
		if kmsg, ok := msg.(tea.KeyPressMsg); ok {
			switch kmsg.String() {
			case "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			default:
				m.helpOpen = false
				return m, nil
			}
		}
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.query != "" {
			key := msg.Key()
			if key.Text != "" && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
				r := []rune(key.Text)
				if len(r) == 1 && unicode.IsLetter(r[0]) {
					m.query += key.Text
					m.filter()
					return m, nil
				}
			}
		}
		switch msg.String() {
		case "ctrl+k":
			m.helpOpen = !m.helpOpen
			return m, nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "shift+tab":
			return m.switchTab()
		case "ctrl+s":
			return m.showTab(tabServers)
		case "ctrl+t":
			if slices.Contains(m.tabs(), tabTasks) {
				return m.showTab(tabTasks)
			}
		case "ctrl+b":
			if slices.Contains(m.tabs(), tabBundles) {
				return m.showTab(tabBundles)
			}
		case "ctrl+g":
			if slices.Contains(m.tabs(), tabGroups) {
				return m.showTab(tabGroups)
			}
		case "ctrl+h":
			if slices.Contains(m.tabs(), tabHistory) {
				return m.showTab(tabHistory)
			}
		case "esc", "q":
			// on the runs list these quit the history view; here they go back
			if m.history.state == historyList && !m.history.askKeep {
				return m.showTab(tabServers)
			}
		}
	case tea.MouseClickMsg:
		if tab, ok := m.tabAt(msg.X, msg.Y); ok && tab != m.tab {
			return m.showTab(tab)
		}
		return m, nil
	case tea.MouseWheelMsg:
		return m, nil
	}

	if m.historyErr != nil {
		return m, nil
	}
	next, cmd := m.history.Update(msg)
	m.history = next.(historyModel)
	return m, cmd
}

// tabsWidth is the width of the tabs part of the title line.
func (m serverSelectionModel) tabsWidth() int {
	return lipgloss.Width(m.tabsLine())
}

// tabsLine renders the tab labels, after the title.
func (m serverSelectionModel) tabsLine() string {
	labels := []string{}
	for _, tab := range m.tabs() {
		labels = append(labels, m.tabLabel(tabNames[tab], tab))
	}
	return "   " + strings.Join(labels, mutedStyle.Render(" │ ")) + mutedStyle.Render("  shift+tab")
}

// titlePrefix renders "gorun" and the title, shortening the title so the tabs still fit
// on the line.
func (m serverSelectionModel) titlePrefix() string {
	title := m.title
	if len(m.tabs()) > 1 {
		room := m.width - lipgloss.Width(titleStyle.Render("gorun")+" ") - m.tabsWidth()
		title = truncate(title, max(room, 0))
	}
	return titleStyle.Render("gorun") + " " + mutedStyle.Render(title)
}

// titleLine renders the top line: the title and, with more than one tab, the tabs.
func (m serverSelectionModel) titleLine() string {
	line := m.titlePrefix()
	if len(m.tabs()) < 2 {
		return line
	}
	return line + m.tabsLine()
}

func (m serverSelectionModel) tabLabel(name string, tab int) string {
	if name == "" {
		return ""
	}
	underlined := lipgloss.NewStyle().Underline(true).Render(string(name[0]))
	rest := name[1:]
	if m.tab == tab {
		return activeTabStyle.Render(underlined) + activeTabStyle.Render(rest)
	}
	return mutedStyle.Render(underlined) + mutedStyle.Render(rest)
}

// tabAt returns the tab whose label is at screen position x, y.
func (m serverSelectionModel) tabAt(x, y int) (int, bool) {
	if len(m.tabs()) < 2 || y != 0 {
		return 0, false
	}
	start := lipgloss.Width(m.titlePrefix() + "   ")
	for _, tab := range m.tabs() {
		w := lipgloss.Width(tabNames[tab])
		if x >= start && x < start+w {
			return tab, true
		}
		start += w + lipgloss.Width(" │ ")
	}
	return 0, false
}

// historyContent renders the History tab, under the picker's title line.
func (m serverSelectionModel) historyContent() string {
	if m.historyErr != nil {
		return m.titleLine() + "\n\n" + failStyle.Render("history unavailable: ") + m.historyErr.Error()
	}
	// the history view starts with its own title line; show the tabs there instead
	lines := strings.Split(m.history.View().Content, "\n")
	lines[0] = m.titleLine()
	return strings.Join(lines, "\n")
}

// newServerSelectionModel builds the picker for servers, which should have their Tasks
// resolved so they can be listed.
func newServerSelectionModel(title string, servers []gorun.Server) serverSelectionModel {
	m := serverSelectionModel{
		title:    title,
		servers:  servers,
		selected: map[string]bool{},
		expanded: map[string]bool{},
		envOpen:  map[string]bool{},
		varsOpen: map[string]bool{},
		skipped:  map[string]map[string]bool{},
		width:    80,
		height:   24,
	}
	for _, s := range servers {
		m.envOpen[s.Name] = true
		m.varsOpen[s.Name] = true
	}

	groups, tags := map[string]bool{}, map[string]bool{}
	for _, s := range servers {
		for _, g := range s.Groups {
			groups[g] = true
		}
		for _, t := range s.Tags {
			tags[t] = true
		}
	}
	m.groupNames = slices.Sorted(maps.Keys(groups))
	m.tags = slices.Sorted(maps.Keys(tags))

	m.filter()
	for i, row := range m.rows {
		if row.kind == rowServer {
			m.cursor = i
			break
		}
	}
	return m
}

// rowKey identifies a row across rebuilds: its kind and the server, task or item name.
type rowKey struct {
	kind         int
	server, name string
}

func (m serverSelectionModel) keyOf(row selectionRow) rowKey {
	key := rowKey{kind: row.kind, name: row.item.name}
	if s, ok := m.serverOfRow(row); ok {
		key.server = s.Name
		key.name = row.label
	}
	return key
}

// buildRows lays out the matching groups and tags, each under a title, then the
// matching servers with the details of expanded servers under them. The cursor stays on
// the same row (or a gone detail's server) when the rows around it change.
func (m *serverSelectionModel) buildRows() {
	m.rebuildRows(m.cursorKey())
}

// cursorKey returns the key of the row under the cursor, or nil if there is none.
func (m serverSelectionModel) cursorKey() *rowKey {
	row, ok := m.currentRow()
	if !ok {
		return nil
	}
	key := m.keyOf(row)
	return &key
}

// rebuildRows is buildRows, keeping the cursor on the row identified by keep.
func (m *serverSelectionModel) rebuildRows(keep *rowKey) {
	m.rows = m.rows[:0]

	section := func(title string, kind int, items []itemMatch, open bool) {
		if len(items) == 0 {
			return
		}
		m.rows = append(m.rows, selectionRow{kind: rowHeader, item: itemMatch{name: title}})
		if !open {
			return
		}
		for _, item := range items {
			m.rows = append(m.rows, selectionRow{kind: kind, item: item})
		}
	}
	section("Groups", rowGroup, m.groupMatches, m.groupsOpen)
	section("Tags", rowTag, m.tagMatches, m.tagsOpen)
	if len(m.rows) > 0 && len(m.matches) > 0 {
		m.rows = append(m.rows, selectionRow{kind: rowHeader, item: itemMatch{name: "Servers"}})
	}

	for i, match := range m.matches {
		m.rows = append(m.rows, selectionRow{kind: rowServer, match: i})
		s := m.servers[match.index]
		if m.expanded[s.Name] {
			m.rows = append(m.rows, m.detailRows(s, i)...)
		}
	}

	if keep != nil {
		m.cursor = m.findRow(*keep)
	}
	m.cursor = max(min(m.cursor, len(m.rows)-1), 0)
	m.moveCursor(0)
}

// findRow returns the index of the row with key, falling back to its server's row for a
// detail that is no longer listed, or the current cursor if neither is found.
func (m serverSelectionModel) findRow(key rowKey) int {
	serverRow := -1
	for i, row := range m.rows {
		k := m.keyOf(row)
		if k == key {
			return i
		}
		if key.server != "" && k.kind == rowServer && k.server == key.server {
			serverRow = i
		}
	}
	if serverRow >= 0 {
		return serverRow
	}
	return m.cursor
}

// moveCursor moves the cursor by delta rows, stepping over section titles and details.
// The tasks pane follows the server the cursor lands on.
func (m *serverSelectionModel) moveCursor(delta int) {
	defer m.followServer()
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}

	step := 1
	if delta < 0 {
		step = -1
	}
	from := m.cursor
	m.cursor = max(min(m.cursor+delta, len(m.rows)-1), 0)
	for !selectable(m.rows[m.cursor]) {
		next := m.cursor + step
		if next < 0 || next >= len(m.rows) {
			// nothing selectable that way: stay put, or for a fresh list look the other way
			if delta != 0 {
				m.cursor = from
				return
			}
			step = -step
			next = m.cursor + step
			if next < 0 || next >= len(m.rows) {
				return
			}
		}
		m.cursor = next
	}
}

// currentRow returns the row under the cursor.
func (m serverSelectionModel) currentRow() (selectionRow, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return selectionRow{}, false
	}
	return m.rows[m.cursor], true
}

// selectable reports whether the cursor can stop on row.
func selectable(row selectionRow) bool {
	switch row.kind {
	case rowDetail, rowEnvVar, rowVarLine:
		return false
	case rowHeader:
		return row.item.name != "Servers"
	}
	return true
}

// serverOfRow returns the server of a server row or of one of its detail rows.
func (m serverSelectionModel) serverOfRow(row selectionRow) (gorun.Server, bool) {
	if row.kind != rowServer && row.kind != rowDetail && row.kind != rowEnv && row.kind != rowEnvVar && row.kind != rowVars && row.kind != rowVarLine {
		return gorun.Server{}, false
	}
	return m.servers[m.matches[row.match].index], true
}

// members returns the servers in a group or with a tag.
func (m serverSelectionModel) members(row selectionRow) []gorun.Server {
	servers := []gorun.Server{}
	for _, s := range m.servers {
		if (row.kind == rowGroup && slices.Contains(s.Groups, row.item.name)) ||
			(row.kind == rowTag && slices.Contains(s.Tags, row.item.name)) {
			servers = append(servers, s)
		}
	}
	return servers
}

// countMembersSelected returns how many of a group's or tag's servers are selected, and
// how many servers it has.
func (m serverSelectionModel) countMembersSelected(row selectionRow) (int, int) {
	members := m.members(row)
	n := 0
	for _, s := range members {
		if m.selected[s.Name] {
			n++
		}
	}
	return n, len(members)
}

// taskSelected reports whether a server's task is part of the run.
func (m serverSelectionModel) taskSelected(server, task string) bool {
	return !m.skipped[server][task]
}

// countTasks returns how many of a server's tasks are selected.
func (m serverSelectionModel) countTasks(s gorun.Server) int {
	n := 0
	for _, t := range s.Tasks {
		if m.taskSelected(s.Name, t.Name) {
			n++
		}
	}
	return n
}

// toggleServer selects or deselects a server. Selecting a server whose tasks were all
// left out brings them all back.
func (m *serverSelectionModel) toggleServer(s gorun.Server) {
	m.selected[s.Name] = !m.selected[s.Name]
	if m.selected[s.Name] && len(s.Tasks) > 0 && m.countTasks(s) == 0 {
		delete(m.skipped, s.Name)
	}
}

// toggleMembers selects every server in a group or with a tag, or deselects them all if
// they are all selected already.
func (m *serverSelectionModel) toggleMembers(row selectionRow) {
	n, total := m.countMembersSelected(row)
	all := n == total
	for _, s := range m.members(row) {
		if m.selected[s.Name] == all {
			m.toggleServer(s)
		}
	}
}

// toggleTask adds a task to the run or leaves it out, keeping the choice consistent with
// depends_on: leaving a task out also leaves out the tasks that need it, and adding a task
// also adds what it needs. The server is selected while any of its tasks are.
func (m *serverSelectionModel) toggleTask(s gorun.Server, task string) {
	if m.skipped[s.Name] == nil {
		m.skipped[s.Name] = map[string]bool{}
	}
	skipped := m.skipped[s.Name]

	deps := map[string][]string{}
	for _, t := range s.Tasks {
		deps[t.Name] = t.DependsOn
	}

	if skipped[task] {
		// add the task and, transitively, its dependencies
		var include func(string)
		include = func(name string) {
			if !skipped[name] {
				return
			}
			delete(skipped, name)
			for _, dep := range deps[name] {
				include(dep)
			}
		}
		include(task)
		m.selected[s.Name] = true
		return
	}

	// leave the task out and, transitively, every task depending on it
	var exclude func(string)
	exclude = func(name string) {
		if skipped[name] {
			return
		}
		skipped[name] = true
		for _, t := range s.Tasks {
			if slices.Contains(t.DependsOn, name) {
				exclude(t.Name)
			}
		}
	}
	exclude(task)

	m.selected[s.Name] = m.countTasks(s) > 0
}

// setAllTasks ticks or unticks every task of a server. With all its tasks ticked the
// server is selected; with none, it is deselected, as when unticking them one by one.
func (m *serverSelectionModel) setAllTasks(s gorun.Server, on bool) {
	if on || len(s.Tasks) == 0 {
		delete(m.skipped, s.Name)
		m.selected[s.Name] = on
		return
	}

	skipped := map[string]bool{}
	for _, t := range s.Tasks {
		skipped[t.Name] = true
	}
	m.skipped[s.Name] = skipped
	m.selected[s.Name] = false
}

// toggleRow toggles whatever is under the given row.
func (m *serverSelectionModel) toggleRow(row selectionRow) {
	switch row.kind {
	case rowHeader:
		if row.item.name == "Groups" {
			m.groupsOpen = !m.groupsOpen
			m.buildRows()
		} else if row.item.name == "Tags" {
			m.tagsOpen = !m.tagsOpen
			m.buildRows()
		}
	case rowServer:
		s, _ := m.serverOfRow(row)
		m.toggleServer(s)
	case rowEnv:
		s, _ := m.serverOfRow(row)
		m.envOpen[s.Name] = !m.envOpen[s.Name]
		m.buildRows()
	case rowVars:
		s, _ := m.serverOfRow(row)
		m.varsOpen[s.Name] = !m.varsOpen[s.Name]
		m.buildRows()
	case rowGroup, rowTag:
		m.toggleMembers(row)
	}
}

// filter rebuilds the match lists from the query, best matches first.
func (m *serverSelectionModel) filter() {
	query := strings.TrimSpace(m.query)
	m.matches = m.matches[:0]

	for i, s := range m.servers {
		if score, pos, ok := fuzzyMatch(query, s.Name); ok {
			m.matches = append(m.matches, serverMatch{index: i, score: score, field: matchName, positions: pos})
		} else if score, pos, ok := fuzzyMatch(query, s.Hostname); ok {
			m.matches = append(m.matches, serverMatch{index: i, score: score, field: matchHost, positions: pos})
		} else {
			// the best matching tag
			best := serverMatch{index: i, field: matchTag, tag: -1}
			for t, tag := range s.Tags {
				if score, pos, ok := fuzzyMatch(query, tag); ok && (best.tag < 0 || score > best.score) {
					best.score, best.tag, best.positions = score, t, pos
				}
			}
			if best.tag >= 0 {
				m.matches = append(m.matches, best)
			}
		}
	}

	// name matches rank above hostname matches, then tag matches; ties keep the config order
	slices.SortStableFunc(m.matches, func(a, b serverMatch) int {
		if a.field != b.field {
			return a.field - b.field
		}
		return b.score - a.score
	})

	matchItems := func(names []string) []itemMatch {
		items := []itemMatch{}
		for _, name := range names {
			if score, pos, ok := fuzzyMatch(query, name); ok {
				items = append(items, itemMatch{name: name, score: score, positions: pos})
			}
		}
		slices.SortStableFunc(items, func(a, b itemMatch) int { return b.score - a.score })
		return items
	}
	m.groupMatches = matchItems(m.groupNames)
	m.tagMatches = matchItems(m.tags)

	// a new filter starts at the top
	m.rows = m.rows[:0]
	m.cursor = 0
	m.buildRows()
}

func (m serverSelectionModel) current() (gorun.Server, bool) {
	row, ok := m.currentRow()
	if !ok {
		return gorun.Server{}, false
	}
	return m.serverOfRow(row)
}

func (m serverSelectionModel) countSelected() int {
	n := 0
	for _, s := range m.servers {
		if m.selected[s.Name] {
			n++
		}
	}
	return n
}

// chosen returns the servers to deploy to, in config order. Only servers that were
// explicitly selected run; with nothing selected, nothing runs. A server with tasks left
// out runs only the remaining tasks.
func (m serverSelectionModel) chosen() []gorun.Server {
	servers := []gorun.Server{}
	for _, s := range m.servers {
		if m.selected[s.Name] {
			servers = append(servers, m.withChosenTasks(s))
		}
	}
	return servers
}

// withChosenTasks limits the server's task list to the selected tasks. The choices keep
// every dependency of a selected task selected, so the pipeline is exactly these tasks.
func (m serverSelectionModel) withChosenTasks(s gorun.Server) gorun.Server {
	if len(m.skipped[s.Name]) == 0 {
		return s
	}

	s.TaskNames = []string{}
	tasks := []gorun.Task{}
	for _, t := range s.Tasks {
		if m.taskSelected(s.Name, t.Name) {
			s.TaskNames = append(s.TaskNames, t.Name)
			tasks = append(tasks, t)
		}
	}
	s.Tasks = tasks
	return s
}

func (m serverSelectionModel) Init() tea.Cmd {
	return nil
}

func (m serverSelectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeTabs()
		m.buildRows() // the env vars tables fit the pane width
		return m, nil
	case historyRunsMsg, historyRunMsg:
		next, cmd := m.history.Update(msg)
		m.history = next.(historyModel)
		return m, cmd
	case editorFinishedMsg:
		m.editorErr = msg.err
		m.quitting = true
		return m, tea.Quit
	}

	if m.helpOpen {
		if kmsg, ok := msg.(tea.KeyPressMsg); ok {
			switch kmsg.String() {
			case "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			default:
				m.helpOpen = false
				return m, nil
			}
		}
		return m, nil
	}

	if m.tab == tabHistory {
		return m.updateHistory(msg)
	}
	if m.treeTab() != nil {
		return m.updateTree(msg)
	}

	switch msg := msg.(type) {

	case tea.PasteMsg:
		m.query += strings.Map(func(r rune) rune {
			if unicode.IsPrint(r) {
				return r
			}
			return -1
		}, msg.Content)
		m.filter()
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}

		if tab, ok := m.tabAt(msg.X, msg.Y); ok && tab != m.tab {
			return m.showTab(tab)
		}

		l := m.layout()
		runW := lipgloss.Width(m.runButton())
		allW := lipgloss.Width(m.runAllButton())
		if msg.Y == l.buttonY {
			if msg.X < runW {
				return m.run()
			}
			if msg.X >= runW+2 && msg.X < runW+2+allW {
				return m.runAll()
			}
		}
		if msg.Y < l.listTop || msg.Y >= l.listTop+l.rows {
			return m, nil
		}

		// clicking a task focuses the tasks pane and toggles it
		if msg.X >= l.leftWidth {
			m.focus = paneTasks
			rows := m.taskRows()
			if j := m.taskOffset(l.rows) + msg.Y - l.listTop; j < len(rows) {
				m.taskCursor = j
				m.toggleTaskRow(rows[j])
			}
			return m, nil
		}

		// clicking a server moves the cursor to it and toggles it; clicking its ▸ shows
		// or hides its details
		m.focus = paneServers
		if i := l.offset + msg.Y - l.listTop; i < min(l.offset+l.rows, len(m.rows)) {
			row := m.rows[i]
			if !selectable(row) {
				return m, nil
			}
			m.cursor = i
			m.followServer()
			if s, ok := m.serverOfRow(row); ok && row.kind == rowServer && msg.X == toggleColumn {
				m.expanded[s.Name] = !m.expanded[s.Name]
				m.buildRows()
				return m, nil
			}
			m.toggleRow(row)
		}
		return m, nil

	case tea.MouseWheelMsg:
		delta := 1
		if msg.Button == tea.MouseWheelUp {
			delta = -1
		} else if msg.Button != tea.MouseWheelDown {
			return m, nil
		}
		if msg.X >= m.layout().leftWidth {
			m.moveTaskCursor(delta)
		} else {
			m.moveCursor(delta)
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.focus == paneTasks {
			if next, ok := m.updateTasksPane(msg); ok {
				return next, nil
			}
		}
		switch msg.String() {
		case "ctrl+s":
			return m.showTab(tabServers)
		case "ctrl+t":
			if slices.Contains(m.tabs(), tabTasks) {
				return m.showTab(tabTasks)
			}
		case "ctrl+b":
			if slices.Contains(m.tabs(), tabBundles) {
				return m.showTab(tabBundles)
			}
		case "ctrl+g":
			if slices.Contains(m.tabs(), tabGroups) {
				return m.showTab(tabGroups)
			}
		case "ctrl+h":
			if slices.Contains(m.tabs(), tabHistory) {
				return m.showTab(tabHistory)
			}
		case "ctrl+k":
			m.helpOpen = !m.helpOpen
			return m, nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			// first esc clears the filter, the second quits
			if m.query != "" {
				m.query = ""
				m.filter()
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		case "enter", "ctrl+r":
			if row, ok := m.currentRow(); ok && (row.kind == rowEnv || row.kind == rowHeader && (row.item.name == "Groups" || row.item.name == "Tags")) {
				m.toggleRow(row)
				return m, nil
			}
			return m.run()
		case "shift+tab":
			if len(m.tabs()) > 1 {
				return m.switchTab()
			}
		case "tab":
			if len(m.taskRows()) > 0 {
				m.focus = paneTasks
			}
		case "alt+v":
			m.allTasks = !m.allTasks
			m.followServer()
		case "up":
			m.moveCursor(-1)
		case "down":
			m.moveCursor(1)
		case "right":
			// show the server's details, then move to its tasks; open the env vars table;
			// or expand a collapsed group/tag section
			row, ok := m.currentRow()
			if !ok {
				break
			}
			switch {
			case row.kind == rowHeader && (row.item.name == "Groups" && !m.groupsOpen || row.item.name == "Tags" && !m.tagsOpen):
				m.toggleRow(row)
			case row.kind == rowEnv:
				if s, _ := m.serverOfRow(row); !m.envOpen[s.Name] {
					m.toggleRow(row)
				}
			case row.kind == rowServer:
				if s, _ := m.serverOfRow(row); !m.expanded[s.Name] {
					m.expanded[s.Name] = true
					m.buildRows()
				} else if len(m.taskRows()) > 0 {
					m.focus = paneTasks
				}
			}
		case "left":
			// close the env vars table, hide the server's details, or collapse an expanded
			// group/tag section
			row, ok := m.currentRow()
			if !ok {
				break
			}
			switch {
			case row.kind == rowHeader && (row.item.name == "Groups" && m.groupsOpen || row.item.name == "Tags" && m.tagsOpen):
				m.toggleRow(row)
			case row.kind == rowEnv && m.envOpen[m.servers[m.matches[row.match].index].Name]:
				m.toggleRow(row)
			default:
				if s, ok := m.serverOfRow(row); ok && m.expanded[s.Name] {
					m.expanded[s.Name] = false
					m.buildRows()
				}
			}
		case "alt+e":
			if s, ok := m.current(); ok {
				m.expanded[s.Name] = true
				m.buildRows()
			}
		case "alt+E", "alt+shift+e":
			for _, s := range m.servers {
				m.expanded[s.Name] = true
			}
			m.buildRows()
		case "alt+c":
			if s, ok := m.current(); ok && m.expanded[s.Name] {
				m.expanded[s.Name] = false
				m.buildRows()
			}
		case "alt+C", "alt+shift+c":
			for _, s := range m.servers {
				m.expanded[s.Name] = false
			}
			m.buildRows()
		case "space":
			if row, ok := m.currentRow(); ok {
				m.toggleRow(row)
			}
		case "alt+a", "alt+A", "alt+shift+a":
			return m.runAll()
		case "ctrl+a":
			// select every visible server, or deselect them if they are all selected already
			all := true
			for _, match := range m.matches {
				all = all && m.selected[m.servers[match.index].Name]
			}
			for _, match := range m.matches {
				s := m.servers[match.index]
				if m.selected[s.Name] == all {
					m.toggleServer(s)
				}
			}
		case "ctrl+d":
			clear(m.selected)
			clear(m.skipped)
		case "ctrl+x":
			// untick every task of the focused server
			if s, ok := m.focusServer(); ok {
				m.setAllTasks(s, false)
			}
		case "alt+t":
			// tick every task of the focused server
			if s, ok := m.focusServer(); ok {
				m.setAllTasks(s, true)
			}
		case "backspace":
			if r := []rune(m.query); len(r) > 0 {
				m.query = string(r[:len(r)-1])
				m.filter()
			}
		case "ctrl+w":
			m.query = strings.TrimRightFunc(m.query, unicode.IsSpace)
			m.query = strings.TrimRightFunc(m.query, func(r rune) bool { return !unicode.IsSpace(r) })
			m.filter()
		case "ctrl+u":
			m.query = ""
			m.filter()
		default:
			key := msg.Key()
			if key.Text != "" && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
				m.query += key.Text
				m.filter()
			}
		}
		return m, nil
	}

	return m, nil
}

// run confirms the selection and quits, if there is anything to run on.
func (m serverSelectionModel) run() (tea.Model, tea.Cmd) {
	if len(m.chosen()) == 0 {
		return m, nil
	}
	m.confirmed = true
	m.quitting = true
	return m, tea.Quit
}

// selectAllServers ticks every server (with all of its tasks) so the whole
// deployment runs without the user selecting them one by one.
func (m *serverSelectionModel) selectAllServers() {
	for _, s := range m.servers {
		m.selected[s.Name] = true
		delete(m.skipped, s.Name)
	}
}

// runAll selects every server and confirms the run.
func (m serverSelectionModel) runAll() (tea.Model, tea.Cmd) {
	m.selectAllServers()
	return m.run()
}

func (m serverSelectionModel) View() tea.View {
	var v tea.View
	if m.quitting {
		return v
	}

	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	switch {
	case m.helpOpen:
		v.SetContent(m.helpContent())
	case m.tab == tabHistory:
		v.SetContent(m.historyContent())
	case m.treeTab() != nil:
		v.SetContent(m.treeContent())
	default:
		if m.helpOpen {
			v.SetContent(m.helpContent())
		} else {
			v.SetContent(m.viewContent())
		}
	}
	return v
}

// sectionHints are shown next to each section title, to say what selecting in it does.
var sectionHints = map[string]string{
	"Groups":  "select all servers in a group",
	"Tags":    "select all servers with a tag",
	"Servers": "→ for details",
}

// toggleColumn is the screen column of a server's ▸/▾, after the pane border, the pointer
// and the checkbox.
const toggleColumn = 7

// selectionLayout is where things are drawn on screen, so mouse clicks can be mapped back.
type selectionLayout struct {
	listTop   int // screen row of the first row of both panes
	offset    int // index into rows of the first row shown
	rows      int // how many rows fit in a pane
	leftWidth int // width of the servers pane, borders included; the tasks pane is the rest
	buttonY   int // screen row of the run button
}

func (m serverSelectionModel) layout() selectionLayout {
	// title, blank, search box (3), count, blank, pane top border | panes | pane bottom
	// border, blank, button, blank, help
	const above, below = 8, 5

	l := selectionLayout{listTop: above, rows: max(m.height-above-below, 3), leftWidth: m.width / 2}

	// keep the cursor in view, with the details and env vars under it that it can't
	// step onto, as far as they fit
	if len(m.rows) > l.rows {
		end := m.cursor
		for end+1 < len(m.rows) && !selectable(m.rows[end+1]) {
			end++
		}
		l.offset = min(max(end-l.rows+1, 0), m.cursor, len(m.rows)-l.rows)
	}

	l.buttonY = l.listTop + l.rows + 2
	return l
}

func (m serverSelectionModel) searchBox() string {
	// keep the box to one line of text, since layout() counts on its height: the box
	// has a border and padding each side, then "Search " and the cursor
	width := min(m.width-2, 70)
	room := max(width-4-len("Search ")-1, 1)

	input := cursorStyle.Render("█") + mutedStyle.Render(truncate("type to filter servers, groups and tags…", room))
	if m.query != "" {
		query := []rune(m.query)
		if len(query) > room {
			query = append([]rune("…"), query[len(query)-room+1:]...)
		}
		input = string(query) + cursorStyle.Render("█")
	}

	return searchBoxStyle.Width(width).Render(searchLabelStyle.Render("Search ") + input)
}

func (m serverSelectionModel) runButton() string {
	chosen := m.chosen()

	switch {
	case len(chosen) == 0:
		return offButtonStyle.Render("▶ Run · select servers to run")
	case len(chosen) == 1:
		return runButtonStyle.Render("▶ Run on 1 server")
	}
	return runButtonStyle.Render(fmt.Sprintf("▶ Run on %d servers", len(chosen)))
}

func (m serverSelectionModel) runAllButton() string {
	return runAllButtonStyle.Render("▶ Run all")
}

func (m serverSelectionModel) viewContent() string {
	l := m.layout()

	lines := []string{m.titleLine(), ""}
	lines = append(lines, strings.Split(m.searchBox(), "\n")...)
	count := fmt.Sprintf("  %d/%d servers · %d selected", len(m.matches), len(m.servers), m.countSelected())
	if bundles := m.bundleSummary(); bundles != "" {
		count += " · " + bundles
	}
	lines = append(lines, mutedStyle.Render(truncate(count, m.width)), "")

	left := paneBox("Servers", m.serverPaneLines(l, l.leftWidth-2), l.leftWidth, l.rows, m.focus == paneServers)
	right := paneBox(m.tasksPaneTitle(), m.tasksPaneLines(l.rows, m.width-l.leftWidth-2), m.width-l.leftWidth, l.rows, m.focus == paneTasks)
	for i := range left {
		lines = append(lines, left[i]+right[i])
	}

	help := "↑/↓ move · space select · →/← details · tab tasks · alt+v all tasks · ctrl-k help · enter run · esc quit"
	if m.focus == paneTasks {
		help = "↑/↓ move · space toggle task · tab/← servers · alt+v all tasks · ctrl-k help · enter run · esc quit"
	}
	lines = append(lines, "", truncateANSI(m.runButton()+"  "+m.runAllButton()+mutedStyle.Render("  enter / alt+a"), m.width), "")
	lines = append(lines, truncateANSI(mutedStyle.Render(help), m.width))
	return strings.Join(lines, "\n")
}

// serverPaneLines renders the visible rows of the servers pane, width cells wide at most.
func (m serverSelectionModel) serverPaneLines(l selectionLayout, width int) []string {
	if len(m.rows) == 0 {
		return []string{mutedStyle.Render("  nothing matches")}
	}

	nameWidth := 0
	for _, s := range m.servers {
		nameWidth = max(nameWidth, len([]rune(s.Name)))
	}

	lines := []string{}
	for i := l.offset; i < min(l.offset+l.rows, len(m.rows)); i++ {
		row := m.rows[i]

		// the pointer stays on the server whose tasks are shown, dimmed while the tasks
		// pane has the focus
		pointer := "  "
		if i == m.cursor {
			pointer = cursorStyle.Render("▶ ")
			if m.focus != paneServers {
				pointer = mutedStyle.Render("▶ ")
			}
		}

		var line string
		switch row.kind {
		case rowHeader:
			title := row.item.name
			arrow := ""
			if title == "Groups" || title == "Tags" {
				arrow = "▶ "
				if (title == "Groups" && m.groupsOpen) || (title == "Tags" && m.tagsOpen) {
					arrow = "▼ "
				}
			}
			line = pointer + headerStyle.Render(arrow+title) + mutedStyle.Render("  "+sectionHints[title])
		case rowGroup, rowTag:
			line = pointer + m.itemLine(row)
		case rowDetail:
			line = detailIndent + mutedStyle.Render(fmt.Sprintf("%-*s", detailLabelWidth, row.label)) + row.value
		case rowEnv:
			s, _ := m.serverOfRow(row)
			toggle := "▸"
			if m.envOpen[s.Name] {
				toggle = "▾"
			}
			line = pointer + detailIndent[2:] + mutedStyle.Render(toggle+" ") + "Env vars" + mutedStyle.Render(fmt.Sprintf(" (%d)", len(s.EnvVars)))
		case rowEnvVar:
			line = detailIndent + "  " + row.value
		case rowVars:
			s, _ := m.serverOfRow(row)
			toggle := "▸"
			if m.varsOpen[s.Name] {
				toggle = "▾"
			}
			line = pointer + detailIndent[2:] + mutedStyle.Render(toggle+" ") + "Vars" + mutedStyle.Render(fmt.Sprintf(" (%d)", len(s.Vars)))
		case rowVarLine:
			line = detailIndent + "  " + row.value
		default:
			line = pointer + m.serverLine(row, nameWidth)
		}
		line = truncateANSI(line, width)
		if i == m.cursor && m.focus == paneServers {
			line = highlightSelectedRow(line, width)
		}
		lines = append(lines, line)
	}
	return lines
}

// serverLine renders a server row, after the pointer.
func (m serverSelectionModel) serverLine(row selectionRow, nameWidth int) string {
	match := m.matches[row.match]
	s := m.servers[match.index]

	check := mutedStyle.Render("[ ]")
	if m.selected[s.Name] {
		check = okStyle.Render("[x]")
	}

	name := serverStyle.Render(s.Name)
	host := mutedStyle.Render(s.Address())
	switch match.field {
	case matchName:
		name = highlight(s.Name, match.positions, serverStyle)
	case matchHost:
		host = highlight(s.Address(), match.positions, mutedStyle)
	}
	name += strings.Repeat(" ", nameWidth-len([]rune(s.Name)))

	toggle := mutedStyle.Render("▸")
	if m.expanded[s.Name] {
		toggle = mutedStyle.Render("▾")
	}

	tags := make([]string, 0, len(s.Tags))
	for t, tag := range s.Tags {
		if match.field == matchTag && t == match.tag {
			tags = append(tags, mutedStyle.Render("#")+highlight(tag, match.positions, mutedStyle))
		} else {
			tags = append(tags, mutedStyle.Render("#"+tag))
		}
	}

	line := fmt.Sprintf("%s %s %s  %s", check, toggle, name, host)
	if n := m.countTasks(s); n < len(s.Tasks) {
		line += "  " + skipStyle.Render(fmt.Sprintf("%d/%d tasks", n, len(s.Tasks)))
	}
	if len(tags) > 0 {
		line += "  " + strings.Join(tags, " ")
	}
	return line
}

// itemLine renders a group or tag, with how many of its servers are selected:
// [x] all, [-] some, [ ] none.
func (m serverSelectionModel) itemLine(row selectionRow) string {
	prefix := ""
	if row.kind == rowTag {
		prefix = "#"
	}
	name := mutedStyle.Render(prefix) + highlight(row.item.name, row.item.positions, serverStyle)

	n, total := m.countMembersSelected(row)
	check := mutedStyle.Render("[ ]")
	switch {
	case n == total && total > 0:
		check = okStyle.Render("[x]")
	case n > 0:
		check = skipStyle.Render("[-]")
	}
	return check + "   " + name + mutedStyle.Render(fmt.Sprintf("  %d/%s", n, plural(total, "server")))
}

// plural formats a count with a noun, e.g. "1 task" or "3 tasks".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// taskLine renders one of a server's tasks in the tasks pane, after the pointer.
func (m serverSelectionModel) taskLine(s gorun.Server, t gorun.Task) string {
	check := mutedStyle.Render("[ ]")
	name := mutedStyle.Render(t.Name)
	if m.taskSelected(s.Name, t.Name) {
		check = okStyle.Render("[x]")
		name = t.Name
	}

	line := check + " " + name
	if t.Description != "" {
		line += mutedStyle.Render(" — " + t.Description)
	}
	if len(t.DependsOn) > 0 {
		line += mutedStyle.Render(" (needs " + strings.Join(t.DependsOn, ", ") + ")")
	}
	if t.Rerun == gorun.RerunUnsafe {
		line += skipStyle.Render(" ⚠")
	}
	return line
}

// highlight renders s with style, picking out the runes at positions with matchStyle.
func highlight(s string, positions []int, style lipgloss.Style) string {
	b := strings.Builder{}
	for i, r := range []rune(s) {
		if slices.Contains(positions, i) {
			b.WriteString(matchStyle.Render(string(r)))
		} else {
			b.WriteString(style.Render(string(r)))
		}
	}
	return b.String()
}

// runServerSelectionTUI lets the user pick servers to deploy to, from servers with their
// pipelines resolved, with cfg's tasks and bundles and past runs in their own tabs.
// It returns an error if the selection was cancelled.
func runServerSelectionTUI(cfg *gorun.Config, servers []gorun.Server) ([]gorun.Server, error) {
	st, stErr := store.Open(getHistoryDBPath())
	if stErr == nil {
		defer func() { _ = st.Close() }()
	}

	// slog output would corrupt the TUI; put it back for the run that follows
	previous := gorun.LogOutput()
	gorun.SetLogOutput(io.Discard)
	defer gorun.SetLogOutput(previous)
	defer drainTerminalInput()
	final, err := tea.NewProgram(newServerSelectionModel(cfg.Title, servers).withLists(cfg).withHistory(st, stErr), tea.WithEnvironment(append(os.Environ(), "SSH_TTY=1"))).Run()
	if err != nil {
		return nil, fmt.Errorf("error running server selection TUI: %w", err)
	}

	m := final.(serverSelectionModel)
	if m.editorOpened {
		if m.editorErr != nil {
			return nil, fmt.Errorf("edit task command file: %w", m.editorErr)
		}
		return nil, errTaskCommandEditorClosed
	}
	if !m.confirmed {
		return nil, errors.New("server selection cancelled")
	}

	return m.chosen(), nil
}

// helpContent renders the ctrl-k help dialog.
func (m serverSelectionModel) helpContent() string {
	lines := []string{m.titleLine(), "", "Keyboard shortcuts (ctrl-k to close)", ""}
	navigation := [][2]string{
		{"↑/↓", "move cursor"},
		{"→ / ←", "show/hide a server's details and env vars; expand/collapse groups & tags"},
		{"tab", "switch between the servers and tasks panes"},
		{"enter", "run (or expand a group/tag section)"},
		{"shift+tab", "next tab"},
		{"ctrl+s/t/b/g/h", "jump to Servers/Tasks/Bundles/Groups/History"},
	}
	if m.tab == tabTasks {
		navigation = [][2]string{
			{"↑/↓", "move in the tree / select a detail section"},
			{"tab", "focus details / return to the task tree"},
			{"enter / →", "expand / collapse the selected detail section"},
			{"←", "collapse a section / return to the task tree"},
			{"pgup / pgdn", "move in the tree / scroll details"},
			{"home / end", "jump to the start/end of details"},
			{"ctrl+e", "edit selected cmd_file / cmd_template_file, then exit"},
			{"→ / ←", "expand / collapse a task"},
			{"shift+tab", "next tab"},
			{"ctrl+s/t/b/g/h", "jump to Servers/Tasks/Bundles/Groups/History"},
		}
	}
	sections := []struct {
		title string
		keys  [][2]string
	}{
		{"Navigation", navigation},
		{"Selection", [][2]string{
			{"space", "select/toggle the row under the cursor; on Bundles, check the bundle"},
			{"alt+v", "tasks pane: the server under the cursor / every server"},
			{"alt+e / alt+E", "show details of one/all servers"},
			{"alt+c / alt+C", "hide details of one/all servers"},
			{"alt+a", "select all servers and run"},
			{"ctrl+a", "select all shown servers"},
			{"ctrl+d", "clear selection"},
			{"ctrl+x", "clear a server's tasks"},
			{"alt+t", "tick all of a server's tasks"},
		}},
		{"Filter", [][2]string{
			{"type", "filter servers, groups and tags"},
			{"backspace", "delete one character"},
			{"ctrl+w / ctrl+u", "delete word / clear filter"},
			{"esc", "clear filter, then quit"},
		}},
		{"Other", [][2]string{
			{"ctrl+k", "show/hide this help"},
			{"ctrl+c", "quit"},
			{"click", "select, expand, or run"},
		}},
	}
	for _, sec := range sections {
		lines = append(lines, headerStyle.Render(sec.title))
		for _, kv := range sec.keys {
			lines = append(lines, fmt.Sprintf("  %-28s %s", kv[0], mutedStyle.Render(kv[1])))
		}
		lines = append(lines, "")
	}
	lines = append(lines, mutedStyle.Render("press any key to close"))
	return strings.Join(lines, "\n")
}
