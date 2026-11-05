package ui

import (
	"fmt"
	"time"

	"github.com/jenmud/gorun/internal/store"
)

// ServerOption is a server the run page can pick.
type ServerOption struct {
	Name     string
	Address  string
	Tags     string
	Groups   string
	Local    bool
	Tasks    int
	Selected bool
}

// TaskOption is a task the run page can pick.
type TaskOption struct {
	Name        string
	Description string
	Details     string
	Rerun       string
	Local       bool
}

// Bar is one bar in a chart.
type Bar struct {
	ID    string // the run the bar stands for
	Label string
	Value float64
	Tip   string
	Bad   bool
}

// SlowTask is a task ranked by its average duration.
type SlowTask struct {
	Name    string
	Server  string
	Average time.Duration
	Runs    int
}

// StatsView is what the stats panel shows, computed from the run history.
type StatsView struct {
	Runs        int
	Finished    int
	Succeeded   int
	SuccessRate int
	AvgDuration time.Duration
	TasksRun    int
	TasksFailed int
	Recent      []Bar
	Slowest     []SlowTask
}

// RunPageData is everything the run page shows.
type RunPageData struct {
	Servers   []ServerOption
	Tasks     []TaskOption
	Bundles   []BundleOption
	Stats     StatsView
	Dashboard DashboardState
	NATSOK    bool
	Flash     string
	FlashBad  bool
	Path      string
}

// BundleOption is a bundle the run page can run.
type BundleOption struct {
	Name        string
	Description string
	Tasks       int // tasks it expands to
	Selected    bool
}

// RunRow is one row of the history list.
type RunRow struct {
	ID         string
	Title      string
	Hostname   string
	StartedAt  time.Time
	Duration   time.Duration
	State      string // running, ok or failed
	Servers    int
	TotalTasks int
	Failed     int
	Skipped    int
}

// HistoryPageData is everything the history list shows.
type HistoryPageData struct {
	Runs  []RunRow
	Flash string
}

// ServerBlock groups one server's tasks in a run.
type ServerBlock struct {
	Name     string
	Address  string
	Error    string // why the server couldn't run its tasks at all
	Failed   bool
	Duration time.Duration
	EnvVars  map[string]string
	Vars     map[string]any
	Tasks    []TaskView
}

// RunDetailData is everything the run detail page shows.
type RunDetailData struct {
	Run     store.RunDetail
	Summary RunRow
	Servers []ServerBlock
}

// EntityRow is one table, server, group, task or bundle, in the config editor.
type EntityRow struct {
	Name    string
	Detail  string
	Source  string
	Pending string // "", "add" or "remove"
}

// FileOption is a file an entity can be added to: its path, and a short label for it.
type FileOption struct {
	Path  string
	Label string
}

// BrowseEntry is one folder or file in the Gorunfile picker.
type BrowseEntry struct {
	Name   string
	Path   string
	Dir    bool
	Likely bool // named like a Gorunfile
}

// Browser is one folder of the Gorunfile picker, on the machine running gorun.
type Browser struct {
	Dir     string
	Parent  string // empty at the filesystem root
	Entries []BrowseEntry
	Err     string
}

// ConfigPageData is everything the config editor shows.
type ConfigPageData struct {
	Browser       Browser
	Path          string
	Files         []FileOption
	Servers       []EntityRow
	Groups        []EntityRow
	Tasks         []EntityRow
	Bundles       []EntityRow
	Pending       []string
	Flash         string
	FlashBad      bool
	HasPending    bool
	Loaded        bool
	EditingKind   string // kind of entity being edited (e.g. "task")
	EditingName   string // name of entity being edited
}

// formatDuration renders a duration compactly for the UI, such as 1m02s or 340ms.
func formatDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "—"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

// stateLabel names a task state for the UI.
func stateLabel(state int) string {
	switch state {
	case 0:
		return "pending"
	case 1:
		return "running"
	case 2:
		return "done"
	case 3:
		return "failed"
	case 4:
		return "skipped"
	default:
		return "unknown"
	}
}

