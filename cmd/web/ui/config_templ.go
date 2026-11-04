// Hand-written equivalent of config.templ. The templ generator isn't installed in the build
// environment, so this mirrors the markup by hand. `make templ-gen` replaces it with generated
// code, which must render the same HTML.

package ui

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

// ConfigPage is the config editor: the saved entities, the unsaved changes and the forms.
func ConfigPage(d ConfigPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if d.Flash != "" {
			if err := flash(d.Flash, d.FlashBad).Render(ctx, w); err != nil {
				return err
			}
		}
		if err := raw(w, `<div class="mb-6 max-w-3xl"><h1 class="text-xl font-semibold text-zinc-100">Config</h1><p class="mt-1 text-sm text-zinc-400">Add, remove and load the servers, groups, tasks and bundles in your Gorunfile. Changes stay here until you press <span class="font-semibold text-zinc-200">Save</span>, which writes each one back to the file it came from. Comments and layout elsewhere in each file are kept. Runs use the saved file.</p></div><div class="grid gap-6 lg:grid-cols-[1fr_24rem]"><div class="min-w-0 space-y-6"><section class="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><h2 class="text-xs font-semibold uppercase tracking-wider text-zinc-400">Files</h2>`); err != nil {
			return err
		}
		if d.Loaded {
			if err := raw(w, `<ul class="mt-2 space-y-1 font-mono text-xs text-zinc-300">`); err != nil {
				return err
			}
			for _, f := range d.Files {
				if err := raw(w, `<li>`); err != nil {
					return err
				}
				if err := text(w, f.Label); err != nil {
					return err
				}
				if err := raw(w, `</li>`); err != nil {
					return err
				}
			}
			if err := raw(w, `</ul>`); err != nil {
				return err
			}
		} else {
			if err := raw(w, `<p class="mt-2 text-sm text-zinc-500">No Gorunfile loaded yet. Load one on the right.</p>`); err != nil {
				return err
			}
		}
		if err := raw(w, `</section>`); err != nil {
			return err
		}
		for _, table := range []struct {
			title, kind string
			rows        []EntityRow
		}{
			{"Servers", "host", d.Servers},
			{"Server groups", "group", d.Groups},
			{"Tasks", "task", d.Tasks},
			{"Bundles", "bundle", d.Bundles},
		} {
			if err := entityTable(table.title, table.kind, "name", table.rows).Render(ctx, w); err != nil {
				return err
			}
		}
		if err := raw(w, `</div><aside class="space-y-6 self-start">`); err != nil {
			return err
		}
		if err := pendingPanel(d).Render(ctx, w); err != nil {
			return err
		}
		if err := raw(w, `<section class="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><h2 class="text-sm font-semibold text-zinc-100">Load a Gorunfile</h2><p class="mt-1 text-xs text-zinc-500">A path on the machine running gorun. Unsaved changes are dropped when you load.</p><form method="post" action="/config/load" class="mt-3 space-y-2"><input name="path" required value="`); err != nil {
			return err
		}
		if err := text(w, d.Path); err != nil {
			return err
		}
		if err := raw(w, `" placeholder="/path/to/Gorunfile" class="`); err != nil {
			return err
		}
		if err := text(w, inputClass()); err != nil {
			return err
		}
		if err := raw(w, `"/><button class="w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-sm text-zinc-200 transition hover:bg-zinc-700">Load</button></form></section>`); err != nil {
			return err
		}
		if d.Loaded {
			for _, form := range []templ.Component{
				addServerForm(d.Files),
				addTaskForm(d.Files),
				addGroupForm(d.Files),
				addBundleForm(d.Files),
			} {
				if err := form.Render(ctx, w); err != nil {
					return err
				}
			}
		}
		return raw(w, `</aside></div>`)
	})
}

