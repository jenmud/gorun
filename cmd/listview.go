package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/envvars"
)

// treeNode is one entry of a list shown in the tree TUI. Children are either
// given up front or loaded the first time the node is expanded, so trees that
// fan out (like the task dependency DAG) are only built as far as they are opened.
type treeNode struct {
	label     string
	detail    string // shown muted after the label
	detailSep string // defaults to " — "; use " " for a label that reads as a sentence
	task      *taskDetail
	children  []*treeNode
	load      func() []*treeNode
	expanded  bool
}

// hasChildren reports whether the node can be expanded.
func (n *treeNode) hasChildren() bool {
	return len(n.children) > 0 || n.load != nil
}

// expand builds the node's children the first time it is opened.
func (n *treeNode) expand() {
	if n.load != nil {
		n.children = n.load()
		n.load = nil
	}
	n.expanded = len(n.children) > 0
}

// treeRow is a visible line of the tree.
type treeRow struct {
	node   *treeNode
	depth  int
	prefix string // tree guides drawn before the node
	parent int    // row index of the parent, -1 for top-level nodes
}

// treeModel is a collapsible tree viewer for the --list-* flags.
type treeModel struct {
	title        string
	kind         string
	roots        []*treeNode
	rows         []treeRow
	cursor       int
	offset       int
	detailOffset int
	detailCursor int
	width        int
	height       int
	detailFocus  bool

	// embedded is set when the tree is a tab of the server picker, where esc and q
	// go back to the servers instead of quitting
	embedded bool

	// checkbox draws a checkbox before nodes it reports on, for the picker's Bundles
	// tab
	checkbox func(*treeNode) (checked, ok bool)

	// hint is shown under the title in an embedded view that needs instructions
	hint string
}

// newTreeModel returns the tree browser for nodes.
func newTreeModel(title, kind string, roots []*treeNode) treeModel {
	m := treeModel{title: title, kind: kind, roots: roots, width: 80, height: 24}
	m.rebuild()
	return m
}

// rebuild flattens the expanded parts of the tree into rows.
func (m *treeModel) rebuild() {
	m.rows = m.rows[:0]
	var walk func(nodes []*treeNode, depth int, prefix string, parent int)
	walk = func(nodes []*treeNode, depth int, prefix string, parent int) {
		for i, n := range nodes {
			last := i == len(nodes)-1
			guide, childPrefix := "", ""
			if depth == 0 {
				childPrefix = "  " // shift the subtree in from the top-level entries
			} else {
				guide, childPrefix = prefix+"├── ", prefix+"│   "
				if last {
					guide, childPrefix = prefix+"└── ", prefix+"    "
				}
			}
			m.rows = append(m.rows, treeRow{node: n, depth: depth, prefix: guide, parent: parent})
			if n.expanded {
				walk(n.children, depth+1, childPrefix, len(m.rows)-1)
			}
		}
	}
	walk(m.roots, 0, "", -1)
	m.cursor = max(0, min(m.cursor, len(m.rows)-1))
}

// Init has nothing to load.
func (m treeModel) Init() tea.Cmd { return nil }

// pageSize is how many rows fit between the header and the footer.
func (m treeModel) pageSize() int {
	if m.kind == "tasks" && m.embedded {
		return max(1, m.height-6)
	}
	header := 1
	if hint := m.hintLines(); len(hint) > 0 {
		header = len(hint) + 2 // blank line, box, blank line
	}
	return max(1, m.height-1-header-2)
}

// hintLines renders the hint as a bordered info box, wrapped to the screen width.
func (m treeModel) hintLines() []string {
	if m.hint == "" {
		return nil
	}
	width := max(m.width, 20)
	text := wrapText(m.hint, max(width-4, 10))

	inner := width - 2
	title := " Info "
	box := []string{focusedBorderStyle.Render("┌") + headerStyle.Render(title) + focusedBorderStyle.Render(strings.Repeat("─", max(inner-lipgloss.Width(title), 0))+"┐")}
	content := max(width-4, 1)
	row := func(line string) string {
		line = truncateANSI(line, content)
		return focusedBorderStyle.Render("│") + " " + line + strings.Repeat(" ", max(content-lipgloss.Width(line), 0)) + " " + focusedBorderStyle.Render("│")
	}
	for _, line := range text {
		box = append(box, row(line))
	}
	return append(box, focusedBorderStyle.Render("└"+strings.Repeat("─", inner)+"┘"))
}

// wrapText breaks s into lines of at most width cells, on word boundaries.
func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{words[0]}
	for _, word := range words[1:] {
		last := len(lines) - 1
		if lipgloss.Width(lines[last])+1+lipgloss.Width(word) > width {
			lines = append(lines, word)
			continue
		}
		lines[last] += " " + word
	}
	return lines
}