// stateIcon is the one-character mark the TUI uses for a task state.
func stateIcon(state int) string {
	switch state {
	case 1:
		return "●"
	case 2:
		return "✓"
	case 3:
		return "✗"
	case 4:
		return "⊘"
	default:
		return "○"
	}
}

// stateClass colours a task state.
func stateClass(state int) string {
	switch state {
	case 1:
		return "text-accent"
	case 2:
		return "text-emerald-400"
	case 3:
		return "text-red-400"
	case 4:
		return "text-amber-400"
	default:
		return "text-zinc-500"
	}
}

// runBadge describes a run's state in a badge.
func runBadge(state string) (label, class string) {
	switch state {
	case "running":
		return "running", "bg-accent/20 text-violet-300 ring-accent/40"
	case "failed":
		return "failed", "bg-red-500/15 text-red-300 ring-red-500/40"
	default:
		return "ok", "bg-emerald-500/15 text-emerald-300 ring-emerald-500/40"
	}
}

// percent formats n as a percentage string.
func percent(n int) string {
	return fmt.Sprintf("%d%%", n)
}

// barHeight scales v against max to a pixel height between 2 and h.
func barHeight(v, max float64, h int) int {
	if max <= 0 || v <= 0 {
		return 2
	}
	px := int(v / max * float64(h))
	if px < 2 {
		px = 2
	}
	return px
}

// maxBar returns the largest bar value, so charts share one scale.
func maxBar(bars []Bar) float64 {
	var m float64
	for _, b := range bars {
		if b.Value > m {
			m = b.Value
		}
	}
	return m
}

// hintFor returns the one-line hint shown under a task state, so people know what to do next.
func hintFor(state int) string {
	switch state {
	case 2:
		return "Finished. Open the output to see what it printed."
	case 3:
		return "Failed. Open the output to see why, and check the exit code."
	case 4:
		return "Skipped because something it depends on failed."
	case 1:
		return "Running now. Output appears when it finishes."
	default:
		return "Waiting for its turn."
	}
}

// TaskView is one task in a saved run. Its command, output and variables are only Loaded
// when the task is opened, so large runs stay quick to show.
type TaskView struct {
	ID           string // element id the detail is patched into
	RunID        string
	Address      string
	Loaded       bool
	NotRun       bool // it never started: its server failed or the run ended first
	ServerFailed bool // its server could not be used
	store.TaskDetail
}

// Option is one labelled setting of a task.
type Option struct {
	Name  string
	Value string
}

// TaskCard is one task of the library, as the Tasks page shows it.
// FileRef is a file referenced by a task.
type FileRef struct {
	Type string // "cmd_file", "cmd_template_file", "template", "src_file", "src_template"
	Path string
}

type TaskCard struct {
	Name          string
	Description   string
	Details       string
	Source        string
	Badges        []string // short flags such as sudo or local
	Average       time.Duration
	CommandSource string
	Command       string
	Options       []Option
	DependsOn     []string
	InBundles     []string
	RunsOn        []string
	EnvVars       map[string]string
	ConfigVars    map[string]any
	Vars          map[string]any
	Files         []FileRef // files referenced by the task
}

// TasksPageData is everything the Tasks page shows.
type TasksPageData struct {
	Tasks  []TaskCard
	Path   string
	Loaded bool
}

// BundleItem is one entry of a bundle: a task, or another bundle.
type BundleItem struct {
	Ref         string
	IsBundle    bool
	Description string
}

// BundleCard is one bundle, as the Bundles page shows it.
type BundleCard struct {
	Name        string
	Description string
	Source      string
	Items       []BundleItem
	Expanded    []string // every task it runs, nested bundles flattened
	Vars        map[string]any
	UsedBy      []string
}

// BundlesPageData is everything the Bundles page shows.
type BundlesPageData struct {
	Bundles []BundleCard
	Path    string
	Loaded  bool
}
