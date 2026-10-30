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
	matchStyle       = lipgloss.NewStyle().Foreground(colorSkip).Bold(true)
	searchBoxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(0, 1)
	searchLabelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	runButtonStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colorOK).Padding(0, 2)
	offButtonStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Background(lipgloss.Color("#374151")).Padding(0, 2)
	headerStyle      = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	activeTabStyle   = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Underline(true)
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
	rowTask   // a task of an expanded server
	rowHeader // a section title
	rowGroup
	rowTag
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
	match int       // rowServer, rowTask: index into serverSelectionModel.matches
	task  int       // rowTask: index into the server's Tasks
	item  itemMatch // rowGroup, rowTag: the item; rowHeader: the title
}

// serverSelectionModel is the bubbletea model for picking which servers to deploy to.
// Typing filters the list with a fuzzy match on the server name (or hostname, or tags).
// Expanding a server lists its task pipeline, so individual tasks can be left out.
// Groups and tags select all their servers at once.
type serverSelectionModel struct {
	title   string
	servers []gorun.Server // with Tasks resolved, so they can be listed
	groups  []string
	tags    []string

	selected map[string]bool
	expanded map[string]bool
	skipped  map[string]map[string]bool // server name -> task names left out of the run

	query        string
	matches      []serverMatch
	groupMatches []itemMatch
	tagMatches   []itemMatch
	rows         []selectionRow
	cursor       int // index into rows
	width        int
	height       int

	// the History tab, shown when history is set up with withHistory
	tab        int
	hasHistory bool
	history    historyModel
	historyErr error // why the history database couldn't be opened

	confirmed bool
	quitting  bool
}

// the picker's tabs
const (
	tabServers = iota
	tabHistory
)

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

// switchTab shows the other tab. The history is reloaded each time it is shown, so it
// includes runs that finished since.
func (m serverSelectionModel) switchTab() (tea.Model, tea.Cmd) {
	if m.tab == tabHistory {
		m.tab = tabServers
		return m, nil
	}
	m.tab = tabHistory
	if m.historyErr != nil {
		return m, nil
	}
	return m, m.history.Init()
}

// updateHistory handles input while the History tab is shown.
func (m serverSelectionModel) updateHistory(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "shift+tab":
			return m.switchTab()
		case "esc", "q":
			// on the runs list these quit the history view; here they go back
			if m.history.state == historyList && !m.history.askKeep {
				return m.switchTab()
			}
		}
	case tea.MouseClickMsg:
		if tab, ok := m.tabAt(msg.X, msg.Y); ok && tab != m.tab {
			return m.switchTab()
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
var tabsWidth = lipgloss.Width("   Servers │ History  shift+tab")

// titlePrefix renders "gorun" and the title, shortening the title so the tabs still fit
// on the line.
func (m serverSelectionModel) titlePrefix() string {
	title := m.title
	if m.hasHistory {
		room := m.width - lipgloss.Width(titleStyle.Render("gorun")+" ") - tabsWidth
		title = truncate(title, max(room, 0))
	}
	return titleStyle.Render("gorun") + " " + mutedStyle.Render(title)
}

// titleLine renders the top line: the title and, with history, the tabs.
func (m serverSelectionModel) titleLine() string {
	line := m.titlePrefix()
	if !m.hasHistory {
		return line
	}
	return line + "   " + m.tabLabel("Servers", tabServers) + mutedStyle.Render(" │ ") + m.tabLabel("History", tabHistory) + mutedStyle.Render("  shift+tab")
}

func (m serverSelectionModel) tabLabel(name string, tab int) string {
	if m.tab == tab {
		return activeTabStyle.Render(name)
	}
	return mutedStyle.Render(name)
}

// tabAt returns the tab whose label is at screen position x, y.
func (m serverSelectionModel) tabAt(x, y int) (int, bool) {
	if !m.hasHistory || y != 0 {
		return 0, false
	}
	start := lipgloss.Width(m.titlePrefix() + "   ")
	servers := lipgloss.Width("Servers")
	history := start + servers + lipgloss.Width(" │ ")
	switch {
	case x >= start && x < start+servers:
		return tabServers, true
	case x >= history && x < history+lipgloss.Width("History"):
		return tabHistory, true
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
		skipped:  map[string]map[string]bool{},
		width:    80,
		height:   24,
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
	m.groups = slices.Sorted(maps.Keys(groups))
	m.tags = slices.Sorted(maps.Keys(tags))

	m.filter()
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
		key.name = ""
		if row.kind == rowTask {
			key.name = s.Tasks[row.task].Name
		}
	}
	return key
}

// buildRows lays out the matching groups and tags, each under a title, then the
// matching servers with the tasks of expanded servers under them. The cursor stays on the
// same row (or a gone task's server) when the rows around it change.
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

	section := func(title string, kind int, items []itemMatch) {
		if len(items) == 0 {
			return
		}
		m.rows = append(m.rows, selectionRow{kind: rowHeader, item: itemMatch{name: title}})
		for _, item := range items {
			m.rows = append(m.rows, selectionRow{kind: kind, item: item})
		}
	}
	section("Groups", rowGroup, m.groupMatches)
	section("Tags", rowTag, m.tagMatches)
	if len(m.rows) > 0 && len(m.matches) > 0 {
		m.rows = append(m.rows, selectionRow{kind: rowHeader, item: itemMatch{name: "Servers"}})
	}

	for i, match := range m.matches {
		m.rows = append(m.rows, selectionRow{kind: rowServer, match: i})
		s := m.servers[match.index]
		if m.expanded[s.Name] {
			for t := range s.Tasks {
				m.rows = append(m.rows, selectionRow{kind: rowTask, match: i, task: t})
			}
		}
	}

	if keep != nil {
		m.cursor = m.findRow(*keep)
	}
	m.cursor = max(min(m.cursor, len(m.rows)-1), 0)
	m.moveCursor(0)
}

