package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	name     string
	state    TaskState
	started  time.Time
	event    TaskEvent
	expanded bool
}

type serverView struct {
	name    string
	address string
	tasks   []*taskView
	err     error
}

func (s *serverView) task(name string) *taskView {
	for _, t := range s.tasks {
		if t.name == name {
			return t
		}
	}
	return nil
}

func (s *serverView) count(states ...TaskState) int {
	n := 0
	for _, t := range s.tasks {
		if slices.Contains(states, t.state) {
			n++
		}
	}
	return n
}

func (s *serverView) finished() bool {
	return s.err != nil || s.count(TaskPending, TaskRunning) == 0
}

// tuiModel is the bubbletea model for a gorun deployment.
type tuiModel struct {
	title   string
	servers []*serverView
	spinner spinner.Model
	okBar   progress.Model
	failBar progress.Model
	cancel  context.CancelFunc

	runErr   error // the run failed before any server started
	verbose  bool
	cursor   int // index into the flattened task list (verbose view)
	width    int
	height   int
	started  time.Time
	elapsed  time.Duration
	done     bool
	quitting bool
}

func newTUIModel(title string, verbose bool, cancel context.CancelFunc) tuiModel {
	return tuiModel{
		title:   title,
		verbose: verbose,
		cancel:  cancel,
		started: time.Now(),
		width:   80,
		height:  24,
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(cursorStyle)),
		okBar:   progress.New(progress.WithColors(colorAccent, colorOK), progress.WithFillCharacters('█', '░'), progress.WithoutPercentage()),
		failBar: progress.New(progress.WithColors(colorFail), progress.WithFillCharacters('█', '░'), progress.WithoutPercentage()),
	}
}

func (m tuiModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m tuiModel) server(name string) *serverView {
	for _, s := range m.servers {
		if s.name == name {
			return s
		}
	}
	return nil
}

// flatTasks returns every task across all servers in display order.
func (m tuiModel) flatTasks() []*taskView {
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
		if s.err != nil || s.count(TaskFailed) > 0 {
			return true
		}
	}
	return false
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			if !m.done {
				m.cancel()
			}
			m.quitting = true
			return m, tea.Quit
		case "v":
			m.verbose = !m.verbose
		case "up", "k":
			m.cursor = max(m.cursor-1, 0)
		case "down", "j":
			m.cursor = min(m.cursor+1, len(m.flatTasks())-1)
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

	case PlanEvent:
		s := &serverView{name: msg.Server, address: msg.Address}
		for _, name := range msg.Tasks {
			s.tasks = append(s.tasks, &taskView{name: name})
		}
		m.servers = append(m.servers, s)
		return m, nil

	case TaskEvent:
		if s := m.server(msg.Server); s != nil {
			if t := s.task(msg.Task); t != nil {
				t.state = msg.State
				t.event = msg
				if msg.State == TaskRunning {
					t.started = time.Now()
				}
				// failures always open so the reason is visible straight away
				if msg.State == TaskFailed {
					t.expanded = true
				}
			}
		}
		return m, nil

	case ServerErrorEvent:
		if s := m.server(msg.Server); s != nil {
			s.err = msg.Err
		}
		return m, nil

	case RunDoneEvent:
		m.done = true
		// with no servers planned there is nothing else on screen to explain the failure
		if msg.Err != nil && len(m.servers) == 0 {
			m.runErr = msg.Err
		}
		m.elapsed = time.Since(m.started)
		// stay open in verbose mode so the output can be browsed
		if !m.verbose {
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
	finished := s.count(TaskDone, TaskFailed, TaskSkipped)

	percent := 0.0
	if total > 0 {
		percent = float64(finished) / float64(total)
	}

	bar := m.okBar
	if s.err != nil || s.count(TaskFailed) > 0 {
		bar = m.failBar
	}
	bar.SetWidth(min(40, max(10, m.width-50)))

	var status string
	switch {
	case s.err != nil:
		status = failStyle.Render("✗ could not run tasks")
	case s.count(TaskFailed) > 0 && s.finished():
		status = failStyle.Render(fmt.Sprintf("✗ %d failed", s.count(TaskFailed)))
		if n := s.count(TaskSkipped); n > 0 {
			status += skipStyle.Render(fmt.Sprintf(", %d skipped", n))
		}
	case s.finished():
		status = okStyle.Render("✓ done")
	default:
		for _, t := range s.tasks {
			if t.state == TaskRunning {
				status = m.spinner.View() + " " + t.name + " " + mutedStyle.Render(formatDuration(time.Since(t.started)))
				break
			}
		}
		if status == "" {
			status = m.spinner.View() + mutedStyle.Render(" connecting")
		}
	}

	return fmt.Sprintf(
		"%s %s\n%s %s  %s",
		serverStyle.Render(s.name),
		mutedStyle.Render(s.address),
		bar.ViewAs(percent),
		mutedStyle.Render(fmt.Sprintf("%2d/%d", finished, total)),
		status,
	)
}

