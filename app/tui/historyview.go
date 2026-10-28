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

type historyState int

const (
	historyList historyState = iota
	historyDetail
)

type historyItem struct {
	isServer bool
	server   string
	task     *store.TaskDetail
	expanded bool
	showCmd  bool
	showOut  bool
	showErr  bool
}

type historyModel struct {
	state    historyState
	st       *store.Store
	runs     []store.RunSummary
	cursor   int
	offset   int
	width    int
	height   int
	run      *store.RunDetail
	items    []historyItem
	err      error
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
	m2 := &historyModel{}
	m2.run = run
	m2.buildItemsForRun(run)
	return historyRunMsg{run: run, err: nil}
}

func (m *historyModel) buildItems() {
	m.buildItemsForRun(m.run)
}

func (m *historyModel) buildItemsForRun(run *store.RunDetail) {
	items := []historyItem{}
	if run == nil {
		m.items = items
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
				t := run.Tasks[i]
				items = append(items, historyItem{isServer: false, server: srv, task: &t, expanded: false, showCmd: false, showOut: false, showErr: false})
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
		m.runs = msg.runs
		m.err = msg.err
		if len(m.runs) > 0 && m.cursor >= len(m.runs) {
			m.cursor = len(m.runs) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.offset = 0
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
					serverExpanded := false
					for _, si := range m.items {
						if si.isServer && si.server == it.server && si.expanded {
							serverExpanded = true
							break
						}
					}
					if serverExpanded {
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
				// skip non-visible items (tasks under collapsed servers)
				for m.cursor < len(m.items)-1 {
					m.cursor++
					if m.cursor >= len(m.items) {
						break
					}
					it := m.items[m.cursor]
					if it.isServer {
						break
					}
					// check if server expanded
					serverExpanded := false
					for _, si := range m.items {
						if si.isServer && si.server == it.server && si.expanded {
							serverExpanded = true
							break
						}
					}
					if serverExpanded {
						break
					}
					// else skip
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
				if !it.expanded {
					it.expanded = true
					it.showCmd = true
					it.showOut = it.task != nil && it.task.Stdout != ""
					it.showErr = it.task != nil && it.task.Stderr != ""
					if !it.showErr && it.task != nil && it.task.Stderr == "" && it.task.State == 3 {
						it.showErr = true
					}
					return m, nil
				}
				if it.showCmd {
					it.showCmd = false
					if it.task != nil && it.task.Stdout != "" {
						it.showOut = true
						return m, nil
					}
					if it.task != nil && it.task.Stderr != "" {
						it.showErr = true
						return m, nil
					}
					it.expanded = false
					return m, nil
				}
				if it.showOut {
					it.showOut = false
					if it.task != nil && it.task.Stderr != "" {
						it.showErr = true
						return m, nil
					}
					it.expanded = false
					return m, nil
				}
				if it.showErr {
					it.showErr = false
					it.expanded = false
					return m, nil
				}
				it.expanded = false
				it.showCmd = false
				it.showOut = false
				it.showErr = false
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
			visible := m.height - 12
			if visible < 3 {
				visible = 3
			}
			start := m.offset
			if start < 0 {
				start = 0
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
				if r.Finished {
					line += mutedStyle.Render(fmt.Sprintf(" (%ds, %d failed)", int(r.Duration.Seconds()), r.Failed))
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
		b.WriteString("\n" + mutedStyle.Render("↑/↓ move · enter view · q quit"))
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
		b.WriteString(okStyle.Render("✓ success") + mutedStyle.Render(fmt.Sprintf(" · %d tasks, %d failed, %d skipped (%ds)", run.TotalTasks, run.Failed, run.Skipped, int(run.Duration.Seconds()))) + "\n\n")
	} else {
		b.WriteString(failStyle.Render("✗ failed") + mutedStyle.Render(fmt.Sprintf(" · %d tasks, %d failed, %d skipped (%ds)", run.TotalTasks, run.Failed, run.Skipped, int(run.Duration.Seconds()))) + "\n\n")
	}

	for i, it := range m.items {
		if it.isServer {
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}
			arrow := "▶"
			if it.expanded {
				arrow = "▼"
			}
			b.WriteString(cursor + arrow + " " + lipgloss.NewStyle().Bold(true).Render(it.server) + "\n")
			continue
		}
		serverExpanded := false
		for _, si := range m.items {
			if si.isServer && si.server == it.server && si.expanded {
				serverExpanded = true
				break
			}
		}
		if !serverExpanded {
			continue
		}
		if it.task == nil {
			continue
		}
		t := it.task
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		var mark string
		if t.State == 2 {
			mark = okStyle.Render("  ✓")
		} else if t.State == 3 {
			mark = failStyle.Render("  ✗")
		} else if t.State == 4 {
			mark = skipStyle.Render("  ⊘")
		} else if t.State == 1 {
			mark = skipStyle.Render("  ●")
		} else {
			mark = mutedStyle.Render("  ○")
		}
		line := cursor + mark + " " + t.Name
		if t.ExitCode != 0 {
			line += failStyle.Render(fmt.Sprintf(" (exit %d)", t.ExitCode))
		}
		if t.Duration > 0 {
			line += mutedStyle.Render(fmt.Sprintf(" %ds", int(t.Duration.Seconds())))
		}
		if !it.expanded {
			line += mutedStyle.Render("  [expand]")
		} else {
			line += mutedStyle.Render("  [details]")
		}
		b.WriteString(line + "\n")
		if it.expanded && it.showCmd && t.Command != "" {
			b.WriteString(mutedStyle.Render("      $ ") + strings.ReplaceAll(t.Command, "\n", " ") + "\n")
		}
		if it.expanded && it.showOut && t.Stdout != "" {
			b.WriteString(mutedStyle.Render("      stdout:") + "\n")
			for _, l := range strings.Split(strings.TrimRight(t.Stdout, "\n"), "\n") {
				b.WriteString(mutedStyle.Render("        ") + l + "\n")
			}
		}
		if it.expanded && it.showErr && t.Stderr != "" {
			b.WriteString(failStyle.Render("      stderr:") + "\n")
			for _, l := range strings.Split(strings.TrimRight(t.Stderr, "\n"), "\n") {
				b.WriteString(failStyle.Render("        ") + l + "\n")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\n" + mutedStyle.Render("↑/↓ move · enter expand/cycle · esc back · q quit"))
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
	// try common locations
	if _, err := os.Stat("gorun.db"); err == nil {
		return "gorun.db"
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".gorun", "gorun.sqlite")
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
