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

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/store"
)

var (
	colorAccent = lipgloss.Color("#7C3AED")
	colorOK     = lipgloss.Color("#22C55E")
	colorFail   = lipgloss.Color("#EF4444")
	colorSkip   = lipgloss.Color("#F59E0B")
	colorMuted  = lipgloss.Color("#6B7280")

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(colorAccent).Padding(0, 1)
	serverStyle = lipgloss.NewStyle().Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(colorMuted)
	okStyle     = lipgloss.NewStyle().Foreground(colorOK)
	failStyle   = lipgloss.NewStyle().Foreground(colorFail).Bold(true)
	skipStyle   = lipgloss.NewStyle().Foreground(colorSkip)
	cursorStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	outputBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorMuted).Padding(0, 1)
	failBoxStyle   = outputBoxStyle.BorderForeground(colorFail)
)

// how many lines of output to show in a box before truncating from the top
const (
	failTailLines   = 10
	outputTailLines = 20
)

type taskView struct {
	name        string
	description string
	details     string
	rerun       string
	state       gorun.TaskState
	started     time.Time
	event       gorun.TaskEvent
	expanded    bool
}

type serverView struct {
	name         string
	address      string
	tasks        []*taskView
	taskMap      map[string]*taskView
	err          error
	doneCount    int
	failedCount  int
	skippedCount int
	runningCount int
}

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

func (s *serverView) count(states ...gorun.TaskState) int {
	if len(states) == 1 {
		switch states[0] {
		case gorun.TaskDone:
			return s.doneCount
		case gorun.TaskFailed:
			return s.failedCount
		case gorun.TaskSkipped:
			return s.skippedCount
		case gorun.TaskRunning:
			return s.runningCount
		case gorun.TaskPending:
			return len(s.tasks) - s.doneCount - s.failedCount - s.skippedCount - s.runningCount
		}
	}
	n := 0
	for _, st := range states {
		n += s.count(st)
	}
	return n
}

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
	servers        []*serverView
	serverMap      map[string]*serverView
	flatTasksCache []*taskView
	spinner        spinner.Model
	okBar          progress.Model
	failBar        progress.Model
	cancel         context.CancelFunc

	// compact view failure/skip boxes are rendered off the UI loop (see
	// maybeRenderBoxes) so a run with thousands of failures can't freeze input
	boxLines   []string
	boxesDirty bool
	boxesBusy  bool
	boxGen     int // generation of the latest box snapshot
	boxLinesAt int // generation boxLines was rendered from
	boxCache   *boxCache

	runErr   error // the run failed before any server started
	verbose  bool
	cursor   int  // index into the flattened task list (verbose view)
	scroll   int  // line offset for the compact view
	follow   bool // keep compact view pinned to the bottom as output grows
	width    int
	height   int
	started  time.Time
	elapsed  time.Duration
	done     bool
	quitting bool
}

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
		okBar:     progress.New(progress.WithColors(colorAccent, colorOK), progress.WithFillCharacters('█', '░'), progress.WithoutPercentage()),
		failBar:   progress.New(progress.WithColors(colorFail), progress.WithFillCharacters('█', '░'), progress.WithoutPercentage()),
	}
}

