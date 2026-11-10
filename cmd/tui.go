package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/history"
)

var (
	colorAccent = lipgloss.Color("#7C3AED")
	colorOK     = lipgloss.Color("#22C55E")
	colorFail   = lipgloss.Color("#EF4444")
	colorSkip   = lipgloss.Color("#F59E0B")
	colorMuted  = lipgloss.Color("#6B7280")
	colorTime   = lipgloss.Color("#38BDF8")

	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colorAccent).Padding(0, 1)
	serverStyle      = lipgloss.NewStyle().Bold(true)
	mutedStyle       = lipgloss.NewStyle().Foreground(colorMuted)
	timeStyle        = lipgloss.NewStyle().Foreground(colorTime).Bold(true)
	okStyle          = lipgloss.NewStyle().Foreground(colorOK)
	failStyle        = lipgloss.NewStyle().Foreground(colorFail).Bold(true)
	skipStyle        = lipgloss.NewStyle().Foreground(colorSkip)
	copyStyle        = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	cursorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#DDD6FE")).Bold(true)
	selectedRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#3B2A66")).Bold(true)

	outputBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorMuted).Padding(0, 1)
	failBoxStyle   = outputBoxStyle.BorderForeground(colorFail)
)

// how many lines of output to show in a box before truncating from the top
const (
	failTailLines   = 10
	outputTailLines = 20
)

// taskView is a task in the live view, with its latest event.
type taskView struct {
	name        string
	description string
	details     string
	rerun       string
	state       gorun.TaskState
	started     time.Time
	event       gorun.TaskEvent
	expanded    bool
	// copy is the latest progress of the file the task is copying, while it copies one.
	copy *gorun.CopyProgressEvent
}

// serverView is a server in the live view, with its tasks and running totals.
type serverView struct {
	name           string
	address        string
	tasks          []*taskView
	taskMap        map[string]*taskView
	err            error
	doneCount      int
	failedCount    int
	skippedCount   int
	unchangedCount int
	wouldRunCount  int
	runningCount   int
	open           bool // the compact view lists this server's tasks
}

// task returns the server's task by name, or nil.
func (s *serverView) task(name string) *taskView {
	if s.taskMap != nil {
		return s.taskMap[name]
	}
	for _, t := range s.tasks {
		if t.name == name {
			return t
		}
	}
	return nil
}

// count is how many of the server's tasks are in any of the given states.
func (s *serverView) count(states ...gorun.TaskState) int {
	if len(states) == 1 {
		switch states[0] {
		case gorun.TaskDone:
			return s.doneCount
		case gorun.TaskFailed:
			return s.failedCount
		case gorun.TaskSkipped:
			return s.skippedCount
		case gorun.TaskUnchanged:
			return s.unchangedCount
		case gorun.TaskWouldRun:
			return s.wouldRunCount
		case gorun.TaskRunning:
			return s.runningCount
		case gorun.TaskPending:
			return len(s.tasks) - s.doneCount - s.failedCount - s.skippedCount - s.unchangedCount - s.wouldRunCount - s.runningCount
		}
	}
	n := 0
	for _, st := range states {
		n += s.count(st)
	}
	return n
}

// finished reports whether the server has nothing left to run.
func (s *serverView) finished() bool {
	return s.err != nil || s.count(gorun.TaskPending, gorun.TaskRunning) == 0
}

// tuiBatchMsg carries a batch of gorun events applied in a single Update, so the
// TUI stays responsive when a run emits thousands of events.
type tuiBatchMsg struct {
	events []any
}

// tuiModel is the bubbletea model for a gorun deployment.
type tuiModel struct {
	title          string
	checkOnly      bool // a --check run: only checks run
	servers        []*serverView
	serverMap      map[string]*serverView
	flatTasksCache []*taskView
	spinner        spinner.Model
	okBar          progress.Model
	failBar        progress.Model
	copyBar        progress.Model // a copy's progress, in one solid colour
	cancel         context.CancelFunc

	// compact view failure/skip boxes are rendered off the UI loop (see
	// maybeRenderBoxes) so a run with thousands of failures can't freeze input
	boxLines   []string
	boxesDirty bool
	boxesBusy  bool
	boxGen     int // generation of the latest box snapshot
	boxLinesAt int // generation boxLines was rendered from
	boxCache   *boxCache

	runErr    error // the run failed before any server started
	verbose   bool
	cursor    int  // index into the flattened task list (verbose view)
	selected  int  // index into servers (compact view)
	scroll    int  // line offset for the compact view
	follow    bool // keep compact view pinned to the bottom as output grows
	width     int
	height    int
	started   time.Time
	elapsed   time.Duration
	done      bool
	quitting  bool
	forceQuit bool
}

// newTUIModel returns the live view for a run. cancel stops the run when the user quits.
func newTUIModel(title string, verbose bool, cancel context.CancelFunc) tuiModel {
	return tuiModel{
		title:     title,
		serverMap: make(map[string]*serverView),
		boxCache:  &boxCache{},
		verbose:   verbose,
		cancel:    cancel,
		follow:    false,
		started:   time.Now(),
		width:     80,
		height:    24,
		spinner:   spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(cursorStyle)),
		okBar:     progress.New(progress.WithColors(colorAccent, colorOK), progress.WithFillCharacters('■', '□'), progress.WithoutPercentage()),
		failBar:   progress.New(progress.WithColors(colorFail), progress.WithFillCharacters('■', '□'), progress.WithoutPercentage()),
		copyBar:   progress.New(progress.WithColors(colorAccent), progress.WithFillCharacters('■', '□'), progress.WithoutPercentage()),
	}
}

