package ui

import (
	"net/url"
	"time"
)

// runCardClass frames a run: red when a task failed, a calm card while running.
func runCardClass(failed, finished bool) string {
	switch {
	case failed:
		return "overflow-hidden rounded-2xl border border-red-900/50 bg-red-950/10"
	case finished:
		return "overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900/40"
	default:
		return "overflow-hidden rounded-2xl border border-accent/30 bg-zinc-900/60 shadow-lg shadow-accent/5"
	}
}

// runStateLabel is the word on a run's badge.
func runStateLabel(p *ProcessState) string {
	switch {
	case !p.Finished && processHasFailed(p):
		return "failing"
	case !p.Finished:
		return "running"
	case p.Success:
		return "passed"
	default:
		return "failed"
	}
}

// runStateClass colours a run's badge.
func runStateClass(p *ProcessState) string {
	base := "rounded-full px-2.5 py-0.5 text-xs font-semibold ring-1 "
	switch {
	case !p.Finished && processHasFailed(p):
		return base + "bg-red-500/15 text-red-300 ring-red-500/40"
	case !p.Finished:
		return base + "bg-accent/20 text-violet-300 ring-accent/40"
	case p.Success:
		return base + "bg-emerald-500/15 text-emerald-300 ring-emerald-500/40"
	default:
		return base + "bg-red-500/15 text-red-300 ring-red-500/40"
	}
}

// progressClass colours a progress bar.
func progressClass(failed bool) string {
	if failed {
		return "h-full bg-red-500 transition-all"
	}
	return "h-full bg-accent transition-all"
}

// serverNameClass colours a server's name when one of its tasks failed.
func serverNameClass(failed bool) string {
	if failed {
		return "font-semibold text-red-400"
	}
	return "font-semibold text-zinc-100"
}

// processDuration is how long a finished run took.
func processDuration(p *ProcessState) string {
	return formatDuration(time.Duration(p.Duration))
}

// taskTiming shows a task's duration, or its state while it hasn't finished.
func taskTiming(t *TaskInfo) string {
	if t.Duration > 0 {
		return formatDuration(time.Duration(t.Duration))
	}
	return stateLabel(t.State)
}

// runCancelPath is the form target that stops a run.
func runCancelPath(uuid string) string {
	return "/runs/" + url.PathEscape(uuid) + "/cancel"
}

// rerunLabel says in words what a task's rerun setting means.
func rerunLabel(rerun string) string {
	if rerun == "unsafe" {
		return "Not safe to rerun"
	}
	return rerun
}

// flashClass styles the one-line message shown after an action.
func flashClass(bad bool) string {
	if bad {
		return "mb-4 rounded-lg border border-red-900/60 bg-red-950/30 px-4 py-2 text-sm text-red-200"
	}
	return "mb-4 rounded-lg border border-emerald-900/60 bg-emerald-950/30 px-4 py-2 text-sm text-emerald-200"
}

// barRect is one bar in the 300x120 chart viewBox.
type barRect struct {
	X, Y, W, H int
	Class, Tip string
}

// barRects lays bars out left to right in the chart, scaled to the largest one.
func barRects(bars []Bar) []barRect {
	n := len(bars)
	if n == 0 {
		return nil
	}
	slot := 300 / n
	if slot < 1 {
		slot = 1
	}
	width := slot - 3
	if width < 2 {
		width = 2
	}
	max := maxBar(bars)
	rects := make([]barRect, 0, n)
	for i, b := range bars {
		h := barHeight(b.Value, max, 110)
		class := "fill-accent/80"
		if b.Bad {
			class = "fill-red-500/80"
		}
		rects = append(rects, barRect{X: i * slot, Y: 120 - h, W: width, H: h, Class: class, Tip: b.Label + ": " + b.Tip})
	}
	return rects
}

// runBadgeClass styles a run's status badge in the history list.
func runBadgeClass(state string) string {
	_, class := runBadge(state)
	return "rounded-full px-2.5 py-0.5 text-xs font-semibold ring-1 " + class
}

// runBadgeLabel is the word on a run's status badge in the history list.
func runBadgeLabel(state string) string {
	label, _ := runBadge(state)
	return label
}

// startedText shows when a run started, in a short form.
func startedText(r RunRow) string {
	if r.StartedAt.IsZero() {
		return "—"
	}
	return r.StartedAt.Local().Format("Mon 2 Jan 15:04")
}

// runPath is the detail page of a saved run.
func runPath(id string) string {
	return "/history/" + url.PathEscape(id)
}

// deletePath is the form target that removes a saved run.
func deletePath(id string) string {
	return "/runs/" + url.PathEscape(id) + "/delete"
}

// entityRowClass highlights a row the editor has changed but not saved yet.
func entityRowClass(pending string) string {
	switch pending {
	case "add":
		return "flex items-start gap-3 bg-emerald-950/20 px-4 py-3"
	case "remove":
		return "flex items-start gap-3 bg-red-950/20 px-4 py-3 opacity-60"
	default:
		return "flex items-start gap-3 px-4 py-3"
	}
}

// pendingBadgeClass styles the add or remove badge on an unsaved row.
func pendingBadgeClass(pending string) string {
	if pending == "add" {
		return "rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase text-emerald-300 ring-1 ring-emerald-500/40"
	}
	return "rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase text-red-300 ring-1 ring-red-500/40"
}

// inputClass styles text inputs and selects in the config editor.
func inputClass() string {
	return "w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-1.5 text-sm text-zinc-100 placeholder:text-zinc-600 focus:border-accent focus:outline-none"
}

// addButtonClass styles the submit button of the add forms.
func addButtonClass() string {
	return "w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-sm text-zinc-200 transition hover:bg-zinc-700"
}

// navClass styles a navigation tab, highlighting the current page.
func navClass(on bool) string {
	if on {
		return "rounded-md bg-zinc-800 px-3 py-1.5 font-medium text-zinc-100"
	}
	return "rounded-md px-3 py-1.5 text-zinc-400 transition hover:bg-zinc-800/60 hover:text-zinc-200"
}
