package websrv

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// tasksPage lists every task of the loaded file with its settings, like the terminal's task list.
func (a *app) tasksPage(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	d := ui.TasksPageData{}
	d.Notice, d.Error = notes(r)
	if cfg != nil {
		d.Loaded, d.Path = true, cfg.Path
		d.Tasks = taskCards(cfg, a.taskAverages(r.Context()))
		d.Files = fileOptions(cfg)
		if name := r.URL.Query().Get("edit"); name != "" {
			a.mu.Lock()
			pending := slices.Clone(a.pending)
			a.mu.Unlock()
			if fields, msg := editPanel(cfg, pending, kindTask, name); msg != "" {
				d.Error = msg
			} else {
				d.EditingName, d.Edit = name, fields
			}
		}
	}
	a.page(w, "Tasks", "tasks", ui.TasksPage(d))
}

// notes reads the message a form action left on the page: an error, or a confirmation.
func notes(r *http.Request) (notice, errMsg string) {
	msg, bad := flashFrom(r)
	if bad {
		return "", msg
	}
	return msg, r.URL.Query().Get("error")
}

// bundlesPage lists every bundle of the loaded file and the tasks it expands to.
func (a *app) bundlesPage(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	d := ui.BundlesPageData{}
	d.Notice, d.Error = notes(r)
	if cfg != nil {
		d.Loaded, d.Path = true, cfg.Path
		d.Bundles = bundleCards(cfg)
		d.Files = fileOptions(cfg)
		d.Targets = targetNames(cfg)
		if name := r.URL.Query().Get("edit"); name != "" {
			a.mu.Lock()
			pending := slices.Clone(a.pending)
			a.mu.Unlock()
			if fields, msg := editPanel(cfg, pending, kindBundle, name); msg != "" {
				d.Error = msg
			} else {
				d.EditingName, d.Edit = name, fields
			}
		}
	}
	a.page(w, "Bundles", "bundles", ui.BundlesPage(d))
}

// taskAverages is each task's average duration over the last finished runs.
func (a *app) taskAverages(ctx context.Context) map[string]time.Duration {
	out := map[string]time.Duration{}
	if a.store == nil {
		return out
	}
	runs, err := a.runList(ctx)
	if err != nil {
		return out
	}
	total := map[string]time.Duration{}
	count := map[string]int{}
	seen := 0
	for _, r := range runs {
		if !r.Finished {
			continue
		}
		if seen++; seen > 10 {
			break
		}
		d, err := a.store.GetRun(ctx, r.ID)
		if err != nil || d == nil {
			continue
		}
		for _, t := range d.Tasks {
			if t.Duration > 0 {
				total[t.Name] += t.Duration
				count[t.Name]++
			}
		}
	}
	for n, c := range count {
		out[n] = total[n] / time.Duration(c)
	}
	return out
}

// taskCards describes the tasks of cfg, slowest first by their average time in past runs; tasks
// never run come last, by name.
func taskCards(cfg *gorun.Config, avg map[string]time.Duration) []ui.TaskCard {
	pipes, _ := cfg.ServersPipeline()
	runsOn := map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(pipes)) {
		for _, t := range pipes[name].Tasks {
			runsOn[t.Name] = append(runsOn[t.Name], name)
		}
	}
	inBundles := map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(cfg.Bundles)) {
		tasks, err := cfg.ExpandTaskRefs(gorun.BundlePrefix + name)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			inBundles[t] = append(inBundles[t], gorun.BundlePrefix+name)
		}
	}

	cards := make([]ui.TaskCard, 0, len(cfg.Tasks))
	for _, name := range slices.Sorted(maps.Keys(cfg.Tasks)) {
		t := cfg.Tasks[name]
		c := ui.TaskCard{
			Name:        name,
			Description: t.Description,
			Details:     t.Details,
			Source:      rel(cfg.Path, t.Source),
			Badges:      taskBadges(t),
			Average:     avg[name],
			Options:     executionOptions(t),
			Copies:      t.CopySummaries(),
			Check:       t.Check,
			CheckCopies: t.CheckCopies,
			Verify:      t.Verify,
			DependsOn:   t.DependsOn,
			InBundles:   inBundles[name],
			RunsOn:      runsOn[name],
			ConfigVars:  maps.Clone(cfg.Vars),
			Vars:        t.Vars,
			EnvVars:     map[string]string{},
			Files:       taskFiles(t, filepath.Dir(cfg.Path)),
		}
		if c.Description == name {
			c.Description = "" // a description that only repeats the name adds nothing
		}
		for k, v := range cfg.EnvVars {
			c.EnvVars[strings.ToUpper(k)] = v
		}
		for k, v := range t.EnvVars {
			c.EnvVars[strings.ToUpper(k)] = v
		}
		c.CommandSource, c.Command = taskCommand(t, filepath.Dir(cfg.Path))
		cards = append(cards, c)
	}
	slices.SortStableFunc(cards, func(x, y ui.TaskCard) int {
		switch {
		case x.Average != y.Average:
			if x.Average > y.Average {
				return -1
			}
			return 1
		default:
			return strings.Compare(x.Name, y.Name)
		}
	})
	return cards
}

// taskBadges are the short flags worth seeing without opening a task.
func taskBadges(t gorun.Task) []string {
	var b []string
	if t.Sudo {
		b = append(b, "sudo")
	}
	if t.Local {
		b = append(b, "local only")
	}
	if t.Check != "" || t.CheckCopies {
		b = append(b, "check")
	}
	if t.Verify != "" {
		b = append(b, "verify")
	}
	if t.Rerun == gorun.RerunUnsafe {
		b = append(b, "not safe to rerun")
	}
	if t.Retries > 0 {
		b = append(b, fmt.Sprintf("retries ×%d", t.Retries))
	}
	if len(t.DependsOn) > 0 {
		b = append(b, "depends on "+strings.Join(t.DependsOn, ", "))
	}
	return b
}