// Init starts the spinner.
func (m tuiModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// server returns the server by name, or nil.
func (m tuiModel) server(name string) *serverView {
	if m.serverMap != nil {
		return m.serverMap[name]
	}
	for _, s := range m.servers {
		if s.name == name {
			return s
		}
	}
	return nil
}

// flatTasks returns every task across all servers in display order.
func (m tuiModel) flatTasks() []*taskView {
	if m.flatTasksCache != nil {
		return m.flatTasksCache
	}
	tasks := []*taskView{}
	for _, s := range m.servers {
		tasks = append(tasks, s.tasks...)
	}
	return tasks
}

// failed reports whether any server or task has failed.
func (m tuiModel) failed() bool {
	if m.runErr != nil {
		return true
	}
	for _, s := range m.servers {
		if s.err != nil || s.count(gorun.TaskFailed) > 0 {
			return true
		}
	}
	return false
}

// applyEvent folds a single gorun event into the model. It is called for every
// event in a tuiBatchMsg, so the model is updated in bulk instead of per event.
func (m tuiModel) applyEvent(event any) tuiModel {
	switch msg := event.(type) {
	case gorun.PlanEvent:
		if len(msg.Tasks) == 0 {
			return m
		}
		s := &serverView{name: msg.Server, address: msg.Address, taskMap: make(map[string]*taskView, len(msg.Tasks))}
		meta := make(map[string]gorun.TaskMeta, len(msg.TaskMeta))
		for _, tm := range msg.TaskMeta {
			meta[tm.Name] = tm
		}
		for _, name := range msg.Tasks {
			tv := &taskView{name: name}
			if tm, ok := meta[name]; ok {
				tv.description = tm.Description
				tv.details = tm.Details
				tv.rerun = tm.Rerun
			}
			s.tasks = append(s.tasks, tv)
			s.taskMap[name] = tv
		}
		m.servers = append(m.servers, s)
		if m.serverMap == nil {
			m.serverMap = make(map[string]*serverView)
		}
		m.serverMap[s.name] = s
		m.flatTasksCache = append(m.flatTasksCache, s.tasks...)

	case gorun.TaskEvent:
		if s := m.server(msg.Server); s != nil {
			if t := s.task(msg.Task); t != nil {
				// keep cached counters in sync
				switch t.state {
				case gorun.TaskRunning:
					if s.runningCount > 0 {
						s.runningCount--
					}
				}
				t.state = msg.State
				t.event = msg
				t.copy = nil
				if msg.State == gorun.TaskRunning {
					t.started = time.Now()
					s.runningCount++
				}
				switch msg.State {
				case gorun.TaskDone:
					s.doneCount++
				case gorun.TaskFailed:
					s.failedCount++
					t.expanded = true
				case gorun.TaskSkipped:
					s.skippedCount++
					t.expanded = true
				case gorun.TaskUnchanged:
					s.unchangedCount++
				case gorun.TaskWouldRun:
					s.wouldRunCount++
				}
			}
		}

	case gorun.ServerErrorEvent:
		if s := m.server(msg.Server); s != nil {
			s.err = msg.Err
		}

	case gorun.CopyProgressEvent:
		if s := m.server(msg.Server); s != nil {
			if t := s.task(msg.Task); t != nil && t.state == gorun.TaskRunning {
				if msg.Done {
					t.copy = nil
				} else {
					ev := msg
					t.copy = &ev
				}
			}
		}
	}
	return m
}

// applyBatch folds a batch of gorun events into the model.
func (m tuiModel) applyBatch(events []any) tuiModel {
	for _, ev := range events {
		m = m.applyEvent(ev)
	}
	return m
}

// Update applies run events and handles key presses and window resizes.
func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Only drop incoming events on a forced quit; while waiting for a normal
	// quit to finish, keep applying them so progress stays up to date.
	if m.forceQuit {
		switch msg.(type) {
		case tuiBatchMsg, boxesRenderedMsg, gorun.TaskEvent, gorun.PlanEvent, gorun.ServerErrorEvent, gorun.ProcessFinishedEvent, gorun.CopyProgressEvent:
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.boxesDirty = true
		return m.maybeRenderBoxes()

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			if m.forceQuit {
				return m, tea.Quit
			}
			if !m.quitting {
				m.cancel()
				m.quitting = true
				return m, nil
			}
			m.forceQuit = true
			return m, tea.Quit
		case "v":
			m.verbose = !m.verbose
		case "up", "k":
			switch {
			case m.verbose:
				m.cursor = max(m.cursor-1, 0)
			case m.selectedServer() > 0:
				m.selected = m.selectedServer() - 1
				m.showSelected()
			default:
				m.scroll = max(0, m.compactScroll()-1)
				m.follow = false
			}
		case "down", "j":
			switch {
			case m.verbose:
				m.cursor = min(m.cursor+1, len(m.flatTasks())-1)
			case m.selectedServer() < len(m.servers)-1:
				m.selected = m.selectedServer() + 1
				m.showSelected()
			default:
				// past the last server, keep scrolling down to the failure boxes
				m.scroll = min(m.compactMaxScroll(), m.compactScroll()+1)
				m.follow = false
			}
		case "pgup":
			if m.verbose {
				m.cursor = max(0, m.cursor-5)
			} else {
				m.scroll = max(0, m.scroll-5)
				m.follow = false
			}
		case "pgdown":
			if m.verbose {
				m.cursor = min(len(m.flatTasks())-1, m.cursor+5)
			} else {
				m.scroll = min(m.compactMaxScroll(), m.scroll+5)
				m.follow = false
			}
		case "home", "g":
			if m.verbose {
				m.cursor = 0
			} else {
				m.selected = 0
				m.scroll = 0
				m.follow = false
			}
		case "end", "G":
			if m.verbose {
				if tasks := m.flatTasks(); len(tasks) > 0 {
					m.cursor = len(tasks) - 1
				}
			} else {
				m.selected = max(0, len(m.servers)-1)
				m.scroll = m.compactMaxScroll()
				m.follow = true
			}
		case "enter", "space":
			if tasks := m.flatTasks(); m.verbose && m.cursor < len(tasks) {
				tasks[m.cursor].expanded = !tasks[m.cursor].expanded
			} else if !m.verbose && len(m.servers) > 0 {
				m.setOpen(!m.servers[m.selectedServer()].open)
			}
		case "right", "l":
			if !m.verbose && len(m.servers) > 0 {
				m.setOpen(true)
			}
		case "left", "h":
			if !m.verbose && len(m.servers) > 0 {
				m.setOpen(false)
			}
		case "e":
			if m.verbose {
				if tasks := m.flatTasks(); m.cursor >= 0 && m.cursor < len(tasks) {
					tasks[m.cursor].expanded = true
				}
			} else if len(m.servers) > 0 {
				m.setOpen(true)
			}
		case "E":
			if m.verbose {
				for _, t := range m.flatTasks() {
					t.expanded = true
				}
			} else {
				for _, s := range m.servers {
					s.open = true
				}
			}
		case "c":
			if m.verbose {
				if tasks := m.flatTasks(); m.cursor >= 0 && m.cursor < len(tasks) {
					tasks[m.cursor].expanded = false
				}
			} else if len(m.servers) > 0 {
				m.setOpen(false)
			}
		case "C":
			if m.verbose {
				for _, t := range m.flatTasks() {
					t.expanded = false
				}
			} else {
				for _, s := range m.servers {
					s.open = false
				}
			}
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tuiBatchMsg:
		m = m.applyBatch(msg.events)
		m.boxesDirty = true
		return m.maybeRenderBoxes()

	case boxesRenderedMsg:
		// a final synchronous render may already have replaced these lines
		if msg.gen > m.boxLinesAt {
			m.boxLines, m.boxLinesAt = msg.lines, msg.gen
		}
		m.boxesBusy = false
		return m.maybeRenderBoxes()

	case gorun.RunDoneEvent:
		m.done = true
		// with no servers planned there is nothing else on screen to explain the failure
		if msg.Err != nil && len(m.servers) == 0 {
			m.runErr = msg.Err
		}
		m.elapsed = time.Since(m.started)
		if m.quitting || !m.verbose {
			// the compact view is left on the terminal, so make sure it shows
			// every failure rather than the last background render
			m.boxGen++
			m.boxLines, m.boxLinesAt = m.boxSnapshot().render(), m.boxGen
			m.boxesDirty = false
		}
		// stay open in verbose mode so the output can be browsed, unless the
		// run was cancelled, in which case quit now that it has finished
		if m.quitting || !m.verbose {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}

	return m, nil
}

// View draws the compact view (a line per server) or the verbose view (a line per task).
func (m tuiModel) View() tea.View {
	var v tea.View

	if m.verbose && !m.quitting {
		v.AltScreen = true
		v.SetContent(m.verboseView())
		return v
	}

	v.SetContent(m.compactView())
	return v
}

// header draws the title line.
func (m tuiModel) header() string {
	header := titleStyle.Render("gorun") + " " + mutedStyle.Render(m.title)
	if m.checkOnly {
		header += " " + skipStyle.Render("· check only: nothing will change")
	}
	return header
}

// serverSummary renders the server name, progress bar and current status.
func (m tuiModel) serverSummary(s *serverView) string {
	total := len(s.tasks)
	finished := s.count(gorun.TaskDone, gorun.TaskFailed, gorun.TaskSkipped, gorun.TaskUnchanged, gorun.TaskWouldRun)

	percent := 0.0
	if total > 0 {
		percent = float64(finished) / float64(total)
	}

	bar := m.okBar
	if s.err != nil || s.count(gorun.TaskFailed) > 0 {
		bar = m.failBar
	}
	bar.SetWidth(min(38, max(8, m.width-52)))

	var status string
	switch {
	case s.err != nil:
		status = failStyle.Render("✗ could not run tasks")
	case s.count(gorun.TaskFailed) > 0 && s.finished():
		status = failStyle.Render(fmt.Sprintf("✗ %d failed", s.count(gorun.TaskFailed)))
		if n := s.count(gorun.TaskSkipped); n > 0 {
			status += skipStyle.Render(fmt.Sprintf(", %d skipped", n))
		}
	case s.finished():
		status = okStyle.Render("✓ done")
	default:
		for _, t := range s.tasks {
			if t.state == gorun.TaskRunning {
				status = m.spinner.View() + " " + t.name + " " + timeStyle.Render(formatDuration(time.Since(t.started)))
				if t.copy != nil {
					status = m.spinner.View() + " " + t.name + " " + m.copyStatus(t.copy, false)
				}
				break
			}
		}
		if status == "" {
			startedAny := false
			for _, t := range s.tasks {
				if t.state != gorun.TaskPending {
					startedAny = true
					break
				}
			}
			if startedAny {
				status = m.spinner.View() + mutedStyle.Render(" waiting for next task")
			} else {
				status = m.spinner.View() + mutedStyle.Render(" connecting to host")
			}
		}
	}

	// Make server name red if it has failed tasks
	serverNameStyle := serverStyle
	if s.err != nil || s.count(gorun.TaskFailed) > 0 {
		serverNameStyle = failStyle
	}

	return fmt.Sprintf(
		"%s %s\n%s %s  %s",
		serverNameStyle.Render(s.name),
		mutedStyle.Render(s.address),
		mutedStyle.Render("[")+bar.ViewAs(percent)+mutedStyle.Render("]"),
		mutedStyle.Render(fmt.Sprintf("%2d/%d", finished, total)),
		status,
	)
}

// compactHead returns the lines above the server summaries.
func (m tuiModel) compactHead() []string {
	lines := []string{m.header(), ""}
	switch {
	case m.runErr != nil:
		lines = append(lines, strings.Split(m.runErrorBox(), "\n")...)
	case len(m.servers) == 0:
		lines = append(lines, m.spinner.View()+" building pipelines")
	}
	return lines
}

// each server summary takes two lines plus a blank one
const serverSummaryLines = 3

// serverBlockLines is how many lines server s takes in the compact view: its
// summary, its tasks when it is open, and a blank line.
func serverBlockLines(s *serverView) int {
	if s.open {
		return serverSummaryLines + len(s.tasks)
	}
	return serverSummaryLines
}

// compactLineCount is how many lines the compact view needs in full.
func (m tuiModel) compactLineCount() int {
	n := len(m.compactHead()) + len(m.boxLines)
	for _, s := range m.servers {
		n += serverBlockLines(s)
	}
	return n
}

// selectedServer is the index of the server under the compact view's cursor.
func (m tuiModel) selectedServer() int {
	return max(0, min(m.selected, len(m.servers)-1))
}

// serverBlock renders lines [from, to) of server i's block in the compact view.
func (m tuiModel) serverBlock(i, from, to int) []string {
	s := m.servers[i]
	n := serverBlockLines(s)
	to = min(to, n)
	lines := make([]string, 0, max(0, to-from))
	if from < 2 {
		pointer := "  "
		selected := i == m.selectedServer() && !m.quitting
		if selected {
			pointer = cursorStyle.Render("▶ ")
		}
		summary := strings.SplitN(m.serverSummary(s), "\n", 2)
		summary[0], summary[1] = pointer+summary[0], "  "+summary[1]
		if selected {
			summary[0] = highlightSelectedRow(summary[0], m.width)
		}
		lines = append(lines, summary[from:min(to, 2)]...)
	}
	for j := max(from, 2); j < min(to, n-1); j++ {
		lines = append(lines, "    "+m.taskStatus(s.tasks[j-2]))
	}
	if to == n {
		lines = append(lines, "")
	}
	return lines
}

// compactWindow renders lines [from, to) of the compact view. Only the visible
// server summaries and tasks are rendered and the failure boxes come
// pre-rendered, so the cost depends on the window height rather than the size
// of the run.
func (m tuiModel) compactWindow(from, to int) []string {
	lines := make([]string, 0, max(0, to-from))
	pos := 0 // first line of the part being added
	add := func(n int, render func(from, to int) []string) {
		if pos < to && pos+n > from {
			lines = append(lines, render(max(0, from-pos), min(n, to-pos))...)
		}
		pos += n
	}

	head := m.compactHead()
	add(len(head), func(from, to int) []string { return head[from:to] })
	for i, s := range m.servers {
		if pos >= to {
			break
		}
		add(serverBlockLines(s), func(from, to int) []string { return m.serverBlock(i, from, to) })
	}
	add(len(m.boxLines), func(from, to int) []string { return m.boxLines[from:to] })
	return lines
}

// compactHeight is how many lines of the compact view fit above the footer.
func (m tuiModel) compactHeight() int {
	return max(m.height-len(strings.Split(m.footer(), "\n")), 5)
}

// compactMaxScroll is how far the compact view can scroll.
func (m tuiModel) compactMaxScroll() int {
	return max(0, m.compactLineCount()-m.compactHeight())
}

// compactScroll is the line the compact view starts at.
func (m tuiModel) compactScroll() int {
	if m.follow {
		return m.compactMaxScroll()
	}
	return max(0, min(m.scroll, m.compactMaxScroll()))
}

// setOpen shows or hides the selected server's tasks in the compact view.
func (m *tuiModel) setOpen(open bool) {
	m.servers[m.selectedServer()].open = open
	m.showSelected()
}

// showSelected scrolls the compact view so the selected server is on screen,
// as much of it as fits, starting with its summary.
func (m *tuiModel) showSelected() {
	if len(m.servers) == 0 {
		return
	}
	start := len(m.compactHead())
	for _, s := range m.servers[:m.selectedServer()] {
		start += serverBlockLines(s)
	}
	end := start + serverBlockLines(m.servers[m.selectedServer()]) - 1 // without the blank line

	scroll := m.compactScroll()
	if end > scroll+m.compactHeight() {
		scroll = end - m.compactHeight()
	}
	scroll = min(scroll, start)
	m.scroll = max(0, min(scroll, m.compactMaxScroll()))
	m.follow = false
}

// compactView draws a line and a progress bar per server, with failures and skips boxed below.
func (m tuiModel) compactView() string {
	scroll := m.compactScroll()
	return strings.Join(m.compactWindow(scroll, scroll+m.compactHeight()), "\n") + "\n" + m.footer()
}

// boxesRenderedMsg delivers compact view failure/skip boxes rendered in the background.
type boxesRenderedMsg struct {
	gen   int
	lines []string
}

// boxCache keeps rendered boxes between background renders, so only boxes
// whose content changed are rendered again.
type boxCache struct {
	mu    sync.Mutex
	boxes map[string]cachedBox
}

// cachedBox is a rendered failure or skip box, kept until what it shows changes.
type cachedBox struct {
	sig   string
	lines []string
}

// boxKind is what a box reports: a server error, a failed task or a skipped one.
type boxKind int

const (
	serverErrorBoxKind boxKind = iota
	failureBoxKind
	skipBoxKind
)

// boxItem is a copy of everything a box needs, so it can be rendered off the
// UI loop without touching the live model.
type boxItem struct {
	kind      boxKind
	server    string
	serverErr error
	task      taskView
	skipped   []string
}

// key identifies the box in the cache.
func (it boxItem) key() string {
	return fmt.Sprintf("%d\x00%s\x00%s", it.kind, it.server, it.task.name)
}

// sig changes whenever the rendered box would.
func (it boxItem) sig(width int) string {
	ev := it.task.event
	errText := ""
	if ev.Err != nil {
		errText = ev.Err.Error()
	}
	if it.serverErr != nil {
		errText = it.serverErr.Error()
	}
	return fmt.Sprintf("%d|%d|%d|%d|%d|%d|%s|%d|%d|%d", width, it.task.state, ev.ExitCode, ev.Duration, ev.Attempt, ev.Attempts, errText, len(ev.Stdout), len(ev.Stderr), len(it.skipped))
}

// boxSnapshot is a copy of what the compact view's boxes show, rendered off the UI loop.
type boxSnapshot struct {
	gen   int
	width int
	items []boxItem
	cache *boxCache
}

// boxSnapshot copies the failures, skips and server errors shown in the compact view.
func (m tuiModel) boxSnapshot() boxSnapshot {
	snap := boxSnapshot{gen: m.boxGen, width: m.width, cache: m.boxCache}
	for _, s := range m.servers {
		if s.err != nil {
			snap.items = append(snap.items, boxItem{kind: serverErrorBoxKind, server: s.name, serverErr: s.err})
		}
		if s.failedCount == 0 && s.skippedCount == 0 {
			continue
		}
		var skipped []string
		if s.failedCount > 0 {
			skipped = s.skipped()
		}
		for _, t := range s.tasks {
			switch t.state {
			case gorun.TaskFailed:
				snap.items = append(snap.items, boxItem{kind: failureBoxKind, server: s.name, task: *t, skipped: skipped})
			case gorun.TaskSkipped:
				snap.items = append(snap.items, boxItem{kind: skipBoxKind, server: s.name, task: *t})
			}
		}
	}
	return snap
}

// render returns the lines of every box, reusing cached boxes that haven't changed.
func (snap boxSnapshot) render() []string {
	if snap.cache == nil {
		snap.cache = &boxCache{}
	}
	snap.cache.mu.Lock()
	defer snap.cache.mu.Unlock()

	r := tuiModel{width: snap.width}
	next := make(map[string]cachedBox, len(snap.items))
	lines := []string{}
	for _, it := range snap.items {
		key, sig := it.key(), it.sig(snap.width)
		c, ok := snap.cache.boxes[key]
		if !ok || c.sig != sig {
			var box string
			switch it.kind {
			case serverErrorBoxKind:
				box = r.renderServerErrorBox(it.server, it.serverErr)
			case failureBoxKind:
				box = r.renderFailureBox(it.server, &it.task, it.skipped)
			case skipBoxKind:
				box = r.skipBox(it.server, &it.task)
			}
			c = cachedBox{sig: sig, lines: strings.Split(box, "\n")}
		}
		next[key] = c
		lines = append(lines, c.lines...)
	}
	snap.cache.boxes = next
	return lines
}

// maybeRenderBoxes starts a background render of the compact view boxes when
// they are out of date and no render is already running.
func (m tuiModel) maybeRenderBoxes() (tea.Model, tea.Cmd) {
	if !m.boxesDirty || m.boxesBusy {
		return m, nil
	}
	m.boxGen++
	snap := m.boxSnapshot()
	m.boxesDirty = false
	m.boxesBusy = true
	return m, func() tea.Msg {
		return boxesRenderedMsg{gen: snap.gen, lines: snap.render()}
	}
}

// verboseView draws every server with a row per task, and the output of expanded tasks.
func (m tuiModel) verboseView() string {
	lines := []string{m.header(), ""}
	if m.runErr != nil {
		lines = append(lines, strings.Split(m.runErrorBox(), "\n")...)
	}

	tasks := m.flatTasks()
	total := len(tasks)
	if total == 0 {
		return strings.Join(lines, "\n") + "\n" + m.footer()
	}

	cursor := m.cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}

	footer := m.footer()
	// leave room for the header, footer and the "tasks above/below" markers
	bodyHeight := max(m.height-len(lines)-len(strings.Split(footer, "\n"))-2, 3)
	startIdx := max(0, cursor-bodyHeight/3)
	endIdx := min(total, startIdx+bodyHeight)
	startIdx = max(0, min(startIdx, endIdx-bodyHeight))

	// body holds the rendered tasks, lineTask the task each row line belongs to
	body := []string{}
	lineTask := []int{}
	cursorLine := 0
	add := func(task int, s ...string) {
		for _, l := range s {
			body = append(body, l)
			lineTask = append(lineTask, task)
		}
	}

	i := 0
	for _, s := range m.servers {
		serverStart := i
		serverEnd := i + len(s.tasks)
		if serverEnd <= startIdx || serverStart >= endIdx {
			i = serverEnd
			continue
		}

		add(-1, strings.Split(m.serverSummary(s), "\n")...)
		if s.err != nil {
			add(-1, strings.Split(indent(m.serverErrorBox(s), 2), "\n")...)
		}

		for _, t := range s.tasks {
			if i >= startIdx && i < endIdx {
				pointer := "  "
				if i == cursor {
					pointer = cursorStyle.Render("▶ ")
					cursorLine = len(body)
				}
				line := pointer + m.taskRow(t)
				if i == cursor {
					line = highlightSelectedRow(line, m.width)
				}
				add(i, line)

				if t.expanded {
					box := m.outputBox(t)
					if t.state == gorun.TaskFailed {
						box = m.failureBox(s, t)
					}
					if box != "" {
						add(-1, strings.Split(indent(box, 4), "\n")...)
					}
				}
			}
			i++
		}
		add(-1, "")
	}

	// expanded boxes can make the body taller than the screen: crop it around
	// the cursor so the selected task is always visible
	top := max(0, min(cursorLine-bodyHeight/3, len(body)-bodyHeight))
	bottom := min(len(body), top+bodyHeight)
	first, last := cursor, cursor
	for l := top; l < bottom; l++ {
		if lineTask[l] >= 0 {
			first, last = min(first, lineTask[l]), max(last, lineTask[l])
		}
	}

	if first > 0 {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("… %d tasks above", first)))
	}
	lines = append(lines, body[top:bottom]...)
	if below := total - 1 - last; below > 0 {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("… %d tasks below", below)))
	}

	return strings.Join(lines, "\n") + "\n" + footer
}

