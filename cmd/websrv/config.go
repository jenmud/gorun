package websrv

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// The entity kinds, as the forms and the saved changes name them.
const (
	kindHost     = "host"
	kindGroup    = "group"
	kindTask     = "task"
	kindBundle   = "bundle"
	kindSchedule = gorun.KindSchedule
)

// kindNames is how each kind reads in messages.
var kindNames = map[string]string{
	kindHost:     "server",
	kindGroup:    "server group",
	kindTask:     "task",
	kindBundle:   "bundle",
	kindSchedule: "schedule",
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
	a.startScheduler(cfg)
	return nil
}

// startScheduler replaces the running scheduler with one for cfg's schedules. The web
// server is the long-running host, so this is the only place a scheduler is started.
func (a *app) startScheduler(cfg *gorun.Config) {
	old := a.schedulerStatus()
	a.stopScheduler()
	if len(cfg.Schedules) == 0 {
		return
	}
	// Without NATS nothing carries a scheduled run's events to the dashboard and history, so
	// the web service records them itself. With NATS its subscriber already does, and recording
	// here too would save every event twice.
	if a.nc == nil {
		cfg.Recorder = a
	}
	cfg.Origin.Version = Version
	s := gorun.NewScheduler(cfg, func(ev gorun.ScheduleEvent) {
		slog.Info("schedule", "schedule", ev.Schedule, "kind", ev.Kind, "at", ev.At, "reason", ev.Reason)
		a.recordScheduleEvent(ev)
	})
	if err := s.Start(context.Background()); err != nil {
		log.Printf("gorun web: scheduler: %v", err)
		return
	}
	// a reload keeps the pauses and skips set on schedules that still exist
	for _, st := range old {
		if st.Paused {
			_ = s.Pause(st.Name)
		}
		if st.SkipNext {
			_ = s.Skip(st.Name)
		}
	}
	a.mu.Lock()
	a.sched = s
	a.mu.Unlock()
}

// schedulerStatus is the running scheduler's status, or nil when none is running.
func (a *app) schedulerStatus() []gorun.ScheduleStatus {
	a.mu.Lock()
	s := a.sched
	a.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.Status()
}

// stopScheduler stops the running scheduler, if there is one.
func (a *app) stopScheduler() {
	a.mu.Lock()
	s := a.sched
	a.sched = nil
	a.mu.Unlock()
	if s != nil {
		s.Stop()
	}
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
	d.NATS = a.natsStatus()
	d.Flash, d.FlashBad = msg, bad
	d.Browser = browse(pickerDir(r.URL.Query().Get("dir"), d.Path))
	a.page(w, "Config", "config", ui.ConfigPage(d))
}

// editPage shows the config page with the edit form for one entity, filled with its current values.
func (a *app) editPage(w http.ResponseWriter, r *http.Request) {
	kind, name := r.URL.Query().Get("kind"), r.URL.Query().Get("name")

	a.mu.Lock()
	cfg := a.cfg
	pending := slices.Clone(a.pending)
	a.mu.Unlock()
	if cfg == nil {
		redirectWith(w, r, "/config", "Load a Gorunfile first.", true)
		return
	}
	body, err := currentBody(cfg, pending, kind, name)
	if err != nil {
		redirectWith(w, r, "/config", err.Error(), true)
		return
	}

	d := configData(cfg, pending)
	d.NATS = a.natsStatus()
	d.Browser = browse(pickerDir("", d.Path))
	d.EditingKind, d.EditingName = kind, name
	d.Edit = editFields(body)
	a.page(w, "Config", "config", ui.ConfigPage(d))
}

// entityPath is where each kind's tables live in a Gorunfile.
var entityPath = map[string][]string{
	kindHost:     {"servers", "hosts"},
	kindGroup:    {"server_groups"},
	kindTask:     {"tasks"},
	kindBundle:   {"bundles"},
	kindSchedule: {"schedule"},
}

