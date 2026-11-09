package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jenmud/gorun/history"
	"github.com/jenmud/gorun/internal/store"
)

// historyState is which history screen is showing.
type historyState int

const (
	historyList historyState = iota
	historyDetail
)

// historyItem is a row of a run's left pane: a server, or one of its tasks.
type historyItem struct {
	isServer bool
	server   string
	taskIdx  int
	expanded bool
}

// historySection is a foldable block of a run's right pane, such as a task's stdout.
type historySection struct {
	key   string
	title string
	lines []string
}

// historyPanelRow is one line of the right pane: a detail line, a section heading or a line inside a section.
type historyPanelRow struct {
	text        string
	section     int
	sectionHead bool
}

// historyModel is the history screen: the list of saved runs, and one run opened with its servers and tasks.
type historyModel struct {
	expanded     map[string]bool
	loadedTasks  map[string]bool
	sectionsOpen map[string]bool
	pendingCopy  bool
	listCursor   int
	status       string
	helpOpen     bool
	state        historyState
	st           *store.Store
	runs         []store.RunSummary
	cursor       int
	offset       int
	leftOffset   int
	rightOffset  int
	rightCursor  int
	focusRight   bool
	width        int
	height       int
	run          *store.RunDetail
	items        []historyItem
	err          error
	askKeep      bool
	keepInput    string

	// embedded is set when the history is a tab of the server picker, where esc and q
	// on the runs list go back to the servers instead of quitting
	embedded bool
}

// newHistoryModel returns the history screen for the runs in st, starting on the list.
func newHistoryModel(st *store.Store) historyModel {
	return historyModel{
		state:        historyList,
		st:           st,
		width:        80,
		height:       24,
		expanded:     map[string]bool{},
		loadedTasks:  map[string]bool{},
		sectionsOpen: map[string]bool{},
	}
}

// Init loads the list of runs.
func (m historyModel) Init() tea.Cmd {
	return func() tea.Msg { return m.loadRuns() }
}

// historyRunsMsg carries the loaded list of runs.
type historyRunsMsg struct {
	runs []store.RunSummary
	err  error
}

// historyRunMsg carries one loaded run.
type historyRunMsg struct {
	run *store.RunDetail
	err error
}

// historyTaskMsg carries one task loaded with its command and output.
type historyTaskMsg struct {
	task store.TaskDetail
	err  error
}

// historyCopyClearMsg clears the "copied" status line.
type historyCopyClearMsg struct{}

// isLocalHost reports whether a run's hostname is this machine.
func isLocalHost(h string) bool {
	if h == "" {
		return true
	}
	nh, err := os.Hostname()
	if err != nil {
		return true
	}
	return h == nh
}

// listRows returns how many runs fit on the runs list: each run takes three lines (summary,
// start time and a blank line), under the title (two lines) and above the position
// counter and help (three lines).
func (m historyModel) listRows() int {
	return max((m.height-5)/3, 1)
}

// loadRuns reads every saved run.
func (m historyModel) loadRuns() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runs, err := m.st.ListRuns(ctx)
	return historyRunsMsg{runs: runs, err: err}
}

// loadRun reads one run with its servers and tasks.
func (m historyModel) loadRun(id string) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, err := m.st.GetRun(ctx, id)
	if err != nil {
		return historyRunMsg{run: nil, err: err}
	}
	m.run = run
	m.buildItemsForRun(run)
	return historyRunMsg{run: run, err: nil}
}

// loadTask reads one task with its command, output, envvars and vars.
func (m historyModel) loadTask(runID, server, task string) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	td, err := m.st.GetTask(ctx, runID, server, task)
	return historyTaskMsg{task: td, err: err}
}

// isServerExpanded reports whether a server's tasks are visible. It prefers the
// expanded map and falls back to the item state for callers (such as tests)
// that toggle items directly.
func (m historyModel) isServerExpanded(server string) bool {
	if v, ok := m.expanded[server]; ok {
		return v
	}
	for _, si := range m.items {
		if si.isServer && si.server == server && si.expanded {
			return true
		}
	}
	return false
}

