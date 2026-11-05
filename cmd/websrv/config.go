package websrv

import (
	"errors"
	"fmt"
	"log"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// The entity kinds, as the forms and the saved changes name them.
const (
	kindHost   = "host"
	kindGroup  = "group"
	kindTask   = "task"
	kindBundle = "bundle"
)

// kindNames is how each kind reads in messages.
var kindNames = map[string]string{
	kindHost:   "server",
	kindGroup:  "server group",
	kindTask:   "task",
	kindBundle: "bundle",
}

// tablePath is where each kind lives in a Gorunfile.
var tablePath = map[string][]string{
	kindHost:   {"servers", "hosts"},
	kindGroup:  {"server_groups"},
	kindTask:   {"tasks"},
	kindBundle: {"bundles"},
}

// loadConfig reads path into the editor. Runs use this file from now on, and unsaved changes
// are dropped.
func (a *app) loadConfig(path string) error {
	cfg := &gorun.Config{}
	if err := cfg.LoadFile(expandHome(path)); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = cfg
	a.cfgPath = cfg.Path
	a.pending = nil
	a.mu.Unlock()
	return nil
}

// expandHome replaces a leading ~/ with the home folder.
func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}

// editor is one request's view of the Gorunfile: the loaded file with the unsaved changes applied.
type editor struct {
	cfg     *gorun.Config
	pending []gorun.Change
	docs    map[string]map[string]any // source files, decoded once for reading
}

// entityState is one entity as it stands once the unsaved changes are applied.
type entityState struct {
	body    map[string]any // read only: edits work on a fresh copy
	file    string
	pending string // "", "add", "edit" or "remove"
	present bool
}

// newEditor copies the state a request works on, so a failed request changes nothing. The
// caller holds a.mu.
func (a *app) newEditor() *editor {
	ed := &editor{cfg: a.cfg, docs: map[string]map[string]any{}}
	for _, p := range a.pending {
		if p.Body != nil {
			p.Body = maps.Clone(p.Body)
		}
		ed.pending = append(ed.pending, p)
	}
	return ed
}

// lastChange returns the latest unsaved change to kind and name.
func (ed *editor) lastChange(kind, name string) (gorun.Change, bool) {
	for i := len(ed.pending) - 1; i >= 0; i-- {
		if p := ed.pending[i]; p.Kind == kind && p.Name == name {
			return p, true
		}
	}
	return gorun.Change{}, false
}

// lookup returns kind and name with the unsaved changes applied.
func (ed *editor) lookup(kind, name string) entityState {
	loaded := inFile(ed.cfg, kind, name)
	if p, ok := ed.lastChange(kind, name); ok {
		if p.Remove {
			if !loaded {
				return entityState{}
			}
			file := loadedFile(ed.cfg, kind, name)
			return entityState{body: ed.decoded(file, kind, name), file: file, pending: "remove"}
		}
		if loaded {
			return entityState{body: p.Body, file: p.File, pending: "edit", present: true}
		}
		return entityState{body: p.Body, file: p.File, pending: "add", present: true}
	}
	if !loaded {
		return entityState{}
	}
	file := loadedFile(ed.cfg, kind, name)
	return entityState{body: ed.decoded(file, kind, name), file: file, present: true}
}

// decoded returns an entity as its file defines it. The result is shared, so do not change it.
func (ed *editor) decoded(file, kind, name string) map[string]any {
	doc, ok := ed.docs[file]
	if !ok {
		doc = readDoc(file)
		ed.docs[file] = doc
	}
	return entityIn(doc, kind, name)
}