func (m tuiModel) compactView() string {
	b := strings.Builder{}
	b.WriteString(m.header() + "\n\n")

	switch {
	case m.runErr != nil:
		b.WriteString(m.runErrorBox() + "\n")
	case len(m.servers) == 0:
		b.WriteString(m.spinner.View() + " building pipelines\n")
	}

	for _, s := range m.servers {
		b.WriteString(m.serverSummary(s) + "\n\n")
	}

	// failures are always shown in full, even in the compact view
	for _, s := range m.servers {
		if s.err != nil {
			b.WriteString(m.serverErrorBox(s) + "\n")
		}
		for _, t := range s.tasks {
			if t.state == TaskFailed {
				b.WriteString(m.failureBox(s, t) + "\n")
			}
		}
	}

	b.WriteString(m.footer())
	return b.String()
}

func (m tuiModel) verboseView() string {
	lines := []string{m.header(), ""}
	if m.runErr != nil {
		lines = append(lines, strings.Split(m.runErrorBox(), "\n")...)
	}
	cursorLine := 0
	i := 0

	for _, s := range m.servers {
		lines = append(lines, strings.Split(m.serverSummary(s), "\n")...)

		if s.err != nil {
			lines = append(lines, strings.Split(indent(m.serverErrorBox(s), 2), "\n")...)
		}

		for _, t := range s.tasks {
			pointer := "  "
			if i == m.cursor {
				pointer = cursorStyle.Render("▸ ")
				cursorLine = len(lines)
			}
			lines = append(lines, pointer+m.taskRow(t))

			if t.expanded {
				box := m.outputBox(t)
				if t.state == TaskFailed {
					box = m.failureBox(s, t)
				}
				if box != "" {
					lines = append(lines, strings.Split(indent(box, 4), "\n")...)
				}
			}
			i++
		}
		lines = append(lines, "")
	}

	// scroll so the cursor stays on screen, keeping some context above it
	height := max(m.height-2, 5)
	offset := 0
	if len(lines) > height {
		offset = min(max(cursorLine-height/3, 0), len(lines)-height)
		lines = lines[offset : offset+height]
	}

	return strings.Join(lines, "\n") + "\n" + m.footer()
}

func (m tuiModel) taskRow(t *taskView) string {
	var icon, detail string

	switch t.state {
	case TaskPending:
		icon = mutedStyle.Render("○")
	case TaskRunning:
		icon = m.spinner.View()
		detail = mutedStyle.Render(formatDuration(time.Since(t.started)))
	case TaskDone:
		icon = okStyle.Render("✓")
		detail = mutedStyle.Render(formatDuration(t.event.Duration))
	case TaskFailed:
		icon = failStyle.Render("✗")
		detail = failStyle.Render(fmt.Sprintf("exit %d", t.event.ExitCode)) + mutedStyle.Render(" · "+formatDuration(t.event.Duration))
	case TaskSkipped:
		icon = skipStyle.Render("⊘")
		detail = skipStyle.Render("skipped")
	}

	toggle := ""
	if t.state == TaskDone || t.state == TaskFailed || t.state == TaskSkipped {
		toggle = mutedStyle.Render(" ▸")
		if t.expanded {
			toggle = mutedStyle.Render(" ▾")
		}
	}

	return fmt.Sprintf("%s %-24s %s%s", icon, t.name, detail, toggle)
}

