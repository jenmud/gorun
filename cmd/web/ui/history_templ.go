// Hand-written equivalent of history.templ. The templ generator isn't installed in the build
// environment, so this mirrors the markup by hand. `make templ-gen` replaces it with generated
// code, which must render the same HTML.

package ui

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

// HistoryPage is the list of saved runs, with a short guide to reading it.
func HistoryPage(d HistoryPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if d.Flash != "" {
			if err := flash(d.Flash, false).Render(ctx, w); err != nil {
				return err
			}
		}
		if err := raw(w, `<div class="mb-6 flex flex-wrap items-end justify-between gap-4"><div class="max-w-2xl"><h1 class="text-xl font-semibold text-zinc-100">History</h1><p class="mt-1 text-sm text-zinc-400">Every run is saved here with the commands it ran and what they printed. Open a run to see each server's tasks, how long they took, and their output. Failed tasks are marked in red with their exit code.</p></div><form method="post" action="/runs/truncate" class="flex items-center gap-3 text-xs text-zinc-400"><span>Keep only the newest 10 runs</span><button class="rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-zinc-200 transition hover:bg-zinc-700">Trim history</button></form></div>`); err != nil {
			return err
		}
		if len(d.Runs) == 0 {
			return raw(w, `<div class="rounded-2xl border border-dashed border-zinc-800 p-10 text-center text-sm"><p class="text-zinc-200">No runs saved yet.</p><p class="mt-1 text-zinc-500">Start one from <a href="/" class="text-accent hover:underline">Run</a>. It will appear here as soon as it starts.</p></div>`)
		}
		if err := raw(w, `<div class="overflow-hidden rounded-2xl border border-zinc-800"><table class="w-full text-left text-sm"><thead class="bg-zinc-900/60 text-xs uppercase tracking-wider text-zinc-500"><tr><th class="px-4 py-2 font-medium">Status</th><th class="px-4 py-2 font-medium">Run</th><th class="px-4 py-2 font-medium">Started</th><th class="px-4 py-2 text-right font-medium">Took</th><th class="px-4 py-2 text-right font-medium">Servers</th><th class="px-4 py-2 text-right font-medium">Tasks</th><th class="px-4 py-2 text-right font-medium">Failed</th></tr></thead><tbody class="divide-y divide-zinc-800">`); err != nil {
			return err
		}
		for _, r := range d.Runs {
			if err := historyRow(r).Render(ctx, w); err != nil {
				return err
			}
		}
		return raw(w, `</tbody></table></div>`)
	})
}

