// Hand-written equivalent of run.templ. The templ generator isn't installed in the build
// environment, so this mirrors the markup by hand. `make templ-gen` replaces it with generated
// code, which must render the same HTML.

package ui

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

// RunPage is the run page: the picker, the stats and the live dashboard.
func RunPage(d RunPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if d.Flash != "" {
			if err := flash(d.Flash, d.FlashBad).Render(ctx, w); err != nil {
				return err
			}
		}
		if err := raw(w, `<div class="grid gap-6 lg:grid-cols-[22rem_1fr]">`); err != nil {
			return err
		}
		if err := runPicker(d).Render(ctx, w); err != nil {
			return err
		}
		if err := raw(w, `<section class="min-w-0 space-y-6"><div id="stats">`); err != nil {
			return err
		}
		if err := statsPanel(d.Stats).Render(ctx, w); err != nil {
			return err
		}
		if err := raw(w, `</div><div><h2 class="mb-3 text-sm font-semibold uppercase tracking-wider text-zinc-400">Live</h2><div id="dashboard" data-init="@get('/events')">`); err != nil {
			return err
		}
		if err := Dashboard(d.Dashboard).Render(ctx, w); err != nil {
			return err
		}
		return raw(w, `</div></div></section></div>`)
	})
}

// runPicker is the form that picks servers and tasks to run.
func runPicker(d RunPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<form method="post" action="/run" class="space-y-5 self-start rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><div><h2 class="text-base font-semibold text-zinc-100">Pick and run</h2><p class="mt-1 text-xs text-zinc-500">Tick the servers to run on. Tick tasks to run only those; leave them all unticked to run each server's own list. Uses the saved <span class="font-mono">`); err != nil {
			return err
		}
		if err := text(w, d.Path); err != nil {
			return err
		}
		disabled := ""
		if !d.NATSOK {
			disabled = " disabled"
		}
		if err := raw(w, `</span>.</p></div><button type="submit"`+disabled+` class="w-full rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-white shadow-lg shadow-accent/20 transition hover:bg-violet-600 disabled:cursor-not-allowed disabled:opacity-40">Run selected</button><section><div class="mb-2 flex items-center justify-between gap-2"><h3 class="text-xs font-semibold uppercase tracking-wider text-zinc-400">Servers</h3><div class="flex gap-1 text-xs"><button type="button" onclick="gorunSelect('server-list', true)" class="rounded px-2 py-0.5 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">all</button><button type="button" onclick="gorunSelect('server-list', false)" class="rounded px-2 py-0.5 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">none</button></div></div><input type="search" placeholder="filter by name, tag or group" oninput="gorunFilter(this, 'server-list')" class="mb-2 w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-1.5 text-sm placeholder:text-zinc-600 focus:border-accent focus:outline-none"/><ul id="server-list" class="max-h-96 space-y-1 overflow-y-auto pr-1">`); err != nil {
			return err
		}
		for _, s := range d.Servers {
			if err := serverOption(s).Render(ctx, w); err != nil {
				return err
			}
		}
		if err := raw(w, `</ul></section><section><div class="mb-2 flex items-center justify-between gap-2"><h3 class="text-xs font-semibold uppercase tracking-wider text-zinc-400">Tasks</h3><div class="flex gap-1 text-xs"><button type="button" onclick="gorunSelect('task-list', true)" class="rounded px-2 py-0.5 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">all</button><button type="button" onclick="gorunSelect('task-list', false)" class="rounded px-2 py-0.5 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">none</button></div></div><input type="search" placeholder="filter tasks" oninput="gorunFilter(this, 'task-list')" class="mb-2 w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-1.5 text-sm placeholder:text-zinc-600 focus:border-accent focus:outline-none"/><ul id="task-list" class="max-h-96 space-y-1 overflow-y-auto pr-1">`); err != nil {
			return err
		}
		for _, t := range d.Tasks {
			if err := taskOption(t).Render(ctx, w); err != nil {
				return err
			}
		}
		return raw(w, `</ul></section></form>`)
	})
}