func (m tuiModel) boxWidth() int {
	return min(m.width-6, 110)
}

// outputBox renders a finished task's output for the verbose view.
func (m tuiModel) outputBox(t *taskView) string {
	sections := []string{}

	if t.state == TaskSkipped && t.event.Err != nil {
		sections = append(sections, skipStyle.Render("skipped: ")+t.event.Err.Error())
	}
	if out := tail(t.event.Stdout, outputTailLines); out != "" {
		sections = append(sections, mutedStyle.Render("stdout")+"\n"+out)
	}
	if out := tail(t.event.Stderr, outputTailLines); out != "" {
		sections = append(sections, mutedStyle.Render("stderr")+"\n"+out)
	}
	if len(sections) == 0 {
		sections = append(sections, mutedStyle.Render("(no output)"))
	}

	return outputBoxStyle.Width(m.boxWidth()).Render(strings.Join(sections, "\n\n"))
}

// failureBox renders why a task failed: the error, then the tail of stderr (or stdout).
func (m tuiModel) failureBox(s *serverView, t *taskView) string {
	title := failStyle.Render(fmt.Sprintf("✗ %s › %s failed", s.name, t.name)) +
		mutedStyle.Render(fmt.Sprintf("  exit %d · %s", t.event.ExitCode, formatDuration(t.event.Duration)))

	sections := []string{title}

	if t.event.Err != nil {
		sections = append(sections, failStyle.Render("reason: ")+t.event.Err.Error())
	}
	if out := tail(t.event.Stderr, failTailLines); out != "" {
		sections = append(sections, mutedStyle.Render("stderr")+"\n"+out)
	} else if out := tail(t.event.Stdout, failTailLines); out != "" {
		sections = append(sections, mutedStyle.Render("stdout")+"\n"+out)
	}

	// a failure stops the rest of the server's pipeline, so name what didn't run
	skipped := []string{}
	for _, st := range s.tasks {
		if st.state == TaskSkipped {
			skipped = append(skipped, st.name)
		}
	}
	if len(skipped) > 0 {
		sections = append(sections, skipStyle.Render(fmt.Sprintf("skipped %d tasks: ", len(skipped)))+strings.Join(skipped, ", "))
	}

	return failBoxStyle.Width(m.boxWidth()).Render(strings.Join(sections, "\n"))
}

func (m tuiModel) runErrorBox() string {
	title := failStyle.Render("✗ could not start the run")
	return failBoxStyle.Width(m.boxWidth()).Render(title + "\n" + failStyle.Render("reason: ") + m.runErr.Error())
}

func (m tuiModel) serverErrorBox(s *serverView) string {
	title := failStyle.Render(fmt.Sprintf("✗ %s could not run tasks", s.name))
	return failBoxStyle.Width(m.boxWidth()).Render(title + "\n" + failStyle.Render("reason: ") + s.err.Error())
}

func (m tuiModel) footer() string {
	if m.done || m.quitting {
		total, failed, skipped := 0, 0, 0
		for _, s := range m.servers {
			total += len(s.tasks)
			failed += s.count(TaskFailed)
			skipped += s.count(TaskSkipped)
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
		return summary + "\n" + mutedStyle.Render("↑/↓ move · enter toggle output · v compact · q quit")
	}

	if m.verbose {
		return mutedStyle.Render("↑/↓ move · enter toggle output · v compact · q quit")
	}
	return mutedStyle.Render("v verbose · q quit")
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

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}

// runTUI runs the deployment with the interactive TUI and returns an error if anything failed.
func runTUI(ctx context.Context, cancel context.CancelFunc, cfg *Config, verbose bool) error {
	// slog output would corrupt the TUI
	logOutput = io.Discard

	p := tea.NewProgram(newTUIModel(cfg.Title, verbose, cancel))
	cfg.OnEvent = func(event any) { p.Send(event) }

	go func() {
		err := cfg.Run(ctx)
		p.Send(RunDoneEvent{Err: err})
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