// historyRow is one saved run in the list.
func historyRow(r RunRow) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<tr class="transition hover:bg-zinc-900/60"><td class="px-4 py-3"><span class="`); err != nil {
			return err
		}
		if err := text(w, runBadgeClass(r.State)); err != nil {
			return err
		}
		if err := raw(w, `">`); err != nil {
			return err
		}
		if err := text(w, runBadgeLabel(r.State)); err != nil {
			return err
		}
		if err := raw(w, `</span></td><td class="px-4 py-3"><a href="`); err != nil {
			return err
		}
		if err := text(w, runPath(r.ID)); err != nil {
			return err
		}
		if err := raw(w, `" class="font-medium text-zinc-100 hover:text-accent">`); err != nil {
			return err
		}
		if err := text(w, r.Title); err != nil {
			return err
		}
		if err := raw(w, `</a><div class="font-mono text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, r.Hostname); err != nil {
			return err
		}
		if err := raw(w, `</div></td><td class="px-4 py-3 text-zinc-400">`); err != nil {
			return err
		}
		if err := text(w, startedText(r)); err != nil {
			return err
		}
		if err := raw(w, `</td><td class="px-4 py-3 text-right font-mono text-xs text-zinc-300">`); err != nil {
			return err
		}
		if err := text(w, formatDuration(r.Duration)); err != nil {
			return err
		}
		if err := raw(w, `</td><td class="px-4 py-3 text-right text-zinc-300">`); err != nil {
			return err
		}
		if err := text(w, itoa(r.Servers)); err != nil {
			return err
		}
		if err := raw(w, `</td><td class="px-4 py-3 text-right text-zinc-300">`); err != nil {
			return err
		}
		if err := text(w, itoa(r.TotalTasks)); err != nil {
			return err
		}
		if err := raw(w, `</td><td class="px-4 py-3 text-right">`); err != nil {
			return err
		}
		if r.Failed > 0 {
			if err := raw(w, `<span class="text-red-400">`); err != nil {
				return err
			}
			if err := text(w, itoa(r.Failed)); err != nil {
				return err
			}
			if err := raw(w, `</span>`); err != nil {
				return err
			}
		} else {
			if err := raw(w, `<span class="text-zinc-500">0</span>`); err != nil {
				return err
			}
		}
		return raw(w, `</td></tr>`)
	})
}

// HistoryDetailPage shows one saved run: its summary, then each server's tasks and output.
func HistoryDetailPage(d RunDetailData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<div class="mb-4 text-xs"><a href="/history" class="text-zinc-400 hover:text-zinc-200">← back to history</a></div><header class="mb-6 flex flex-wrap items-start justify-between gap-4"><div class="min-w-0"><div class="flex items-center gap-3"><span class="`); err != nil {
			return err
		}
		if err := text(w, runBadgeClass(d.Summary.State)); err != nil {
			return err
		}
		if err := raw(w, `">`); err != nil {
			return err
		}
		if err := text(w, runBadgeLabel(d.Summary.State)); err != nil {
			return err
		}
		if err := raw(w, `</span><h1 class="truncate text-xl font-semibold text-zinc-100">`); err != nil {
			return err
		}
		if err := text(w, d.Summary.Title); err != nil {
			return err
		}
		if err := raw(w, `</h1></div><p class="mt-1 font-mono text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, d.Summary.ID); err != nil {
			return err
		}
		if err := raw(w, `</p><p class="mt-1 text-sm text-zinc-400">Started `); err != nil {
			return err
		}
		if err := text(w, startedText(d.Summary)); err != nil {
			return err
		}
		if err := raw(w, ` on <span class="font-mono">`); err != nil {
			return err
		}
		if err := text(w, d.Summary.Hostname); err != nil {
			return err
		}
		if err := raw(w, `</span></p></div><form method="post" action="`); err != nil {
			return err
		}
		if err := text(w, deletePath(d.Summary.ID)); err != nil {
			return err
		}
		if err := raw(w, `"><button class="rounded-lg border border-red-900/60 px-3 py-1.5 text-xs text-red-300 transition hover:bg-red-950/40">Delete this run</button></form></header><div class="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4">`); err != nil {
			return err
		}
		tiles := []struct{ label, value, sub string }{
			{"Took", formatDuration(d.Summary.Duration), "wall clock"},
			{"Tasks", itoa(d.Summary.TotalTasks), itoa(d.Summary.Servers) + " servers"},
			{"Failed", itoa(d.Summary.Failed), "tasks that exited non-zero or errored"},
			{"Skipped", itoa(d.Summary.Skipped), "because a dependency failed"},
		}
		for _, tile := range tiles {
			if err := statTile(tile.label, tile.value, tile.sub).Render(ctx, w); err != nil {
				return err
			}
		}
		if err := raw(w, `</div><p class="mb-4 text-sm text-zinc-400">Each server ran its tasks in order. Open a task to see the command it ran, what it printed, and its exit code. Errors from a server that couldn't be reached are listed at the top of its card.</p><div class="space-y-4">`); err != nil {
			return err
		}
		for _, srv := range d.Servers {
			if err := historyServer(srv).Render(ctx, w); err != nil {
				return err
			}
		}
		return raw(w, `</div>`)
	})
}