// allNames lists every name of kind in the file or the unsaved changes, removed ones included.
func (ed *editor) allNames(kind string) []string {
	set := map[string]bool{}
	if ed.cfg != nil {
		for _, n := range loadedNames(ed.cfg, kind) {
			set[n] = true
		}
	}
	for _, p := range ed.pending {
		if p.Kind == kind {
			set[p.Name] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// names lists the entities of kind that exist once the unsaved changes are applied.
func (ed *editor) names(kind string) []string {
	var out []string
	for _, n := range ed.allNames(kind) {
		if ed.lookup(kind, n).present {
			out = append(out, n)
		}
	}
	return out
}

// present reports whether kind and name exist once the unsaved changes are applied.
func (ed *editor) present(kind, name string) bool {
	return ed.lookup(kind, name).present
}

// add queues a new entity, with fill setting its fields.
func (ed *editor) add(kind, name, file string, fill func(map[string]any) error) error {
	m := map[string]any{}
	if err := fill(m); err != nil {
		return err
	}
	ed.pending = append(ed.pending, gorun.Change{Kind: kind, Name: name, File: file, Body: m})
	return nil
}

// edit changes an existing entity. One added in this session is changed in place; a saved one is
// rewritten from its definition in its file, so fields the form doesn't manage are kept.
func (ed *editor) edit(kind, name string, change func(map[string]any) error) error {
	s := ed.lookup(kind, name)
	if !s.present {
		return fmt.Errorf("no %s named %s", kindNames[kind], name)
	}
	if p, ok := ed.lastChange(kind, name); ok && !p.Remove {
		return change(p.Body)
	}
	m := entityIn(readDoc(s.file), kind, name)
	if m == nil {
		return fmt.Errorf("%s %s is not in %s", kindNames[kind], name, s.file)
	}
	if err := change(m); err != nil {
		return err
	}
	ed.pending = append(ed.pending,
		gorun.Change{Kind: kind, Name: name, File: s.file, Remove: true},
		gorun.Change{Kind: kind, Name: name, File: s.file, Body: m},
	)
	return nil
}

// groupsOf lists the groups a server belongs to, once the unsaved changes are applied.
func (ed *editor) groupsOf(host string) []string {
	return strList(ed.lookup(kindHost, host).body["groups"])
}

// members lists the servers in group.
func (ed *editor) members(group string) []string {
	var out []string
	for _, h := range ed.names(kindHost) {
		if slices.Contains(ed.groupsOf(h), group) {
			out = append(out, h)
		}
	}
	return out
}

// setMembers makes the given servers the group's members. Only servers whose membership changes
// are rewritten.
func (ed *editor) setMembers(group string, members []string) error {
	for _, h := range ed.names(kindHost) {
		want := slices.Contains(members, h)
		if want == slices.Contains(ed.groupsOf(h), group) {
			continue
		}
		err := ed.edit(kindHost, h, func(m map[string]any) error {
			setStrs(m, "groups", toggle(strList(m["groups"]), group, want))
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// referrers lists the entities that use kind and name, so removing it would leave them broken.
func (ed *editor) referrers(kind, name string) []string {
	var out []string
	for _, owner := range []string{kindHost, kindGroup, kindTask, kindBundle} {
		for _, n := range ed.names(owner) {
			if owner == kind && n == name {
				continue
			}
			if usesEntity(owner, ed.lookup(owner, n).body, kind, name) {
				out = append(out, kindNames[owner]+" "+n)
			}
		}
	}
	return out
}

// ref is one entity a definition points at.
type ref struct{ kind, name string }

// refsOf lists what owner's definition refers to. A bundle is written @name in a task list.
func refsOf(owner string, m map[string]any) []ref {
	var out []ref
	taskRefs := func(keys ...string) {
		for _, key := range keys {
			for _, v := range strList(m[key]) {
				if b, ok := strings.CutPrefix(v, "@"); ok {
					out = append(out, ref{kindBundle, b})
				} else {
					out = append(out, ref{kindTask, v})
				}
			}
		}
	}
	switch owner {
	case kindTask:
		taskRefs("depends_on")
	case kindBundle, kindGroup:
		taskRefs("tasks")
	case kindHost:
		taskRefs("tasks", "add_tasks", "skip_tasks")
		for _, g := range strList(m["groups"]) {
			out = append(out, ref{kindGroup, g})
		}
	}
	return out
}

// unresolved lists references in the editor's config that point at entities that don't exist.
// The loader doesn't catch these, so a save checks them before writing.
func (ed *editor) unresolved() []string {
	var out []string
	for _, owner := range []string{kindHost, kindGroup, kindTask, kindBundle} {
		for _, n := range ed.names(owner) {
			for _, r := range refsOf(owner, ed.lookup(owner, n).body) {
				if !ed.present(r.kind, r.name) {
					out = append(out, fmt.Sprintf("%s %s uses %s %s, which does not exist", kindNames[owner], n, kindNames[r.kind], r.name))
				}
			}
		}
	}
	return out
}

// usesEntity reports whether owner's definition refers to kind and name.
func usesEntity(owner string, m map[string]any, kind, name string) bool {
	for _, r := range refsOf(owner, m) {
		if r.kind == kind && r.name == name {
			return true
		}
	}
	return false
}

// configPage is the editor: every entity in the loaded file with the unsaved changes marked, and
// the dialog named in the URL, if any.
func (a *app) configPage(w http.ResponseWriter, r *http.Request) {
	msg, bad := flashFrom(r)
	a.mu.Lock()
	ed := a.newEditor()
	a.mu.Unlock()
	a.renderConfig(w, ed, ed.draftFromQuery(r.URL.Query()), msg, bad)
}

// renderConfig writes the editor page. d is the dialog to open, if any.
func (a *app) renderConfig(w http.ResponseWriter, ed *editor, d ui.Draft, msg string, bad bool) {
	data := ui.ConfigPageData{Draft: d, Flash: msg, FlashBad: bad, Pending: pendingLabels(ed.pending)}
	data.HasPending = len(data.Pending) > 0
	if ed.cfg != nil {
		data.Loaded = true
		data.Path = ed.cfg.Path
		for _, f := range ed.cfg.Files {
			data.Files = append(data.Files, ui.FileOption{Path: f, Label: rel(ed.cfg.Path, f)})
		}
		data.Servers = ed.rows(kindHost)
		data.Groups = ed.rows(kindGroup)
		data.Tasks = ed.rows(kindTask)
		data.Bundles = ed.rows(kindBundle)
		data.DependChoices, data.ListChoices, data.ServerChoices, data.GroupChoices = ed.choiceLists(d)
	}
	a.page(w, "Config", "config", ui.ConfigPage(data))
}

// rows lists the entities of kind for the editor, with their unsaved state.
func (ed *editor) rows(kind string) []ui.EntityRow {
	out := []ui.EntityRow{}
	for _, name := range ed.allNames(kind) {
		s := ed.lookup(kind, name)
		if !s.present && s.pending != "remove" {
			continue
		}
		detail, chips := describeEntity(kind, s.body)
		out = append(out, ui.EntityRow{
			Name:    name,
			Detail:  detail,
			Chips:   chips,
			Source:  rel(ed.cfg.Path, s.file),
			Pending: s.pending,
		})
	}
	return out
}

// choiceLists builds the pickers for the open dialog. Each option is ticked when the dialog's
// draft already uses it.
func (ed *editor) choiceLists(d ui.Draft) (depend, list, servers, groups []ui.Choice) {
	for _, n := range ed.names(kindTask) {
		if d.Kind == kindTask && n == d.Name {
			continue
		}
		depend = append(depend, choice(n, n, d.Depends))
		list = append(list, choice(n, n, d.Tasks))
	}
	for _, n := range ed.names(kindBundle) {
		if d.Kind == kindBundle && n == d.Name {
			continue
		}
		list = append(list, choice("@"+n, "@"+n, d.Tasks))
	}
	for _, n := range ed.names(kindHost) {
		servers = append(servers, choice(n, n, d.Servers))
	}
	for _, n := range ed.names(kindGroup) {
		if d.Kind == kindGroup && n == d.Name {
			continue
		}
		groups = append(groups, choice(n, n, d.Groups))
	}
	return depend, list, servers, groups
}

// choice is one option of a picker, ticked when picked holds its value.
func choice(value, label string, picked []string) ui.Choice {
	return ui.Choice{Value: value, Label: label, Checked: slices.Contains(picked, value)}
}

// draftFromQuery opens the dialog named by ?modal=add-<kind> or ?modal=edit-<kind>&name=<name>.
func (ed *editor) draftFromQuery(q url.Values) ui.Draft {
	mode, kind, ok := strings.Cut(q.Get("modal"), "-")
	if !ok || !validKind(kind) || (mode != "add" && mode != "edit") || ed.cfg == nil {
		return ui.Draft{}
	}
	if mode == "add" {
		return ui.Draft{Mode: mode, Kind: kind}
	}
	return ed.draftFor(kind, q.Get("name"))
}

// draftFor fills the dialog for an existing entity from its definition with the unsaved changes.
func (ed *editor) draftFor(kind, name string) ui.Draft {
	s := ed.lookup(kind, name)
	if !s.present {
		return ui.Draft{}
	}
	m := s.body
	ssh, _ := m["ssh"].(map[string]any)
	d := ui.Draft{
		Mode:         "edit",
		Kind:         kind,
		Name:         name,
		File:         s.file,
		Hostname:     str(m, "hostname"),
		Tags:         strings.Join(strList(m["tags"]), ", "),
		Description:  str(m, "description"),
		Cmd:          str(m, "cmd"),
		Sudo:         boolOf(m, "sudo"),
		Local:        boolOf(m, "local"),
		SSHUser:      str(ssh, "username"),
		IdentityFile: str(ssh, "identityfile"),
		Depends:      strList(m["depends_on"]),
		Tasks:        strList(m["tasks"]),
		Groups:       strList(m["groups"]),
	}
	if kind == kindGroup {
		d.Servers = ed.members(name)
	}
	return d
}

// draftFromForm keeps what was typed, so a failed save shows the dialog as it was left.
func draftFromForm(mode, kind, name string, form url.Values) ui.Draft {
	return ui.Draft{
		Mode:         mode,
		Kind:         kind,
		Name:         name,
		File:         form.Get("file"),
		Hostname:     form.Get("hostname"),
		Tags:         form.Get("tags"),
		Description:  form.Get("description"),
		Cmd:          form.Get("cmd"),
		Sudo:         form.Has("sudo"),
		Local:        form.Has("local"),
		SSHUser:      form.Get("ssh_user"),
		IdentityFile: form.Get("identityfile"),
		Depends:      form["depends_on"],
		Tasks:        form["tasks"],
		Groups:       form["groups"],
		Servers:      form["servers"],
	}
}

// addHandler queues a new server, group, task or bundle to be saved into the chosen file.
func (a *app) addHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/config", "Could not read the form.", true)
		return
	}
	kind := r.PostFormValue("kind")
	name := strings.TrimSpace(r.PostFormValue("name"))

	a.mu.Lock()
	defer a.mu.Unlock()
	ed := a.newEditor()
	draft := draftFromForm("add", kind, name, r.PostForm)
	fail := func(msg string) { a.renderConfig(w, ed, draft, msg, true) }

	switch {
	case ed.cfg == nil:
		fail("Load a Gorunfile first.")
		return
	case !validKind(kind):
		fail("Unknown kind of entity.")
		return
	}
	if err := validName(name); err != nil {
		fail(err.Error())
		return
	}
	file := r.PostFormValue("file")
	if !slices.Contains(ed.cfg.Files, file) {
		fail("Pick the file to save into.")
		return
	}
	if ed.present(kind, name) {
		fail(fmt.Sprintf("A %s named %s already exists.", kindNames[kind], name))
		return
	}
	if err := ed.add(kind, name, file, func(m map[string]any) error { return applyEntity(kind, m, r.PostForm, true) }); err != nil {
		fail(err.Error())
		return
	}
	if kind == kindGroup {
		if err := ed.setMembers(name, r.PostForm["servers"]); err != nil {
			fail(err.Error())
			return
		}
	}
	a.pending = ed.pending
	redirectWith(w, r, "/config", fmt.Sprintf("Added %s %s. Save to write it to the file.", kindNames[kind], name), false)
}

// updateHandler queues the new definition of an existing server, group, task or bundle.
func (a *app) updateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/config", "Could not read the form.", true)
		return
	}
	kind := r.PostFormValue("kind")
	name := r.PostFormValue("name")

	a.mu.Lock()
	defer a.mu.Unlock()
	ed := a.newEditor()
	draft := draftFromForm("edit", kind, name, r.PostForm)
	fail := func(msg string) { a.renderConfig(w, ed, draft, msg, true) }

	switch {
	case ed.cfg == nil:
		fail("Load a Gorunfile first.")
		return
	case !validKind(kind):
		fail("Unknown kind of entity.")
		return
	case !ed.present(kind, name):
		fail(fmt.Sprintf("No %s named %s to edit.", kindNames[kind], name))
		return
	}
	if err := ed.edit(kind, name, func(m map[string]any) error { return applyEntity(kind, m, r.PostForm, false) }); err != nil {
		fail(err.Error())
		return
	}
	if kind == kindGroup {
		if err := ed.setMembers(name, r.PostForm["servers"]); err != nil {
			fail(err.Error())
			return
		}
	}
	a.pending = ed.pending
	redirectWith(w, r, "/config", fmt.Sprintf("Updated %s %s. Save to write it to the file.", kindNames[kind], name), false)
}

// removeHandler queues the removal of a server, group, task or bundle. It refuses when something
// else still uses it, since the file would no longer load.
func (a *app) removeHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/config", "Could not read the form.", true)
		return
	}
	kind := r.PostFormValue("kind")
	name := r.PostFormValue("name")

	a.mu.Lock()
	defer a.mu.Unlock()
	ed := a.newEditor()
	switch {
	case ed.cfg == nil:
		redirectWith(w, r, "/config", "Load a Gorunfile first.", true)
		return
	case !validKind(kind):
		redirectWith(w, r, "/config", "Unknown kind of entity.", true)
		return
	}
	s := ed.lookup(kind, name)
	if !s.present {
		redirectWith(w, r, "/config", fmt.Sprintf("No %s named %s to remove.", kindNames[kind], name), true)
		return
	}
	if refs := ed.referrers(kind, name); len(refs) > 0 {
		msg := fmt.Sprintf("Can't remove %s %s yet. Used by %s; change those first.", kindNames[kind], name, strings.Join(refs, ", "))
		redirectWith(w, r, "/config", msg, true)
		return
	}
	ed.pending = append(ed.pending, gorun.Change{Kind: kind, Name: name, File: s.file, Remove: true})
	a.pending = ed.pending
	redirectWith(w, r, "/config", fmt.Sprintf("Removed %s %s. Save to write it to the file.", kindNames[kind], name), false)
}

// saveHandler writes the unsaved changes into their files and reloads them to check. If the result
// won't load, the files go back as they were and the changes stay pending.
func (a *app) saveHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	ed := a.newEditor()
	path, cfg := a.cfgPath, a.cfg
	a.mu.Unlock()
	pending := ed.pending
	if len(pending) == 0 || cfg == nil {
		redirectWith(w, r, "/config", "Nothing to save.", true)
		return
	}
	if problems := ed.unresolved(); len(problems) > 0 {
		msg := fmt.Sprintf("Not saved: %s. Fix it, or remove what's missing.", problems[0])
		redirectWith(w, r, "/config", msg, true)
		return
	}
	snap, err := snapshotFiles(cfg.Files)
	if err != nil {
		redirectWith(w, r, "/config", "Could not read the files: "+err.Error(), true)
		return
	}
	if err := gorun.SaveChanges(pending); err != nil {
		restoreFiles(snap)
		redirectWith(w, r, "/config", "Could not save, so your files are back as they were: "+err.Error(), true)
		return
	}
	if err := a.loadConfig(path); err != nil {
		restoreFiles(snap)
		if err := a.loadConfig(path); err != nil {
			log.Printf("gorun web: reload after restore: %v", err)
		}
		a.mu.Lock()
		a.pending = pending
		a.mu.Unlock()
		msg := "Not saved: the changes would not load (" + err.Error() + "). Your files are back as they were."
		redirectWith(w, r, "/config", msg, true)
		return
	}
	redirectWith(w, r, "/config", fmt.Sprintf("Saved %d change(s).", len(pending)), false)
}

