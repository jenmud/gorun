// Hand-written equivalent of config.templ. The templ generator isn't installed in the build
// environment, so this mirrors the markup by hand. `make templ-gen` replaces it with generated
// code, which must render the same HTML.

package ui

import (
	"context"
	"io"

	"github.com/a-h/templ"
)

// markup is HTML written as it is; escaped is text, escaped on the way out.
type markup string

type escaped string

// emit writes each part in order: markup as it is, escaped text escaped, components rendered.
func emit(ctx context.Context, w io.Writer, parts ...any) error {
	for _, p := range parts {
		var err error
		switch v := p.(type) {
		case markup:
			_, err = io.WriteString(w, string(v))
		case escaped:
			err = text(w, string(v))
		case templ.Component:
			err = v.Render(ctx, w)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// boolAttr is the attribute name when on is true, and nothing otherwise.
func boolAttr(on bool, name string) markup {
	if on {
		return markup(" " + name)
	}
	return ""
}

// ConfigPage is the config editor: the saved entities, the unsaved changes and the dialogs.
func ConfigPage(d ConfigPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		parts := []any{}
		if d.Flash != "" {
			parts = append(parts, flash(d.Flash, d.FlashBad))
		}
		parts = append(parts, markup(`<div class="mb-6 max-w-3xl"><h1 class="text-xl font-semibold text-zinc-100">Config</h1><p class="mt-1 text-sm text-zinc-400">Add, edit and remove the servers, groups, tasks and bundles in your Gorunfile. Changes stay here until you press <span class="font-semibold text-zinc-200">Save</span>, which writes each one back to the file it came from. Comments and layout elsewhere in each file are kept. Runs use the saved file.</p></div><div class="grid gap-6 lg:grid-cols-[1fr_24rem]"><div class="min-w-0 space-y-6"><section class="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><h2 class="text-xs font-semibold uppercase tracking-wider text-zinc-400">Files</h2>`))
		if d.Loaded {
			parts = append(parts, markup(`<ul class="mt-2 space-y-1 font-mono text-xs text-zinc-300">`))
			for _, f := range d.Files {
				parts = append(parts, markup(`<li>`), escaped(f.Label), markup(`</li>`))
			}
			parts = append(parts, markup(`</ul>`))
		} else {
			parts = append(parts, markup(`<p class="mt-2 text-sm text-zinc-500">No Gorunfile loaded yet. Load one on the right.</p>`))
		}
		parts = append(parts,
			markup(`</section>`),
			entityTable("Servers", "host", "Add server", d.Servers, d.Loaded),
			entityTable("Server groups", "group", "Add group", d.Groups, d.Loaded),
			entityTable("Tasks", "task", "Add task", d.Tasks, d.Loaded),
			entityTable("Bundles", "bundle", "Add bundle", d.Bundles, d.Loaded),
			markup(`</div><aside class="space-y-6 self-start">`),
			pendingPanel(d),
			markup(`<section class="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><h2 class="text-sm font-semibold text-zinc-100">Load a Gorunfile</h2><p class="mt-1 text-xs text-zinc-500">A path on the machine running gorun. Unsaved changes are dropped when you load.</p><form method="post" action="/config/load" class="mt-3 space-y-2"><input name="path" required value="`),
			escaped(d.Path),
			markup(`" placeholder="/path/to/Gorunfile" class="`),
			escaped(inputClass()),
			markup(`"/><button class="w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-sm text-zinc-200 transition hover:bg-zinc-700">Load</button></form></section></aside></div>`),
		)
		if d.Loaded {
			parts = append(parts, serverDialog(d), groupDialog(d), taskDialog(d), bundleDialog(d))
		}
		if d.Draft.Mode != "" {
			parts = append(parts, markup("<script>\ndocument.querySelectorAll('dialog[data-open]').forEach(function (d) { d.showModal(); });\n\t\t</script>"))
		}
		return emit(ctx, w, parts...)
	})
}

// entityTable is one saved kind: its rows, and the button that adds another.
func entityTable(title, kind, addLabel string, rows []EntityRow, loaded bool) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		parts := []any{
			markup(`<section class="rounded-2xl border border-zinc-800 bg-zinc-900/40"><header class="flex items-center justify-between gap-3 border-b border-zinc-800 px-4 py-3"><h2 class="text-sm font-semibold text-zinc-100">`),
			escaped(title),
			markup(`</h2><div class="flex items-center gap-3"><span class="text-xs text-zinc-500">`),
			escaped(itoa(len(rows))),
			markup(`</span>`),
		}
		if loaded {
			parts = append(parts, markup(`<a href="`), escaped(modalPath("add", kind, "")), markup(`" class="rounded-lg border border-zinc-700 bg-zinc-800 px-2.5 py-1 text-xs text-zinc-200 transition hover:bg-zinc-700">`), escaped(addLabel), markup(`</a>`))
		}
		parts = append(parts, markup(`</div></header>`))
		if len(rows) == 0 {
			parts = append(parts, markup(`<p class="px-4 py-6 text-center text-sm text-zinc-500">None yet.</p>`))
		} else {
			parts = append(parts, markup(`<ul class="divide-y divide-zinc-800">`))
			for _, r := range rows {
				parts = append(parts, entityItem(kind, r))
			}
			parts = append(parts, markup(`</ul>`))
		}
		parts = append(parts, markup(`</section>`))
		return emit(ctx, w, parts...)
	})
}

