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

type historyModel struct {
	expanded    map[string]bool
	loadedTasks map[string]bool
	pendingCopy bool
	listCursor  int
	status      string
	helpOpen    bool
	state       historyState
	st          *store.Store
	runs        []store.RunSummary
	cursor      int
	offset      int
	width       int
	height      int
	run         *store.RunDetail
	items       []historyItem
	err         error
	askKeep     bool
	keepInput   string

	// embedded is set when the history is a tab of the server picker, where esc and q
	// on the runs list go back to the servers instead of quitting
	embedded bool
}

func newHistoryModel(st *store.Store) historyModel {
	return historyModel{state: historyList, st: st, width: 80, height: 24, expanded: map[string]bool{}, loadedTasks: map[string]bool{}}
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
		for i := range run.Tasks {
			if run.Tasks[i].Server == srv {
				items = append(items, historyItem{isServer: false, server: srv, taskIdx: i, expanded: false})
			}
		}
	}
	m.items = items
	m.cursor = 0
	m.offset = 0
	m.expanded = map[string]bool{}
	m.loadedTasks = map[string]bool{}
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
				for m.cursor > 0 {
					m.cursor--
					if m.cursor < 0 {
						break
					}
					it := m.items[m.cursor]
					if it.isServer {
						break
					}
					vis := m.isServerExpanded(it.server)
					if vis {
						break
					}
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
				for m.cursor < len(m.items)-1 {
					m.cursor++
					it := m.items[m.cursor]
					if it.isServer {
						break
					}
					vis := m.isServerExpanded(it.server)
					if vis {
						break
					}
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
			if m.state == historyDetail && m.cursor < len(m.items) {
				it := &m.items[m.cursor]
				if it.isServer {
					it.expanded = !it.expanded
					m.expanded[it.server] = it.expanded
					return m, nil
				}
				it.expanded = !it.expanded
				if it.expanded && m.run != nil && it.taskIdx >= 0 && it.taskIdx < len(m.run.Tasks) {
					td := m.run.Tasks[it.taskIdx]
					key := td.Server + "\x00" + td.Name
					if !m.loadedTasks[key] {
						return m, func() tea.Msg { return m.loadTask(m.run.ID, td.Server, td.Name) }
					}
				}
				return m, nil
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
						it.expanded = true
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
			if m.state == historyDetail && m.cursor < len(m.items) {
				it := &m.items[m.cursor]
				if it.isServer {
					it.expanded = !it.expanded
					m.expanded[it.server] = it.expanded
					return m, nil
				}
				it.expanded = !it.expanded
				if it.expanded && m.run != nil && it.taskIdx >= 0 && it.taskIdx < len(m.run.Tasks) {
					td := m.run.Tasks[it.taskIdx]
					key := td.Server + "\x00" + td.Name
					if !m.loadedTasks[key] {
						return m, func() tea.Msg { return m.loadTask(m.run.ID, td.Server, td.Name) }
					}
				}
				return m, nil
			}
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

func (m historyModel) View() tea.View {
	if m.helpOpen {
		var b strings.Builder
		b.WriteString(titleStyle.Render("gorun history") + "\n\n")
		b.WriteString("Keyboard shortcuts (ctrl-k to close)\n\n")
		for _, line := range []string{
			"↑/↓, j/k       move",
			"enter, space   expand/collapse task",
			"y, c           yank/copy command to clipboard",
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
			if msg := run.ServerErrors[it.server]; msg != "" && it.expanded {
				lines = append(lines, "  "+failStyle.Render("✗ "+msg))
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
	cursorLine := 0
	for idx, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			cursorLine = idx
		}
	}
	start := 0
	if len(lines) > visible {
		start = cursorLine - visible/2
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
