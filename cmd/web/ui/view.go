package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/jenmud/gorun/internal/store"
)

type noGorunfileKey struct{}

// WithoutGorunfile marks ctx for a web UI started without a Gorunfile. It shows only the Live
// and History pages, so the pages that need one are left out of the menu, and names that
// would link to them are drawn as plain text.
func WithoutGorunfile(ctx context.Context) context.Context {
	return context.WithValue(ctx, noGorunfileKey{}, true)
}

type storedKey struct{}

// WithStored marks ctx for a web UI on the Gorunfiles kept in its history database, with name
// the open one. The menu then links to the Gorunfiles page, and names the open one.
func WithStored(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, storedKey{}, name)
}

// storedName is the open stored Gorunfile, and whether ctx was marked by WithStored.
func storedName(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(storedKey{}).(string)
	return name, ok
}

// Viewer is who is looking at a page, when the web UI has a login.
type Viewer struct {
	LoginOn bool   // changes need signing in
	User    string // who is signed in, or ""
	Admin   bool   // may manage users and models: an admin, or anyone while there is no login
	Users   bool   // the history database is open, so there are users to manage
	CanRun  bool   // signed in, so may start runs
	Proxy   bool   // signed in by an auth proxy, so there is no sign out here
	Path    string // the page, to come back to after signing in
}

type viewerKey struct{}

// WithViewer marks ctx with who is looking at the page, for the menu's sign in and the parts of
// a page that only an admin may use.
func WithViewer(ctx context.Context, v Viewer) context.Context {
	return context.WithValue(ctx, viewerKey{}, v)
}

func viewer(ctx context.Context) Viewer {
	v, _ := ctx.Value(viewerKey{}).(Viewer)
	return v
}

// canAdmin reports whether the viewer may manage users and the assistant's models.
func canAdmin(ctx context.Context) bool { return viewer(ctx).Admin }

// noGorunfile reports whether ctx was marked by WithoutGorunfile.
func noGorunfile(ctx context.Context) bool {
	no, _ := ctx.Value(noGorunfileKey{}).(bool)
	return no
}

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
	Servers  []ServerOption
	Tasks    []TaskOption
	Bundles  []BundleOption
	NATSOK   bool
	Flash    string
	FlashBad bool
	Path     string
}

// LivePageData is what the live page shows: the recent runs and what is running now.
type LivePageData struct {
	Stats     StatsView
	Dashboard DashboardState
	Flash     string
	FlashBad  bool
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
	Schedule   string        // the schedule that started the run, or empty
	CheckOnly  bool          // a check-only run, which changed nothing
	Origin     *store.Origin // who started the run, where, and with what; nil for older runs
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
	NATS        NATSStatus
	Browser     Browser
	Path        string
	Files       []FileOption
	Servers     []EntityRow
	Groups      []EntityRow
	Tasks       []EntityRow
	Bundles     []EntityRow
	Schedules   []EntityRow
	Targets     []string // what a schedule can target: tasks, then @bundles
	Pending     []string
	Flash       string
	FlashBad    bool
	HasPending  bool
	Loaded      bool
	EditingKind string // kind of entity being edited (e.g. "task")
	EditingName string // name of entity being edited
	Edit        EditFields
	Model       string // the model the task generator asks, as the page names it; empty hides it
	// Stored is set when the Gorunfile is kept in the history database, not as a file: it is
	// then named StoredName, has StoredVersions saved versions, and is imported into, not loaded
	Stored         bool
	StoredName     string
	StoredVersions int
}

// GorunfilesPageData is the list of Gorunfiles kept in the history database.
type GorunfilesPageData struct {
	Flash    string
	FlashBad bool
	Open     string // the one open now
	Items    []StoredGorunfileRow
}

// StoredGorunfileRow is one Gorunfile kept in the history database.
type StoredGorunfileRow struct {
	Name     string
	Versions int
	SavedAt  string
	Shares   []string // the stored Gorunfiles it includes, sharing their tasks and bundles
	SharedBy []string // the stored Gorunfiles that include it
}

// AssistantPageData is the chat page.
type AssistantPageData struct {
	Model    string // the model, as the page names it
	Private  bool   // the model runs on this machine
	Host     string // where a model that isn't private runs
	CanAdd   bool   // a Gorunfile is loaded, so tasks can be added
	Files    []FileOption
	RunID    string // the run the chat starts on, if any
	RunTitle string
	Stats    AssistantStats
	Models   *ModelsData // the built-in model's models; nil with a model server
	// Capturing is set when answers are kept for training, which the page says
	Capturing bool
}

// ModelsData is the Assistant page's list of downloaded models and the one in use.
type ModelsData struct {
	Rows     []ModelRow
	Download *ModelDownload // the download from Hugging Face going on or just over, if any
	Error    string         // why the last action failed
	// Retry, when set, offers to do the refused action anyway: it was refused because someone
	// was using the assistant
	Retry *ModelRetry
}

// ModelRetry is a model action to try again, regardless of who is using the assistant.
type ModelRetry struct {
	Action, Name, Label string
}

// ModelRow is one model the assistant can use: gorun's own, or a downloaded GGUF file.
type ModelRow struct {
	Name    string // its file name; "" for gorun's own model
	Label   string // what the page calls it
	Size    string // "" when it isn't downloaded yet
	InUse   bool   // questions go to it
	Loaded  bool   // it is in memory now
	Builtin bool   // gorun's own model, which can't be deleted
}