// historyServer is one server's card in a saved run.
func historyServer(srv ServerBlock) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<section class="`); err != nil {
			return err
		}
		if err := text(w, runCardClass(srv.Failed, true)); err != nil {
			return err
		}
		if err := raw(w, `"><header class="flex flex-wrap items-baseline justify-between gap-2 p-4"><div><span class="`); err != nil {
			return err
		}
		if err := text(w, serverNameClass(srv.Failed)); err != nil {
			return err
		}
		if err := raw(w, `">`); err != nil {
			return err
		}
		if err := text(w, srv.Name); err != nil {
			return err
		}
		if err := raw(w, `</span><span class="ml-2 font-mono text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, srv.Address); err != nil {
			return err
		}
		if err := raw(w, `</span></div><span class="text-xs text-zinc-400">`); err != nil {
			return err
		}
		if err := text(w, itoa(len(srv.Tasks))+" tasks · "+formatDuration(srv.Duration)); err != nil {
			return err
		}
		if err := raw(w, `</span></header>`); err != nil {
			return err
		}
		if srv.Error != "" {
			if err := raw(w, `<p class="border-t border-zinc-800 px-4 py-3 text-xs text-red-300">Could not run on this server: `); err != nil {
				return err
			}
			if err := text(w, srv.Error); err != nil {
				return err
			}
			if err := raw(w, `</p>`); err != nil {
				return err
			}
		}
		if err := raw(w, `<ul class="space-y-1 border-t border-zinc-800 p-3">`); err != nil {
			return err
		}
		for _, t := range srv.Tasks {
			if err := historyTask(t).Render(ctx, w); err != nil {
				return err
			}
		}
		return raw(w, `</ul></section>`)
	})
}

// historyTask is one task in a saved run, with its output.
func historyTask(t historyTaskView) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		open := ""
		if t.State == 3 {
			open = " open"
		}
		if err := raw(w, `<li class="rounded-lg px-2 py-1 transition hover:bg-zinc-800/40"><details`+open+`><summary class="flex cursor-pointer list-none items-center gap-2 text-sm"><span class="`); err != nil {
			return err
		}
		if err := text(w, stateClass(t.State)); err != nil {
			return err
		}
		if err := raw(w, `">`); err != nil {
			return err
		}
		if err := text(w, stateIcon(t.State)); err != nil {
			return err
		}
		if err := raw(w, `</span><span class="font-mono text-zinc-200">`); err != nil {
			return err
		}
		if err := text(w, t.Name); err != nil {
			return err
		}
		if err := raw(w, `</span><span class="ml-auto text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, formatDuration(t.Duration)); err != nil {
			return err
		}
		if err := raw(w, `</span></summary><div class="mt-2 space-y-2 border-l border-zinc-800 pl-3 text-xs"><p class="text-zinc-400">`); err != nil {
			return err
		}
		if err := text(w, hintFor(t.State)); err != nil {
			return err
		}
		if err := raw(w, `</p>`); err != nil {
			return err
		}
		if t.Command != "" {
			if err := raw(w, `<pre class="overflow-x-auto rounded-lg bg-zinc-950 p-2 font-mono text-zinc-300">$ `); err != nil {
				return err
			}
			if err := text(w, t.Command); err != nil {
				return err
			}
			if err := raw(w, `</pre>`); err != nil {
				return err
			}
		}
		if t.Stdout != "" {
			if err := raw(w, `<pre class="max-h-64 overflow-auto rounded-lg bg-zinc-950 p-2 font-mono text-zinc-300">`); err != nil {
				return err
			}
			if err := text(w, t.Stdout); err != nil {
				return err
			}
			if err := raw(w, `</pre>`); err != nil {
				return err
			}
		}
		if t.Stderr != "" {
			if err := raw(w, `<pre class="max-h-64 overflow-auto rounded-lg bg-zinc-950 p-2 font-mono text-red-300">`); err != nil {
				return err
			}
			if err := text(w, t.Stderr); err != nil {
				return err
			}
			if err := raw(w, `</pre>`); err != nil {
				return err
			}
		}
		if t.State == 3 {
			if err := raw(w, `<p class="text-red-300">exit code `); err != nil {
				return err
			}
			if err := text(w, itoa(t.ExitCode)); err != nil {
				return err
			}
			if err := raw(w, `</p>`); err != nil {
				return err
			}
		}
		return raw(w, `</div></details></li>`)
	})
}