// taskRow renders a task's row, with a marker showing whether its output is open.
func (m tuiModel) taskRow(t *taskView) string {
	toggle := mutedStyle.Render(" ▸")
	if t.expanded {
		toggle = mutedStyle.Render(" ▾")
	}
	return m.taskStatus(t) + toggle
}

// taskStatus renders a task's state, name, description and timing on one line.
func (m tuiModel) taskStatus(t *taskView) string {
	var icon, detail string

	attempt := ""
	if t.event.Attempts > 1 {
		attempt = fmt.Sprintf("%d/%d · ", t.event.Attempt, t.event.Attempts)
	}

	switch t.state {
	case gorun.TaskPending:
		icon = mutedStyle.Render("○")
	case gorun.TaskRunning:
		icon = m.spinner.View()
		if t.event.Attempts > 1 && t.event.Err != nil {
			attempt = fmt.Sprintf("retry %d/%d · ", t.event.Attempt, t.event.Attempts)
		}
		if phase := phaseLabel(t.event.Phase); phase != "" {
			attempt += phase + " · "
		}
		detail = mutedStyle.Render(attempt) + timeStyle.Render(formatDuration(time.Since(t.started)))
		if t.copy != nil {
			detail = mutedStyle.Render(attempt) + m.copyStatus(t.copy, true) + " " + timeStyle.Render(formatDuration(time.Since(t.started)))
		}
	case gorun.TaskDone:
		icon = okStyle.Render("✓")
		if t.event.Verify != nil {
			attempt += "verified · "
		}
		detail = mutedStyle.Render(attempt) + timeStyle.Render(formatDuration(t.event.Duration))
	case gorun.TaskUnchanged:
		icon = mutedStyle.Render("=")
		detail = mutedStyle.Render("unchanged · "+attempt) + timeStyle.Render(formatDuration(t.event.Duration))
	case gorun.TaskWouldRun:
		icon = skipStyle.Render("~")
		why := "no check"
		if t.event.Check != nil {
			why = "check found changes"
		}
		detail = skipStyle.Render("would run") + mutedStyle.Render(" · "+why)
	case gorun.TaskFailed:
		icon = failStyle.Render("✗")
		if t.event.Phase == gorun.PhaseCheck || t.event.Phase == gorun.PhaseVerify {
			attempt = string(t.event.Phase) + " failed · " + attempt
		}
		detail = failStyle.Render(fmt.Sprintf("exit %d", t.event.ExitCode)) + mutedStyle.Render(" · "+attempt) + timeStyle.Render(formatDuration(t.event.Duration))
	case gorun.TaskSkipped:
		icon = skipStyle.Render("⊘")
		detail = skipStyle.Render("skipped")
	}

	description := ""
	if t.description != "" && t.copy == nil { // a copy's progress needs the room
		// Leave room for the icon, name, duration and toggle so rows don't wrap.
		width := max(10, min(48, m.width-44))
		description = " " + mutedStyle.Render(truncate(t.description, width))
	}

	warning := ""
	if t.rerun == gorun.RerunUnsafe {
		warning = " " + skipStyle.Render("⚠")
	}

	return fmt.Sprintf("%s %-24s%s%s %s", icon, t.name, description, warning, detail)
}

