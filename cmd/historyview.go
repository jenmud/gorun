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

	"github.com/jenmud/gorun/internal/store"
)

type historyState int

const (
	historyList historyState = iota
	historyDetail
)

type historyItem struct {
	isServer bool
	server   string
	taskIdx  int
	expanded bool
}

type historySection struct {
	key   string
	title string
	lines []string
}

type historyPanelRow struct {
	text        string
	section     int
	sectionHead bool
}

type historyModel struct {
	expanded         map[string]bool
	loadedTasks      map[string]bool
	sectionsOpen     map[string]bool
	pendingCopy      bool
	listCursor       int
	status           string
	helpOpen         bool
	state            historyState
	st               *store.Store
	runs             []store.RunSummary
	cursor           int
	offset           int
	leftOffset       int
	rightOffset      int
	rightCursor      int
	focusRight       bool
	detailScroll     int
	detailCursorLine *int
	detailLineCount  *int
	width            int
	height           int
	run              *store.RunDetail
	items            []historyItem
	err              error
	askKeep          bool
	keepInput        string

	// embedded is set when the history is a tab of the server picker, where esc and q
	// on the runs list go back to the servers instead of quitting
	embedded bool
}

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

func (m historyModel) Init() tea.Cmd {
	return func() tea.Msg { return m.loadRuns() }
}

type historyRunsMsg struct {
	runs []store.RunSummary
	err  error
}

type historyRunMsg struct {
	run *store.RunDetail
	err error
}
type historyTaskMsg struct {
	task store.TaskDetail
	err  error
}

type historyCopyClearMsg struct{}

// Aliases for callers that use other names.
type historyTaskDetailMsg = historyTaskMsg
type historyTaskLoadedMsg = historyTaskMsg
type taskDetailMsg = historyTaskMsg

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

func (m historyModel) loadRuns() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runs, err := m.st.ListRuns(ctx)
	return historyRunsMsg{runs: runs, err: err}
}

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

func (m historyModel) loadTask(runID, server, task string) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	td, err := m.st.GetTask(ctx, runID, server, task)
	return historyTaskMsg{task: td, err: err}
}
func (m historyModel) loadTaskDetail(runID, server, task string) tea.Msg {
	return m.loadTask(runID, server, task)
}
func (m historyModel) fetchTask(runID, server, task string) tea.Msg {
	return m.loadTask(runID, server, task)
}
func (m historyModel) loadTaskOutput(runID, server, task string) tea.Msg {
	return m.loadTask(runID, server, task)
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

func (m historyModel) visibleItems() []int {
	visible := make([]int, 0, len(m.items))
	for i, item := range m.items {
		if item.isServer || m.isServerExpanded(item.server) {
			visible = append(visible, i)
		}
	}
	return visible
}

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

func (m historyModel) detailRows() int {
	return max(m.height-8, 3)
}

func (m historyModel) currentItem() (historyItem, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return historyItem{}, false
	}
	return m.items[m.cursor], true
}

func (m historyModel) detailSections() ([]string, []historySection, []string) {
	item, ok := m.currentItem()
	if !ok || m.run == nil {
		return nil, nil, nil
	}
	runScope := "run:" + m.run.ID + ":"
	serverScope := "server:" + item.server + ":"
	sections := []historySection{}
	add := func(key, title string, values []string) {
		sections = append(sections, historySection{key: key, title: title, lines: values})
	}
	jsonLines := func(v map[string]any) []string { return varsTableLines(v, max(m.width/2-8, 8)) }
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
		return meta, sections, nil
	}
	if item.taskIdx < 0 || item.taskIdx >= len(m.run.Tasks) {
		return meta, sections, nil
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
	return meta, sections, nil
}

func outputLines(s string) []string {
	if s == "" {
		return []string{"(empty)"}
	}
	return strings.Split(strings.TrimRight(s, "\r\n"), "\n")
}

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

