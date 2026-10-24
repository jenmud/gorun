package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"gorun"
)

var (
	matchStyle       = lipgloss.NewStyle().Foreground(colorSkip).Bold(true)
	searchBoxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(0, 1)
	searchLabelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	runButtonStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colorOK).Padding(0, 2)
	offButtonStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Background(lipgloss.Color("#374151")).Padding(0, 2)
)

// serverMatch is a server that matches the current filter.
type serverMatch struct {
	index     int   // into serverSelectionModel.servers
	score     int   // fuzzy match score, higher is better
	onName    bool  // matched on the name rather than the hostname
	positions []int // rune indexes of the matched characters, for highlighting
}

// serverSelectionModel is the bubbletea model for picking which servers to deploy to.
// Typing filters the list with a fuzzy match on the server name (or hostname).
type serverSelectionModel struct {
	title    string
	servers  []gorun.Server
	selected map[string]bool
	query    string
	matches  []serverMatch
	cursor   int // index into matches
	width    int
	height   int

	confirmed bool
	quitting  bool
}

func newServerSelectionModel(title string, servers []gorun.Server) serverSelectionModel {
	m := serverSelectionModel{
		title:    title,
		servers:  servers,
		selected: map[string]bool{},
		width:    80,
		height:   24,
	}
	m.filter()
	return m
}

// filter rebuilds the match list from the query, best matches first.
func (m *serverSelectionModel) filter() {
	query := strings.TrimSpace(m.query)
	m.matches = m.matches[:0]

	for i, s := range m.servers {
		if score, pos, ok := fuzzyMatch(query, s.Name); ok {
			m.matches = append(m.matches, serverMatch{index: i, score: score, onName: true, positions: pos})
		} else if score, pos, ok := fuzzyMatch(query, s.Hostname); ok {
			m.matches = append(m.matches, serverMatch{index: i, score: score, positions: pos})
		}
	}

	// name matches rank above hostname matches; ties keep the config order
	slices.SortStableFunc(m.matches, func(a, b serverMatch) int {
		if a.onName != b.onName {
			if a.onName {
				return -1
			}
			return 1
		}
		return b.score - a.score
	})

	m.cursor = 0
}