// boxWidth is how wide the failure and output boxes are drawn.
func (m tuiModel) boxWidth() int {
	return min(m.width-6, 110)
}

// outputBox renders a finished task's output for the verbose view.
func (m tuiModel) outputBox(t *taskView) string {
	sections := []string{}

	if t.description != "" {
		sections = append(sections, serverStyle.Render(t.description))
	}
	if t.details != "" {
		sections = append(sections, mutedStyle.Render(t.details))
	}
	if t.rerun == gorun.RerunUnsafe {
		sections = append(sections, skipStyle.Render("⚠ not safe to rerun: running this task again may change the server"))
	}

	if t.state == gorun.TaskSkipped && t.event.Err != nil {
		sections = append(sections, skipStyle.Render("skipped: ")+t.event.Err.Error())
	}
	if t.state == gorun.TaskRunning && t.event.Err != nil && t.event.Attempts > 1 {
		sections = append(sections, failStyle.Render(fmt.Sprintf("attempt %d/%d failed: ", t.event.Attempt-1, t.event.Attempts))+t.event.Err.Error())
	}
	if out := tail(t.event.Stdout, outputTailLines); out != "" {
		sections = append(sections, mutedStyle.Render("stdout")+"\n"+out)
	}
	if out := tail(t.event.Stderr, outputTailLines); out != "" {
		sections = append(sections, mutedStyle.Render("stderr")+"\n"+out)
	}
	if len(sections) == 0 {
		if t.state == gorun.TaskPending || t.state == gorun.TaskRunning {
			sections = append(sections, mutedStyle.Render("(no description or output yet)"))
		} else {
			sections = append(sections, mutedStyle.Render("(no output)"))
		}
	}

	return outputBoxStyle.Width(m.boxWidth()).Render(strings.Join(sections, "\n\n"))
}