// entityItem is one saved entity, with its chips, its edit link and its remove button.
func entityItem(kind string, r EntityRow) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		parts := []any{
			markup(`<li class="`), escaped(entityRowClass(r.Pending)),
			markup(`"><div class="min-w-0 flex-1"><div class="flex items-center gap-2"><span class="truncate font-mono text-sm text-zinc-100">`),
			escaped(r.Name),
			markup(`</span>`),
		}
		if r.Pending != "" {
			parts = append(parts, markup(`<span class="`), escaped(pendingBadgeClass(r.Pending)), markup(`">`), escaped(r.Pending), markup(`</span>`))
		}
		parts = append(parts, markup(`</div><p class="truncate text-xs text-zinc-500">`), escaped(r.Detail), markup(`</p>`))
		if len(r.Chips) > 0 {
			parts = append(parts, markup(`<div class="mt-1 flex flex-wrap gap-1">`))
			for _, c := range r.Chips {
				parts = append(parts, markup(`<span class="rounded-full bg-zinc-800 px-2 py-0.5 font-mono text-[11px] text-zinc-300">`), escaped(c), markup(`</span>`))
			}
			parts = append(parts, markup(`</div>`))
		}
		if r.Source != "" {
			parts = append(parts, markup(`<p class="truncate font-mono text-[11px] text-zinc-600">`), escaped(r.Source), markup(`</p>`))
		}
		parts = append(parts, markup(`</div><div class="flex shrink-0 items-center gap-1">`))
		if r.Pending != "remove" {
			parts = append(parts, markup(`<a href="`), escaped(modalPath("edit", kind, r.Name)), markup(`" class="rounded-lg px-2.5 py-1 text-xs text-zinc-300 transition hover:bg-zinc-800">Edit</a>`))
		}
		parts = append(parts,
			markup(`<form method="post" action="/config/remove"><input type="hidden" name="kind" value="`), escaped(kind),
			markup(`"/><input type="hidden" name="name" value="`), escaped(r.Name),
			markup(`"/><button class="rounded-lg px-2.5 py-1 text-xs text-red-300 transition hover:bg-red-950/40">Remove</button></form></div></li>`),
		)
		return emit(ctx, w, parts...)
	})
}

// pendingPanel lists the unsaved changes, with Save and Discard.
func pendingPanel(d ConfigPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		parts := []any{markup(`<section class="rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"><h2 class="text-sm font-semibold text-zinc-100">Unsaved changes</h2>`)}
		if len(d.Pending) == 0 {
			parts = append(parts, markup(`<p class="mt-1 text-xs text-zinc-500">None. Adding, editing or removing something here lists it until you save.</p>`))
		} else {
			parts = append(parts, markup(`<ul class="mt-2 space-y-1 text-xs text-zinc-300">`))
			for _, p := range d.Pending {
				parts = append(parts, markup(`<li>• `), escaped(p), markup(`</li>`))
			}
			parts = append(parts,
				markup(`</ul><div class="mt-4 flex gap-2"><form method="post" action="/config/save" class="flex-1"><button class="w-full rounded-lg bg-accent px-3 py-1.5 text-sm font-semibold text-white transition hover:bg-violet-600">Save</button></form><form method="post" action="/config/discard"><button class="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 transition hover:bg-zinc-800">Discard</button></form></div>`),
			)
		}
		parts = append(parts, markup(`</section>`))
		return emit(ctx, w, parts...)
	})
}