// taskCommand is what a task runs and where it comes from. A command file is read here, so the
// page shows the script and not only its path.
func taskCommand(t gorun.Task, dir string) (source, command string) {
	file := func(kind, path string) (string, string) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return kind + " · " + path, "unable to read " + path + ": " + err.Error()
		}
		return kind + " · " + path, string(data)
	}
	switch {
	case t.CmdTemplate != "":
		return "cmd_template", t.CmdTemplate
	case t.CmdTemplateFile != "":
		return file("cmd_template_file", t.CmdTemplateFile)
	case t.Cmd != "":
		return "cmd", t.Cmd
	case t.CmdFile != "":
		return file("cmd_file", t.CmdFile)
	}
	return "none", "(no command configured; this task may only copy files)"
}

// executionOptions lists a task's execution settings, defaults marked, as the terminal does.
func executionOptions(t gorun.Task) []ui.Option {
	def := func(set bool, v string) string {
		if set {
			return v
		}
		return v + " (default)"
	}
	or := func(v, empty string) string {
		if v == "" {
			return empty
		}
		return v
	}
	yes := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	attempts := t.Attempts()
	noun := "attempts"
	if attempts == 1 {
		noun = "attempt"
	}
	copies := len(t.Copy) + len(t.Copies) + len(t.Upload) + len(t.Files)
	return []ui.Option{
		{Name: "Timeout", Value: def(t.Timeout > 0, t.TimeoutDuration().String())},
		{Name: "Retries", Value: fmt.Sprintf("%d extra · %d %s", t.Retries, attempts, noun)},
		{Name: "Retry delay", Value: def(t.RetryDelay != nil, t.RetryDelayDuration().String())},
		{Name: "Rerun", Value: or(t.Rerun, "not specified")},
		{Name: "Retry unsafe", Value: yes(t.RetryUnsafe)},
		{Name: "Shell", Value: or(t.Shell, "default")},
		{Name: "Sudo", Value: yes(t.Sudo)},
		{Name: "Local only", Value: yes(t.Local)},
		{Name: "Log level", Value: or(t.LogLevel, "inherit")},
		{Name: "File copies", Value: fmt.Sprint(copies)},
		{Name: "Check", Value: or(t.Check, "none")},
		{Name: "Check copies", Value: yes(t.CheckCopies)},
		{Name: "Verify", Value: or(t.Verify, "none")},
	}
}

// taskFiles lists the local files a task reads: its command file or template and the sources
// of its copies. Relative paths are resolved against dir, as a run resolves them.
func taskFiles(t gorun.Task, dir string) []ui.FileRef {
	var files []ui.FileRef
	add := func(kind, path string) {
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		files = append(files, ui.FileRef{Type: kind, Path: path})
	}
	add("cmd_file", t.CmdFile)
	add("cmd_template_file", t.CmdTemplateFile)
	for _, c := range slices.Concat(t.Copy, t.Copies, t.Upload, t.Files) {
		dst := cmp.Or(c.Dst, c.DstPath, c.Dest, c.Dir)
		add("template → "+dst, cmp.Or(c.SrcTpl, c.Template, c.SrcTmpl))
		add("copy → "+dst, cmp.Or(c.Src, c.SrcFile))
	}
	return files
}

// bundleCards describes the bundles of cfg.
func bundleCards(cfg *gorun.Config) []ui.BundleCard {
	usedBy := map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(cfg.Bundles)) {
		for _, ref := range cfg.Bundles[name].Tasks {
			if n, ok := strings.CutPrefix(ref, gorun.BundlePrefix); ok {
				usedBy[n] = append(usedBy[n], gorun.BundlePrefix+name)
			}
		}
	}
	cards := make([]ui.BundleCard, 0, len(cfg.Bundles))
	for _, name := range slices.Sorted(maps.Keys(cfg.Bundles)) {
		b := cfg.Bundles[name]
		c := ui.BundleCard{
			Name:        name,
			Description: b.Description,
			Source:      rel(cfg.Path, b.Source),
			Vars:        b.Vars,
			UsedBy:      usedBy[name],
		}
		for _, ref := range b.Tasks {
			item := ui.BundleItem{Ref: ref}
			if n, ok := strings.CutPrefix(ref, gorun.BundlePrefix); ok {
				item.IsBundle, item.Description = true, cfg.Bundles[n].Description
			} else {
				item.Description = cfg.Tasks[ref].Description
			}
			c.Items = append(c.Items, item)
		}
		c.Expanded, _ = cfg.ExpandTaskRefs(gorun.BundlePrefix + name)
		cards = append(cards, c)
	}
	return cards
}

// bundleOptions lists the bundles for the run page picker; want is the one to tick.
func bundleOptions(cfg *gorun.Config, want string) []ui.BundleOption {
	names := slices.Sorted(maps.Keys(cfg.Bundles))
	out := make([]ui.BundleOption, 0, len(names))
	for _, n := range names {
		tasks, _ := cfg.ExpandTaskRefs(gorun.BundlePrefix + n)
		out = append(out, ui.BundleOption{Name: n, Description: cfg.Bundles[n].Description, Tasks: len(tasks), Selected: n == want})
	}
	return out
}