// discardHandler drops the unsaved changes.
func (a *app) discardHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.pending = nil
	a.mu.Unlock()
	redirectWith(w, r, "/config", "Discarded the unsaved changes.", false)
}

// loadHandler loads another Gorunfile into the editor.
func (a *app) loadHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/config", "Could not read the form.", true)
		return
	}
	path := strings.TrimSpace(r.PostFormValue("path"))
	if path == "" {
		redirectWith(w, r, "/config", "Give the path to a Gorunfile.", true)
		return
	}
	if err := a.loadConfig(path); err != nil {
		redirectWith(w, r, "/config", "Could not load: "+err.Error(), true)
		return
	}
	redirectWith(w, r, "/config", "Loaded "+path+".", false)
}

// applyEntity sets the fields a form manages on an entity's definition. Fields the form doesn't
// show are left as they are.
func applyEntity(kind string, m map[string]any, form url.Values, adding bool) error {
	switch kind {
	case kindHost:
		setStr(m, "hostname", form.Get("hostname"))
		setStrs(m, "tags", splitList(form.Get("tags")))
		setStrs(m, "groups", cleanList(form["groups"]))
		setSSH(m, form.Get("ssh_user"), form.Get("identityfile"))
	case kindGroup:
		setStrs(m, "tags", splitList(form.Get("tags")))
		setStrs(m, "tasks", cleanList(form["tasks"]))
		setSSH(m, form.Get("ssh_user"), form.Get("identityfile"))
	case kindTask:
		if cmd := strings.TrimSpace(form.Get("cmd")); cmd != "" {
			m["cmd"] = cmd
		} else if adding {
			return errors.New("a task needs a command")
		}
		setStr(m, "description", form.Get("description"))
		setBool(m, "sudo", form.Has("sudo"))
		setBool(m, "local", form.Has("local"))
		setStrs(m, "depends_on", cleanList(form["depends_on"]))
	case kindBundle:
		tasks := cleanList(form["tasks"])
		if len(tasks) == 0 {
			return errors.New("a bundle needs at least one task")
		}
		setStr(m, "description", form.Get("description"))
		setStrs(m, "tasks", tasks)
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}
	return nil
}