// ModelDownload is how a download from Hugging Face is going.
type ModelDownload struct {
	Repo, File, Status, Error string
	Running, Done             bool
}

// HubResults is a Hugging Face search, or one repository's files, for the Assistant page.
type HubResults struct {
	Query  string
	Models []HubModelRow
	Repo   *HubModelRow // set when listing one repository's files
	Files  []HubFileRow
	Error  string
}

// HubModelRow is a model repository on Hugging Face.
type HubModelRow struct {
	ID, License, Downloads string
	Permissive             bool // Apache 2.0 or MIT, like gorun's own model
	Gated                  bool // needs a Hugging Face login to download
}

// HubFileRow is one GGUF file of a repository.
type HubFileRow struct {
	Name, Size string
	Have       bool // a model with its file name is downloaded already
}

// AssistantStats describes the assistant's model: where it runs and how it is doing.
type AssistantStats struct {
	State string // one line: not downloaded, loaded on the GPU, and so on
	Tone  string // "good", "warn" or "" for the state's colour
	Rows  []StatRow
}

// StatRow is one fact in AssistantStats.
type StatRow struct {
	Label, Value, Note string
}

// EditFields are the current values the edit form starts from.
type EditFields struct {
	Hostname, Groups, Tags, SSHUser, IdentityFile string
	Cmd, Description, Tasks                       string
	Check, Verify                                 string
	Copies, DependsOn, Timeout, Retries           string // copies one per line, "src -> dst"
	InlineCopies                                  int    // inline-text copies, kept as they are
	Sudo, Local, CheckCopies                      bool
	Target, Cron, At, Timezone, Missed            string
}

// kindLabel names an entity kind for headings.
func kindLabel(kind string) string {
	switch kind {
	case "host":
		return "server"
	case "group":
		return "server group"
	}
	return kind
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
	case store.StatePending:
		return "pending"
	case store.StateRunning:
		return "running"
	case store.StateDone:
		return "done"
	case store.StateFailed:
		return "failed"
	case store.StateSkipped:
		return "skipped"
	case store.StateUnchanged:
		return "unchanged"
	case store.StateWouldRun:
		return "would run"
	default:
		return "unknown"
	}
}

// stateIcon is the one-character mark the TUI uses for a task state.
func stateIcon(state int) string {
	switch state {
	case store.StateRunning:
		return "●"
	case store.StateDone:
		return "✓"
	case store.StateFailed:
		return "✗"
	case store.StateSkipped:
		return "⊘"
	case store.StateUnchanged:
		return "="
	case store.StateWouldRun:
		return "~"
	default:
		return "○"
	}
}

// stateClass colours a task state.
func stateClass(state int) string {
	switch state {
	case store.StateRunning:
		return "text-accent"
	case store.StateDone:
		return "text-emerald-400"
	case store.StateFailed:
		return "text-red-400"
	case store.StateSkipped:
		return "text-amber-400"
	case store.StateUnchanged:
		return "text-sky-400"
	case store.StateWouldRun:
		return "text-amber-300"
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
	case store.StateDone:
		return "Finished. Open the output to see what it printed."
	case store.StateFailed:
		return "Failed. Open the output to see why, and check the exit code."
	case store.StateSkipped:
		return "Skipped because something it depends on failed."
	case store.StateUnchanged:
		return "Unchanged: its check found nothing to do, so its command didn't run."
	case store.StateWouldRun:
		return "Would run: this was a check-only run, and its check found something to do (or it has no check). Nothing was changed."
	case store.StateRunning:
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
	Type string // "cmd_file", or a copy's source: "copy → dst" or "template → dst"
	Path string
}

// TaskCard is everything the Tasks page shows for one task.
type TaskCard struct {
	Name          string
	Description   string
	Details       string
	Source        string
	Badges        []string // short flags such as sudo or local
	Average       time.Duration
	CommandSource string
	Command       string
	Copies        []string // what the task copies to the server before its command
	Check         string   // the check, run first; empty when the task has none
	CheckCopies   bool     // it compares its copies with the server's
	Verify        string   // the verify, run after the command
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
	Notice string
	Error  string

	// the add form's file choices, and the task being edited, if any
	Files       []FileOption
	EditingName string
	Edit        EditFields
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
	Notice  string
	Error   string

	// the add form's file choices and task suggestions, and the bundle being edited, if any
	Files       []FileOption
	Targets     []string
	EditingName string
	Edit        EditFields
}

// NATSStatus is how the web service reaches NATS, for the header and the Config page.
type NATSStatus struct {
	Mode        string // "embedded" (started by this web service), "external" or "off"
	URL         string // the server clients connect to
	ConfigPath  string // the config the embedded server was started with; empty for the defaults
	Connections int    // clients connected to the embedded server
	Reason      string // why NATS is off
}

// natsModeLabel says in words how NATS is reached.
func natsModeLabel(mode string) string {
	switch mode {
	case "embedded":
		return "embedded, started by gorun -serve"
	case "external":
		return "external, already running"
	}
	return "off"
}

// natsConfigLabel names the config an embedded server was started with.
func natsConfigLabel(path string) string {
	if path == "" {
		return "defaults (127.0.0.1:4222)"
	}
	return path
}