// serverDialog adds or edits a server.
func serverDialog(d ConfigPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return emit(ctx, w,
			markup(`<dialog class="`), escaped(dialogClass()), markup(`"`), boolAttr(dialogOpen(d.Draft, "host"), "data-open"),
			markup(`><form method="post" action="`), escaped(dialogAction(d.Draft)), markup(`" class="space-y-4 p-6"><input type="hidden" name="kind" value="host"/><h2 class="text-base font-semibold text-zinc-100">`),
			escaped(dialogTitle(d.Draft, "server")),
			markup(`</h2><p class="text-xs text-zinc-500">Leave the SSH user empty for a server that runs on this machine.</p>`),
			nameField(d.Draft, "e.g. web-01"),
			markup(`<label class="block text-xs text-zinc-400">Hostname<input name="hostname" value="`), escaped(d.Draft.Hostname),
			markup(`" placeholder="defaults to the name" class="`), escaped(inputClass()),
			markup(`"/></label><label class="block text-xs text-zinc-400">Tags, comma separated<input name="tags" value="`), escaped(d.Draft.Tags),
			markup(`" class="`), escaped(inputClass()), markup(`"/></label>`),
			choices("groups", "Server groups", d.GroupChoices, "No server groups yet."),
			sshFields(d.Draft),
			fileField(d),
			dialogFooter(dialogSubmit(d.Draft, "server")),
			markup(`</form></dialog>`),
		)
	})
}

// groupDialog adds or edits a server group, and the servers that belong to it.
func groupDialog(d ConfigPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return emit(ctx, w,
			markup(`<dialog class="`), escaped(dialogClass()), markup(`"`), boolAttr(dialogOpen(d.Draft, "group"), "data-open"),
			markup(`><form method="post" action="`), escaped(dialogAction(d.Draft)), markup(`" class="space-y-4 p-6"><input type="hidden" name="kind" value="group"/><h2 class="text-base font-semibold text-zinc-100">`),
			escaped(dialogTitle(d.Draft, "server group")),
			markup(`</h2><p class="text-xs text-zinc-500">Settings servers share by joining the group. Tick the servers that belong to it; they are added to or taken out of the group when you save.</p>`),
			nameField(d.Draft, "e.g. web"),
			markup(`<label class="block text-xs text-zinc-400">Tags, comma separated<input name="tags" value="`), escaped(d.Draft.Tags),
			markup(`" class="`), escaped(inputClass()), markup(`"/></label>`),
			choices("tasks", "Tasks and bundles", d.ListChoices, "No tasks yet."),
			choices("servers", "Servers in this group", d.ServerChoices, "No servers yet."),
			sshFields(d.Draft),
			fileField(d),
			dialogFooter(dialogSubmit(d.Draft, "server group")),
			markup(`</form></dialog>`),
		)
	})
}

// taskDialog adds or edits a task, and the tasks it depends on.
func taskDialog(d ConfigPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return emit(ctx, w,
			markup(`<dialog class="`), escaped(dialogClass()), markup(`"`), boolAttr(dialogOpen(d.Draft, "task"), "data-open"),
			markup(`><form method="post" action="`), escaped(dialogAction(d.Draft)), markup(`" class="space-y-4 p-6"><input type="hidden" name="kind" value="task"/><h2 class="text-base font-semibold text-zinc-100">`),
			escaped(dialogTitle(d.Draft, "task")),
			markup(`</h2>`),
			nameField(d.Draft, "e.g. restart-nginx"),
			markup(`<label class="block text-xs text-zinc-400">Command<input name="cmd" value="`), escaped(d.Draft.Cmd),
			markup(`" placeholder="command to run" class="`), escaped(inputClass()),
			markup(`"/></label><label class="block text-xs text-zinc-400">Description<input name="description" value="`), escaped(d.Draft.Description),
			markup(`" placeholder="shown in the picker" class="`), escaped(inputClass()),
			markup(`"/></label><label class="flex items-center gap-2 text-xs text-zinc-300"><input type="checkbox" name="sudo"`), boolAttr(d.Draft.Sudo, "checked"),
			markup(` class="accent-violet-600"/> run with sudo</label><label class="flex items-center gap-2 text-xs text-zinc-300"><input type="checkbox" name="local"`), boolAttr(d.Draft.Local, "checked"),
			markup(` class="accent-violet-600"/> run only on local servers</label>`),
			choices("depends_on", "Depends on", d.DependChoices, "No other tasks yet. A task can depend on tasks that run before it."),
			fileField(d),
			dialogFooter(dialogSubmit(d.Draft, "task")),
			markup(`</form></dialog>`),
		)
	})
}