// pendingLabels says what each unsaved change does. An edit is a remove and an add of the same
// name, so the pair is shown once.
func pendingLabels(pending []gorun.Change) []string {
	var out []string
	for i := 0; i < len(pending); i++ {
		p := pending[i]
		if p.Remove && i+1 < len(pending) && pending[i+1].Kind == p.Kind && pending[i+1].Name == p.Name && !pending[i+1].Remove {
			out = append(out, fmt.Sprintf("Edit %s %s", kindNames[p.Kind], p.Name))
			i++
			continue
		}
		out = append(out, describe(p))
	}
	return out
}

// describe says what an unsaved change does, for the pending list.
func describe(p gorun.Change) string {
	verb := "Add"
	if p.Remove {
		verb = "Remove"
	}
	return fmt.Sprintf("%s %s %s", verb, kindNames[p.Kind], p.Name)
}

// describeEntity gives an entity's detail line and the chips that show what it uses.
func describeEntity(kind string, m map[string]any) (string, []string) {
	switch kind {
	case kindHost:
		if _, ok := m["ssh"]; !ok {
			return "local", strList(m["groups"])
		}
		return str(m, "hostname"), strList(m["groups"])
	case kindGroup:
		return strings.Join(strList(m["tags"]), ", "), strList(m["tasks"])
	case kindTask:
		detail := str(m, "description")
		if detail == "" {
			detail = str(m, "cmd")
		}
		var after []string
		for _, d := range strList(m["depends_on"]) {
			after = append(after, "after "+d)
		}
		return detail, after
	case kindBundle:
		return str(m, "description"), strList(m["tasks"])
	}
	return "", nil
}

