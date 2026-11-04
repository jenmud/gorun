// Hand-written equivalent of dashboard.templ. The templ generator isn't installed in the build
// environment, so this mirrors the markup by hand. `make templ-gen` replaces it with generated
// code, which must render the same HTML.

package ui

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

// Dashboard renders the live runs: each run as a card with its servers and tasks.
func Dashboard(s DashboardState) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if len(s.ProcessOrder) == 0 {
			if err := raw(w, `<div class="rounded-2xl border border-dashed border-zinc-800 p-10 text-center text-sm"><p class="text-zinc-200">Nothing running.</p><p class="mt-1 text-zinc-500">Pick servers and tasks on the left, then press <span class="font-semibold text-accent">Run</span>. Runs started from the terminal show up here too.</p></div>`); err != nil {
				return err
			}
		}
		if hasFinished(s) {
			if err := raw(w, `<div class="mb-3 flex justify-end"><button data-on:click="@post('/clear')" class="rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-xs text-zinc-300 transition hover:bg-zinc-700">Clear finished</button></div>`); err != nil {
				return err
			}
		}
		for _, uuid := range s.ProcessOrder {
			if err := processCard(s.Processes[uuid]).Render(ctx, w); err != nil {
				return err
			}
		}
		return nil
	})
}

// processCard renders one run.
func processCard(p *ProcessState) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<section class="`); err != nil {
			return err
		}
		if err := text(w, runCardClass(processHasFailed(p), p.Finished)); err != nil {
			return err
		}
		if err := raw(w, `"><header class="flex flex-wrap items-center gap-3 p-4"><div class="min-w-0 flex-1"><div class="flex items-center gap-2"><span class="`); err != nil {
			return err
		}
		if err := text(w, runStateClass(p)); err != nil {
			return err
		}
		if err := raw(w, `">`); err != nil {
			return err
		}
		if err := text(w, runStateLabel(p)); err != nil {
			return err
		}
		if err := raw(w, `</span><h3 class="truncate font-semibold text-zinc-100">`); err != nil {
			return err
		}
		if err := text(w, p.Title); err != nil {
			return err
		}
		if err := raw(w, `</h3></div><p class="mt-1 truncate font-mono text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, p.UUID); err != nil {
			return err
		}
		if err := raw(w, `</p></div><div class="flex items-center gap-4 text-xs text-zinc-400"><span>`); err != nil {
			return err
		}
		if err := text(w, itoa(processDone(p))+"/"+itoa(processTotal(p))); err != nil {
			return err
		}
		if err := raw(w, ` tasks</span>`); err != nil {
			return err
		}
		if p.Finished {
			if err := raw(w, `<span>`); err != nil {
				return err
			}
			if err := text(w, processDuration(p)); err != nil {
				return err
			}
			if err := raw(w, `</span>`); err != nil {
				return err
			}
		}
		if err := raw(w, `</div>`); err != nil {
			return err
		}
		if !p.Finished {
			if err := raw(w, `<form method="post" action="`); err != nil {
				return err
			}
			if err := text(w, runCancelPath(p.UUID)); err != nil {
				return err
			}
			if err := raw(w, `"><button class="rounded-lg border border-red-900/60 px-3 py-1.5 text-xs text-red-300 transition hover:bg-red-950/40">Stop</button></form>`); err != nil {
				return err
			}
		}
		if err := raw(w, `</header><div class="h-1 w-full bg-zinc-800"><div class="`); err != nil {
			return err
		}
		if err := text(w, progressClass(processHasFailed(p))); err != nil {
			return err
		}
		if err := raw(w, `" style="width: `); err != nil {
			return err
		}
		if err := text(w, itoa(processProgress(p))+"%"); err != nil {
			return err
		}
		if err := raw(w, `"></div></div><div class="grid gap-3 p-4 md:grid-cols-2">`); err != nil {
			return err
		}
		for _, sname := range p.ServerOrder {
			if err := serverBlock(p.Servers[sname]).Render(ctx, w); err != nil {
				return err
			}
		}
		return raw(w, `</div></section>`)
	})
}

// serverBlock renders one server's tasks inside a run.
func serverBlock(srv *ServerInfo) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<div class="`); err != nil {
			return err
		}
		if err := text(w, serverBoxClass(serverHasFailed(srv))); err != nil {
			return err
		}
		if err := raw(w, `"><div class="mb-2 flex items-baseline justify-between gap-2"><span class="`); err != nil {
			return err
		}
		if err := text(w, serverNameClass(serverHasFailed(srv))); err != nil {
			return err
		}
		if err := raw(w, `">`); err != nil {
			return err
		}
		if err := text(w, srv.Name); err != nil {
			return err
		}
		if err := raw(w, `</span><span class="font-mono text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, srv.Address); err != nil {
			return err
		}
		if err := raw(w, `</span></div><div class="mb-3 h-1 w-full overflow-hidden rounded bg-zinc-800"><div class="`); err != nil {
			return err
		}
		if err := text(w, progressClass(serverHasFailed(srv))); err != nil {
			return err
		}
		if err := raw(w, `" style="width: `); err != nil {
			return err
		}
		if err := text(w, itoa(serverProgress(srv))+"%"); err != nil {
			return err
		}
		if err := raw(w, `"></div></div><ul class="space-y-1">`); err != nil {
			return err
		}
		for _, tname := range srv.Order {
			if err := taskRow(srv.Tasks[tname]).Render(ctx, w); err != nil {
				return err
			}
		}
		return raw(w, `</ul></div>`)
	})
}

// taskRow renders one task: its state, timing, hint and output.
func taskRow(t *TaskInfo) templ.Component {
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
		if err := raw(w, `</span>`); err != nil {
			return err
		}
		if t.Attempts > 1 {
			if err := raw(w, `<span class="text-xs text-amber-400">attempt `); err != nil {
				return err
			}
			if err := text(w, itoa(t.Attempt)+"/"+itoa(t.Attempts)); err != nil {
				return err
			}
			if err := raw(w, `</span>`); err != nil {
				return err
			}
		}
		if err := raw(w, `<span class="ml-auto text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, taskTiming(t)); err != nil {
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
		if t.Description != "" {
			if err := raw(w, `<p class="text-zinc-300">`); err != nil {
				return err
			}
			if err := text(w, t.Description); err != nil {
				return err
			}
			if err := raw(w, `</p>`); err != nil {
				return err
			}
		}
		if t.Details != "" {
			if err := raw(w, `<p class="text-zinc-400">`); err != nil {
				return err
			}
			if err := text(w, t.Details); err != nil {
				return err
			}
			if err := raw(w, `</p>`); err != nil {
				return err
			}
		}
		if t.Rerun != "" {
			if err := raw(w, `<p class="text-amber-400">`); err != nil {
				return err
			}
			if err := text(w, rerunLabel(t.Rerun)); err != nil {
				return err
			}
			if err := raw(w, `</p>`); err != nil {
				return err
			}
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