// buildItemsForRun lists the run's servers, slowest first, each followed by its tasks, slowest first, and resets the cursors.
func (m *historyModel) buildItemsForRun(run *store.RunDetail) {
	items := []historyItem{}
	if run == nil {
		m.items = items
		m.cursor = 0
		m.offset = 0
		return
	}
	servers := run.Servers
	if len(servers) == 0 {
		seen := map[string]bool{}
		for _, t := range run.Tasks {
			if !seen[t.Server] {
				servers = append(servers, t.Server)
				seen[t.Server] = true
			}
		}
	}
	// sort servers slowest to fastest by total task duration
	durByServer := map[string]time.Duration{}
	for i := range run.Tasks {
		durByServer[run.Tasks[i].Server] += run.Tasks[i].Duration
	}
	sort.SliceStable(servers, func(i, j int) bool {
		return durByServer[servers[i]] > durByServer[servers[j]]
	})
	for _, srv := range servers {
		items = append(items, historyItem{isServer: true, server: srv, expanded: false})
		tasks := []int{}
		for i := range run.Tasks {
			if run.Tasks[i].Server == srv {
				tasks = append(tasks, i)
			}
		}
		sort.SliceStable(tasks, func(i, j int) bool {
			return run.Tasks[tasks[i]].Duration > run.Tasks[tasks[j]].Duration
		})
		for _, i := range tasks {
			items = append(items, historyItem{isServer: false, server: srv, taskIdx: i, expanded: false})
		}
	}
	m.items = items
	m.cursor = 0
	m.offset = 0
	m.leftOffset = 0
	m.rightOffset = 0
	m.rightCursor = 0
	m.focusRight = false
	m.expanded = map[string]bool{}
	m.loadedTasks = map[string]bool{}
	m.sectionsOpen = map[string]bool{}
}

// visibleItems returns the indexes of the rows showing: every server, and the tasks of expanded ones.
func (m historyModel) visibleItems() []int {
	visible := make([]int, 0, len(m.items))
	for i, item := range m.items {
		if item.isServer || m.isServerExpanded(item.server) {
			visible = append(visible, i)
		}
	}
	return visible
}

// moveLeftCursor moves the left pane's cursor by delta rows, scrolling to keep it in view.
func (m *historyModel) moveLeftCursor(delta int) {
	visible := m.visibleItems()
	if len(visible) == 0 {
		m.cursor = 0
		return
	}
	pos := 0
	for i, idx := range visible {
		if idx == m.cursor {
			pos = i
			break
		}
	}
	pos = max(0, min(pos+delta, len(visible)-1))
	m.cursor = visible[pos]
	m.rightOffset = 0
	m.rightCursor = 0
	if pos < m.leftOffset {
		m.leftOffset = pos
	}
	if pos >= m.leftOffset+m.detailRows() {
		m.leftOffset = pos - m.detailRows() + 1
	}
}

// detailRows is how many rows fit in each pane of an opened run.
func (m historyModel) detailRows() int {
	return max(m.height-8, 3)
}

// currentItem returns the row under the left pane's cursor.
func (m historyModel) currentItem() (historyItem, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return historyItem{}, false
	}
	return m.items[m.cursor], true
}