// loadedNames lists the names of kind in the loaded file.
func loadedNames(cfg *gorun.Config, kind string) []string {
	switch kind {
	case kindHost:
		out := make([]string, 0, len(cfg.Servers))
		for _, s := range cfg.Servers {
			out = append(out, s.Name)
		}
		return out
	case kindGroup:
		return slices.Collect(maps.Keys(cfg.Groups))
	case kindTask:
		return slices.Collect(maps.Keys(cfg.Tasks))
	case kindBundle:
		return slices.Collect(maps.Keys(cfg.Bundles))
	}
	return nil
}

// inFile reports whether the loaded file defines kind and name.
func inFile(cfg *gorun.Config, kind, name string) bool {
	return cfg != nil && slices.Contains(loadedNames(cfg, kind), name)
}

// loadedFile is the file that defines kind and name in the loaded config.
func loadedFile(cfg *gorun.Config, kind, name string) string {
	src := ""
	switch kind {
	case kindHost:
		for _, s := range cfg.Servers {
			if s.Name == name {
				src = s.Source
			}
		}
	case kindGroup:
		src = cfg.Groups[name].Source
	case kindTask:
		src = cfg.Tasks[name].Source
	case kindBundle:
		src = cfg.Bundles[name].Source
	}
	if src == "" {
		return cfg.Path
	}
	return src
}

