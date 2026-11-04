package websrv

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

// configPage is the editor: every server, group, task and bundle in the loaded file, with the
// unsaved changes marked.
func (a *app) configPage(w http.ResponseWriter, r *http.Request) {
	msg, bad := flashFrom(r)
	a.mu.Lock()
	cfg := a.cfg
	pending := slices.Clone(a.pending)
	a.mu.Unlock()

	d := configData(cfg, pending)
	d.Flash, d.FlashBad = msg, bad
	d.Browser = browse(pickerDir(r.URL.Query().Get("dir"), d.Path))
	a.page(w, "Config", "config", ui.ConfigPage(d))
}

// maxBrowse caps how many entries the picker lists, so a huge folder stays usable.
const maxBrowse = 500

// pickerDir is the folder the picker opens: the one asked for, else the loaded file's, else
// the working directory.
func pickerDir(asked, loaded string) string {
	switch {
	case asked != "":
		return filepath.Clean(expandHome(asked))
	case loaded != "":
		return filepath.Dir(loaded)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return string(filepath.Separator)
}

// browse lists dir for the picker: folders first, then files, hidden ones left out.
func browse(dir string) ui.Browser {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	b := ui.Browser{Dir: dir}
	if parent := filepath.Dir(dir); parent != dir {
		b.Parent = parent
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		b.Err = "Could not open this folder: " + err.Error()
		return b
	}
	var dirs, files []ui.BrowseEntry
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(path); err == nil {
				isDir = fi.IsDir()
			}
		}
		if isDir {
			dirs = append(dirs, ui.BrowseEntry{Name: name, Path: path, Dir: true})
			continue
		}
		files = append(files, ui.BrowseEntry{Name: name, Path: path, Likely: likelyGorunfile(name)})
	}
	b.Entries = append(dirs, files...)
	if len(b.Entries) > maxBrowse {
		b.Entries = b.Entries[:maxBrowse]
		b.Err = fmt.Sprintf("Showing the first %d entries.", maxBrowse)
	}
	return b
}

// likelyGorunfile reports whether name looks like a Gorunfile or an included TOML file.
func likelyGorunfile(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "gorunfile") || strings.EqualFold(filepath.Ext(name), ".toml")
}