// bundleDialog adds or edits a bundle: a named list of tasks.
func bundleDialog(d ConfigPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return emit(ctx, w,
			markup(`<dialog class="`), escaped(dialogClass()), markup(`"`), boolAttr(dialogOpen(d.Draft, "bundle"), "data-open"),
			markup(`><form method="post" action="`), escaped(dialogAction(d.Draft)), markup(`" class="space-y-4 p-6"><input type="hidden" name="kind" value="bundle"/><h2 class="text-base font-semibold text-zinc-100">`),
			escaped(dialogTitle(d.Draft, "bundle")),
			markup(`</h2><p class="text-xs text-zinc-500">A named list of tasks. Use it as <span class="font-mono">@name</span> anywhere a task list goes.</p>`),
			nameField(d.Draft, "e.g. baseline"),
			markup(`<label class="block text-xs text-zinc-400">Description<input name="description" value="`), escaped(d.Draft.Description),
			markup(`" class="`), escaped(inputClass()), markup(`"/></label>`),
			choices("tasks", "Tasks", d.ListChoices, "No tasks yet."),
			fileField(d),
			dialogFooter(dialogSubmit(d.Draft, "bundle")),
			markup(`</form></dialog>`),
		)
	})
}

// nameField is the name input of a dialog. It can't change when an entity is edited.
func nameField(d Draft, placeholder string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return emit(ctx, w,
			markup(`<label class="block text-xs text-zinc-400">Name<input name="name" required value="`), escaped(d.Name),
			markup(`"`), boolAttr(d.Mode == "edit", "readonly"),
			markup(` placeholder="`), escaped(placeholder), markup(`" class="`), escaped(inputClass()), markup(`"/></label>`),
		)
	})
}

// sshFields are the SSH user and identity file of a server or group.
func sshFields(d Draft) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return emit(ctx, w,
			markup(`<div class="grid grid-cols-2 gap-3"><label class="block text-xs text-zinc-400">SSH user<input name="ssh_user" value="`), escaped(d.SSHUser),
			markup(`" class="`), escaped(inputClass()),
			markup(`"/></label><label class="block text-xs text-zinc-400">Identity file<input name="identityfile" value="`), escaped(d.IdentityFile),
			markup(`" placeholder="~/.ssh/id_ed25519" class="`), escaped(inputClass()), markup(`"/></label></div>`),
		)
	})
}

// choices is a picker of checkboxes shown as chips: the ticked ones are the selection.
func choices(name, label string, items []Choice, none string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		parts := []any{markup(`<fieldset class="space-y-2"><legend class="text-xs font-semibold uppercase tracking-wider text-zinc-400">`), escaped(label), markup(`</legend>`)}
		if len(items) == 0 {
			parts = append(parts, markup(`<p class="text-xs text-zinc-500">`), escaped(none), markup(`</p>`))
		}
		parts = append(parts, markup(`<div class="flex flex-wrap gap-2">`))
		for _, c := range items {
			parts = append(parts,
				markup(`<label class="`), escaped(chipPickClass(c.Checked)),
				markup(`"><input type="checkbox" name="`), escaped(name), markup(`" value="`), escaped(c.Value), markup(`"`), boolAttr(c.Checked, "checked"),
				markup(` class="accent-violet-600"/><span class="font-mono">`), escaped(c.Label), markup(`</span></label>`),
			)
		}
		parts = append(parts, markup(`</div></fieldset>`))
		return emit(ctx, w, parts...)
	})
}

// fileField picks the file a new entity is saved into, or keeps an edited entity in its file.
func fileField(d ConfigPageData) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if d.Draft.Mode == "edit" {
			return emit(ctx, w, markup(`<input type="hidden" name="file" value="`), escaped(d.Draft.File), markup(`"/>`))
		}
		parts := []any{markup(`<label class="block text-xs text-zinc-400">Save into<select name="file" class="`), escaped(inputClass()), markup(`">`)}
		for _, f := range d.Files {
			parts = append(parts, markup(`<option value="`), escaped(f.Path), markup(`">`), escaped(f.Label), markup(`</option>`))
		}
		parts = append(parts, markup(`</select></label>`))
		return emit(ctx, w, parts...)
	})
}

// dialogFooter is the cancel and submit row of a dialog.
func dialogFooter(submit string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return emit(ctx, w,
			markup(`<div class="flex justify-end gap-2 pt-2"><a href="/config" class="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-800">Cancel</a><button class="rounded-lg bg-accent px-4 py-1.5 text-sm font-semibold text-white hover:bg-violet-600">`),
			escaped(submit),
			markup(`</button></div>`),
		)
	})
}