// Update handles key presses, mouse clicks and the editor closing.
func (m treeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	oldCursor := m.cursor
	if m.kind == "tasks" && m.embedded && m.detailFocus {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "tab", "left", "h":
				if key.String() == "left" || key.String() == "h" {
					if n := m.current(); n != nil && n.task != nil && n.task.isExpanded(m.detailCursor) {
						n.task.toggle(taskSectionAt(m.detailCursor))
						break
					}
				}
				m.detailFocus = false
			case "up", "k":
				m.detailCursor = max(0, m.detailCursor-1)
				m.keepDetailCursorVisible()
			case "down", "j":
				m.detailCursor = min(m.detailCursor+1, int(taskSectionCount)-1)
				m.keepDetailCursorVisible()
			case "pgup":
				m.detailOffset -= m.pageSize()
			case "pgdown":
				m.detailOffset += m.pageSize()
			case "home":
				m.detailOffset = 0
			case "end":
				m.detailOffset = m.maxDetailOffset()
			case "enter", "space", "right", "l":
				if n := m.current(); n != nil && n.task != nil {
					n.task.toggle(taskSectionAt(m.detailCursor))
				}
			default:
				if key.String() == "q" || key.String() == "esc" || key.String() == "ctrl+c" {
					return m, tea.Quit
				}
			}
			m.detailOffset = max(0, min(m.detailOffset, m.maxDetailOffset()))
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.cursor = max(0, m.cursor-1)
		case tea.MouseWheelDown:
			m.cursor = min(len(m.rows)-1, m.cursor+1)
		}

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "tab":
			if m.kind == "tasks" && m.embedded {
				m.detailFocus = true
				m.keepDetailCursorVisible()
			}
		case "up", "k":
			m.cursor--
		case "down", "j":
			m.cursor++
		case "pgup":
			m.cursor -= m.pageSize()
		case "pgdown":
			m.cursor += m.pageSize()
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			m.cursor = len(m.rows) - 1
		case "enter", "space":
			if n := m.current(); n != nil && n.hasChildren() {
				if n.expanded {
					n.expanded = false
				} else {
					n.expand()
				}
				m.rebuild()
			}
		case "right", "l":
			if n := m.current(); n != nil && n.hasChildren() {
				if n.expanded {
					m.cursor++ // step into the first child
				} else {
					n.expand()
					m.rebuild()
				}
			}
		case "left", "h":
			if n := m.current(); n != nil && n.expanded {
				n.expanded = false
				m.rebuild()
			} else if n != nil {
				m.cursor = max(0, m.rows[m.cursor].parent)
			}
		case "e":
			// expand everything under the cursor
			if n := m.current(); n != nil {
				expandAll([]*treeNode{n})
				m.rebuild()
			}
		case "E":
			// expand the whole tree
			expandAll(m.roots)
			m.rebuild()
		case "c":
			// collapse everything under the cursor
			if n := m.current(); n != nil {
				collapseAll([]*treeNode{n})
				m.rebuild()
			}
		case "C":
			// collapse the whole tree, keeping the cursor on its top-level entry
			for m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].parent >= 0 {
				m.cursor = m.rows[m.cursor].parent
			}
			collapseAll(m.roots)
			m.rebuild()
		}
		m.cursor = max(0, min(m.cursor, len(m.rows)-1))
	}
	if m.cursor != oldCursor {
		m.detailOffset = 0
		m.detailCursor = 0
	}

	// keep the cursor on screen
	page := m.pageSize()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+page {
		m.offset = m.cursor - page + 1
	}
	m.offset = max(0, min(m.offset, len(m.rows)-page))
	return m, nil
}

// taskSectionAt returns the detail section on the given line of the task pane.
func taskSectionAt(i int) taskSection {
	return [...]taskSection{
		taskSectionCommand,
		taskSectionCopies,
		taskSectionCheck,
		taskSectionVerify,
		taskSectionExecution,
		taskSectionEnvVars,
		taskSectionConfigVars,
		taskSectionTaskVars,
	}[max(0, min(i, int(taskSectionCount)-1))]
}

// isExpanded reports whether a section of the task pane is open.
func (d *taskDetail) isExpanded(i int) bool {
	return d.expanded&taskSections(taskSectionAt(i)) != 0
}

// keepDetailCursorVisible scrolls the task pane so its cursor stays in view.
func (m *treeModel) keepDetailCursorVisible() {
	_ = m.taskDetailLines(max(m.width-m.width/2-4, 1))
	n := m.current()
	if n == nil || n.task == nil {
		return
	}
	row := n.task.sectionRows[m.detailCursor]
	page := max(m.height-6, 1)
	if row < m.detailOffset {
		m.detailOffset = row
	} else if row >= m.detailOffset+page {
		m.detailOffset = row - page + 1
	}
}