// detailSections returns the right pane for the row under the cursor: its summary lines and its foldable sections.
func (m historyModel) detailSections() ([]string, []historySection) {
	item, ok := m.currentItem()
	if !ok || m.run == nil {
		return nil, nil
	}
	runScope := "run:" + m.run.ID + ":"
	serverScope := "server:" + item.server + ":"
	sections := []historySection{}
	add := func(key, title string, values []string) {
		sections = append(sections, historySection{key: key, title: title, lines: values})
	}
	jsonLines := varsTableLines
	envLines := func(v map[string]string) []string { return envTableLines(v, max(m.width/2-8, 8)) }

	meta := []string{}
	if item.isServer {
		address := m.run.ServerAddress[item.server]
		if address == "" {
			address = item.server
		}
		meta = append(meta, headerStyle.Render(item.server), mutedStyle.Render(address))
		if msg := m.run.ServerErrors[item.server]; msg != "" {
			meta = append(meta, failStyle.Render("✗ "+msg))
		}
		if len(m.run.RunEnvVars) > 0 {
			add(runScope+"env", "Run env vars", envLines(m.run.RunEnvVars))
		}
		if len(m.run.RunVars) > 0 {
			add(runScope+"vars", "Run vars", jsonLines(m.run.RunVars))
		}
		if v := m.run.ServerEnvVars[item.server]; len(v) > 0 {
			add(serverScope+"env", "Server env vars", envLines(v))
		}
		if v := m.run.ServerVars[item.server]; len(v) > 0 {
			add(serverScope+"vars", "Server vars", jsonLines(v))
		}
		return meta, sections
	}
	if item.taskIdx < 0 || item.taskIdx >= len(m.run.Tasks) {
		return meta, sections
	}
	task := m.run.Tasks[item.taskIdx]
	taskScope := "task:" + item.server + "\x00" + task.Name + ":"
	status := historyTaskState(task.State)
	meta = append(meta, headerStyle.Render(task.Name), mutedStyle.Render(item.server))
	meta = append(meta, status+mutedStyle.Render(" · "+task.Duration.Round(time.Millisecond).String()))
	if task.ExitCode != 0 || task.State == 3 {
		meta = append(meta, failStyle.Render(fmt.Sprintf("Exit code: %d", task.ExitCode)))
	}
	if task.Command != "" {
		meta = append(meta, mutedStyle.Render("Command recorded"))
	}

	if len(m.run.RunEnvVars) > 0 {
		add(runScope+"env", "Run env vars", envLines(m.run.RunEnvVars))
	}
	if len(m.run.RunVars) > 0 {
		add(runScope+"vars", "Run vars", jsonLines(m.run.RunVars))
	}
	if v := m.run.ServerEnvVars[item.server]; len(v) > 0 {
		add(serverScope+"env", "Server env vars", envLines(v))
	}
	if v := m.run.ServerVars[item.server]; len(v) > 0 {
		add(serverScope+"vars", "Server vars", jsonLines(v))
	}
	taskEnv := envLines(task.EnvVars)
	if len(task.EnvVars) == 0 {
		taskEnv = []string{"(empty or not recorded)"}
	}
	taskVars := jsonLines(task.Vars)
	if len(task.Vars) == 0 {
		taskVars = []string{"(empty or not recorded)"}
	}
	add(taskScope+"env", "Task env vars", taskEnv)
	add(taskScope+"vars", "Task vars", taskVars)
	command := []string{"(not recorded)"}
	if task.Command != "" {
		command = make([]string, 0)
		for i, line := range strings.Split(strings.TrimRight(task.Command, "\r\n"), "\n") {
			prefix := "$ "
			if i > 0 {
				prefix = "  "
			}
			command = append(command, prefix+strings.ReplaceAll(strings.TrimRight(line, "\r"), "\t", "    "))
		}
	}
	add(taskScope+"command", "Command", command)
	add(taskScope+"stdout", "Stdout", outputLines(task.Stdout))
	add(taskScope+"stderr", "Stderr", outputLines(task.Stderr))
	return meta, sections
}

// outputLines splits a task's output into lines, or says there was none.
func outputLines(s string) []string {
	if s == "" {
		return []string{"(empty)"}
	}
	return strings.Split(strings.TrimRight(s, "\r\n"), "\n")
}

// historyTaskState renders a task's state as a coloured label.
func historyTaskState(state int) string {
	switch state {
	case 1:
		return skipStyle.Render("● running")
	case 2:
		return okStyle.Render("✓ done")
	case 3:
		return failStyle.Render("✗ failed")
	case 4:
		return skipStyle.Render("⊘ skipped")
	default:
		return mutedStyle.Render("○ pending")
	}
}

// detailPanelRows lays the right pane out as rows: the summary, then each section's heading and, when open, its lines.
func (m historyModel) detailPanelRows(meta []string, sections []historySection) []historyPanelRow {
	rows := make([]historyPanelRow, 0, len(meta)+len(sections))
	for _, line := range meta {
		rows = append(rows, historyPanelRow{text: line, section: -1})
	}
	for i, section := range sections {
		arrow := "▸"
		if m.sectionsOpen[section.key] {
			arrow = "▾"
		}
		rows = append(rows, historyPanelRow{text: fmt.Sprintf("%s %s", arrow, section.title), section: i, sectionHead: true})
		if m.sectionsOpen[section.key] {
			for _, line := range section.lines {
				rows = append(rows, historyPanelRow{text: "  " + line, section: i})
			}
		}
	}
	return rows
}

// ensureRightCursorVisible clamps the right pane's cursor to rows and scrolls to keep it in view.
func (m *historyModel) ensureRightCursorVisible(rows []historyPanelRow) {
	if len(rows) == 0 {
		m.rightOffset = 0
		m.rightCursor = 0
		return
	}
	m.rightCursor = max(0, min(m.rightCursor, len(rows)-1))
	visible := m.detailRows()
	line := m.rightCursor
	if line < m.rightOffset {
		m.rightOffset = line
	} else if line >= m.rightOffset+visible {
		m.rightOffset = line - visible + 1
	}
}