// readDoc decodes a Gorunfile as it is on disk. It returns nil when the file can't be read.
func readDoc(file string) map[string]any {
	var doc map[string]any
	if _, err := toml.DecodeFile(file, &doc); err != nil {
		log.Printf("gorun web: read %s: %v", file, err)
		return nil
	}
	return doc
}

// entityIn finds kind and name in a decoded Gorunfile.
func entityIn(doc map[string]any, kind, name string) map[string]any {
	tbl := doc
	for _, key := range tablePath[kind] {
		next, ok := tbl[key].(map[string]any)
		if !ok {
			return nil
		}
		tbl = next
	}
	e, _ := tbl[name].(map[string]any)
	return e
}

// setSSH sets the SSH user and identity file, keeping any other SSH settings.
func setSSH(m map[string]any, user, identity string) {
	ssh := map[string]any{}
	if old, ok := m["ssh"].(map[string]any); ok {
		maps.Copy(ssh, old)
	}
	setStr(ssh, "username", user)
	setStr(ssh, "identityfile", identity)
	if len(ssh) == 0 {
		delete(m, "ssh")
		return
	}
	m["ssh"] = ssh
}

// setStr sets key to value, or removes it when value is blank.
func setStr(m map[string]any, key, value string) {
	if v := strings.TrimSpace(value); v != "" {
		m[key] = v
		return
	}
	delete(m, key)
}