// configData builds the editor page from the loaded file and the unsaved changes.
func configData(cfg *gorun.Config, pending []gorun.Change) ui.ConfigPageData {
	var d ui.ConfigPageData
	for _, p := range pending {
		d.Pending = append(d.Pending, describe(p))
	}
	d.HasPending = len(pending) > 0
	if cfg == nil {
		return d
	}

	root := cfg.Path
	d.Path = root
	d.Loaded = true
	for _, f := range cfg.Files {
		d.Files = append(d.Files, ui.FileOption{Path: f, Label: rel(root, f)})
	}
	d.Servers = entityRows(kindHost, hostRows(cfg, root), pending)
	d.Groups = entityRows(kindGroup, groupRows(cfg, root), pending)
	d.Tasks = entityRows(kindTask, taskRows(cfg, root), pending)
	d.Bundles = entityRows(kindBundle, bundleRows(cfg, root), pending)
	return d
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

// hostRows lists the servers of the loaded file.
func hostRows(cfg *gorun.Config, root string) []ui.EntityRow {
	out := make([]ui.EntityRow, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		detail := s.Address()
		if len(s.Groups) > 0 {
			detail += " · groups " + strings.Join(s.Groups, ", ")
		}
		out = append(out, ui.EntityRow{Name: s.Name, Detail: detail, Source: rel(root, s.Source)})
	}
	return out
}

// groupRows lists the server groups of the loaded file.
func groupRows(cfg *gorun.Config, root string) []ui.EntityRow {
	names := slices.Sorted(maps.Keys(cfg.Groups))
	out := make([]ui.EntityRow, 0, len(names))
	for _, n := range names {
		g := cfg.Groups[n]
		out = append(out, ui.EntityRow{Name: n, Detail: strings.Join(g.Tags, ", "), Source: rel(root, g.Source)})
	}
	return out
}

// taskRows lists the task library of the loaded file.
func taskRows(cfg *gorun.Config, root string) []ui.EntityRow {
	names := slices.Sorted(maps.Keys(cfg.Tasks))
	out := make([]ui.EntityRow, 0, len(names))
	for _, n := range names {
		t := cfg.Tasks[n]
		detail := t.Description
		if detail == "" {
			detail = t.Cmd
		}
		out = append(out, ui.EntityRow{Name: n, Detail: detail, Source: rel(root, t.Source)})
	}
	return out
}

// bundleRows lists the bundles of the loaded file.
func bundleRows(cfg *gorun.Config, root string) []ui.EntityRow {
	names := slices.Sorted(maps.Keys(cfg.Bundles))
	out := make([]ui.EntityRow, 0, len(names))
	for _, n := range names {
		b := cfg.Bundles[n]
		out = append(out, ui.EntityRow{Name: n, Detail: strings.Join(b.Tasks, ", "), Source: rel(root, b.Source)})
	}
	return out
}

// entityRows applies the unsaved changes of one kind to its rows: removals are marked, and
// additions are added to the list.
func entityRows(kind string, rows []ui.EntityRow, pending []gorun.Change) []ui.EntityRow {
	for _, p := range pending {
		if p.Kind != kind {
			continue
		}
		i := slices.IndexFunc(rows, func(r ui.EntityRow) bool { return r.Name == p.Name })
		switch {
		case p.Remove && i >= 0 && rows[i].Pending == "add":
			rows = slices.Delete(rows, i, i+1)
		case p.Remove && i >= 0:
			rows[i].Pending = "remove"
		case !p.Remove && i >= 0:
			rows[i].Pending = "add"
		case !p.Remove:
			rows = append(rows, ui.EntityRow{Name: p.Name, Detail: "not saved yet", Source: rel("", p.File), Pending: "add"})
		}
	}
	return rows
}

// describe says what an unsaved change does, for the pending list.
func describe(p gorun.Change) string {
	verb := "Add"
	if p.Remove {
		verb = "Remove"
	}
	return fmt.Sprintf("%s %s %s", verb, kindNames[p.Kind], p.Name)
}

// present reports whether kind and name exist once the unsaved changes are applied.
func present(cfg *gorun.Config, pending []gorun.Change, kind, name string) bool {
	in := inFile(cfg, kind, name)
	for _, p := range pending {
		if p.Kind == kind && p.Name == name {
			in = !p.Remove
		}
	}
	return in
}

// inFile reports whether the loaded file defines kind and name.
func inFile(cfg *gorun.Config, kind, name string) bool {
	switch kind {
	case kindHost:
		return slices.ContainsFunc(cfg.Servers, func(s gorun.Server) bool { return s.Name == name })
	case kindGroup:
		_, ok := cfg.Groups[name]
		return ok
	case kindTask:
		_, ok := cfg.Tasks[name]
		return ok
	case kindBundle:
		_, ok := cfg.Bundles[name]
		return ok
	}
	return false
}

// sourceOf is the file that defines kind and name: the file of its last unsaved add, or the
// file it was loaded from.
func sourceOf(cfg *gorun.Config, pending []gorun.Change, kind, name string) string {
	for i := len(pending) - 1; i >= 0; i-- {
		if p := pending[i]; p.Kind == kind && p.Name == name && !p.Remove {
			return p.File
		}
	}
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
	if a.cfg == nil {
		redirectWith(w, r, "/config", "Load a Gorunfile first.", true)
		return
	}
	if err := validName(name); err != nil {
		redirectWith(w, r, "/config", err.Error(), true)
		return
	}
	if present(a.cfg, a.pending, kind, name) {
		redirectWith(w, r, "/config", fmt.Sprintf("A %s named %s already exists.", kindNames[kind], name), true)
		return
	}
	file := r.PostFormValue("file")
	if !slices.Contains(a.cfg.Files, file) {
		redirectWith(w, r, "/config", "Pick the file to save into.", true)
		return
	}
	change, err := newChange(kind, name, file, r.PostForm)
	if err != nil {
		redirectWith(w, r, "/config", err.Error(), true)
		return
	}
	a.pending = append(a.pending, change)
	redirectWith(w, r, "/config", describe(change)+". Save to write it to the file.", false)
}

// removeHandler queues the removal of a server, group, task or bundle.
func (a *app) removeHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/config", "Could not read the form.", true)
		return
	}
	kind := r.PostFormValue("kind")
	name := r.PostFormValue("name")

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil {
		redirectWith(w, r, "/config", "Load a Gorunfile first.", true)
		return
	}
	if !present(a.cfg, a.pending, kind, name) {
		redirectWith(w, r, "/config", fmt.Sprintf("No %s named %s to remove.", kindNames[kind], name), true)
		return
	}
	change := gorun.Change{Kind: kind, Name: name, File: sourceOf(a.cfg, a.pending, kind, name), Remove: true}
	a.pending = append(a.pending, change)
	redirectWith(w, r, "/config", describe(change)+". Save to write it to the file.", false)
}

