package main

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/jenmud/gorun"
)

// the Servers tab's panes, which take the keyboard focus in turn
const (
	paneServers = iota
	paneTasks
)

var focusedBorderStyle = lipgloss.NewStyle().Foreground(colorAccent)

// detailIndent lines an expanded server's details up under its name.
const detailIndent = "        "

// detailLabelWidth is the width of the field names in a server's details.
const detailLabelWidth = 11

// detailRows lists an expanded server's details: its connection, groups, tags and, as a
// table that opens on request, its env vars. The password is never shown.
func (m serverSelectionModel) detailRows(s gorun.Server, match int) []selectionRow {
	none := mutedStyle.Render("—")
	orNone := func(v string) string {
		if v == "" {
			return none
		}
		return v
	}
	detail := func(label, value string) selectionRow {
		return selectionRow{kind: rowDetail, match: match, label: label, value: value}
	}

	rows := []selectionRow{detail("Name", s.Name), detail("Hostname", orNone(s.Hostname))}
	if s.IsLocal() {
		rows = append(rows, detail("SSH", mutedStyle.Render("none, runs locally")))
	} else {
		port := s.SSH.Port
		if port == 0 {
			port = 22
		}
		password := none
		if s.SSH.Password != "" {
			password = "********"
		}
		config := s.SSH.ConfigFile
		if config == "" && s.SSH.Config != "" {
			config = "inline"
		}
		rows = append(rows,
			detail("User", orNone(s.SSH.User)),
			detail("Port", strconv.Itoa(port)),
			detail("Password", password),
			detail("Identity", orNone(s.SSH.Identityfile)),
			detail("SSH config", orNone(config)),
		)
	}

	tags := make([]string, 0, len(s.Tags))
	for _, tag := range s.Tags {
		tags = append(tags, "#"+tag)
	}
	rows = append(rows, detail("Groups", orNone(strings.Join(s.Groups, ", "))), detail("Tags", orNone(strings.Join(tags, " "))))
	if s.LogLevel != "" {
		rows = append(rows, detail("Log level", s.LogLevel))
	}
	switch {
	case s.MaxTaskParallel < 0:
		rows = append(rows, detail("Parallel", "all tasks at once"))
	case s.MaxTaskParallel > 0:
		rows = append(rows, detail("Parallel", fmt.Sprintf("%d tasks at once", s.MaxTaskParallel)))
	}

	if len(s.EnvVars) == 0 {
		return append(rows, detail("Env vars", none))
	}
	rows = append(rows, selectionRow{kind: rowEnv, match: match})
	if m.envOpen[s.Name] {
		width := m.width/2 - 2 - len(detailIndent) - 2
		for _, line := range envTableLines(s.EnvVars, width) {
			rows = append(rows, selectionRow{kind: rowEnvVar, match: match, value: line})
		}
	}
	return rows
}

// envTableLines renders env as a two column table, sorted by name, at most width cells
// wide. Long values are cut first, then long names.
func envTableLines(env map[string]string, width int) []string {
	keys := slices.Sorted(maps.Keys(env))
	keyWidth, valueWidth := len("NAME"), len("VALUE")
	for _, k := range keys {
		keyWidth = max(keyWidth, lipgloss.Width(k))
		valueWidth = max(valueWidth, lipgloss.Width(env[k]))
	}

	// the borders and padding take 7 cells
	room := max(width-7, 6)
	if keyWidth+valueWidth > room {
		valueWidth = max(room-keyWidth, 3)
		keyWidth = max(min(keyWidth, room-valueWidth), 3)
	}

	pad := func(s string, w int) string {
		s = truncate(s, w)
		return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
	}
	rule := func(left, mid, right string) string {
		return mutedStyle.Render(left + strings.Repeat("─", keyWidth+2) + mid + strings.Repeat("─", valueWidth+2) + right)
	}
	bar := mutedStyle.Render("│")

	lines := []string{
		rule("┌", "┬", "┐"),
		bar + " " + headerStyle.Render(pad("NAME", keyWidth)) + " " + bar + " " + headerStyle.Render(pad("VALUE", valueWidth)) + " " + bar,
		rule("├", "┼", "┤"),
	}
	for _, k := range keys {
		lines = append(lines, bar+" "+serverStyle.Render(pad(k, keyWidth))+" "+bar+" "+pad(env[k], valueWidth)+" "+bar)
	}
	return append(lines, rule("└", "┴", "┘"))
}

// taskRow is one line of the tasks pane: a task, or in the all tasks view, a server's
// heading.
type taskRow struct {
	server int // index into serverSelectionModel.servers
	task   int // index into the server's Tasks, or -1 for its heading
}

// taskRows lists the tasks pane: the tasks of the server under the cursor or, in the
// all tasks view, the tasks of every matching server under a heading for each.
func (m serverSelectionModel) taskRows() []taskRow {
	rows := []taskRow{}
	if !m.allTasks {
		row, ok := m.currentRow()
		if !ok {
			return rows
		}
		if _, isServer := m.serverOfRow(row); !isServer {
			return rows
		}
		i := m.matches[row.match].index
		for t := range m.servers[i].Tasks {
			rows = append(rows, taskRow{server: i, task: t})
		}
		return rows
	}

	for _, match := range m.matches {
		if len(m.servers[match.index].Tasks) == 0 {
			continue
		}
		rows = append(rows, taskRow{server: match.index, task: -1})
		for t := range m.servers[match.index].Tasks {
			rows = append(rows, taskRow{server: match.index, task: t})
		}
	}
	return rows
}