// failureBox renders why a task failed: the error, then the tail of stderr (or stdout).
func (m tuiModel) failureBox(s *serverView, t *taskView) string {
	return m.renderFailureBox(s.name, t, s.skipped())
}

// skipped names the server's skipped tasks.
func (s *serverView) skipped() []string {
	skipped := []string{}
	for _, st := range s.tasks {
		if st.state == gorun.TaskSkipped {
			skipped = append(skipped, st.name)
		}
	}
	return skipped
}

// renderFailureBox draws a failed task: the reason, exit code, the end of its stderr and the tasks skipped because of it.
func (m tuiModel) renderFailureBox(server string, t *taskView, skipped []string) string {
	attempt := ""
	if t.event.Attempts > 1 {
		attempt = fmt.Sprintf(" · attempt %d/%d", t.event.Attempt, t.event.Attempts)
	}
	title := failStyle.Render(fmt.Sprintf("✗ %s › %s failed", server, t.name)) +
		mutedStyle.Render(fmt.Sprintf("  exit %d · ", t.event.ExitCode)) + timeStyle.Render(formatDuration(t.event.Duration)) + mutedStyle.Render(attempt)

	sections := []string{title}

	if t.details != "" {
		sections = append(sections, mutedStyle.Render(t.details))
	}
	if t.rerun == gorun.RerunUnsafe {
		sections = append(sections, skipStyle.Render("⚠ not safe to rerun: running this task again may change the server"))
	}

	if t.event.Err != nil {
		sections = append(sections, failStyle.Render("reason: ")+t.event.Err.Error())
	}
	if out := tail(t.event.Stderr, failTailLines); out != "" && (t.event.Err == nil || strings.TrimSpace(out) != t.event.Err.Error()) {
		sections = append(sections, mutedStyle.Render("stderr")+"\n"+out)
	} else if out := tail(t.event.Stdout, failTailLines); out != "" {
		sections = append(sections, mutedStyle.Render("stdout")+"\n"+out)
	}

	// a failure stops the rest of the server's pipeline, so name what didn't run
	if len(skipped) > 0 {
		sections = append(sections, skipStyle.Render(fmt.Sprintf("skipped %d tasks: ", len(skipped)))+strings.Join(skipped, ", "))
	}

	return failBoxStyle.Width(m.boxWidth()).Render(strings.Join(sections, "\n"))
}