// setStrs sets key to items, or removes it when there are none.
func setStrs(m map[string]any, key string, items []string) {
	if len(items) == 0 {
		delete(m, key)
		return
	}
	m[key] = items
}

// setBool sets key to true, or removes it.
func setBool(m map[string]any, key string, on bool) {
	if on {
		m[key] = true
		return
	}
	delete(m, key)
}

// str reads a string value, or "" when it is missing.
func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// boolOf reads a boolean value, or false when it is missing.
func boolOf(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

// strList reads a list of strings from a decoded value.
func strList(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// cleanList trims the values, drops blanks and repeats, and returns nil when none are left.
func cleanList(values []string) []string {
	var out []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// splitList reads a comma separated value.
func splitList(value string) []string {
	return cleanList(strings.Split(value, ","))
}

// toggle returns list with item added when on is true, and removed otherwise.
func toggle(list []string, item string, on bool) []string {
	out := slices.DeleteFunc(slices.Clone(list), func(s string) bool { return s == item })
	if on {
		out = append(out, item)
	}
	return out
}

// validName rejects names that would not survive as a TOML key or a task reference.
func validName(name string) error {
	if name == "" {
		return errors.New("give it a name")
	}
	if strings.ContainsAny(name, " \t.[]\"'#@,") {
		return fmt.Errorf("%s has characters a name can't use", name)
	}
	return nil
}

// validKind reports whether kind is one of the editable kinds.
func validKind(kind string) bool {
	_, ok := kindNames[kind]
	return ok
}

// rel shows path relative to the folder of the root Gorunfile, when it is inside it.
func rel(root, path string) string {
	if path == "" || root == "" {
		return path
	}
	r, err := filepath.Rel(filepath.Dir(root), path)
	if err != nil {
		return path
	}
	return r
}

// fileCopy is a file's contents and mode, kept so a failed save can put them back.
type fileCopy struct {
	data []byte
	mode os.FileMode
}

// snapshotFiles reads each file so it can be restored.
func snapshotFiles(files []string) (map[string]fileCopy, error) {
	out := map[string]fileCopy{}
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		out[f] = fileCopy{data: data, mode: info.Mode().Perm()}
	}
	return out, nil
}

// restoreFiles writes the snapshot back.
func restoreFiles(snap map[string]fileCopy) {
	for f, c := range snap {
		if err := os.WriteFile(f, c.data, c.mode); err != nil {
			log.Printf("gorun web: restore %s: %v", f, err)
		}
	}
}