// focusServer returns the server the focused pane is on: the one under the cursor in
// the servers pane, or the one the task cursor is in in the tasks pane.
func (m serverSelectionModel) focusServer() (gorun.Server, bool) {
	if m.focus == paneTasks {
		if rows := m.taskRows(); m.taskCursor >= 0 && m.taskCursor < len(rows) {
			return m.servers[rows[m.taskCursor].server], true
		}
	}
	return m.current()
}

// followServer points the tasks pane at the server under the cursor: its first task, or
// in the all tasks view, its heading. With no tasks to show, the focus goes back to the
// servers pane.
func (m *serverSelectionModel) followServer() {
	rows := m.taskRows()
	if len(rows) == 0 {
		m.taskCursor = 0
		m.focus = paneServers
		return
	}
	if !m.allTasks {
		m.taskCursor = 0
		return
	}
	if row, ok := m.currentRow(); ok {
		if _, isServer := m.serverOfRow(row); isServer {
			i := m.matches[row.match].index
			if j := slices.Index(rows, taskRow{server: i, task: -1}); j >= 0 {
				m.taskCursor = j
				return
			}
		}
	}
	m.taskCursor = max(min(m.taskCursor, len(rows)-1), 0)
}

// moveTaskCursor moves the task cursor by delta rows.
func (m *serverSelectionModel) moveTaskCursor(delta int) {
	m.taskCursor = max(min(m.taskCursor+delta, len(m.taskRows())-1), 0)
}

// taskOffset is the index of the first task row shown in a pane of height rows, keeping
// the task cursor in view.
func (m serverSelectionModel) taskOffset(rows int) int {
	n := len(m.taskRows())
	if n <= rows {
		return 0
	}
	return min(max(m.taskCursor-rows+1, 0), n-rows)
}

// toggleTaskRow toggles a task or, on a server's heading, all of its tasks.
func (m *serverSelectionModel) toggleTaskRow(row taskRow) {
	s := m.servers[row.server]
	if row.task < 0 {
		m.setAllTasks(s, m.countTasks(s) < len(s.Tasks))
		return
	}
	m.toggleTask(s, s.Tasks[row.task].Name)
}

// updateTasksPane handles the keys that act on the tasks pane while it has the focus.
// It reports false for keys that work the same in both panes.
func (m serverSelectionModel) updateTasksPane(msg tea.KeyPressMsg) (serverSelectionModel, bool) {
	switch msg.String() {
	case "up":
		m.moveTaskCursor(-1)
	case "down":
		m.moveTaskCursor(1)
	case "space":
		if rows := m.taskRows(); m.taskCursor >= 0 && m.taskCursor < len(rows) {
			m.toggleTaskRow(rows[m.taskCursor])
		}
	case "tab", "left":
		m.focus = paneServers
	case "right":
		// already in the rightmost pane
	default:
		return m, false
	}
	return m, true
}

// tasksPaneTitle names what the tasks pane shows.
func (m serverSelectionModel) tasksPaneTitle() string {
	if m.allTasks {
		return "All tasks"
	}
	if row, ok := m.currentRow(); ok {
		if s, isServer := m.serverOfRow(row); isServer && len(s.Tasks) > 0 {
			return fmt.Sprintf("Tasks · %s · %d/%d", s.Name, m.countTasks(s), len(s.Tasks))
		}
	}
	return "Tasks"
}

// tasksPaneLines renders the visible rows of the tasks pane, width cells wide at most.
func (m serverSelectionModel) tasksPaneLines(height, width int) []string {
	rows := m.taskRows()
	if len(rows) == 0 {
		msg := "move to a server to see its tasks"
		if s, ok := m.current(); ok && !m.allTasks {
			msg = s.Name + " has no tasks"
		} else if m.allTasks {
			msg = "no tasks"
		}
		return []string{mutedStyle.Render("  " + msg)}
	}

	lines := []string{}
	offset := m.taskOffset(height)
	for j := offset; j < min(offset+height, len(rows)); j++ {
		row := rows[j]
		s := m.servers[row.server]

		pointer := "  "
		if m.focus == paneTasks && j == m.taskCursor {
			pointer = cursorStyle.Render("▸ ")
		}

		var line string
		if row.task < 0 {
			n := m.countTasks(s)
			check := mutedStyle.Render("[ ]")
			switch {
			case n == len(s.Tasks):
				check = okStyle.Render("[x]")
			case n > 0:
				check = skipStyle.Render("[-]")
			}
			line = pointer + check + " " + serverStyle.Render(s.Name) + mutedStyle.Render(fmt.Sprintf("  %d/%d tasks", n, len(s.Tasks)))
		} else {
			indent := ""
			if m.allTasks {
				indent = "  "
			}
			line = pointer + indent + m.taskLine(s, s.Tasks[row.task])
		}
		lines = append(lines, truncateANSI(line, width))
	}
	return lines
}

// paneBox draws lines in a box width cells wide with height rows inside, titled on its
// top border. The focused pane's border is highlighted.
func paneBox(title string, lines []string, width, height int, focused bool) []string {
	border, titled := mutedStyle, mutedStyle
	if focused {
		border, titled = focusedBorderStyle, headerStyle
	}
	inner := max(width-2, 0)

	title = truncate(title, max(inner-3, 0))
	top := border.Render("┌ ") + titled.Render(title) + " "
	top += border.Render(strings.Repeat("─", max(inner-lipgloss.Width(title)-2, 0)) + "┐")

	box := []string{top}
	for i := range height {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		box = append(box, border.Render("│")+line+strings.Repeat(" ", max(inner-lipgloss.Width(line), 0))+border.Render("│"))
	}
	return append(box, border.Render("└"+strings.Repeat("─", inner)+"┘"))
}