// saveHandler writes the unsaved changes into their files, then reloads the file to check it.
func (a *app) saveHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	pending, path := slices.Clone(a.pending), a.cfgPath
	a.mu.Unlock()
	if len(pending) == 0 {
		redirectWith(w, r, "/config", "Nothing to save.", true)
		return
	}
	if err := gorun.SaveChanges(pending); err != nil {
		redirectWith(w, r, "/config", "Could not save: "+err.Error(), true)
		return
	}
	a.mu.Lock()
	a.pending = nil
	a.mu.Unlock()
	if err := a.loadConfig(path); err != nil {
		redirectWith(w, r, "/config", "Saved, but the file no longer loads: "+err.Error(), true)
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

// newChange builds the change an add form asks for.
func newChange(kind, name, file string, form url.Values) (gorun.Change, error) {
	body := map[string]any{}
	switch kind {
	case kindHost:
		setIf(body, "hostname", form.Get("hostname"))
		setList(body, "groups", form.Get("groups"))
		setList(body, "tags", form.Get("tags"))
		setSSH(body, form)
	case kindGroup:
		setList(body, "tags", form.Get("tags"))
		setList(body, "tasks", form.Get("tasks"))
		setSSH(body, form)
	case kindTask:
		cmd := strings.TrimSpace(form.Get("cmd"))
		if cmd == "" {
			return gorun.Change{}, errors.New("a task needs a command")
		}
		body["cmd"] = cmd
		setIf(body, "description", form.Get("description"))
		if form.Get("sudo") != "" {
			body["sudo"] = true
		}
		if form.Get("local") != "" {
			body["local"] = true
		}
	case kindBundle:
		setList(body, "tasks", form.Get("tasks"))
		if _, ok := body["tasks"]; !ok {
			return gorun.Change{}, errors.New("a bundle needs at least one task")
		}
		setIf(body, "description", form.Get("description"))
	default:
		return gorun.Change{}, fmt.Errorf("unknown kind %q", kind)
	}
	return gorun.Change{Kind: kind, Name: name, File: file, Body: body}, nil
}

// setSSH adds the SSH settings a form gave, if any.
func setSSH(body map[string]any, form url.Values) {
	ssh := map[string]any{}
	setIf(ssh, "username", form.Get("ssh_user"))
	setIf(ssh, "identityfile", form.Get("identityfile"))
	if len(ssh) > 0 {
		body["ssh"] = ssh
	}
}

// setIf sets key to value when the value is not blank.
func setIf(body map[string]any, key, value string) {
	if v := strings.TrimSpace(value); v != "" {
		body[key] = v
	}
}

// setList sets key to the comma separated items in value, when there are any.
func setList(body map[string]any, key, value string) {
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	if len(items) > 0 {
		body[key] = items
	}
}