// currentBody is the entity's table as written: its latest unsaved add, else the table in the
// file that defines it. Edits start from it so settings the form doesn't show are kept.
func currentBody(cfg *gorun.Config, pending []gorun.Change, kind, name string) (map[string]any, error) {
	if _, ok := entityPath[kind]; !ok {
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	if !present(cfg, pending, kind, name) {
		return nil, fmt.Errorf("no %s named %s to edit", kindNames[kind], name)
	}
	for i := len(pending) - 1; i >= 0; i-- {
		if p := pending[i]; p.Kind == kind && p.Name == name && !p.Remove {
			return cloneMap(p.Body), nil
		}
	}
	file := sourceOf(cfg, pending, kind, name)
	if kind == kindSchedule {
		return scheduleBodyIn(file, name)
	}
	var doc map[string]any
	if _, err := toml.DecodeFile(file, &doc); err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	var node any = doc
	for _, key := range append(slices.Clone(entityPath[kind]), name) {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s %s is not a table in %s, so it can't be edited here", kindNames[kind], name, file)
		}
		node = m[key]
	}
	body, ok := node.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s %s is not a table in %s, so it can't be edited here", kindNames[kind], name, file)
	}
	return body, nil
}

// cloneMap copies m and every table inside it, so edits never touch a queued change's body.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			v = cloneMap(sub)
		}
		out[k] = v
	}
	return out
}

// editFields reads the values the edit form shows from an entity's table.
func editFields(body map[string]any) ui.EditFields {
	list := func(k string) string {
		var items []string
		switch v := body[k].(type) {
		case []any:
			for _, it := range v {
				items = append(items, fmt.Sprint(it))
			}
		case []string:
			items = v
		}
		return strings.Join(items, ", ")
	}
	ssh, _ := body["ssh"].(map[string]any)
	sudo, _ := body["sudo"].(bool)
	local, _ := body["local"].(bool)
	checkCopies, _ := body["check_copies"].(bool)
	copies, inline := copyLines(body)
	retries := ""
	switch n := body["retries"].(type) {
	case int64:
		retries = strconv.FormatInt(n, 10)
	case int:
		retries = strconv.Itoa(n)
	}
	return ui.EditFields{
		Hostname:     str(body, "hostname"),
		Groups:       list("groups"),
		Tags:         list("tags"),
		SSHUser:      str(ssh, "username"),
		IdentityFile: str(ssh, "identityfile"),
		Cmd:          str(body, "cmd"),
		Check:        str(body, "check"),
		Verify:       str(body, "verify"),
		CheckCopies:  checkCopies,
		Copies:       copies,
		InlineCopies: inline,
		DependsOn:    list("depends_on"),
		Timeout:      str(body, "timeout"),
		Retries:      retries,
		Description:  str(body, "description"),
		Tasks:        list("tasks"),
		Sudo:         sudo,
		Local:        local,
		Target:       str(body, "target"),
		Cron:         str(body, "cron"),
		At:           atForPicker(str(body, "at"), zoneOr(str(body, "timezone"))),
		Timezone:     zoneOr(str(body, "timezone")),
		Missed:       str(body, "missed"),
	}
}

// zoneOr is a schedule's zone, or UTC when it has none.
func zoneOr(zone string) string {
	if zone == "" {
		return "UTC"
	}
	return zone
}

