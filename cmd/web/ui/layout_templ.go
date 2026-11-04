// Hand-written equivalent of layout.templ. The templ generator isn't installed in the build
// environment, so this mirrors the markup by hand. `make templ-gen` replaces it with generated
// code, which must render the same HTML.

package ui

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

// raw writes s as it is.
func raw(w io.Writer, s string) error {
	_, err := io.WriteString(w, s)
	return err
}

// text writes s escaped for HTML.
func text(w io.Writer, s string) error {
	return raw(w, templ.EscapeString(s))
}

// Layout is the page frame: head, navigation and the page body.
func Layout(title, active string, nats bool, body templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<!doctype html><html lang="en" class="dark"><head><meta charset="UTF-8"/><meta name="viewport" content="width=device-width, initial-scale=1.0"/><title>`); err != nil {
			return err
		}
		if err := text(w, title); err != nil {
			return err
		}
		if err := raw(w, ` · gorun</title><script src="https://cdn.tailwindcss.com"></script><script type="module" src="https://cdn.jsdelivr.net/gh/starfederation/datastar@1.0.0-RC.8/bundles/datastar.js"></script><script>`+layoutTailwindConfig+`</script><script>`+layoutHelpers+`</script><link rel="preconnect" href="https://fonts.googleapis.com"/><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin/><link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet"/></head><body class="min-h-screen bg-zinc-950 text-zinc-100 antialiased selection:bg-accent/30"><header class="border-b border-zinc-800 bg-zinc-900/80 backdrop-blur"><div class="mx-auto flex max-w-7xl flex-wrap items-center gap-4 px-6 py-3"><span class="rounded bg-accent px-2 py-0.5 text-sm font-bold text-white">gorun</span><nav class="flex gap-1 text-sm">`); err != nil {
			return err
		}
		if err := navLink("/", "Run", active == "run").Render(ctx, w); err != nil {
			return err
		}
		if err := navLink("/history", "History", active == "history").Render(ctx, w); err != nil {
			return err
		}
		if err := navLink("/config", "Config", active == "config").Render(ctx, w); err != nil {
			return err
		}
		if err := raw(w, `</nav>`); err != nil {
			return err
		}
		if nats {
			if err := raw(w, `<span class="ml-auto flex items-center gap-2 text-xs text-emerald-400"><span class="h-2 w-2 animate-pulse rounded-full bg-emerald-400"></span>live</span>`); err != nil {
				return err
			}
		} else {
			if err := raw(w, `<span class="ml-auto text-xs text-amber-400">NATS is not connected: runs and live updates are off</span>`); err != nil {
				return err
			}
		}
		if err := raw(w, `</div></header><main class="mx-auto max-w-7xl px-6 py-6">`); err != nil {
			return err
		}
		if err := body.Render(ctx, w); err != nil {
			return err
		}
		return raw(w, `</main></body></html>`)
	})
}

// navLink is one tab in the navigation bar.
func navLink(href, label string, on bool) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<a href="`); err != nil {
			return err
		}
		if err := text(w, href); err != nil {
			return err
		}
		if err := raw(w, `" class="`); err != nil {
			return err
		}
		if err := text(w, navClass(on)); err != nil {
			return err
		}
		if err := raw(w, `">`); err != nil {
			return err
		}
		if err := text(w, label); err != nil {
			return err
		}
		return raw(w, `</a>`)
	})
}

// navClass styles a navigation tab, highlighting the current page.
func navClass(on bool) string {
	if on {
		return "rounded-md bg-zinc-800 px-3 py-1.5 font-medium text-zinc-100"
	}
	return "rounded-md px-3 py-1.5 text-zinc-400 transition hover:bg-zinc-800/60 hover:text-zinc-200"
}

const layoutTailwindConfig = `tailwind.config = { darkMode: 'class', theme: { extend: { colors: { accent: '#7C3AED' }, fontFamily: { sans: ['Inter', 'ui-sans-serif', 'system-ui', 'sans-serif'], mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace'] } } } }`

// layoutHelpers filters a list by its search box and ticks or clears its checkboxes.
const layoutHelpers = `
function gorunFilter(input, listId) {
	var q = input.value.toLowerCase();
	document.querySelectorAll('#' + listId + ' [data-filter]').forEach(function (row) {
		row.style.display = row.dataset.filter.indexOf(q) === -1 ? 'none' : '';
	});
}
function gorunSelect(formId, on) {
	document.querySelectorAll('#' + formId + ' [data-filter]').forEach(function (row) {
		if (row.style.display === 'none') { return; }
		var box = row.querySelector('input[type=checkbox]');
		if (box) { box.checked = on; }
	});
}`