// editorFinishedMsg reports that the editor opened on a command file has closed.
type editorFinishedMsg struct {
	err error
}

// editCurrentCommandFile opens the selected task's command file in the editor.
func (m treeModel) editCurrentCommandFile() (tea.Cmd, bool) {
	n := m.current()
	if n == nil || n.task == nil || n.task.commandFile == "" {
		return nil, false
	}

	cmd := taskEditorCommand(envvars.Get("EDITOR"), n.task.commandFile)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorFinishedMsg{err: err} }), true
}

// taskEditorCommand returns the command that opens file in the editor named by editorValue, or in vi.
func taskEditorCommand(editorValue, file string) *exec.Cmd {
	editor := strings.Fields(editorValue)
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	args := append(editor[1:], file)
	return exec.Command(editor[0], args...)
}

// maxExpandRows caps how many rows expandAll opens. A task dependency DAG
// repeats shared subtrees under every parent, so fully expanding a big one can
// produce millions of rows.
const maxExpandRows = 20000

// expandAll expands nodes and all their descendants, breadth first, until
// maxExpandRows rows have been opened.
func expandAll(nodes []*treeNode) {
	budget := maxExpandRows
	queue := slices.Clone(nodes)
	for len(queue) > 0 && budget > 0 {
		n := queue[0]
		queue = queue[1:]
		if !n.hasChildren() {
			continue
		}
		n.expand()
		budget -= len(n.children)
		queue = append(queue, n.children...)
	}
}

// collapseAll collapses nodes and every descendant that has been loaded, so
// expanding one again shows just its children.
func collapseAll(nodes []*treeNode) {
	for _, n := range nodes {
		n.expanded = false
		collapseAll(n.children)
	}
}

// current returns the node under the cursor.
func (m treeModel) current() *treeNode {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].node
}

// View draws the tree, with the selected task's details beside it on the Tasks list.
func (m treeModel) View() tea.View {
	var v tea.View
	v.AltScreen = true
	if m.kind == "tasks" && m.embedded {
		v.SetContent(m.taskView())
		return v
	}

	lines := []string{titleStyle.Render("gorun") + " " + mutedStyle.Render(m.title+" · "+m.kind)}
	if hint := m.hintLines(); len(hint) > 0 {
		lines = append(lines, "")
		lines = append(lines, hint...)
		lines = append(lines, "")
	} else {
		lines = append(lines, "")
	}

	// only the visible rows are rendered, so big lists stay responsive
	end := min(len(m.rows), m.offset+m.pageSize())
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.renderRow(i))
	}
	for i := end - m.offset; i < m.pageSize(); i++ {
		lines = append(lines, "")
	}

	quit := "q quit"
	if m.embedded {
		quit = "esc/q back to servers · shift+tab switch"
	}
	lines = append(lines, "", truncateANSI(mutedStyle.Render(fmt.Sprintf("%d/%d · ↑/↓ move · →/← expand/collapse · e/E expand · c/C collapse · %s", m.cursor+1, len(m.rows), quit)), m.width))
	v.SetContent(strings.Join(lines, "\n"))
	return v
}

// taskView draws the Tasks list: the tree on the left and the selected task on the right.
func (m treeModel) taskView() string {
	leftWidth := m.width / 2
	if leftWidth < 2 {
		leftWidth = 2
	}
	rightWidth := max(m.width-leftWidth, 2)
	bodyHeight := max(m.height-6, 1)

	left := m
	left.width = max(leftWidth-2, 1)
	start := max(m.offset, 0)
	if m.cursor < start {
		start = m.cursor
	}
	if m.cursor >= start+bodyHeight {
		start = m.cursor - bodyHeight + 1
	}
	start = min(start, max(0, len(m.rows)-bodyHeight))
	end := min(len(m.rows), start+bodyHeight)
	leftLines := make([]string, 0, bodyHeight)
	for i := start; i < end; i++ {
		leftLines = append(leftLines, left.renderRow(i))
	}
	for len(leftLines) < bodyHeight {
		leftLines = append(leftLines, "")
	}
	leftBox := paneBox("Tasks", leftLines, leftWidth, bodyHeight, !m.detailFocus)

	detailWidth := max(rightWidth-4, 1)
	detailLines := m.taskDetailLines(detailWidth)
	detailStart := min(max(m.detailOffset, 0), max(0, len(detailLines)-bodyHeight))
	detailEnd := min(len(detailLines), detailStart+bodyHeight)
	visibleDetails := append([]string(nil), detailLines[detailStart:detailEnd]...)
	for len(visibleDetails) < bodyHeight {
		visibleDetails = append(visibleDetails, "")
	}
	name := "Task details"
	if task := m.current(); task != nil {
		name = "Task · " + task.label
	}
	rightBox := paneBox(name, visibleDetails, rightWidth, bodyHeight, m.detailFocus)

	lines := []string{titleStyle.Render("gorun") + " " + mutedStyle.Render(m.title+" · tasks"), ""}
	for i := range leftBox {
		lines = append(lines, leftBox[i]+rightBox[i])
	}
	help := fmt.Sprintf("%d/%d · ↑/↓ move · tab details · pgup/dn tree · →/← expand · ctrl+e edit file · e/E/c/C tree", m.cursor+1, len(m.rows))
	if m.detailFocus {
		help = fmt.Sprintf("%d/%d · ↑/↓ section · enter/→ expand · ← collapse/back · ctrl+e edit file · pgup/dn scroll · home/end · tab tree", m.cursor+1, len(m.rows))
	}
	if m.embedded {
		help += " · esc/q servers · shift+tab switch"
	} else {
		help += " · q quit"
	}
	lines = append(lines, "", truncateANSI(mutedStyle.Render(help), m.width))
	return strings.Join(lines, "\n")
}