// skipBox renders why a task was skipped and which task caused it.
func (m tuiModel) skipBox(server string, t *taskView) string {
	title := skipStyle.Render(fmt.Sprintf("⊘ %s › %s skipped", server, t.name))

	sections := []string{title}
	if t.event.Err != nil {
		sections = append(sections, skipStyle.Render("reason: ")+t.event.Err.Error())
	}
	if out := tail(t.event.Stderr, failTailLines); out != "" {
		sections = append(sections, mutedStyle.Render("stderr")+"\n"+out)
	}

	return outputBoxStyle.BorderForeground(colorSkip).Width(m.boxWidth()).Render(strings.Join(sections, "\n"))
}

// runErrorBox draws the error that stopped the whole run.
func (m tuiModel) runErrorBox() string {
	title := failStyle.Render("✗ could not start the run")
	return failBoxStyle.Width(m.boxWidth()).Render(title + "\n" + failStyle.Render("reason: ") + m.runErr.Error())
}

// serverErrorBox draws why a server could not run its tasks.
func (m tuiModel) serverErrorBox(s *serverView) string {
	return m.renderServerErrorBox(s.name, s.err)
}

// renderServerErrorBox draws a server's error in a red box.
func (m tuiModel) renderServerErrorBox(server string, err error) string {
	title := failStyle.Render(fmt.Sprintf("✗ %s could not run tasks", server))
	return failBoxStyle.Width(m.boxWidth()).Render(title + "\n" + failStyle.Render("reason: ") + err.Error())
}