func (m serverSelectionModel) current() (gorun.Server, bool) {
	if m.cursor < 0 || m.cursor >= len(m.matches) {
		return gorun.Server{}, false
	}
	return m.servers[m.matches[m.cursor].index], true
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

// chosen returns the servers to deploy to, in config order: the selected servers, or
// the one under the cursor when nothing is selected.
func (m serverSelectionModel) chosen() []gorun.Server {
	servers := []gorun.Server{}
	for _, s := range m.servers {
		if m.selected[s.Name] {
			servers = append(servers, s)
		}
	}

	if len(servers) == 0 {
		if s, ok := m.current(); ok {
			servers = append(servers, s)
		}
	}

	return servers
}

func (m serverSelectionModel) Init() tea.Cmd {
	return nil
}

func (m serverSelectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

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

		l := m.layout()
		if msg.Y == l.buttonY && msg.X < lipgloss.Width(m.runButton()) {
			return m.run()
		}

		// clicking a server moves the cursor to it and toggles it
		if i := l.offset + msg.Y - l.listTop; msg.Y >= l.listTop && i < min(l.offset+l.rows, len(m.matches)) {
			m.cursor = i
			name := m.servers[m.matches[i].index].Name
			m.selected[name] = !m.selected[name]
		}
		return m, nil

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.cursor = max(m.cursor-1, 0)
		case tea.MouseWheelDown:
			m.cursor = max(min(m.cursor+1, len(m.matches)-1), 0)
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
		case "up", "ctrl+p", "ctrl+k":
			m.cursor = max(m.cursor-1, 0)
		case "down", "ctrl+n", "ctrl+j":
			m.cursor = max(min(m.cursor+1, len(m.matches)-1), 0)
		case "space", "tab":
			if s, ok := m.current(); ok {
				m.selected[s.Name] = !m.selected[s.Name]
				if msg.String() == "tab" {
					m.cursor = max(min(m.cursor+1, len(m.matches)-1), 0)
				}
			}
		case "ctrl+a":
			// select every visible server, or deselect them if they are all selected already
			all := true
			for _, match := range m.matches {
				all = all && m.selected[m.servers[match.index].Name]
			}
			for _, match := range m.matches {
				m.selected[m.servers[match.index].Name] = !all
			}
		case "ctrl+d":
			clear(m.selected)
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
	v.SetContent(m.viewContent())
	return v
}

// selectionLayout is where things are drawn on screen, so mouse clicks can be mapped back.
type selectionLayout struct {
	listTop int // screen row of the first server
	offset  int // index into matches of the first server shown
	rows    int // how many server rows fit
	buttonY int // screen row of the run button
}

func (m serverSelectionModel) layout() selectionLayout {
	// title, blank, search box (3), count, blank | list | blank, button, blank, help
	const above, below = 7, 4

	l := selectionLayout{listTop: above, rows: max(m.height-above-below, 3)}

	// keep the cursor in view
	if len(m.matches) > l.rows {
		l.offset = min(max(m.cursor-l.rows+1, 0), len(m.matches)-l.rows)
	}

	l.buttonY = l.listTop + max(min(l.rows, len(m.matches)), 1) + 1
	return l
}

func (m serverSelectionModel) searchBox() string {
	input := mutedStyle.Render("type to filter servers by name or hostname…")
	if m.query != "" {
		input = m.query + cursorStyle.Render("█")
	} else {
		input = cursorStyle.Render("█") + input
	}

	return searchBoxStyle.Width(min(m.width-2, 70)).Render(searchLabelStyle.Render("Search ") + input)
}

func (m serverSelectionModel) runButton() string {
	chosen := m.chosen()
	switch {
	case len(chosen) == 0:
		return offButtonStyle.Render("▶ Run · no servers match")
	case m.countSelected() == 0:
		return runButtonStyle.Render("▶ Run on " + chosen[0].Name)
	case len(chosen) == 1:
		return runButtonStyle.Render("▶ Run on 1 server")
	}
	return runButtonStyle.Render(fmt.Sprintf("▶ Run on %d servers", len(chosen)))
}

func (m serverSelectionModel) viewContent() string {
	l := m.layout()

	lines := []string{titleStyle.Render("gorun") + " " + mutedStyle.Render(m.title), ""}
	lines = append(lines, strings.Split(m.searchBox(), "\n")...)
	lines = append(lines, mutedStyle.Render(fmt.Sprintf("  %d/%d servers · %d selected", len(m.matches), len(m.servers), m.countSelected())), "")

	if len(m.matches) == 0 {
		lines = append(lines, mutedStyle.Render("  no servers match"))
	}

	nameWidth := 0
	for _, s := range m.servers {
		nameWidth = max(nameWidth, len([]rune(s.Name)))
	}

	for i := l.offset; i < min(l.offset+l.rows, len(m.matches)); i++ {
		match := m.matches[i]
		s := m.servers[match.index]

		pointer := "  "
		if i == m.cursor {
			pointer = cursorStyle.Render("▸ ")
		}

		check := mutedStyle.Render("[ ]")
		if m.selected[s.Name] {
			check = okStyle.Render("[x]")
		}

		var name, host string
		if match.onName {
			name = highlight(s.Name, match.positions, serverStyle)
			host = mutedStyle.Render(gorun.ServerAddress(s))
		} else {
			name = serverStyle.Render(s.Name)
			host = highlight(gorun.ServerAddress(s), match.positions, mutedStyle)
		}
		name += strings.Repeat(" ", nameWidth-len([]rune(s.Name)))

		lines = append(lines, fmt.Sprintf("%s%s %s  %s", pointer, check, name, host))
	}

	lines = append(lines, "", m.runButton()+mutedStyle.Render("  enter"), "")
	lines = append(lines, mutedStyle.Render("↑/↓ move · space/tab select · ctrl+a select all shown · ctrl+d clear · esc clear/quit · click to select or run"))
	return strings.Join(lines, "\n")
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

// runServerSelectionTUI lets the user pick servers to deploy to. It returns an error
// if the selection was cancelled.
func runServerSelectionTUI(title string, servers []gorun.Server) ([]gorun.Server, error) {
	// slog output would corrupt the TUI
	gorun.SetLogOutput(io.Discard)
	final, err := tea.NewProgram(newServerSelectionModel(title, servers)).Run()
	if err != nil {
		return nil, fmt.Errorf("error running server selection TUI: %w", err)
	}

	m := final.(serverSelectionModel)
	if !m.confirmed {
		return nil, errors.New("server selection cancelled")
	}

	return m.chosen(), nil
}