// taskDetailLines renders the selected task's details, width cells wide.
func (m treeModel) taskDetailLines(width int) []string {
	n := m.current()
	if n == nil || n.task == nil {
		return []string{mutedStyle.Render("Select a task to see its command and variables.")}
	}
	return n.task.lines(width, m.detailCursor, m.detailFocus)
}

// maxDetailOffset is how far the task pane can scroll.
func (m treeModel) maxDetailOffset() int {
	rightWidth := max(m.width-m.width/2, 2)
	lines := m.taskDetailLines(max(rightWidth-4, 1))
	return max(0, len(lines)-max(m.height-6, 1))
}

// wrapDetailText wraps text to width, keeping its line breaks.
func wrapDetailText(text string, width int) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	wrapped := []string{}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			wrapped = append(wrapped, "")
			continue
		}
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(line, max(width, 1), true), "\n")...)
	}
	return wrapped
}

// formatTaskVars renders a task's vars as indented JSON.
func formatTaskVars(vars map[string]any) string {
	data, err := json.MarshalIndent(vars, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", vars)
	}
	return string(data)
}

// taskDetail is what the task pane shows for one task.
type taskDetail struct {
	description   string
	details       string
	execution     []taskOption
	check, verify string   // the task's check (with its copy comparison) and verify; "" for none
	copies        []string // what the task copies to the server before its command, one per copy
	command       string
	commandSource string
	commandFile   string
	commandLoaded bool
	envVars       map[string]string
	configVars    map[string]any
	taskVars      map[string]any
	expanded      taskSections
	sectionRows   [taskSectionCount]int
	renderedWidth int
	renderedState taskSections
	renderedLines []string
}

// taskSections is the set of open sections of the task pane.
type taskSections uint8

// taskSection is one foldable section of the task pane.
type taskSection taskSections

const (
	taskSectionCommand taskSection = 1 << iota
	taskSectionExecution
	taskSectionEnvVars
	taskSectionConfigVars
	taskSectionTaskVars
	taskSectionCheck
	taskSectionVerify
	taskSectionCopies
)

const taskSectionCount = 8

// newTaskDetail gathers what the task pane shows for task.
func newTaskDetail(cfg *gorun.Config, task gorun.Task) *taskDetail {
	d := &taskDetail{
		description:   task.Description,
		details:       task.Details,
		execution:     taskExecutionOptions(task),
		check:         taskCheckText(task),
		verify:        task.VerifyText(),
		copies:        task.CopySummaries(),
		commandSource: "none",
		commandLoaded: true,
		envVars:       map[string]string{},
		configVars:    maps.Clone(cfg.Vars),
		taskVars:      maps.Clone(task.Vars),
	}
	for key, value := range cfg.EnvVars {
		d.envVars[strings.ToUpper(key)] = value
	}
	for _, layer := range []map[string]string{task.EnvVars, task.EnvVarsTemplate} {
		for key, value := range layer {
			d.envVars[strings.ToUpper(key)] = value
		}
	}

	switch {
	case task.CmdTemplate != "":
		d.commandSource, d.command = "cmd_template", task.CmdTemplate
	case task.CmdTemplateFile != "":
		d.commandSource = "cmd_template_file · " + task.CmdTemplateFile
		d.commandFile = task.CmdTemplateFile
		d.commandLoaded = false
	case task.Cmd != "":
		d.commandSource, d.command = "cmd", task.Cmd
	case task.CmdFile != "":
		d.commandSource = "cmd_file · " + task.CmdFile
		d.commandFile = task.CmdFile
		d.commandLoaded = false
	default:
		d.command = "(no command configured; this task may only copy files)"
	}
	return d
}