// serverOption is one tickable server in the picker.
func serverOption(s ServerOption) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<li data-filter="`); err != nil {
			return err
		}
		if err := text(w, s.Name+" "+s.Tags+" "+s.Groups+" "+s.Address); err != nil {
			return err
		}
		if err := raw(w, `" class="rounded-lg px-2 py-1.5 hover:bg-zinc-800/50"><label class="flex cursor-pointer items-start gap-2"><input type="checkbox" name="server" value="`); err != nil {
			return err
		}
		if err := text(w, s.Name); err != nil {
			return err
		}
		checked := ""
		if s.Selected {
			checked = " checked"
		}
		if err := raw(w, `"`+checked+` class="mt-1 accent-violet-600"/><span class="min-w-0 flex-1"><span class="block truncate text-sm text-zinc-100">`); err != nil {
			return err
		}
		if err := text(w, s.Name); err != nil {
			return err
		}
		if err := raw(w, `</span><span class="block truncate font-mono text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, s.Address); err != nil {
			return err
		}
		if err := raw(w, `</span>`); err != nil {
			return err
		}
		if s.Tags != "" {
			if err := raw(w, `<span class="block truncate text-xs text-zinc-500">tags: `); err != nil {
				return err
			}
			if err := text(w, s.Tags); err != nil {
				return err
			}
			if err := raw(w, `</span>`); err != nil {
				return err
			}
		}
		return raw(w, `</span></label></li>`)
	})
}

// taskOption is one tickable task in the picker.
func taskOption(t TaskOption) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<li data-filter="`); err != nil {
			return err
		}
		if err := text(w, t.Name+" "+t.Description); err != nil {
			return err
		}
		if err := raw(w, `" class="rounded-lg px-2 py-1.5 hover:bg-zinc-800/50"><label class="flex cursor-pointer items-start gap-2" title="`); err != nil {
			return err
		}
		if err := text(w, t.Details); err != nil {
			return err
		}
		if err := raw(w, `"><input type="checkbox" name="task" value="`); err != nil {
			return err
		}
		if err := text(w, t.Name); err != nil {
			return err
		}
		if err := raw(w, `" class="mt-1 accent-violet-600"/><span class="min-w-0 flex-1"><span class="block truncate font-mono text-sm text-zinc-100">`); err != nil {
			return err
		}
		if err := text(w, t.Name); err != nil {
			return err
		}
		if err := raw(w, `</span>`); err != nil {
			return err
		}
		if t.Description != "" {
			if err := raw(w, `<span class="block truncate text-xs text-zinc-500">`); err != nil {
				return err
			}
			if err := text(w, t.Description); err != nil {
				return err
			}
			if err := raw(w, `</span>`); err != nil {
				return err
			}
		}
		if t.Rerun != "" {
			if err := raw(w, `<span class="block truncate text-xs text-amber-400">`); err != nil {
				return err
			}
			if err := text(w, rerunLabel(t.Rerun)); err != nil {
				return err
			}
			if err := raw(w, `</span>`); err != nil {
				return err
			}
		}
		return raw(w, `</span></label></li>`)
	})
}

// flash is the one-line message after an action.
func flash(msg string, bad bool) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<div class="`); err != nil {
			return err
		}
		if err := text(w, flashClass(bad)); err != nil {
			return err
		}
		if err := raw(w, `">`); err != nil {
			return err
		}
		if err := text(w, msg); err != nil {
			return err
		}
		return raw(w, `</div>`)
	})
}