func (m tuiModel) Init() tea.Cmd {
	return m.spinner.Tick
}

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
				}
			}
		}

	case gorun.ServerErrorEvent:
		if s := m.server(msg.Server); s != nil {
			s.err = msg.Err
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

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Once the user has asked to quit, drop incoming event batches instead of
	// applying them, so a backlog of queued events can't keep the UI stuck.
	if m.quitting {
		switch msg.(type) {
		case tuiBatchMsg, boxesRenderedMsg, gorun.TaskEvent, gorun.PlanEvent, gorun.ServerErrorEvent, gorun.ProcessFinishedEvent:
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
			m.cancel()
			m.quitting = true
			// Quit right away so the UI never blocks on a run that is slow to
			// wind down; cfg.Run keeps finishing in the background.
			return m, tea.Quit
		case "v":
			m.verbose = !m.verbose
		case "up", "k":
			if m.verbose {
				m.cursor = max(m.cursor-1, 0)
			} else {
				m.scroll = max(0, m.scroll-1)
				m.follow = false
			}
		case "down", "j":
			if m.verbose {
				m.cursor = min(m.cursor+1, len(m.flatTasks())-1)
			} else {
				m.scroll = min(m.compactMaxScroll(), m.scroll+1)
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
				m.scroll = 0
				m.follow = false
			}
		case "end", "G":
			if m.verbose {
				if tasks := m.flatTasks(); len(tasks) > 0 {
					m.cursor = len(tasks) - 1
				}
			} else {
				m.scroll = m.compactMaxScroll()
				m.follow = true
			}
		case "enter", "space":
			if tasks := m.flatTasks(); m.verbose && m.cursor < len(tasks) {
				tasks[m.cursor].expanded = !tasks[m.cursor].expanded
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

func (m tuiModel) header() string {
	return titleStyle.Render("gorun") + " " + mutedStyle.Render(m.title)
}

// serverSummary renders the server name, progress bar and current status.
func (m tuiModel) serverSummary(s *serverView) string {
	total := len(s.tasks)
	finished := s.count(gorun.TaskDone, gorun.TaskFailed, gorun.TaskSkipped)

	percent := 0.0
	if total > 0 {
		percent = float64(finished) / float64(total)
	}

	bar := m.okBar
	if s.err != nil || s.count(gorun.TaskFailed) > 0 {
		bar = m.failBar
	}
	bar.SetWidth(min(40, max(10, m.width-50)))

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
				status = m.spinner.View() + " " + t.name + " " + mutedStyle.Render(formatDuration(time.Since(t.started)))
				break
			}
		}
		if status == "" {
			status = m.spinner.View() + mutedStyle.Render(" connecting")
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
		bar.ViewAs(percent),
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

func (m tuiModel) compactLineCount() int {
	return len(m.compactHead()) + serverSummaryLines*len(m.servers) + len(m.boxLines)
}

// compactWindow renders lines [from, to) of the compact view. Only the visible
// server summaries are rendered and the failure boxes come pre-rendered, so the
// cost depends on the window height rather than the size of the run.
func (m tuiModel) compactWindow(from, to int) []string {
	head := m.compactHead()
	summaries := serverSummaryLines * len(m.servers)
	to = min(to, len(head)+summaries+len(m.boxLines))

	lines := make([]string, 0, max(0, to-from))
	for i := from; i < to; i++ {
		switch {
		case i < len(head):
			lines = append(lines, head[i])
		case i < len(head)+summaries:
			off := i - len(head)
			block := append(strings.Split(m.serverSummary(m.servers[off/serverSummaryLines]), "\n"), "")
			for j := off % serverSummaryLines; j < len(block) && i < to; j++ {
				lines = append(lines, block[j])
				i++
			}
			i--
		default:
			lines = append(lines, m.boxLines[i-len(head)-summaries])
		}
	}
	return lines
}

func (m tuiModel) compactMaxScroll() int {
	footerLines := len(strings.Split(m.footer(), "\n"))
	height := max(m.height-footerLines, 5)
	return max(0, m.compactLineCount()-height)
}

func (m tuiModel) compactView() string {
	footer := m.footer()
	height := max(m.height-len(strings.Split(footer, "\n")), 5)
	total := m.compactLineCount()
	scroll := m.scroll
	if m.follow {
		scroll = total - height
	}
	scroll = max(0, min(scroll, total-height))
	return strings.Join(m.compactWindow(scroll, scroll+height), "\n") + "\n" + footer
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

type cachedBox struct {
	sig   string
	lines []string
}

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
					pointer = cursorStyle.Render("▸ ")
					cursorLine = len(body)
				}
				add(i, pointer+m.taskRow(t))

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

func (m tuiModel) taskRow(t *taskView) string {
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
		detail = mutedStyle.Render(attempt + formatDuration(time.Since(t.started)))
	case gorun.TaskDone:
		icon = okStyle.Render("✓")
		detail = mutedStyle.Render(attempt + formatDuration(t.event.Duration))
	case gorun.TaskFailed:
		icon = failStyle.Render("✗")
		detail = failStyle.Render(fmt.Sprintf("exit %d", t.event.ExitCode)) + mutedStyle.Render(" · "+attempt+formatDuration(t.event.Duration))
	case gorun.TaskSkipped:
		icon = skipStyle.Render("⊘")
		detail = skipStyle.Render("skipped")
	}

	description := ""
	if t.description != "" {
		// Leave room for the icon, name, duration and toggle so rows don't wrap.
		width := max(10, min(48, m.width-44))
		description = " " + mutedStyle.Render(truncate(t.description, width))
	}

	warning := ""
	if t.rerun == gorun.RerunUnsafe {
		warning = " " + skipStyle.Render("⚠")
	}

	toggle := mutedStyle.Render(" ▸")
	if t.expanded {
		toggle = mutedStyle.Render(" ▾")
	}

	return fmt.Sprintf("%s %-24s%s%s %s%s", icon, t.name, description, warning, detail, toggle)
}

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

func (m tuiModel) renderFailureBox(server string, t *taskView, skipped []string) string {
	attempt := ""
	if t.event.Attempts > 1 {
		attempt = fmt.Sprintf(" · attempt %d/%d", t.event.Attempt, t.event.Attempts)
	}
	title := failStyle.Render(fmt.Sprintf("✗ %s › %s failed", server, t.name)) +
		mutedStyle.Render(fmt.Sprintf("  exit %d · %s%s", t.event.ExitCode, formatDuration(t.event.Duration), attempt))

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

func (m tuiModel) runErrorBox() string {
	title := failStyle.Render("✗ could not start the run")
	return failBoxStyle.Width(m.boxWidth()).Render(title + "\n" + failStyle.Render("reason: ") + m.runErr.Error())
}

func (m tuiModel) serverErrorBox(s *serverView) string {
	return m.renderServerErrorBox(s.name, s.err)
}

func (m tuiModel) renderServerErrorBox(server string, err error) string {
	title := failStyle.Render(fmt.Sprintf("✗ %s could not run tasks", server))
	return failBoxStyle.Width(m.boxWidth()).Render(title + "\n" + failStyle.Render("reason: ") + err.Error())
}

func (m tuiModel) footer() string {
	if m.done || m.quitting {
		total, failed, skipped := 0, 0, 0
		for _, s := range m.servers {
			total += len(s.tasks)
			failed += s.count(gorun.TaskFailed)
			skipped += s.count(gorun.TaskSkipped)
		}

		elapsed := m.elapsed
		if !m.done {
			elapsed = time.Since(m.started)
		}

		var summary string
		switch {
		case m.runErr != nil:
			summary = failStyle.Render("✗ could not start the run") + mutedStyle.Render(" after "+formatDuration(elapsed))
		case !m.done:
			summary = skipStyle.Render("⊘ cancelled") + mutedStyle.Render(" after "+formatDuration(elapsed))
		case m.failed():
			summary = failStyle.Render(fmt.Sprintf("✗ %d of %d tasks failed", failed, total))
			if skipped > 0 {
				summary += skipStyle.Render(fmt.Sprintf(", %d skipped", skipped))
			}
			summary += mutedStyle.Render(" in " + formatDuration(elapsed))
		default:
			summary = okStyle.Render(fmt.Sprintf("✓ all %d tasks completed", total)) + mutedStyle.Render(" in "+formatDuration(elapsed))
		}

		if m.quitting {
			return summary + "\n"
		}
		return summary + "\n" + mutedStyle.Render("↑/↓/pgup/pgdown/home/end move · enter details/output · v compact · q quit")
	}

	if m.verbose {
		return mutedStyle.Render("↑/↓/pgup/pgdown/home/end move · enter details/output · v compact · q quit")
	}
	return mutedStyle.Render("↑/↓/pgup/pgdown/home/end scroll · v verbose · q quit")
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

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}

// runTUI runs the deployment with the interactive TUI and returns an error if anything failed.
// main sends the logs to the log file (or discards them) first, as they would corrupt the TUI.
func runTUI(ctx context.Context, cancel context.CancelFunc, cfg *gorun.Config, verbose bool) error {
	defer drainTerminalInput()
	p := tea.NewProgram(newTUIModel(cfg.Title, verbose, cancel), tea.WithEnvironment(append(os.Environ(), "SSH_TTY=1")))

	st, _ := store.Open(getHistoryDBPath())
	defer func() {
		if st != nil {
			_ = st.Close()
		}
	}()

	recordCh := make(chan any, 4096)
	go func() {
		for ev := range recordCh {
			recordEvent(ctx, st, ev)
		}
	}()

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
		select {
		case recordCh <- event:
		default:
		}
		batchMu.Lock()
		batch = append(batch, event)
		batchMu.Unlock()
		scheduleFlush()
	}

	done := make(chan error, 1)
	go func() {
		err := cfg.Run(ctx)
		done <- err
		close(recordCh)
		// flush any events still pending so the final state lands before RunDone
		flushBatch()
		p.Send(gorun.RunDoneEvent{Type: "run_done", GorunUUID: cfg.GorunUUID, Err: err})
	}()

	final, err := p.Run()
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