// taskOption is one execution setting of a task, as a label and a value.
type taskOption struct {
	name  string
	value string
}

// taskExecutionOptions lists a task's execution settings: timeout, retries, shell, sudo and so on.
func taskExecutionOptions(task gorun.Task) []taskOption {
	timeout := task.TimeoutDuration().String()
	if task.Timeout <= 0 {
		timeout += " (default)"
	}
	retryDelay := task.RetryDelayDuration().String()
	if task.RetryDelay == nil {
		retryDelay += " (default)"
	}
	rerun := task.Rerun
	if rerun == "" {
		rerun = "not specified"
	}
	shell := task.Shell
	if shell == "" {
		shell = "default"
	}
	logLevel := task.LogLevel
	if logLevel == "" {
		logLevel = "inherit"
	}
	dependsOn := strings.Join(task.DependsOn, ", ")
	if dependsOn == "" {
		dependsOn = "none"
	}
	copies := len(task.Copy) + len(task.Copies) + len(task.Upload) + len(task.Files)
	attempts := task.Attempts()
	attemptNoun := "attempts"
	if attempts == 1 {
		attemptNoun = "attempt"
	}

	return []taskOption{
		{name: "Timeout", value: timeout},
		{name: "Retries", value: fmt.Sprintf("%d extra · %d %s", task.Retries, attempts, attemptNoun)},
		{name: "Retry delay", value: retryDelay},
		{name: "Rerun", value: rerun},
		{name: "Retry unsafe", value: yesNo(task.RetryUnsafe)},
		{name: "Shell", value: shell},
		{name: "Sudo", value: yesNo(task.Sudo)},
		{name: "Local only", value: yesNo(task.Local)},
		{name: "Log level", value: logLevel},
		{name: "Depends on", value: dependsOn},
		{name: "File copies", value: fmt.Sprint(copies)},
		{name: "Check", value: cmp.Or(task.CheckText(), "none")},
		{name: "Check copies", value: yesNo(task.CheckCopies)},
		{name: "Verify", value: cmp.Or(task.VerifyText(), "none")},
	}
}

// yesNo is "yes" or "no".
func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// loadCommand reads the task's command file for the task pane, the first time it is shown.
func (d *taskDetail) loadCommand() {
	if d.commandLoaded {
		return
	}
	d.commandLoaded = true
	content, err := os.ReadFile(d.commandFile)
	if err != nil {
		d.command = fmt.Sprintf("unable to read %s: %v", d.commandFile, err)
		return
	}
	d.command = string(content)
}

// toggle opens or closes the node under the cursor.
func (d *taskDetail) toggle(section taskSection) {
	d.expanded ^= taskSections(section)
	d.renderedLines = nil
}

