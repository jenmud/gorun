package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jenmud/gorun"
)

// treeNode is one entry of a list shown in the tree TUI. Children are either
// given up front or loaded the first time the node is expanded, so trees that
// fan out (like the task dependency DAG) are only built as far as they are opened.
type treeNode struct {
	label    string
	detail   string // shown muted after the label
	children []*treeNode
	load     func() []*treeNode
	expanded bool
}

func (n *treeNode) hasChildren() bool {
	return len(n.children) > 0 || n.load != nil
}

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
	title  string
	kind   string
	roots  []*treeNode
	rows   []treeRow
	cursor int
	offset int
	width  int
	height int

	// embedded is set when the tree is a tab of the server picker, where esc and q
	// go back to the servers instead of quitting
	embedded bool
}

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
			if depth > 0 {
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

func (m treeModel) Init() tea.Cmd { return nil }

// pageSize is how many rows fit between the header and the footer.
func (m treeModel) pageSize() int {
	return max(1, m.height-4)
}

func (m treeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m treeModel) current() *treeNode {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].node
}

func (m treeModel) View() tea.View {
	var v tea.View
	v.AltScreen = true

	lines := []string{titleStyle.Render("gorun") + " " + mutedStyle.Render(m.title+" · "+m.kind), ""}

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
		label += " " + mutedStyle.Render(n.detail)
	}

	pointer := "  "
	if i == m.cursor {
		pointer = cursorStyle.Render("❯ ")
		marker = cursorStyle.Render(marker)
	}

	line := pointer + mutedStyle.Render(row.prefix) + marker + label
	return truncateANSI(line, m.width)
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

	var node func(name string) *treeNode
	node = func(name string) *treeNode {
		n := &treeNode{label: name, detail: strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(taskLabel(cfg, name), name), " — "))}
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
			n.children = append(n.children, &treeNode{label: "groups:", detail: strings.Join(server.Groups, ", ")})
		}
		if len(server.Tags) > 0 {
			n.children = append(n.children, &treeNode{label: "tags:", detail: strings.Join(server.Tags, ", ")})
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
				n.children = append(n.children, &treeNode{label: "=", detail: strings.Join(names, ", ")})
			}
		}
		nodes = append(nodes, n)
	}
	return nodes
}