func (m historyModel) detailPanelRows(meta []string, sections []historySection) ([]historyPanelRow, []int) {
	rows := make([]historyPanelRow, 0, len(meta)+len(sections))
	for _, line := range meta {
		rows = append(rows, historyPanelRow{text: line, section: -1})
	}
	heads := make([]int, len(sections))
	for i, section := range sections {
		heads[i] = len(rows)
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
	return rows, heads
}

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

func (m *historyModel) moveRightCursor(delta int) {
	meta, sections, _ := m.detailSections()
	rows, _ := m.detailPanelRows(meta, sections)
	if len(rows) == 0 {
		m.rightCursor = 0
		m.rightOffset = 0
		return
	}
	m.rightCursor = max(0, min(m.rightCursor+delta, len(rows)-1))
	m.ensureRightCursorVisible(rows)
}

func (m *historyModel) toggleRightSection() tea.Cmd {
	meta, sections, _ := m.detailSections()
	if len(sections) == 0 {
		return nil
	}
	rows, _ := m.detailPanelRows(meta, sections)
	m.rightCursor = max(0, min(m.rightCursor, len(rows)-1))
	row := rows[m.rightCursor]
	if !row.sectionHead || row.section < 0 || row.section >= len(sections) {
		return nil
	}
	section := sections[row.section]
	m.sectionsOpen[section.key] = !m.sectionsOpen[section.key]
	rows, _ = m.detailPanelRows(meta, sections)
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
					meta, sections, _ := m.detailSections()
					rows, _ := m.detailPanelRows(meta, sections)
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
				meta, sections, _ := m.detailSections()
				rows, _ := m.detailPanelRows(meta, sections)
				m.ensureRightCursorVisible(rows)
				return m, nil
			}
		case "pgup", "pgdown", "home", "end":
			if m.state == historyDetail {
				if m.focusRight {
					meta, sections, _ := m.detailSections()
					rows, _ := m.detailPanelRows(meta, sections)
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
	return m.legacyView()
}

func (m historyModel) legacyView() tea.View {
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

	if m.run == nil {
		b.WriteString(titleStyle.Render("gorun history") + "\n\n")
		b.WriteString(mutedStyle.Render("loading...") + "\n")
		v := tea.NewView(b.String())
		return v
	}
	run := m.run

	b.WriteString(titleStyle.Render("gorun history") + "\n\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(run.Title) + " " + mutedStyle.Render(run.ID))
	if isLocalHost(run.Hostname) {
		b.WriteString(mutedStyle.Render("  [local]"))
	} else if run.Hostname != "" {
		b.WriteString(skipStyle.Render(fmt.Sprintf("  [%s]", run.Hostname)))
	}
	b.WriteString("\n")
	if !run.Finished {
		failedStyle := mutedStyle
		if run.Failed > 0 {
			failedStyle = failStyle
		}
		b.WriteString(skipStyle.Render("● running") + failedStyle.Render(fmt.Sprintf(" · %d failed", run.Failed)) + "\n\n")
	} else if run.Success {
		failedStyle := mutedStyle
		if run.Failed > 0 {
			failedStyle = failStyle
		}
		b.WriteString(okStyle.Render("✓ success") + mutedStyle.Render(fmt.Sprintf(" · %d tasks, ", run.TotalTasks)) + failedStyle.Render(fmt.Sprintf("%d failed", run.Failed)) + mutedStyle.Render(fmt.Sprintf(", %d skipped (", run.Skipped)) + timeStyle.Render(fmt.Sprintf("%.1fs", run.Duration.Seconds())) + mutedStyle.Render(")") + "\n\n")
	} else {
		failedStyle := mutedStyle
		if run.Failed > 0 {
			failedStyle = failStyle
		}
		b.WriteString(failStyle.Render("✗ failed") + mutedStyle.Render(fmt.Sprintf(" · %d tasks, ", run.TotalTasks)) + failedStyle.Render(fmt.Sprintf("%d failed", run.Failed)) + mutedStyle.Render(fmt.Sprintf(", %d skipped (", run.Skipped)) + timeStyle.Render(fmt.Sprintf("%.1fs", run.Duration.Seconds())) + mutedStyle.Render(")") + "\n\n")
	}

	// build visible lines
	var lines []string

	// precompute per-server aggregates so rendering is O(n) instead of O(n^2)
	type serverAgg struct {
		total  int
		done   int
		dur    time.Duration
		failed bool
	}
	agg := map[string]serverAgg{}
	for i := range run.Tasks {
		t := run.Tasks[i]
		a := agg[t.Server]
		a.total++
		if t.State == 2 {
			a.done++
		}
		a.dur += t.Duration
		if t.State == 3 {
			a.failed = true
		}
		agg[t.Server] = a
	}

	if len(run.RunEnvVars) > 0 {
		lines = append(lines, mutedStyle.Render("  run env vars"))
		for _, l := range envTableLines(run.RunEnvVars, m.width-4) {
			lines = append(lines, "  "+l)
		}
	}
	if len(run.RunVars) > 0 {
		lines = append(lines, mutedStyle.Render("  run vars"))
		for _, l := range varsTableLines(run.RunVars, m.width-4) {
			lines = append(lines, "  "+l)
		}
	}

	cursorLine := 0
	for idx, it := range m.items {
		if it.isServer {
			cursor := "  "
			selected := idx == m.cursor
			if selected {
				cursor = cursorStyle.Render("▶ ")
			}
			arrow := "▶"
			if it.expanded {
				arrow = "▼"
			}
			a := agg[it.server]
			total := a.total
			done := a.done
			dur := a.dur
			serverFailed := a.failed || run.ServerErrors[it.server] != ""
			serverStyle := lipgloss.NewStyle().Bold(true)
			if serverFailed {
				serverStyle = serverStyle.Foreground(lipgloss.Color("#EF4444"))
			}
			srvLine := cursor + arrow + " " + serverStyle.Render(it.server)
			if total > 0 {
				srvLine += mutedStyle.Render(fmt.Sprintf(" %d/%d", done, total))
			}
			if dur > 0 {
				srvLine += timeStyle.Render(fmt.Sprintf(" %.1fs", dur.Seconds()))
			}
			if selected {
				srvLine = highlightSelectedRow(srvLine, m.width)
			}
			lines = append(lines, srvLine)
			if selected {
				cursorLine = len(lines) - 1
			}
			if msg := run.ServerErrors[it.server]; msg != "" && it.expanded {
				lines = append(lines, "  "+failStyle.Render("✗ "+msg))
			}
			if it.expanded {
				if ev := run.ServerEnvVars[it.server]; len(ev) > 0 {
					lines = append(lines, mutedStyle.Render("  env vars"))
					for _, l := range envTableLines(ev, m.width-4) {
						lines = append(lines, "  "+l)
					}
				}
				if vv := run.ServerVars[it.server]; len(vv) > 0 {
					lines = append(lines, mutedStyle.Render("  vars"))
					for _, l := range varsTableLines(vv, m.width-4) {
						lines = append(lines, "  "+l)
					}
				}
			}
			continue
		}
		vis := m.isServerExpanded(it.server)
		if !vis {
			continue
		}
		if it.taskIdx < 0 || it.taskIdx >= len(run.Tasks) {
			continue
		}
		t := run.Tasks[it.taskIdx]
		cursor := "  "
		selected := idx == m.cursor
		if selected {
			cursor = cursorStyle.Render("▶ ")
		}
		var mark string
		if t.State == 2 {
			mark = okStyle.Render("✓")
		} else if t.State == 3 {
			mark = failStyle.Render("✗")
		} else if t.State == 4 {
			mark = skipStyle.Render("⊘")
		} else if t.State == 1 {
			mark = skipStyle.Render("●")
		} else {
			mark = mutedStyle.Render("○")
		}
		line := cursor + mark + " " + t.Name
		if (t.State == 3 || t.State == 4) && t.Stderr != "" {
			line += mutedStyle.Render(" — " + strings.SplitN(strings.TrimSpace(t.Stderr), "\n", 2)[0])
		}
		if t.Duration > 0 {
			line += timeStyle.Render(fmt.Sprintf(" %.1fs", t.Duration.Seconds()))
		}
		if !it.expanded {
			line += mutedStyle.Render(" [expand]")
		}
		if selected {
			line = highlightSelectedRow(line, m.width)
			cursorLine = len(lines)
		}
		lines = append(lines, line)
		if it.expanded {
			if t.Command != "" {
				// show the command as written, line by line, with later lines lined up
				// under the first; tabs would throw off the layout, so they become spaces
				lines = append(lines, mutedStyle.Render("      command"))
				for i, l := range strings.Split(strings.TrimRight(t.Command, "\r\n"), "\n") {
					prefix := "        $ "
					if i > 0 {
						prefix = "          "
					}
					lines = append(lines, prefix+strings.ReplaceAll(strings.TrimRight(l, "\r"), "\t", "    "))
				}
				lines = append(lines, mutedStyle.Render("        (y or c to copy)"))
			}
			if t.Stdout != "" {
				lines = append(lines, mutedStyle.Render("      stdout"))
				for _, l := range strings.Split(strings.TrimRight(t.Stdout, "\r\n"), "\n") {
					lines = append(lines, "        "+l)
				}
			}
			if t.Stderr != "" {
				lines = append(lines, failStyle.Render("      stderr"))
				for _, l := range strings.Split(strings.TrimRight(t.Stderr, "\r\n"), "\n") {
					lines = append(lines, "        "+l)
				}
			}
			if len(t.EnvVars) > 0 {
				lines = append(lines, mutedStyle.Render("      env vars"))
				width := m.width - 8
				for _, l := range envTableLines(t.EnvVars, width) {
					lines = append(lines, "        "+l)
				}
			}
			if len(t.Vars) > 0 {
				lines = append(lines, mutedStyle.Render("      vars"))
				for _, l := range varsTableLines(t.Vars, m.width-8) {
					lines = append(lines, "        "+l)
				}
			}
			if t.State == 3 && t.Stderr == "" {
				lines = append(lines, failStyle.Render("      stderr"))
				lines = append(lines, "        ")
			}
		}
	}

	visible := m.height - 12
	if visible < 3 {
		visible = 3
	}
	if m.detailCursorLine != nil {
		*m.detailCursorLine = cursorLine
	}
	if m.detailLineCount != nil {
		*m.detailLineCount = len(lines)
	}
	start := 0
	if m.detailScroll >= 0 {
		start = m.detailScroll
	} else if len(lines) > visible {
		start = cursorLine - visible/2
	}
	if len(lines) > visible {
		if start < 0 {
			start = 0
		}
		if start+visible > len(lines) {
			start = len(lines) - visible
		}
	}
	for i := start; i < start+visible && i < len(lines); i++ {
		b.WriteString(lines[i] + "\n")
	}
	if len(lines) > visible {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("    [%d/%d lines]", cursorLine+1, len(lines))) + "\n")
	}
	b.WriteString("\n")
	help := "↑/↓ move · enter expand task · y/c copy command · d delete run · ctrl-k help · esc back · q quit"
	if m.embedded {
		help = "↑/↓ move · enter expand task · d delete run · esc/q back to runs"
	}
	if m.status != "" {
		b.WriteString(okStyle.Render(m.status) + "\n")
	}
	b.WriteString("\n" + mutedStyle.Render(help))
	v := tea.NewView(b.String())
	return v
}

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
	metaLines, sections, _ := m.detailSections()
	rows, _ := m.detailPanelRows(metaLines, sections)
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

func getHistoryDBPath() string {
	if p := os.Getenv("GORUN_DB_PATH"); p != "" {
		if len(p) > 0 && p[0] == '~' {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[1:])
			}
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := os.Stat("gorun.db"); err == nil {
		return "gorun.db"
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".gorun", "gorun.db")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		dir := filepath.Join(home, ".gorun")
		_ = os.MkdirAll(dir, 0755)
		return p
	}
	return "gorun.db"
}

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