// lines renders the visible part of the tree.
func (d *taskDetail) lines(width, cursor int, focused bool) []string {
	if d.expanded&taskSections(taskSectionCommand) != 0 {
		d.loadCommand()
	}
	if d.renderedLines == nil || d.renderedWidth != width || d.renderedState != d.expanded {
		lines := []string{}
		appendTextBlock := func(title, text string) {
			lines = append(lines, "", "  "+headerStyle.Render(title))
			for _, line := range wrapDetailText(text, max(width-4, 1)) {
				lines = append(lines, "    "+line)
			}
		}
		if d.description != "" {
			appendTextBlock("Description", d.description)
		}
		if d.details != "" {
			appendTextBlock("Details", d.details)
		}
		if d.description != "" || d.details != "" {
			lines = append(lines, "")
		}

		appendSection := func(index int, section taskSection, heading string, content []string) {
			d.sectionRows[index] = len(lines)
			marker, action := "▸", "[expand]"
			if d.expanded&taskSections(section) != 0 {
				marker, action = "▾", "[collapse]"
			}
			headingLine := truncateANSI(headerStyle.Render(marker+" "+heading+" "+action), max(width-2, 1))
			lines = append(lines, "  "+headingLine)
			if d.expanded&taskSections(section) != 0 {
				for _, line := range content {
					lines = append(lines, "    "+line)
				}
			}
			lines = append(lines, "")
		}

		var commandContent []string
		if d.expanded&taskSections(taskSectionCommand) != 0 {
			commandContent = wrapDetailText(expandCommandPreview(d.command, d.envVars), max(width-4, 1))
		}
		appendSection(0, taskSectionCommand, "Command · "+d.commandSource, commandContent)

		// a check or verify names its command in the heading, so it shows even when folded
		stepSection := func(index int, section taskSection, label, command, about, none string) {
			if command == "" {
				appendSection(index, section, label+" · none", []string{none})
				return
			}
			heading := label + " · " + firstLine(command)
			appendSection(index, section, heading, append(wrapDetailText(command, max(width-4, 1)), mutedStyle.Render(about)))
		}
		// copies run before the command: a single one names itself in the heading
		switch len(d.copies) {
		case 0:
			appendSection(1, taskSectionCopies, "Copies · none", []string{"(the task copies no files)"})
		default:
			heading := fmt.Sprintf("Copies · %d", len(d.copies))
			if len(d.copies) == 1 {
				heading = "Copies · " + d.copies[0]
			}
			content := []string{}
			for _, c := range d.copies {
				content = append(content, wrapDetailText(c, max(width-4, 1))...)
			}
			content = append(content, mutedStyle.Render("copied to the server before the command runs, as the login user"))
			appendSection(1, taskSectionCopies, heading, content)
		}
		stepSection(2, taskSectionCheck, "Check", d.check, "runs first: exit 0 means nothing to do, so the task is skipped as unchanged",
			"(none: the task always runs)")
		stepSection(3, taskSectionVerify, "Verify", d.verify, "runs after the command: a non-zero exit fails the task", "(none)")

		executionRows := make([][2]string, 0, len(d.execution))
		for _, option := range d.execution {
			executionRows = append(executionRows, [2]string{option.name, option.value})
		}
		var executionContent []string
		if d.expanded&taskSections(taskSectionExecution) != 0 {
			executionContent = detailTableLines("SETTING", "VALUE", executionRows, max(width-4, 1))
		}
		appendSection(4, taskSectionExecution, "Execution settings", executionContent)

		envRows := make([][2]string, 0, len(d.envVars))
		for _, key := range slices.Sorted(maps.Keys(d.envVars)) {
			envRows = append(envRows, [2]string{key, d.envVars[key]})
		}
		var envContent []string
		if d.expanded&taskSections(taskSectionEnvVars) != 0 {
			envContent = detailTableLines("NAME", "VALUE", envRows, max(width-4, 1))
		}
		appendSection(5, taskSectionEnvVars, fmt.Sprintf("Env vars · config + task (%d)", len(d.envVars)), envContent)

		var configVarsContent []string
		if d.expanded&taskSections(taskSectionConfigVars) != 0 {
			configVarsContent = wrapDetailText(formatTaskVars(d.configVars), max(width-4, 1))
			if len(d.configVars) == 0 {
				configVarsContent = []string{"(none configured)"}
			}
		}
		appendSection(6, taskSectionConfigVars, fmt.Sprintf("Config vars · server templates (%d)", len(d.configVars)), configVarsContent)

		var taskVarsContent []string
		if d.expanded&taskSections(taskSectionTaskVars) != 0 {
			taskVarsContent = wrapDetailText(formatTaskVars(d.taskVars), max(width-4, 1))
			if len(d.taskVars) == 0 {
				taskVarsContent = []string{"(none configured)"}
			}
		}
		appendSection(7, taskSectionTaskVars, fmt.Sprintf("Task vars (%d)", len(d.taskVars)), taskVarsContent)
		d.renderedWidth, d.renderedState, d.renderedLines = width, d.expanded, lines
	}

	lines := slices.Clone(d.renderedLines)
	if focused && cursor >= 0 && cursor < len(d.sectionRows) {
		row := d.sectionRows[cursor]
		line := cursorStyle.Render("▶ ") + strings.TrimPrefix(lines[row], "  ")
		lines[row] = highlightSelectedRow(line, width)
	}
	return lines
}

// expandCommandPreview returns command with its ${NAME} references to envVars filled in.
func expandCommandPreview(command string, envVars map[string]string) string {
	var out strings.Builder
	for i := 0; i < len(command); {
		if i+1 < len(command) && command[i] == '$' && command[i+1] == '{' && (i == 0 || command[i-1] != '$' && command[i-1] != '\\') {
			if end := strings.IndexByte(command[i+2:], '}'); end >= 0 {
				end += i + 2
				name := command[i+2 : end]
				if value, ok := envVars[strings.ToUpper(name)]; ok {
					out.WriteString(value)
				} else {
					out.WriteString(command[i : end+1])
				}
				i = end + 1
				continue
			}
		}
		out.WriteByte(command[i])
		i++
	}
	return out.String()
}