// entityTable lists one kind of entity, each with a remove button.
func entityTable(title, kind, key string, rows []EntityRow) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<section class="rounded-2xl border border-zinc-800 bg-zinc-900/40"><header class="flex items-center justify-between border-b border-zinc-800 px-4 py-3"><h2 class="text-sm font-semibold text-zinc-100">`); err != nil {
			return err
		}
		if err := text(w, title); err != nil {
			return err
		}
		if err := raw(w, `</h2><span class="text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, itoa(len(rows))); err != nil {
			return err
		}
		if err := raw(w, `</span></header>`); err != nil {
			return err
		}
		if len(rows) == 0 {
			return raw(w, `<p class="px-4 py-6 text-center text-sm text-zinc-500">None yet.</p></section>`)
		}
		if err := raw(w, `<ul class="divide-y divide-zinc-800">`); err != nil {
			return err
		}
		for _, r := range rows {
			if err := entityItem(kind, r).Render(ctx, w); err != nil {
				return err
			}
		}
		return raw(w, `</ul></section>`)
	})
}

// entityItem is one row of an entity table.
func entityItem(kind string, r EntityRow) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<li class="`); err != nil {
			return err
		}
		if err := text(w, entityRowClass(r.Pending)); err != nil {
			return err
		}
		if err := raw(w, `"><div class="min-w-0 flex-1"><div class="flex items-center gap-2"><span class="truncate font-mono text-sm text-zinc-100">`); err != nil {
			return err
		}
		if err := text(w, r.Name); err != nil {
			return err
		}
		if err := raw(w, `</span>`); err != nil {
			return err
		}
		if r.Pending != "" {
			if err := raw(w, `<span class="`); err != nil {
				return err
			}
			if err := text(w, pendingBadgeClass(r.Pending)); err != nil {
				return err
			}
			if err := raw(w, `">`); err != nil {
				return err
			}
			if err := text(w, r.Pending); err != nil {
				return err
			}
			if err := raw(w, `</span>`); err != nil {
				return err
			}
		}
		if err := raw(w, `</div><p class="truncate text-xs text-zinc-500">`); err != nil {
			return err
		}
		if err := text(w, r.Detail); err != nil {
			return err
		}
		if err := raw(w, `</p>`); err != nil {
			return err
		}
		if r.Source != "" {
			if err := raw(w, `<p class="truncate font-mono text-[11px] text-zinc-600">`); err != nil {
				return err
			}
			if err := text(w, r.Source); err != nil {
				return err
			}
			if err := raw(w, `</p>`); err != nil {
				return err
			}
		}
		if err := raw(w, `</div><form method="post" action="/config/remove"><input type="hidden" name="kind" value="`); err != nil {
			return err
		}
		if err := text(w, kind); err != nil {
			return err
		}
		if err := raw(w, `"/><input type="hidden" name="name" value="`); err != nil {
			return err
		}
		if err := text(w, r.Name); err != nil {
			return err
		}
		return raw(w, `"/><button class="rounded-lg px-2.5 py-1 text-xs text-red-300 transition hover:bg-red-950/40">Remove</button></form></li>`)
	})
}

// pendingPanel lists the unsaved changes, with Save and Discard.
func pendingPanel(d ConfigPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<section class="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><h2 class="text-sm font-semibold text-zinc-100">Unsaved changes</h2>`); err != nil {
			return err
		}
		if len(d.Pending) == 0 {
			return raw(w, `<p class="mt-1 text-xs text-zinc-500">None. Adding or removing something above lists it here until you save.</p></section>`)
		}
		if err := raw(w, `<ul class="mt-2 space-y-1 text-xs text-zinc-300">`); err != nil {
			return err
		}
		for _, p := range d.Pending {
			if err := raw(w, `<li>• `); err != nil {
				return err
			}
			if err := text(w, p); err != nil {
				return err
			}
			if err := raw(w, `</li>`); err != nil {
				return err
			}
		}
		return raw(w, `</ul><div class="mt-4 flex gap-2"><form method="post" action="/config/save" class="flex-1"><button class="w-full rounded-lg bg-accent px-3 py-1.5 text-sm font-semibold text-white transition hover:bg-violet-600">Save</button></form><form method="post" action="/config/discard"><button class="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 transition hover:bg-zinc-800">Discard</button></form></div></section>`)
	})
}

// addServerForm adds a server to the config.
func addServerForm(files []FileOption) templ.Component {
	return addForm("Add a server", "", "host", files, `<p class="mt-1 text-xs text-zinc-500">Leave the SSH user empty for a server that runs on this machine.</p>`,
		field("name", "name, e.g. web-01", true),
		field("hostname", "hostname (defaults to the name)", false),
		field("groups", "groups, comma separated", false),
		field("tags", "tags, comma separated", false),
		field("ssh_user", "SSH user", false),
		field("identityfile", "identity file, e.g. ~/.ssh/id_ed25519", false),
	)
}