// statsPanel is the stats tiles and the two charts.
func statsPanel(s StatsView) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<div class="grid grid-cols-2 gap-3 md:grid-cols-4">`); err != nil {
			return err
		}
		tiles := []struct{ label, value, sub string }{
			{"Runs", itoa(s.Runs), "in history"},
			{"Success", percent(s.SuccessRate), itoa(s.Succeeded) + " of " + itoa(s.Finished) + " finished"},
			{"Average", formatDuration(s.AvgDuration), "per finished run"},
			{"Tasks failed", itoa(s.TasksFailed), "of " + itoa(s.TasksRun) + " run"},
		}
		for _, tile := range tiles {
			if err := statTile(tile.label, tile.value, tile.sub).Render(ctx, w); err != nil {
				return err
			}
		}
		if err := raw(w, `</div><div class="mt-4 grid gap-3 md:grid-cols-2"><div class="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><h3 class="text-sm font-semibold text-zinc-100">Recent runs</h3><p class="mb-3 text-xs text-zinc-500">Bar height is how long each run took, oldest on the left. Red bars failed. Hover for details.</p>`); err != nil {
			return err
		}
		if len(s.Recent) == 0 {
			if err := raw(w, `<p class="py-8 text-center text-sm text-zinc-500">No runs yet. Your first run will appear here.</p>`); err != nil {
				return err
			}
		} else {
			if err := raw(w, `<svg viewBox="0 0 300 120" class="h-40 w-full" preserveAspectRatio="none" role="img" aria-label="Run durations">`); err != nil {
				return err
			}
			for _, r := range barRects(s.Recent) {
				if err := raw(w, `<rect x="`+itoa(r.X)+`" y="`+itoa(r.Y)+`" width="`+itoa(r.W)+`" height="`+itoa(r.H)+`" rx="2" class="`); err != nil {
					return err
				}
				if err := text(w, r.Class); err != nil {
					return err
				}
				if err := raw(w, `"><title>`); err != nil {
					return err
				}
				if err := text(w, r.Tip); err != nil {
					return err
				}
				if err := raw(w, `</title></rect>`); err != nil {
					return err
				}
			}
			if err := raw(w, `</svg>`); err != nil {
				return err
			}
		}
		if err := raw(w, `</div><div class="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><h3 class="text-sm font-semibold text-zinc-100">Slowest tasks</h3><p class="mb-3 text-xs text-zinc-500">Average time over the last few runs. Start here when a run feels slow.</p>`); err != nil {
			return err
		}
		if len(s.Slowest) == 0 {
			if err := raw(w, `<p class="py-8 text-center text-sm text-zinc-500">Nothing to rank yet.</p>`); err != nil {
				return err
			}
		} else {
			if err := raw(w, `<ul class="space-y-2">`); err != nil {
				return err
			}
			for _, t := range s.Slowest {
				if err := raw(w, `<li class="flex items-center gap-3 text-sm"><span class="min-w-0 flex-1 truncate font-mono text-zinc-200">`); err != nil {
					return err
				}
				if err := text(w, t.Name); err != nil {
					return err
				}
				if err := raw(w, `</span><span class="truncate text-xs text-zinc-500">`); err != nil {
					return err
				}
				if err := text(w, t.Server); err != nil {
					return err
				}
				if err := raw(w, `</span><span class="w-16 text-right font-mono text-xs text-zinc-300">`); err != nil {
					return err
				}
				if err := text(w, formatDuration(t.Average)); err != nil {
					return err
				}
				if err := raw(w, `</span></li>`); err != nil {
					return err
				}
			}
			if err := raw(w, `</ul>`); err != nil {
				return err
			}
		}
		return raw(w, `</div></div>`)
	})
}

// statTile is one number with its label.
func statTile(label, value, sub string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<div class="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><p class="text-xs uppercase tracking-wider text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, label); err != nil {
			return err
		}
		if err := raw(w, `</p><p class="mt-1 text-2xl font-semibold text-zinc-100">`); err != nil {
			return err
		}
		if err := text(w, value); err != nil {
			return err
		}
		if err := raw(w, `</p><p class="mt-1 truncate text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, sub); err != nil {
			return err
		}
		return raw(w, `</p></div>`)
	})
}