// applyEdit writes the edit form's fields over body. A cleared field removes the key.
func applyEdit(kind string, body map[string]any, form url.Values) error {
	put := func(m map[string]any, k, v string) {
		if v = strings.TrimSpace(v); v == "" {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	putList := func(k string) {
		delete(body, k)
		setList(body, k, form.Get(k))
	}
	putFlag := func(k string) {
		if form.Get(k) != "" {
			body[k] = true
		} else {
			delete(body, k)
		}
	}
	putSSH := func() {
		ssh, _ := body["ssh"].(map[string]any)
		if ssh == nil {
			ssh = map[string]any{}
		}
		put(ssh, "username", form.Get("ssh_user"))
		put(ssh, "identityfile", form.Get("identityfile"))
		if len(ssh) == 0 {
			delete(body, "ssh")
		} else {
			body["ssh"] = ssh
		}
	}
	switch kind {
	case kindHost:
		put(body, "hostname", form.Get("hostname"))
		putList("groups")
		putList("tags")
		putSSH()
	case kindGroup:
		putList("tags")
		putList("tasks")
		putSSH()
	case kindTask:
		put(body, "cmd", form.Get("cmd"))
		put(body, "check", form.Get("check"))
		put(body, "verify", form.Get("verify"))
		put(body, "description", form.Get("description"))
		putFlag("sudo")
		putFlag("local")
		putFlag("check_copies")
		if err := applyCopies(body, form); err != nil {
			return err
		}
		if err := applyRunSettings(body, form); err != nil {
			return err
		}
	case kindBundle:
		putList("tasks")
		if _, ok := body["tasks"]; !ok {
			return errors.New("a bundle needs at least one task")
		}
		put(body, "description", form.Get("description"))
	case kindSchedule:
		return applyScheduleEdit(body, form)
	}
	return nil
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
	for i := 0; i < len(pending); i++ {
		p := pending[i]
		if p.Remove && i+1 < len(pending) && isEdit(p, pending[i+1]) {
			d.Pending = append(d.Pending, fmt.Sprintf("Edit %s %s", kindNames[p.Kind], p.Name))
			i++
			continue
		}
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
	d.Schedules = entityRows(kindSchedule, scheduleEntityRows(cfg, root), pending)
	d.Targets = targetNames(cfg)
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
		case !p.Remove && i >= 0 && rows[i].Pending == "remove":
			rows[i].Pending = "edit"
		case !p.Remove && i >= 0:
			rows[i].Pending = "add"
		case !p.Remove:
			rows = append(rows, ui.EntityRow{Name: p.Name, Detail: "not saved yet", Source: rel("", p.File), Pending: "add"})
		}
	}
	return rows
}

// isEdit reports whether rm and add are the pair an edit queues.
func isEdit(rm, add gorun.Change) bool {
	return !add.Remove && rm.Kind == add.Kind && rm.Name == add.Name && rm.File == add.File
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
	case kindSchedule:
		return slices.ContainsFunc(cfg.Schedules, func(s gorun.Schedule) bool { return s.Name == name })
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
	case kindSchedule:
		for _, s := range cfg.Schedules {
			if s.Name == name {
				src = s.Source
			}
		}
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
	if err := validName(kind, name); err != nil {
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
	if err == nil {
		err = a.checkChange(change)
	}
	if err != nil {
		redirectWith(w, r, "/config", err.Error(), true)
		return
	}
	a.pending = append(a.pending, change)
	redirectWith(w, r, "/config", describe(change)+". Save to write it to the file.", false)
}

// editHandler queues an edit: the entity is removed and written back with the form's changes,
// in the file that defines it.
func (a *app) editHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/config", "Could not read the form.", true)
		return
	}
	kind, name := r.PostFormValue("kind"), r.PostFormValue("name")

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil {
		redirectWith(w, r, "/config", "Load a Gorunfile first.", true)
		return
	}
	body, err := currentBody(a.cfg, a.pending, kind, name)
	if err != nil {
		redirectWith(w, r, "/config", err.Error(), true)
		return
	}
	if err := applyEdit(kind, body, r.PostForm); err != nil {
		redirectWith(w, r, "/config", err.Error(), true)
		return
	}
	if err := a.checkChange(gorun.Change{Kind: kind, Name: name, Body: body}); err != nil {
		redirectWith(w, r, "/config", err.Error(), true)
		return
	}
	file := sourceOf(a.cfg, a.pending, kind, name)
	a.pending = append(a.pending,
		gorun.Change{Kind: kind, Name: name, File: file, Remove: true},
		gorun.Change{Kind: kind, Name: name, File: file, Body: body},
	)
	redirectWith(w, r, "/config", fmt.Sprintf("Edited %s %s. Save to write it to the file.", kindNames[kind], name), false)
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
	a.saveMu.Lock()
	note, err := a.addLocalServerIfNeeded(pending)
	a.saveMu.Unlock()
	if err != nil {
		redirectWith(w, r, "/config", fmt.Sprintf("Saved %d change(s), but %v", len(pending), err), true)
		return
	}
	redirectWith(w, r, "/config", fmt.Sprintf("Saved %d change(s).", len(pending))+note, false)
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
func validName(kind, name string) error {
	if name == "" {
		return errors.New("give it a name")
	}
	// a schedule's name is a quoted value, not a table key, so it may have spaces and dots
	if kind == kindSchedule {
		if strings.ContainsAny(name, "\"'\n\r\t") {
			return fmt.Errorf("%s has characters a schedule name can't use: quotes, tabs or line breaks", name)
		}
		return nil
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
		setIf(body, "check", form.Get("check"))
		setIf(body, "verify", form.Get("verify"))
		setIf(body, "description", form.Get("description"))
		if form.Get("sudo") != "" {
			body["sudo"] = true
		}
		if form.Get("check_copies") != "" {
			body["check_copies"] = true
		}
		copies, err := parseCopyLines(form.Get("copies"))
		if err != nil {
			return gorun.Change{}, err
		}
		if len(copies) > 0 {
			body["copy"] = copies
		}
		if err := applyRunSettings(body, form); err != nil {
			return gorun.Change{}, err
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
	case kindSchedule:
		var err error
		if body, err = newScheduleBody(form); err != nil {
			return gorun.Change{}, err
		}
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

// copyKeys are the keys a task's copies can be listed under.
var copyKeys = []string{"copy", "copies", "upload", "files"}

// copyEntries reads one of a task's copy lists, however the TOML decoder typed it.
func copyEntries(v any) []map[string]any {
	switch list := v.(type) {
	case []map[string]any:
		return list
	case []any:
		var out []map[string]any
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// copyLines renders a task's file copies as the form's lines: "src -> dst", or "template src
// -> dst" for a file filled in as a template (render = true). Inline-text and raw copies can't
// be written that way, so they are counted instead, and the form leaves them alone.
func copyLines(body map[string]any) (lines string, inline int) {
	var out []string
	for _, key := range copyKeys {
		for _, c := range copyEntries(body[key]) {
			if keptCopy(c) {
				inline++
				continue
			}
			dst := cmp.Or(str(c, "dst"), str(c, "dst_path"), str(c, "dest"))
			src := cmp.Or(str(c, "src"), str(c, "src_file"))
			if dst == "" && str(c, "dir") != "" && src != "" {
				dst = path.Join(str(c, "dir"), filepath.Base(src))
			}
			if render, _ := c["render"].(bool); render {
				out = append(out, "template "+src+" -> "+dst+copyOptionWords(c))
			} else {
				out = append(out, src+" -> "+dst+copyOptionWords(c))
			}
		}
	}
	return strings.Join(out, "\n"), inline
}

// copyOptionWords writes a copy's mode, owner and group as the form's trailing words, such as
// "mode=0640 owner=deploy", or "" when the copy sets none.
func copyOptionWords(c map[string]any) string {
	var words []string
	if mode := cmp.Or(str(c, "mode"), str(c, "perm")); mode != "" {
		words = append(words, "mode="+mode) // perm is shown as mode
	}
	for _, key := range []string{"owner", "group"} {
		if v := str(c, key); v != "" {
			words = append(words, key+"="+v)
		}
	}
	if len(words) == 0 {
		return ""
	}
	return " " + strings.Join(words, " ")
}

// copyOptionKeys are the words that can follow a copy's destination on the form's line.
var copyOptionKeys = map[string]string{"mode": "mode", "perm": "mode", "owner": "owner", "group": "group"}

// parseCopyLines reads the form's copy lines: "local -> server", or "template local -> server"
// for a file to fill in as a template. A line can end with mode=0644, owner=name and group=name,
// in any order. Blank lines are skipped.
func parseCopyLines(text string) ([]map[string]any, error) {
	var out []map[string]any
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		src, dstPart, ok := strings.Cut(line, "->")
		src = strings.TrimSpace(src)
		if !ok || src == "" {
			return nil, fmt.Errorf("can't read the copy %q: write it as local/path -> /path/on/server", line)
		}
		// the options come last, after the destination; the rest of the line is the destination
		words := strings.Fields(dstPart)
		end := len(words)
		c := map[string]any{}
		for end > 0 {
			key, value, isOpt := strings.Cut(words[end-1], "=")
			field, known := copyOptionKeys[key]
			if !isOpt || !known || value == "" {
				break
			}
			c[field] = value
			end--
		}
		dst := strings.Join(words[:end], " ")
		if dst == "" {
			return nil, fmt.Errorf("can't read the copy %q: write it as local/path -> /path/on/server", line)
		}
		if mode, has := c["mode"].(string); has {
			if _, err := gorun.ParseMode(mode); err != nil {
				return nil, fmt.Errorf("can't read the copy %q: %w", line, err)
			}
		}
		c["dst"] = dst
		if tpl, isTpl := strings.CutPrefix(src, "template "); isTpl {
			c["src"], c["render"] = strings.TrimSpace(tpl), true
		} else {
			c["src"] = src
		}
		out = append(out, c)
	}
	return out, nil
}

// applyCopies replaces a task's file and template copies with the form's lines, keeping its
// inline-text copies. Nothing changes when the form has no copies field, or its lines are as the
// form showed them.
func applyCopies(body map[string]any, form url.Values) error {
	if !form.Has("copies") {
		return nil
	}
	text := strings.ReplaceAll(form.Get("copies"), "\r\n", "\n")
	if was, _ := copyLines(body); strings.TrimSpace(text) == strings.TrimSpace(was) {
		return nil
	}
	copies, err := parseCopyLines(text)
	if err != nil {
		return err
	}
	var kept []map[string]any
	for _, key := range copyKeys {
		for _, c := range copyEntries(body[key]) {
			if keptCopy(c) {
				kept = append(kept, c)
			}
		}
		delete(body, key)
	}
	if all := append(kept, copies...); len(all) > 0 {
		body["copy"] = all
	}
	return nil
}

// applyRunSettings writes the form's depends on, timeout and retries over body. A cleared field
// removes the key; a field the form doesn't have leaves it as it is.
func applyRunSettings(body map[string]any, form url.Values) error {
	if form.Has("depends_on") {
		delete(body, "depends_on")
		setList(body, "depends_on", form.Get("depends_on"))
	}
	if form.Has("timeout") {
		delete(body, "timeout")
		if t := strings.TrimSpace(form.Get("timeout")); t != "" {
			if _, err := time.ParseDuration(t); err != nil {
				return fmt.Errorf("timeout %q isn't a duration: write it like 90s, 5m or 1h", t)
			}
			body["timeout"] = t
		}
	}
	if form.Has("retries") {
		delete(body, "retries")
		if r := strings.TrimSpace(form.Get("retries")); r != "" && r != "0" {
			n, err := strconv.Atoi(r)
			if err != nil || n < 0 {
				return fmt.Errorf("retries %q isn't a number of extra tries, such as 2", r)
			}
			body["retries"] = int64(n)
		}
	}
	return nil
}

// str is m's string value at k, or "" when it has none.
func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// keptCopy reports whether a copy is one the form can't show as a line, and so keeps as it is:
// inline text, or a raw copy.
func keptCopy(c map[string]any) bool {
	raw, _ := c["raw"].(bool)
	return str(c, "content") != "" || raw
}