// detailTableLines renders rows as a two-column table, width cells wide.
func detailTableLines(keyHeading, valueHeading string, rows [][2]string, width int) []string {
	if len(rows) == 0 {
		return []string{"(none configured)"}
	}
	keyWidth := lipgloss.Width(keyHeading)
	for _, row := range rows {
		keyWidth = max(keyWidth, lipgloss.Width(row[0]))
	}
	keyWidth = min(keyWidth, max(1, (width-9)/2))
	valueWidth := max(width-keyWidth-7, 1)
	pad := func(text string, columnWidth int) string {
		text = truncateANSI(text, columnWidth)
		return text + strings.Repeat(" ", max(columnWidth-lipgloss.Width(text), 0))
	}
	rule := func(left, middle, right string) string {
		return left + strings.Repeat("─", keyWidth+2) + middle + strings.Repeat("─", valueWidth+2) + right
	}
	lines := []string{mutedStyle.Render(rule("┌", "┬", "┐"))}
	lines = append(lines, headerStyle.Render("│ ")+headerStyle.Render(pad(keyHeading, keyWidth))+headerStyle.Render(" │ ")+headerStyle.Render(pad(valueHeading, valueWidth))+headerStyle.Render(" │"))
	lines = append(lines, mutedStyle.Render(rule("├", "┼", "┤")))
	for _, row := range rows {
		keys := wrapDetailText(row[0], keyWidth)
		values := wrapDetailText(row[1], valueWidth)
		for i := 0; i < max(len(keys), len(values)); i++ {
			key, value := "", ""
			if i < len(keys) {
				key = keys[i]
			}
			if i < len(values) {
				value = values[i]
			}
			lines = append(lines, "│ "+pad(key, keyWidth)+" │ "+pad(value, valueWidth)+" │")
		}
	}
	return append(lines, mutedStyle.Render(rule("└", "┴", "┘")))
}

// renderRow renders row i of the tree.
func (m treeModel) renderRow(i int) string {
	row := m.rows[i]
	n := row.node

	marker := "  "
	if n.hasChildren() {
		marker = "▸ "
		if n.expanded {
			marker = "▾ "
		}
	}

	label := n.label
	switch {
	case i == m.cursor:
		label = cursorStyle.Render(label)
	case row.depth == 0:
		label = serverStyle.Render(label)
	}
	if n.detail != "" {
		sep := n.detailSep
		if sep == "" {
			sep = " — "
		}
		label += mutedStyle.Render(sep + n.detail)
	}

	pointer := "  "
	if i == m.cursor {
		pointer = cursorStyle.Render("▶ ")
		marker = cursorStyle.Render(marker)
	}

	check := ""
	if m.checkbox != nil && row.depth == 0 {
		if checked, ok := m.checkbox(n); ok {
			if checked {
				check = okStyle.Render("[x]") + " "
			} else {
				check = mutedStyle.Render("[ ]") + " "
			}
		}
	}

	line := pointer + check + mutedStyle.Render(row.prefix) + marker + label
	line = truncateANSI(line, m.width)
	if i == m.cursor {
		line = highlightSelectedRow(line, m.width)
	}
	return line
}

// truncateANSI cuts a styled line to width cells so it never wraps.
func truncateANSI(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// runTreeTUI shows roots in the collapsible tree viewer until the user quits.
func runTreeTUI(title, kind string, roots []*treeNode) error {
	defer drainTerminalInput()
	p := tea.NewProgram(newTreeModel(title, kind, roots), tea.WithEnvironment(append(os.Environ(), "SSH_TTY=1")))
	_, err := p.Run()
	return err
}

// taskTreeNodes builds the --list-tasks tree: tasks without dependencies at the
// top, each expanding to the tasks that depend on it.
func taskTreeNodes(cfg *gorun.Config, shown map[string]bool) []*treeNode {
	roots, children := taskDependents(cfg, shown)

	details := make(map[string]*taskDetail, len(cfg.Tasks))
	var node func(name string) *treeNode
	node = func(name string) *treeNode {
		task := cfg.Tasks[name]
		detail, ok := details[name]
		if !ok {
			detail = newTaskDetail(cfg, task)
			details[name] = detail
		}
		n := &treeNode{label: name, detail: strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(taskLabel(cfg, name), name), " — ")), task: detail}
		if n.detail == name {
			n.detail = "" // a description that only repeats the name adds nothing
		}
		if kids := children[name]; len(kids) > 0 {
			n.load = func() []*treeNode {
				nodes := make([]*treeNode, 0, len(kids))
				for _, kid := range kids {
					nodes = append(nodes, node(kid))
				}
				return nodes
			}
		}
		return n
	}

	nodes := make([]*treeNode, 0, len(roots))
	for _, root := range roots {
		nodes = append(nodes, node(root))
	}
	return nodes
}