// footer draws the run's totals once it is over, and the key help.
func (m tuiModel) footer() string {
	if m.done || m.quitting {
		total, failed, skipped, unchanged, wouldRun := 0, 0, 0, 0, 0
		for _, s := range m.servers {
			total += len(s.tasks)
			failed += s.count(gorun.TaskFailed)
			skipped += s.count(gorun.TaskSkipped)
			unchanged += s.count(gorun.TaskUnchanged)
			wouldRun += s.count(gorun.TaskWouldRun)
		}

		elapsed := m.elapsed
		if !m.done {
			elapsed = time.Since(m.started)
		}

		var summary string
		switch {
		case m.runErr != nil:
			summary = failStyle.Render("✗ could not start the run") + mutedStyle.Render(" after ") + timeStyle.Render(formatDuration(elapsed))
		case !m.done:
			summary = skipStyle.Render("⊘ cancelled") + mutedStyle.Render(" after ") + timeStyle.Render(formatDuration(elapsed))
		case m.failed():
			summary = failStyle.Render(fmt.Sprintf("✗ %d of %d tasks failed", failed, total))
			if skipped > 0 {
				summary += skipStyle.Render(fmt.Sprintf(", %d skipped", skipped))
			}
			summary += mutedStyle.Render(" in ") + timeStyle.Render(formatDuration(elapsed))
		case m.checkOnly:
			summary = okStyle.Render("✓ checked") + mutedStyle.Render(fmt.Sprintf(": %d would run, %d unchanged, nothing changed", wouldRun, unchanged)) +
				mutedStyle.Render(" in ") + timeStyle.Render(formatDuration(elapsed))
		default:
			summary = okStyle.Render(fmt.Sprintf("✓ all %d tasks completed", total))
			if unchanged > 0 {
				summary += mutedStyle.Render(fmt.Sprintf(", %d unchanged", unchanged))
			}
			summary += mutedStyle.Render(" in ") + timeStyle.Render(formatDuration(elapsed))
		}

		if m.quitting {
			if !m.done {
				summary += mutedStyle.Render("  (waiting for tasks to finish; q to force quit)")
			}
			return summary + "\n"
		}
		return summary + "\n" + mutedStyle.Render("↑/↓/pgup/pgdown/home/end move · enter details/output · e/E expand all · c/C collapse all · v compact · q quit")
	}

	if m.verbose {
		return mutedStyle.Render("↑/↓/pgup/pgdown/home/end move · enter details/output · e/E expand all · c/C collapse all · v compact · q quit")
	}
	return mutedStyle.Render("↑/↓ select server · enter/→ show tasks · ← hide · e/E expand all · c/C collapse all · pgup/pgdown/home/end scroll · v verbose · q quit")
}