// moveRightCursor moves the right pane's cursor by delta rows.
func (m *historyModel) moveRightCursor(delta int) {
	meta, sections := m.detailSections()
	rows := m.detailPanelRows(meta, sections)
	if len(rows) == 0 {
		m.rightCursor = 0
		m.rightOffset = 0
		return
	}
	m.rightCursor = max(0, min(m.rightCursor+delta, len(rows)-1))
	m.ensureRightCursorVisible(rows)
}

// toggleRightSection opens or closes the section under the right pane's cursor, loading the task's output the first time it is needed.
func (m *historyModel) toggleRightSection() tea.Cmd {
	meta, sections := m.detailSections()
	if len(sections) == 0 {
		return nil
	}
	rows := m.detailPanelRows(meta, sections)
	m.rightCursor = max(0, min(m.rightCursor, len(rows)-1))
	row := rows[m.rightCursor]
	if !row.sectionHead || row.section < 0 || row.section >= len(sections) {
		return nil
	}
	section := sections[row.section]
	m.sectionsOpen[section.key] = !m.sectionsOpen[section.key]
	rows = m.detailPanelRows(meta, sections)
	m.ensureRightCursorVisible(rows)
	if !m.sectionsOpen[section.key] || !strings.HasPrefix(section.key, "task:") {
		return nil
	}
	item, ok := m.currentItem()
	if !ok || item.isServer || m.run == nil || item.taskIdx < 0 || item.taskIdx >= len(m.run.Tasks) {
		return nil
	}
	task := m.run.Tasks[item.taskIdx]
	key := task.Server + "\x00" + task.Name
	if m.loadedTasks[key] {
		return nil
	}
	return func() tea.Msg { return m.loadTask(m.run.ID, task.Server, task.Name) }
}