// serverTreeNodes builds the --list-servers tree: each server expands to its
// groups, tags and task pipeline.
func serverTreeNodes(servers map[string]gorun.Server) []*treeNode {
	nodes := make([]*treeNode, 0, len(servers))
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		server := servers[name]
		n := &treeNode{label: server.Name, detail: fmt.Sprintf("%s · %d tasks", server.Address(), len(server.Tasks))}
		if len(server.Groups) > 0 {
			n.children = append(n.children, &treeNode{label: "groups:", detail: strings.Join(server.Groups, ", "), detailSep: " "})
		}
		if len(server.Tags) > 0 {
			n.children = append(n.children, &treeNode{label: "tags:", detail: strings.Join(server.Tags, ", "), detailSep: " "})
		}
		for _, t := range server.Tasks {
			n.children = append(n.children, &treeNode{label: serverTaskLabel(t), detail: t.Description})
		}
		nodes = append(nodes, n)
	}
	return nodes
}

// tagTreeNodes builds the --list-tags tree: each tag expands to its servers.
func tagTreeNodes(cfg *gorun.Config, servers []gorun.Server, filtered bool) []*treeNode {
	members := tagMembers(cfg, servers, filtered)
	addresses := make(map[string]string, len(servers))
	for _, s := range servers {
		addresses[s.Name] = s.Address()
	}

	nodes := make([]*treeNode, 0, len(members))
	for _, tag := range slices.Sorted(maps.Keys(members)) {
		names := members[tag]
		n := &treeNode{label: tag, detail: fmt.Sprintf("%d servers", len(names))}
		if len(names) == 0 {
			n.detail = "no servers"
		}
		for _, name := range names {
			n.children = append(n.children, &treeNode{label: name, detail: addresses[name]})
		}
		nodes = append(nodes, n)
	}
	return nodes
}

// groupTreeNodes builds the --list-groups tree: each group expands to its servers.
func groupTreeNodes(cfg *gorun.Config, servers []gorun.Server, filtered bool) []*treeNode {
	members := groupMembers(cfg, servers, filtered)
	addresses := make(map[string]string, len(servers))
	for _, s := range servers {
		addresses[s.Name] = s.Address()
	}

	nodes := make([]*treeNode, 0, len(members))
	for _, group := range slices.Sorted(maps.Keys(members)) {
		names := members[group]
		n := &treeNode{label: group, detail: fmt.Sprintf("%d servers", len(names))}
		if len(names) == 0 {
			n.detail = "no servers"
		}
		for _, name := range names {
			n.children = append(n.children, &treeNode{label: name, detail: addresses[name]})
		}
		nodes = append(nodes, n)
	}
	return nodes
}

// bundleTreeNodes builds the --list-bundles tree: each bundle expands to its
// tasks, with included bundles expandable in turn, then the task names it
// expands to when it includes other bundles.
func bundleTreeNodes(cfg *gorun.Config) []*treeNode {
	var entries func(refs []string) []*treeNode
	entries = func(refs []string) []*treeNode {
		nodes := make([]*treeNode, 0, len(refs))
		for _, ref := range refs {
			n := &treeNode{label: ref}
			if task, ok := cfg.Tasks[ref]; ok {
				n.detail = task.Description
			}
			// Load rejects cyclic and unknown bundles, so this terminates
			if name, ok := strings.CutPrefix(ref, gorun.BundlePrefix); ok {
				n.detail = cfg.Bundles[name].Description
				tasks := cfg.Bundles[name].Tasks
				n.load = func() []*treeNode { return entries(tasks) }
			}
			nodes = append(nodes, n)
		}
		return nodes
	}

	nodes := make([]*treeNode, 0, len(cfg.Bundles))
	for _, name := range slices.Sorted(maps.Keys(cfg.Bundles)) {
		bundle := cfg.Bundles[name]
		n := &treeNode{label: gorun.BundlePrefix + name, detail: bundle.Description, children: entries(bundle.Tasks)}

		nested := slices.ContainsFunc(bundle.Tasks, func(ref string) bool {
			return strings.HasPrefix(ref, gorun.BundlePrefix)
		})
		if nested {
			if names, err := cfg.ExpandTaskRefs(gorun.BundlePrefix + name); err == nil {
				n.children = append(n.children, &treeNode{label: "→", detail: strings.Join(names, ", "), detailSep: " "})
			}
		}
		nodes = append(nodes, n)
	}
	return nodes
}

// taskCheckText is a task's check as it runs: the copy comparison first, when it has one.
func taskCheckText(task gorun.Task) string {
	if !task.CheckCopies {
		return task.CheckText()
	}
	return strings.TrimSuffix("compare copies (md5); "+task.CheckText(), "; ")
}