// tail returns the last n lines of s, noting how many were cut.
func tail(s string, n int) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", "    ")
	s = strings.TrimRight(s, "\n ")
	if s == "" {
		return ""
	}

	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}

	cut := len(lines) - n
	return mutedStyle.Render(fmt.Sprintf("… %d earlier lines", cut)) + "\n" + strings.Join(lines[cut:], "\n")
}

// indent prefixes every line of s with n spaces.
func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// truncate shortens s to n runes, adding an ellipsis when it cuts anything.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// formatDuration shows d to a tenth of a second under a minute, and to the second above.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}

// highlightSelectedRow draws line as the row under the cursor, width cells wide.
func highlightSelectedRow(line string, width int) string {
	if width > 0 {
		line = truncateANSI(line, width)
		return selectedRowStyle.Width(width).Render(ansi.Strip(line))
	}
	return selectedRowStyle.Render(ansi.Strip(line))
}

// runTUI runs the deployment with the interactive TUI and returns an error if anything failed.
// main sends the logs to the log file (or discards them) first, as they would corrupt the TUI.
func runTUI(ctx context.Context, cancel context.CancelFunc, cfg *gorun.Config, verbose bool) error {
	defer drainTerminalInput()
	model := newTUIModel(cfg.Title, verbose, cancel)
	model.checkOnly = cfg.CheckOnly
	p := tea.NewProgram(model, tea.WithEnvironment(append(os.Environ(), "SSH_TTY=1")))

	h, herr := history.Open(getHistoryDBPath())
	if herr == nil {
		defer func() { _ = h.Close() }()
	}

	// the recorder hands events to a goroutine that saves them, so database writes never slow the run
	recordCh := make(chan any, 4096)
	recordDone := make(chan struct{})
	go func() {
		for ev := range recordCh {
			if herr == nil {
				h.Record(ev)
			}
		}
		close(recordDone)
	}()
	cfg.Recorder = gorun.RecorderFunc(func(event any) { recordCh <- event })

	// Batch events instead of forwarding every one to the TUI: a large run can
	// emit tens of thousands of events, and handling them one at a time (with a
	// render after each) freezes input. Events are coalesced over a short window
	// and applied in a single Update.
	var (
		batchMu   sync.Mutex
		batch     []any
		scheduled bool
	)
	flushBatch := func() {
		batchMu.Lock()
		events := batch
		batch = nil
		scheduled = false
		batchMu.Unlock()
		if len(events) > 0 {
			p.Send(tuiBatchMsg{events: events})
		}
	}
	scheduleFlush := func() {
		batchMu.Lock()
		if scheduled {
			batchMu.Unlock()
			return
		}
		scheduled = true
		batchMu.Unlock()
		time.AfterFunc(100*time.Millisecond, flushBatch)
	}

	cfg.OnEvent = func(event any) {
		batchMu.Lock()
		batch = append(batch, event)
		batchMu.Unlock()
		scheduleFlush()
	}

	done := make(chan error, 1)
	go func() {
		err := cfg.Run(ctx)
		close(recordCh)
		<-recordDone
		done <- err
		// flush any events still pending so the final state lands before RunDone
		flushBatch()
		p.Send(gorun.RunDoneEvent{Type: "run_done", GorunUUID: cfg.GorunUUID, Err: err})
	}()

	final, err := p.Run()
	fm, _ := final.(tuiModel)
	if !fm.forceQuit {
		<-done
	}
	if err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	m := final.(tuiModel)
	switch {
	case m.runErr != nil:
		return m.runErr
	case !m.done:
		return errors.New("deployment cancelled")
	case m.failed():
		return errors.New("one or more tasks failed")
	}

	return nil
}

// copyStatus shows how far a copy has got: "↑ release.tar.gz ■■■■□□□□ 43% 440.0 MB / 1.0 GB · 12.3 MB/s".
// The compact view's server line leaves out the file name, which the task row shows.
func (m tuiModel) copyStatus(ev *gorun.CopyProgressEvent, withName bool) string {
	bar := m.copyBar
	bar.SetWidth(12)
	pct := ev.Percent / 100
	out := copyStyle.Render("↑ ")
	if withName {
		out += copyName(*ev) + " "
	}
	return out + bar.ViewAs(pct) + " " + fmt.Sprintf("%3d%%", int(ev.Percent)) + " " + mutedStyle.Render(copySizes(*ev))
}

// phaseLabel says what a running task with a check or verify is doing: "checking" or "verifying".
func phaseLabel(phase gorun.Phase) string {
	switch phase {
	case gorun.PhaseCheck:
		return "checking"
	case gorun.PhaseVerify:
		return "verifying"
	}
	return ""
}