// Update handles loaded data and key presses.
func (m historyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.helpOpen {
		if kmsg, ok := msg.(tea.KeyPressMsg); ok {
			switch kmsg.String() {
			case "ctrl+c":
				return m, tea.Quit
			default:
				m.helpOpen = false
				return m, nil
			}
		}
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case historyRunsMsg:
		wasEmpty := len(m.runs) == 0
		m.runs = msg.runs
		m.err = msg.err
		m.run = nil
		m.items = nil
		m.state = historyList
		if len(m.runs) > 0 {
			if wasEmpty || m.cursor >= len(m.runs) {
				m.cursor = len(m.runs) - 1
			}
		} else {
			m.cursor = 0
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		// set offset so cursor is visible
		visible := m.listRows()
		if m.cursor >= visible {
			m.offset = m.cursor - visible + 1
		} else {
			m.offset = 0
		}
		return m, nil
	case historyRunMsg:
		m.run = msg.run
		m.err = msg.err
		if m.run != nil {
			m.listCursor = m.cursor
			m.status = ""
			m.buildItemsForRun(m.run)
			m.state = historyDetail
		}
		return m, nil
	case historyTaskMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		cmdToCopy := ""
		if m.run != nil {
			for i := range m.run.Tasks {
				if m.run.Tasks[i].Server == msg.task.Server && m.run.Tasks[i].Name == msg.task.Name {
					m.run.Tasks[i].Command = msg.task.Command
					m.run.Tasks[i].Stdout = msg.task.Stdout
					m.run.Tasks[i].Stderr = msg.task.Stderr
					m.run.Tasks[i].ExitCode = msg.task.ExitCode
					m.run.Tasks[i].Duration = msg.task.Duration
					m.run.Tasks[i].State = msg.task.State
					m.run.Tasks[i].EnvVars = msg.task.EnvVars
					m.run.Tasks[i].Vars = msg.task.Vars
					if m.pendingCopy {
						cmdToCopy = msg.task.Command
					}
					break
				}
			}
			key := msg.task.Server + "\x00" + msg.task.Name
			m.loadedTasks[key] = true
			if m.pendingCopy {
				m.pendingCopy = false
			}
		}
		if cmdToCopy != "" {
			m.status = "copied command to clipboard"
			return m, tea.Batch(tea.SetClipboard(cmdToCopy), tea.Tick(2*time.Second, func(time.Time) tea.Msg { return historyCopyClearMsg{} }))
		}
		return m, nil
	case historyCopyClearMsg:
		m.status = ""
		return m, nil
	case tea.KeyPressMsg:
		if m.askKeep {
			switch msg.String() {
			case "enter":
				n := 10
				fmt.Sscanf(m.keepInput, "%d", &n)
				if n < 0 {
					n = 0
				}
				m.askKeep = false
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = m.st.TruncateRuns(ctx, n)
					return m.loadRuns()
				}
			case "esc":
				m.askKeep = false
				m.keepInput = ""
				return m, nil
			case "backspace":
				if len(m.keepInput) > 0 {
					m.keepInput = m.keepInput[:len(m.keepInput)-1]
				}
				return m, nil
			default:
				if len(msg.String()) == 1 && msg.String()[0] >= '0' && msg.String()[0] <= '9' {
					m.keepInput += msg.String()
				}
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+k":
			m.helpOpen = !m.helpOpen
			return m, nil
		case "ctrl+c", "q":
			if m.state == historyDetail {
				m.state = historyList
				m.run = nil
				m.items = nil
				m.status = ""
				m.cursor = m.listCursor
				if m.cursor >= len(m.runs) {
					m.cursor = max(len(m.runs)-1, 0)
				}
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.state == historyDetail {
				m.state = historyList
				m.run = nil
				m.items = nil
				m.status = ""
				m.cursor = m.listCursor
				if m.cursor >= len(m.runs) {
					m.cursor = max(len(m.runs)-1, 0)
				}
				return m, nil
			}
			return m, tea.Quit
		case "tab":
			if m.state == historyDetail {
				m.focusRight = !m.focusRight
				if m.focusRight {
					meta, sections := m.detailSections()
					rows := m.detailPanelRows(meta, sections)
					m.ensureRightCursorVisible(rows)
				}
				return m, nil
			}
		case "left":
			if m.state == historyDetail {
				if m.focusRight {
					m.focusRight = false
				} else if item, ok := m.currentItem(); ok && item.isServer && m.isServerExpanded(item.server) {
					m.expanded[item.server] = false
				}
				return m, nil
			}
		case "right":
			if m.state == historyDetail {
				if item, ok := m.currentItem(); ok && item.isServer {
					if m.isServerExpanded(item.server) {
						m.focusRight = true
					} else {
						m.expanded[item.server] = true
					}
				} else {
					m.focusRight = true
				}
				meta, sections := m.detailSections()
				rows := m.detailPanelRows(meta, sections)
				m.ensureRightCursorVisible(rows)
				return m, nil
			}
		case "pgup", "pgdown", "home", "end":
			if m.state == historyDetail {
				if m.focusRight {
					meta, sections := m.detailSections()
					rows := m.detailPanelRows(meta, sections)
					page := max(m.detailRows()-1, 1)
					switch msg.String() {
					case "pgup":
						m.rightCursor = max(m.rightCursor-page, 0)
					case "pgdown":
						m.rightCursor = min(m.rightCursor+page, max(len(rows)-1, 0))
					case "home":
						m.rightCursor = 0
					case "end":
						m.rightCursor = max(len(rows)-1, 0)
					}
					m.ensureRightCursorVisible(rows)
				} else {
					page := max(m.detailRows()-1, 1)
					switch msg.String() {
					case "pgup":
						m.moveLeftCursor(-page)
					case "pgdown":
						m.moveLeftCursor(page)
					case "home":
						m.moveLeftCursor(-len(m.visibleItems()))
					case "end":
						m.moveLeftCursor(len(m.visibleItems()))
					}
				}
				return m, nil
			}
		case "up", "k":
			if m.state == historyList {
				if m.cursor > 0 {
					m.cursor--
					if m.cursor < m.offset {
						m.offset = m.cursor
					}
				}
				return m, nil
			}
			if m.state == historyDetail {
				if m.focusRight {
					m.moveRightCursor(-1)
				} else {
					m.moveLeftCursor(-1)
				}
			}
			return m, nil
		case "down", "j":
			if m.state == historyList {
				if m.cursor < len(m.runs)-1 {
					m.cursor++
					visible := m.listRows()
					if m.cursor >= m.offset+visible {
						m.offset = m.cursor - visible + 1
					}
				}
				return m, nil
			}
			if m.state == historyDetail {
				if m.focusRight {
					m.moveRightCursor(1)
				} else {
					m.moveLeftCursor(1)
				}
			}
			return m, nil
		case "enter", " ":
			if m.state == historyList {
				if m.cursor < len(m.runs) {
					return m, func() tea.Msg { return m.loadRun(m.runs[m.cursor].ID) }
				}
				return m, nil
			}
			if m.state == historyDetail {
				if m.focusRight {
					return m, m.toggleRightSection()
				}
				if item, ok := m.currentItem(); ok {
					if item.isServer {
						m.expanded[item.server] = !m.isServerExpanded(item.server)
						return m, nil
					}
					m.focusRight = true
					return m, nil
				}
			}
			return m, nil
		case "y", "c":
			if m.state == historyDetail && m.cursor >= 0 && m.cursor < len(m.items) {
				it := &m.items[m.cursor]
				if !it.isServer && m.run != nil && it.taskIdx >= 0 && it.taskIdx < len(m.run.Tasks) {
					td := m.run.Tasks[it.taskIdx]
					key := td.Server + "\x00" + td.Name
					if !m.loadedTasks[key] {
						m.pendingCopy = true
						sectionKey := "task:" + it.server + "\x00" + td.Name + ":command"
						m.sectionsOpen[sectionKey] = true
						return m, func() tea.Msg { return m.loadTask(m.run.ID, td.Server, td.Name) }
					}
					if td.Command != "" {
						m.status = "copied command to clipboard"
						return m, tea.Batch(tea.SetClipboard(td.Command), tea.Tick(2*time.Second, func(time.Time) tea.Msg { return historyCopyClearMsg{} }))
					}
				}
			}
			return m, nil
		case "d":
			if m.state == historyList && m.cursor < len(m.runs) {
				id := m.runs[m.cursor].ID
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = m.st.DeleteRun(ctx, id)
					msg := m.loadRuns()
					return msg
				}
			}
			if m.state == historyDetail && m.run != nil {
				id := m.run.ID
				return m, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = m.st.DeleteRun(ctx, id)
					m.state = historyList
					m.run = nil
					m.items = nil
					return m.loadRuns()
				}
			}
		case "t":
			if m.state == historyList {
				m.askKeep = true
				m.keepInput = "10"
				return m, nil
			}
			if m.state == historyDetail {
				if m.focusRight {
					return m, m.toggleRightSection()
				}
				if item, ok := m.currentItem(); ok && item.isServer {
					m.expanded[item.server] = !m.isServerExpanded(item.server)
					return m, nil
				}
				m.focusRight = true
				return m, nil
			}
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

// View draws the opened run, or the list of runs.
func (m historyModel) View() tea.View {
	if !m.helpOpen && m.state == historyDetail && m.run != nil {
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#7C3AED")).Padding(0, 1)
		mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22C55E"))
		failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
		skipStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
		timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Bold(true)
		return m.historyDetailView(titleStyle, mutedStyle, okStyle, failStyle, skipStyle, timeStyle)
	}
	return m.listView()
}

// listView draws the help, the list of runs, or "loading" while a run is fetched.
func (m historyModel) listView() tea.View {
	if m.helpOpen {
		var b strings.Builder
		b.WriteString(titleStyle.Render("gorun history") + "\n\n")
		b.WriteString("Keyboard shortcuts (ctrl-k to close)\n\n")
		for _, line := range []string{
			"↑/↓, j/k       move in the focused pane",
			"tab            switch between the server list and details",
			"enter, space   expand a server or toggle a detail section",
			"pgup/pgdn      page through the focused pane",
			"y, c           copy the selected task command",
			"d              delete run",
			"t              keep N runs",
			"esc, q         back / quit",
			"ctrl-k         show/hide this help",
		} {
			b.WriteString(mutedStyle.Render(line) + "\n")
		}
		v := tea.NewView(b.String())
		return v
	}
	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#7C3AED")).Padding(0, 1)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22C55E"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
	skipStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Bold(true)

	if m.state == historyList {
		b.WriteString(titleStyle.Render("gorun history") + "\n\n")
		if m.err != nil {
			b.WriteString(failStyle.Render("error: ") + m.err.Error() + "\n")
		} else if len(m.runs) == 0 {
			b.WriteString(mutedStyle.Render("No runs recorded yet.") + "\n")
		} else {
			visible := m.listRows()
			start := m.offset
			if start < 0 {
				start = 0
			}
			if m.cursor < start {
				start = m.cursor
			}
			if m.cursor >= start+visible {
				start = m.cursor - visible + 1
			}
			end := start + visible
			if end > len(m.runs) {
				end = len(m.runs)
			}
			for i := start; i < end; i++ {
				r := m.runs[i]
				cursor := "  "
				selected := i == m.cursor
				if selected {
					cursor = cursorStyle.Render("▶ ")
				}
				status := mutedStyle.Render("● running")
				if r.Finished {
					if r.Success {
						status = okStyle.Render("✓ success")
					} else {
						status = failStyle.Render("✗ failed")
					}
				}
				line := fmt.Sprintf("%s%s %s %s", cursor, status, shortID(r.ID), r.Title)
				if r.Servers > 0 {
					line += mutedStyle.Render(fmt.Sprintf("  [%d servers]", r.Servers))
				}
				if r.Finished {
					failedStyle := mutedStyle
					if r.Failed > 0 {
						failedStyle = failStyle
					}
					line += mutedStyle.Render(" (") + timeStyle.Render(fmt.Sprintf("%.1fs", r.Duration.Seconds())) + failedStyle.Render(fmt.Sprintf(", %d failed)", r.Failed))
				} else {
					if r.Failed > 0 {
						line += failStyle.Render(fmt.Sprintf(" (%d failed)", r.Failed))
					} else {
						line += mutedStyle.Render(fmt.Sprintf(" (%d failed)", r.Failed))
					}
				}
				if isLocalHost(r.Hostname) {
					line += mutedStyle.Render("  [local]")
				} else if r.Hostname != "" {
					line += skipStyle.Render(fmt.Sprintf("  [%s]", r.Hostname))
				}
				if selected {
					line = highlightSelectedRow(line, m.width)
				}
				b.WriteString(line + "\n")
				b.WriteString("    " + mutedStyle.Render(r.StartedAt.Format("2006-01-02 15:04:05")) + "\n\n")
			}
			if len(m.runs) > visible {
				b.WriteString(mutedStyle.Render(fmt.Sprintf("    [%d/%d]", m.cursor+1, len(m.runs))) + "\n")
			}
		}
		if m.askKeep {
			b.WriteString("\n" + mutedStyle.Render("Keep how many recent runs? ") + m.keepInput + mutedStyle.Render("_ (enter confirm, esc cancel)"))
		} else {
			help := "↑/↓ move · enter view · d delete · t keep N · q quit"
			if m.embedded {
				help = "↑/↓ move · enter view · d delete · t keep N · esc/q back to servers · shift+tab switch"
			}
			b.WriteString("\n" + mutedStyle.Render(help))
		}
		v := tea.NewView(b.String())
		return v
	}

	// a run's detail is drawn by historyDetailView once the run has loaded
	b.WriteString(titleStyle.Render("gorun history") + "\n\n")
	b.WriteString(mutedStyle.Render("loading...") + "\n")
	return tea.NewView(b.String())
}

// historyDetailView draws an opened run: servers and tasks on the left, the selected one's details on the right.
func (m historyModel) historyDetailView(title, muted, ok, fail, skip, timing lipgloss.Style) tea.View {
	var b strings.Builder
	run := m.run
	started := run.StartedAt.Format("2006-01-02 15:04:05")
	status := fail.Render("✗ failed")
	if !run.Finished {
		status = skip.Render("● running")
	} else if run.Success {
		status = ok.Render("✓ success")
	}
	meta := fmt.Sprintf("%s · %s · %d tasks · %d failed · %d skipped · %s", started, status, run.TotalTasks, run.Failed, run.Skipped, run.Duration.Round(time.Millisecond))
	b.WriteString(truncateANSI(title.Render("gorun history")+"  "+muted.Render(shortID(run.ID)), m.width) + "\n")
	b.WriteString(truncateANSI(lipgloss.NewStyle().Bold(true).Render(run.Title)+"  "+muted.Render(meta), m.width) + "\n\n")

	leftWidth := max(m.width/3, 24)
	if leftWidth > m.width-18 {
		leftWidth = max(m.width/2, 1)
	}
	rightWidth := max(m.width-leftWidth, 1)
	bodyHeight := max(m.height-7, 3)
	leftInner, rightInner := max(leftWidth-2, 0), max(rightWidth-2, 0)

	visible := m.visibleItems()
	leftOffset := max(0, min(m.leftOffset, max(len(visible)-bodyHeight, 0)))
	leftLines := make([]string, 0, bodyHeight)
	for i := leftOffset; i < min(leftOffset+bodyHeight, len(visible)); i++ {
		idx := visible[i]
		item := m.items[idx]
		pointer := "  "
		selected := idx == m.cursor
		if selected {
			pointer = cursorStyle.Render("▶ ")
		}
		line := ""
		if item.isServer {
			arrow := "▸"
			if m.isServerExpanded(item.server) {
				arrow = "▾"
			}
			state := ""
			if msg := run.ServerErrors[item.server]; msg != "" {
				state = fail.Render("  ✗")
			}
			line = pointer + arrow + " " + serverStyle.Render(item.server) + state
		} else if item.taskIdx >= 0 && item.taskIdx < len(run.Tasks) {
			task := run.Tasks[item.taskIdx]
			mark := muted.Render("○")
			switch task.State {
			case 2:
				mark = ok.Render("✓")
			case 3:
				mark = fail.Render("✗")
			case 4:
				mark = skip.Render("⊘")
			case 1:
				mark = skip.Render("●")
			}
			line = pointer + "  " + mark + " " + task.Name + timing.Render("  "+task.Duration.Round(time.Millisecond).String())
		}
		line = truncateANSI(line, leftInner)
		if selected && !m.focusRight {
			line = highlightSelectedRow(line, leftInner)
		}
		leftLines = append(leftLines, line)
	}
	if len(visible) == 0 {
		leftLines = append(leftLines, muted.Render("  no servers recorded"))
	}

	panelTitle := "Run defaults"
	if item, ok := m.currentItem(); ok {
		if item.isServer {
			panelTitle = "Server details"
		} else if item.taskIdx >= 0 && item.taskIdx < len(run.Tasks) {
			panelTitle = "Task details"
		}
	}
	metaLines, sections := m.detailSections()
	rows := m.detailPanelRows(metaLines, sections)
	rightOffset := max(0, min(m.rightOffset, max(len(rows)-bodyHeight, 0)))
	if len(rows) > bodyHeight {
		panelTitle += fmt.Sprintf(" · %d-%d/%d", rightOffset+1, min(rightOffset+bodyHeight, len(rows)), len(rows))
	}
	rightLines := make([]string, 0, bodyHeight)
	for i := rightOffset; i < min(rightOffset+bodyHeight, len(rows)); i++ {
		line := truncateANSI(rows[i].text, rightInner)
		if m.focusRight && i == m.rightCursor {
			line = highlightSelectedRow(line, rightInner)
		}
		rightLines = append(rightLines, line)
	}
	if len(rightLines) == 0 {
		rightLines = append(rightLines, muted.Render("  select a server or task"))
	}

	left := paneBox("Servers and tasks", leftLines, leftWidth, bodyHeight, !m.focusRight)
	right := paneBox(panelTitle, rightLines, rightWidth, bodyHeight, m.focusRight)
	for i := range left {
		b.WriteString(left[i] + right[i] + "\n")
	}
	if m.status != "" {
		b.WriteString(ok.Render(m.status) + "\n")
	}
	footer := "↑/↓ move · →/tab details · enter expand server / inspect task · d delete · esc back"
	if m.focusRight {
		footer = "↑/↓ move/scroll · space/enter toggle section · pgup/pgdn page · tab/← servers · y copy command · esc back"
	}
	if m.embedded {
		footer = strings.TrimSuffix(footer, " · esc back") + " · esc/q back to runs"
	}
	b.WriteString(muted.Render(footer))
	return tea.NewView(b.String())
}

// shortID returns the start of a run ID, enough to tell runs apart in a list.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// getHistoryDBPath is the history database the CLI uses: GORUN_DB_PATH, else gorun.db in the
// working directory if there is one, else ~/.gorun/gorun.db. Its folder is created if needed.
func getHistoryDBPath() string {
	if os.Getenv("GORUN_DB_PATH") == "" {
		if _, err := os.Stat("gorun.db"); err == nil {
			return "gorun.db"
		}
	}
	path := history.DefaultPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	return path
}

// runHistoryTUI runs the history screen for -history.
func runHistoryTUI() error {
	st, err := store.Open(getHistoryDBPath())
	if err != nil {
		return err
	}
	defer st.Close()
	p := tea.NewProgram(newHistoryModel(st), tea.WithEnvironment(append(os.Environ(), "SSH_TTY=1")))
	_, err = p.Run()
	return err
}
