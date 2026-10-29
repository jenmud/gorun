package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"gorun/internal/store"
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
	state     historyState
	st        *store.Store
	runs      []store.RunSummary
	cursor    int
	offset    int
	width     int
	height    int
	run       *store.RunDetail
	items     []historyItem
	err       error
	askKeep   bool
	keepInput string
}

func newHistoryModel(st *store.Store) historyModel {
	return historyModel{state: historyList, st: st, width: 80, height: 24}
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
}

func (m historyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		visible := m.height - 14
		if visible < 3 {
			visible = 3
		}
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
			m.buildItemsForRun(m.run)
			m.state = historyDetail
		}
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
		case "ctrl+c", "q":
			if m.state == historyDetail {
				m.state = historyList
				m.run = nil
				m.items = nil
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.state == historyDetail {
				m.state = historyList
				m.run = nil
				m.items = nil
				return m, nil
			}
			return m, tea.Quit
		case "up", "k":
			if m.state == historyList {
				if m.cursor > 0 {
					m.cursor--
					visible := m.height - 10
					if visible < 5 {
						visible = 5
					}
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
					vis := false
					for _, si := range m.items {
						if si.isServer && si.server == it.server && si.expanded {
							vis = true
							break
						}
					}
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
					visible := m.height - 10
					if visible < 5 {
						visible = 5
					}
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
					vis := false
					for _, si := range m.items {
						if si.isServer && si.server == it.server && si.expanded {
							vis = true
							break
						}
					}
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
					return m, nil
				}
				it.expanded = !it.expanded
				return m, nil
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
					return m, nil
				}
				it.expanded = !it.expanded
				return m, nil
			}
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

func (m historyModel) View() tea.View {
	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#7C3AED")).Padding(0, 1)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22C55E"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
	skipStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))

	if m.state == historyList {
		b.WriteString(titleStyle.Render("gorun history") + "\n\n")
		if m.err != nil {
			b.WriteString(failStyle.Render("error: ") + m.err.Error() + "\n")
		} else if len(m.runs) == 0 {
			b.WriteString(mutedStyle.Render("No runs recorded yet.") + "\n")
		} else {
			visible := m.height - 14
			if visible < 3 {
				visible = 3
			}
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
				if i == m.cursor {
					cursor = "> "
				}
				status := mutedStyle.Render("● running")
				if r.Finished {
					if r.Success {
						status = okStyle.Render("✓ success")
					} else {
						status = failStyle.Render("✗ failed")
					}
				}
				line := fmt.Sprintf("%s%s %s %s", cursor, status, r.ID[:8], r.Title)
				if r.Servers > 0 {
					line += mutedStyle.Render(fmt.Sprintf("  [%d servers]", r.Servers))
				}
				if r.Finished {
					line += mutedStyle.Render(fmt.Sprintf(" (%.1fs, %d failed)", r.Duration.Seconds(), r.Failed))
				} else {
					line += mutedStyle.Render(fmt.Sprintf(" (%d failed)", r.Failed))
				}
				if isLocalHost(r.Hostname) {
					line += mutedStyle.Render("  [local]")
				} else if r.Hostname != "" {
					line += skipStyle.Render(fmt.Sprintf("  [%s]", r.Hostname))
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
		b.WriteString("\n" + mutedStyle.Render("↑/↓ move · enter view · d delete · t keep N · q quit"))
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
		b.WriteString(skipStyle.Render("● running") + mutedStyle.Render(fmt.Sprintf(" · %d failed", run.Failed)) + "\n\n")
	} else if run.Success {
		b.WriteString(okStyle.Render("✓ success") + mutedStyle.Render(fmt.Sprintf(" · %d tasks, %d failed, %d skipped (%.1fs)", run.TotalTasks, run.Failed, run.Skipped, run.Duration.Seconds())) + "\n\n")
	} else {
		b.WriteString(failStyle.Render("✗ failed") + mutedStyle.Render(fmt.Sprintf(" · %d tasks, %d failed, %d skipped (%.1fs)", run.TotalTasks, run.Failed, run.Skipped, run.Duration.Seconds())) + "\n\n")
	}

	// build visible lines
	var lines []string
	for _, it := range m.items {
		if it.isServer {
			cursor := "  "
			// find index
			for i, ii := range m.items {
				if ii == it {
					if i == m.cursor {
						cursor = "> "
					}
					break
				}
			}
			arrow := "▶"
			if it.expanded {
				arrow = "▼"
			}
			// compute done/total and runtime
			total := 0
			done := 0
			var dur time.Duration
			for _, ii := range m.items {
				if ii.isServer || ii.taskIdx < 0 || ii.taskIdx >= len(run.Tasks) || ii.server != it.server {
					continue
				}
				total++
				if run.Tasks[ii.taskIdx].State == 2 {
					done++
				}
				dur += run.Tasks[ii.taskIdx].Duration
			}


			serverFailed := false
			for _, t := range run.Tasks {
				if t.Server == it.server && t.State == 3 {
					serverFailed = true
					break
				}
			}
			serverStyle := lipgloss.NewStyle().Bold(true)
			if serverFailed {
				serverStyle = serverStyle.Foreground(lipgloss.Color("#EF4444"))
			}
			srvLine := cursor + arrow + " " + serverStyle.Render(it.server)
			if total > 0 {
				srvLine += mutedStyle.Render(fmt.Sprintf(" %d/%d", done, total))
			}
			if dur > 0 {
				srvLine += mutedStyle.Render(fmt.Sprintf(" %.1fs", dur.Seconds()))
			}
			lines = append(lines, srvLine)
			continue
		}
		vis := false
		for _, si := range m.items {
			if si.isServer && si.server == it.server && si.expanded {
				vis = true
				break
			}
		}
		if !vis {
			continue
		}
		if it.taskIdx < 0 || it.taskIdx >= len(run.Tasks) {
			continue
		}
		t := run.Tasks[it.taskIdx]
		cursor := "  "
		for i, ii := range m.items {
			if ii == it {
				if i == m.cursor {
					cursor = "> "
				}
				break
			}
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
		if t.Duration > 0 {
			line += mutedStyle.Render(fmt.Sprintf(" %.1fs", t.Duration.Seconds()))
		}
		if !it.expanded {
			line += mutedStyle.Render(" [expand]")
		}
		lines = append(lines, line)
		if it.expanded {
			if t.Command != "" {
				lines = append(lines, mutedStyle.Render("      command"))
				lines = append(lines, "        $ "+strings.ReplaceAll(t.Command, "\n", " "))
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
	b.WriteString("\n" + mutedStyle.Render("↑/↓ move · enter expand task · d delete run · esc back · q quit"))
	v := tea.NewView(b.String())
	return v
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