// findRow returns the index of the row with key, falling back to its server's row for a
// task that is no longer listed, or the current cursor if neither is found.
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

// moveCursor moves the cursor by delta rows, stepping over section titles.
func (m *serverSelectionModel) moveCursor(delta int) {
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
	for m.rows[m.cursor].kind == rowHeader {
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

// serverOfRow returns the server of a server or task row.
func (m serverSelectionModel) serverOfRow(row selectionRow) (gorun.Server, bool) {
	if row.kind != rowServer && row.kind != rowTask {
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
	case rowServer:
		s, _ := m.serverOfRow(row)
		m.toggleServer(s)
	case rowTask:
		s, _ := m.serverOfRow(row)
		m.toggleTask(s, s.Tasks[row.task].Name)
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
	m.groupMatches = matchItems(m.groups)
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
		m.history.width, m.history.height = msg.Width, msg.Height
		return m, nil
	case historyRunsMsg, historyRunMsg:
		next, cmd := m.history.Update(msg)
		m.history = next.(historyModel)
		return m, cmd
	}

	if m.tab == tabHistory {
		return m.updateHistory(msg)
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
			return m.switchTab()
		}

		l := m.layout()
		if msg.Y == l.buttonY && msg.X < lipgloss.Width(m.runButton()) {
			return m.run()
		}

		// clicking a server or task moves the cursor to it and toggles it; clicking a
		// server's ▸ shows or hides its tasks
		if i := l.offset + msg.Y - l.listTop; msg.Y >= l.listTop && i < min(l.offset+l.rows, len(m.rows)) {
			row := m.rows[i]
			if row.kind == rowHeader {
				return m, nil
			}
			m.cursor = i
			if s, ok := m.serverOfRow(row); ok && row.kind == rowServer && msg.X == toggleColumn && len(s.Tasks) > 0 {
				m.expanded[s.Name] = !m.expanded[s.Name]
				m.buildRows()
				return m, nil
			}
			m.toggleRow(row)
		}
		return m, nil

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.moveCursor(-1)
		case tea.MouseWheelDown:
			m.moveCursor(1)
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
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
			return m.run()
		case "shift+tab":
			if m.hasHistory {
				return m.switchTab()
			}
		case "up", "ctrl+p", "ctrl+k":
			m.moveCursor(-1)
		case "down", "ctrl+n", "ctrl+j":
			m.moveCursor(1)
		case "right":
			// show the server's tasks
			if row, ok := m.currentRow(); ok && row.kind == rowServer {
				if s, _ := m.serverOfRow(row); len(s.Tasks) > 0 {
					m.expanded[s.Name] = true
					m.buildRows()
				}
			}
		case "left":
			// hide the server's tasks, moving the cursor from a task up to its server
			if s, ok := m.current(); ok && m.expanded[s.Name] {
				m.expanded[s.Name] = false
				for m.cursor > 0 && m.rows[m.cursor].kind == rowTask {
					m.cursor--
				}
				m.buildRows()
			}
		case "space", "tab":
			if row, ok := m.currentRow(); ok {
				m.toggleRow(row)
				if msg.String() == "tab" {
					m.moveCursor(1)
				}
			}
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
			// untick every task of the server under the cursor
			if s, ok := m.current(); ok {
				m.setAllTasks(s, false)
			}
		case "ctrl+t":
			// tick every task of the server under the cursor
			if s, ok := m.current(); ok {
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

func (m serverSelectionModel) View() tea.View {
	var v tea.View
	if m.quitting {
		return v
	}

	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if m.tab == tabHistory {
		v.SetContent(m.historyContent())
	} else {
		v.SetContent(m.viewContent())
	}
	return v
}

// sectionHints are shown next to each section title, to say what selecting in it does.
var sectionHints = map[string]string{
	"Groups":  "select all servers in a group",
	"Tags":    "select all servers with a tag",
	"Servers": "→ to pick tasks",
}

// toggleColumn is the screen column of a server's ▸/▾, after the pointer and checkbox.
const toggleColumn = 6

// selectionLayout is where things are drawn on screen, so mouse clicks can be mapped back.
type selectionLayout struct {
	listTop int // screen row of the first row
	offset  int // index into rows of the first row shown
	rows    int // how many rows fit
	buttonY int // screen row of the run button
}

func (m serverSelectionModel) layout() selectionLayout {
	// title, blank, search box (3), count, blank | list | blank, button, blank, help
	const above, below = 7, 4

	l := selectionLayout{listTop: above, rows: max(m.height-above-below, 3)}

	// keep the cursor in view
	if len(m.rows) > l.rows {
		l.offset = min(max(m.cursor-l.rows+1, 0), len(m.rows)-l.rows)
	}

	l.buttonY = l.listTop + max(min(l.rows, len(m.rows)), 1) + 1
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

func (m serverSelectionModel) viewContent() string {
	l := m.layout()

	lines := []string{m.titleLine(), ""}
	lines = append(lines, strings.Split(m.searchBox(), "\n")...)
	lines = append(lines, mutedStyle.Render(fmt.Sprintf("  %d/%d servers · %d selected", len(m.matches), len(m.servers), m.countSelected())), "")

	if len(m.rows) == 0 {
		lines = append(lines, mutedStyle.Render("  nothing matches"))
	}

	nameWidth := 0
	for _, s := range m.servers {
		nameWidth = max(nameWidth, len([]rune(s.Name)))
	}

	for i := l.offset; i < min(l.offset+l.rows, len(m.rows)); i++ {
		row := m.rows[i]

		pointer := "  "
		if i == m.cursor {
			pointer = cursorStyle.Render("▸ ")
		}

		switch row.kind {
		case rowHeader:
			lines = append(lines, "  "+headerStyle.Render(row.item.name)+mutedStyle.Render("  "+sectionHints[row.item.name]))
			continue
		case rowGroup, rowTag:
			lines = append(lines, pointer+m.itemLine(row))
			continue
		}

		match := m.matches[row.match]
		s := m.servers[match.index]

		if row.kind == rowTask {
			lines = append(lines, pointer+m.taskLine(s, s.Tasks[row.task]))
			continue
		}

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

		toggle := " "
		if len(s.Tasks) > 0 {
			toggle = mutedStyle.Render("▸")
			if m.expanded[s.Name] {
				toggle = mutedStyle.Render("▾")
			}
		}

		tags := make([]string, 0, len(s.Tags))
		for t, tag := range s.Tags {
			if match.field == matchTag && t == match.tag {
				tags = append(tags, mutedStyle.Render("#")+highlight(tag, match.positions, mutedStyle))
			} else {
				tags = append(tags, mutedStyle.Render("#"+tag))
			}
		}

		line := fmt.Sprintf("%s%s %s %s  %s", pointer, check, toggle, name, host)
		if n := m.countTasks(s); n < len(s.Tasks) {
			line += "  " + skipStyle.Render(fmt.Sprintf("%d/%d tasks", n, len(s.Tasks)))
		}
		if len(tags) > 0 {
			line += "  " + strings.Join(tags, " ")
		}
		lines = append(lines, line)
	}

	lines = append(lines, "", m.runButton()+mutedStyle.Render("  enter"), "")
	lines = append(lines, mutedStyle.Render("↑/↓ move · →/← show/hide tasks · space/tab select · ctrl+t/ctrl+x tick/clear a server's tasks · ctrl+a select all shown · ctrl+d clear · esc clear/quit · click to select or run"))
	return strings.Join(lines, "\n")
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

// taskLine renders one of an expanded server's tasks.
func (m serverSelectionModel) taskLine(s gorun.Server, t gorun.Task) string {
	check := mutedStyle.Render("[ ]")
	name := mutedStyle.Render(t.Name)
	if m.taskSelected(s.Name, t.Name) {
		check = okStyle.Render("[x]")
		name = t.Name
	}

	line := "      " + check + " " + name
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
// pipelines resolved, with past runs in a History tab. It returns an error if the
// selection was cancelled.
func runServerSelectionTUI(title string, servers []gorun.Server) ([]gorun.Server, error) {
	st, stErr := store.Open(getHistoryDBPath())
	if stErr == nil {
		defer func() { _ = st.Close() }()
	}

	// slog output would corrupt the TUI; put it back for the run that follows
	previous := gorun.LogOutput()
	gorun.SetLogOutput(io.Discard)
	defer gorun.SetLogOutput(previous)
	defer drainTerminalInput()
	final, err := tea.NewProgram(newServerSelectionModel(title, servers).withHistory(st, stErr), tea.WithEnvironment(append(os.Environ(), "SSH_TTY=1"))).Run()
	if err != nil {
		return nil, fmt.Errorf("error running server selection TUI: %w", err)
	}

	m := final.(serverSelectionModel)
	if !m.confirmed {
		return nil, errors.New("server selection cancelled")
	}

	return m.chosen(), nil
}