// addTaskForm adds a task to the config.
func addTaskForm(files []FileOption) templ.Component {
	return addForm("Add a task", "", "task", files, "",
		field("name", "name, e.g. restart-nginx", true),
		field("cmd", "command to run", true),
		field("description", "description shown in the picker", false),
		checkbox("sudo", "run with sudo"),
		checkbox("local", "run only on local servers"),
	)
}

// addGroupForm adds a server group to the config.
func addGroupForm(files []FileOption) templ.Component {
	return addForm("Add a server group", `<p class="mt-1 text-xs text-zinc-500">Settings servers share by joining the group.</p>`, "group", files, "",
		field("name", "name, e.g. web", true),
		field("tags", "tags, comma separated", false),
		field("tasks", "tasks or @bundles, comma separated", false),
		field("ssh_user", "SSH user", false),
		field("identityfile", "identity file", false),
	)
}

// addBundleForm adds a bundle to the config.
func addBundleForm(files []FileOption) templ.Component {
	return addForm("Add a bundle", `<p class="mt-1 text-xs text-zinc-500">A named list of tasks. Use it as <span class="font-mono">@name</span> anywhere a task list goes.</p>`, "bundle", files, "",
		field("name", "name, e.g. baseline", true),
		field("tasks", "tasks, comma separated", true),
		field("description", "description", false),
	)
}

// addForm is the shared shell of the add forms: a heading, a note and the fields.
func addForm(title, note, kind string, files []FileOption, after string, fields ...templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<section class="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><h2 class="text-sm font-semibold text-zinc-100">`); err != nil {
			return err
		}
		if err := text(w, title); err != nil {
			return err
		}
		if err := raw(w, `</h2>`+note+after+`<form method="post" action="/config/add" class="mt-3 space-y-2"><input type="hidden" name="kind" value="`); err != nil {
			return err
		}
		if err := text(w, kind); err != nil {
			return err
		}
		if err := raw(w, `"/>`); err != nil {
			return err
		}
		for _, f := range fields {
			if err := f.Render(ctx, w); err != nil {
				return err
			}
		}
		if err := fileSelect(files).Render(ctx, w); err != nil {
			return err
		}
		if err := raw(w, `<button class="`); err != nil {
			return err
		}
		if err := text(w, addButtonClass()); err != nil {
			return err
		}
		return raw(w, `">`+title+`</button></form></section>`)
	})
}

// field is a text input in an add form.
func field(name, placeholder string, required bool) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		req := ""
		if required {
			req = " required"
		}
		if err := raw(w, `<input name="`+name+`"`+req+` placeholder="`); err != nil {
			return err
		}
		if err := text(w, placeholder); err != nil {
			return err
		}
		if err := raw(w, `" class="`); err != nil {
			return err
		}
		if err := text(w, inputClass()); err != nil {
			return err
		}
		return raw(w, `"/>`)
	})
}

// checkbox is a tick box in an add form.
func checkbox(name, label string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<label class="flex items-center gap-2 text-xs text-zinc-300"><input type="checkbox" name="`+name+`" class="accent-violet-600"/> `); err != nil {
			return err
		}
		if err := text(w, label); err != nil {
			return err
		}
		return raw(w, `</label>`)
	})
}

// fileSelect picks which file a new entity is saved into.
func fileSelect(files []FileOption) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := raw(w, `<label class="block text-xs text-zinc-400">Save into <select name="file" class="`); err != nil {
			return err
		}
		if err := text(w, inputClass()); err != nil {
			return err
		}
		if err := raw(w, `">`); err != nil {
			return err
		}
		for _, f := range files {
			if err := raw(w, `<option value="`); err != nil {
				return err
			}
			if err := text(w, f.Path); err != nil {
				return err
			}
			if err := raw(w, `">`); err != nil {
				return err
			}
			if err := text(w, f.Label); err != nil {
				return err
			}
			if err := raw(w, `</option>`); err != nil {
				return err
			}
		}
		return raw(w, `</select></label>`)
	})
}
