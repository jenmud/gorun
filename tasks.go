package gorun

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"text/template"
	"time"
)

const (
	// DefaultTaskTimeout is how long a task may run when it doesn't set its own
	// timeout. It stops a hung command from holding up a run forever.
	DefaultTaskTimeout = 30 * time.Minute

	// DefaultRetryDelay is how long gorun waits between attempts when a task
	// sets retries but not retry_delay. A short pause gives transient failures
	// (a restarting service, a busy package mirror) a moment to settle.
	DefaultRetryDelay = 50 * time.Millisecond

	// taskKillGrace bounds how long gorun waits for output pipes to close after
	// a canceled task has been killed.
	taskKillGrace = 5 * time.Second

	// RerunSafe and RerunUnsafe are the accepted values of Task.Rerun. An
	// empty value means the author has not said whether rerunning is safe.
	RerunSafe   = "safe"
	RerunUnsafe = "unsafe"
)

// Copy is one file copy of a task. A source is Src (a file or directory) or Content (inline
// text); the target is Dst, or Dir to keep the source's name. Dst, Dir and Content are
// templates, filled in for each server; a source file is copied as it is unless Render is set.
// Raw turns templating off for the copy. The other fields are alternative spellings.
type Copy struct {
	Src     string `toml:"src"`
	SrcFile string `toml:"src_file,omitempty"`
	Content string `toml:"content,omitempty"`
	Dst     string `toml:"dst"`
	DstPath string `toml:"dst_path,omitempty"`
	Dir     string `toml:"dir,omitempty"`
	Dest    string `toml:"dest,omitempty"`
	// Mode is the permission bits the copied files get, as octal, such as "0644". Without it a
	// file keeps its local permissions. Perm is another spelling of Mode.
	Mode string `toml:"mode,omitempty"`
	Perm string `toml:"perm,omitempty"`
	// Owner and Group are who the copied files belong to, as chown sets them. A copy keeps the
	// login user's ownership without them. Setting them needs root: the task's sudo = true runs
	// the chown under sudo.
	Owner string `toml:"owner,omitempty"`
	Group string `toml:"group,omitempty"`
	// Render fills in the source file as a template for each server before copying it. A file
	// is copied as it is without it: it may be a binary, an archive or another tool's template.
	Render bool `toml:"render,omitempty"`
	// Raw copies the copy's dst, dir and content exactly as written, with no templating.
	Raw bool `toml:"raw,omitempty"`
}

// target is where the copy goes on the server: Dst, or Dir with the source's name.
func (c Copy) target() string {
	if c.Dst == "" && c.Dir != "" && c.Src != "" {
		return filepath.Join(c.Dir, filepath.Base(c.Src))
	}
	return c.Dst
}

// renders reports whether the copy's source file is filled in as a template before it is sent:
// it asks to be, and neither it nor its task is raw.
func (c Copy) renders(task *Task) bool {
	return c.Render && !c.Raw && !task.Raw && c.Src != ""
}

// normalized returns the copy with its alternative keys folded into Src and Dst.
func (c Copy) normalized() Copy {
	c.Src = cmp.Or(c.Src, c.SrcFile)
	c.Dst = cmp.Or(c.Dst, c.DstPath, c.Dest)
	return c
}

// Task is a task to execute on one or more servers.
type Task struct {
	LogLevel  string            `toml:"log_level"`
	Name      string            `toml:"-"` // the <name> in [tasks.<name>]
	Source    string            `toml:"-"` // the file that defines it
	Sudo      bool              `toml:"sudo"`
	Cmd       string            `toml:"cmd,omitempty"`
	CmdFile   string            `toml:"cmd_file,omitempty"`
	Copy      []Copy            `toml:"copy,omitempty"`
	Copies    []Copy            `toml:"copies,omitempty"`
	Upload    []Copy            `toml:"upload,omitempty"`
	Files     []Copy            `toml:"files,omitempty"`
	EnvVars   map[string]string `toml:"envvars"`
	Vars      map[string]any    `toml:"vars,omitempty"`
	DependsOn []string          `toml:"depends_on"`
	Shell     string            `toml:"shell"`
	Timeout   time.Duration     `toml:"timeout,omitempty"`

	// Description is a one-line summary shown in task lists and the TUI.
	Description string `toml:"description,omitempty"`
	// Details is an optional longer explanation shown when a task is expanded.
	Details string `toml:"details,omitempty"`
	// Rerun is RerunSafe or RerunUnsafe. Empty means unspecified.
	Rerun string `toml:"rerun,omitempty"`
	// Local, when true, runs the task only on local servers and skips it on any
	// server that connects over SSH. Tasks without it run only over SSH.
	Local bool `toml:"local,omitempty"`
	// Retries is how many extra times to run a failed task. Zero means run once.
	Retries int `toml:"retries,omitempty"`
	// RetryDelay is how long to wait before each retry. Unset uses
	// DefaultRetryDelay; an explicit zero retries immediately.
	RetryDelay *time.Duration `toml:"retry_delay,omitempty"`
	// RetryUnsafe allows automatic retries for a task marked rerun = "unsafe".
	RetryUnsafe bool `toml:"retry_unsafe,omitempty"`
	// Check runs before the task, to see whether there is anything to do. Exit 0 means no: the
	// task is marked unchanged, and its copies, command and verify don't run. Any other exit
	// code means yes, so the task runs. A check that can't run at all (or times out) fails the
	// task. Config.Force skips the check.
	Check string `toml:"check,omitempty"`
	// CheckCopies compares the MD5 of each file the task copies with the file on the server.
	// When they all match, and Check (if set) exits 0 too, the task is unchanged.
	CheckCopies bool `toml:"check_copies,omitempty"`
	// Verify runs after the task's command succeeds, to confirm the change took: a non-zero
	// exit fails the task.
	Verify string `toml:"verify,omitempty"`
	// Raw turns templating off for the task: its command (cmd, or the contents of cmd_file),
	// check, verify, envvars, vars and copies are used exactly as written. Without it every one
	// of them is a template, filled in for each server.
	Raw bool `toml:"raw,omitempty"`

	// Rendered is set by ForServer once it has filled in the task's templates, so nothing fills
	// them in twice (a value may contain {{ once filled in). It isn't read from a Gorunfile.
	Rendered bool `toml:"-"`
}

// Attempts returns how many times the task may run, including the first try.
func (t Task) Attempts() int {
	if t.Retries < 0 {
		return 1
	}
	return t.Retries + 1
}

// CanRetry reports whether a failed attempt may be retried. Tasks marked unsafe
// to rerun require an explicit RetryUnsafe opt-in.
func (t Task) CanRetry() bool {
	return t.Rerun != RerunUnsafe || t.RetryUnsafe
}

// RetryDelayDuration returns the wait between attempts. A task that doesn't set
// retry_delay uses DefaultRetryDelay.
func (t Task) RetryDelayDuration() time.Duration {
	if t.RetryDelay == nil {
		return DefaultRetryDelay
	}
	return *t.RetryDelay
}

// TimeoutDuration returns the timeout to apply to the task. A zero (or
// negative) Timeout uses DefaultTaskTimeout.
func (t Task) TimeoutDuration() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}

	return DefaultTaskTimeout
}

// copies returns every copy of the task, from all four spellings of the key, normalized.
func (t Task) copies() []Copy {
	var out []Copy
	for _, list := range [][]Copy{t.Copy, t.Copies, t.Upload, t.Files} {
		for _, c := range list {
			out = append(out, c.normalized())
		}
	}
	return out
}

// CopySummaries describes the task's copies, one line each in the order they run: what is
// copied (a file or folder, a file filled in as a template, or inline text) and where it goes
// on the server, e.g. "scripts/ok.sh → /opt/app/ok.sh". Copies run before the command.
func (t Task) CopySummaries() []string {
	var out []string
	for _, c := range t.copies() {
		from := c.Src
		switch {
		case c.Content != "":
			from = fmt.Sprintf("inline text (%d bytes)", len(c.Content))
		case c.Render:
			from = "template " + c.Src
		}
		line := from + " → " + c.target()
		if o := c.options(); o != "" {
			line += " (" + o + ")"
		}
		out = append(out, line)
	}
	return out
}

// commandSources are the task's ways to give a command, of which at most one may be set.
func (t Task) commandSources() []string {
	return []string{t.Cmd, t.CmdFile}
}

// hasCheck reports whether the task checks whether there is anything to do before it runs.
func (t Task) hasCheck() bool {
	return t.Check != "" || t.CheckCopies
}

// templated reports whether the task's settings are still templates to fill in: it isn't raw,
// and ForServer hasn't filled them in already.
func (t Task) templated() bool {
	return !t.Raw && !t.Rendered
}

// ForServer returns the task with its templates filled in for server: its vars first (they see
// the server, but not the task's other settings), then its command (cmd, or the contents of
// cmd_file, which becomes cmd), check, verify, envvars, and each copy's dst, dir and content.
// Everything that runs or shows the task afterwards sees plain values. The task's own slices
// and maps are copied, never changed in place, since every server shares them. A raw task is
// returned as it is.
func (t Task) ForServer(server *Server) (Task, error) {
	for _, list := range []*[]Copy{&t.Copy, &t.Copies, &t.Upload, &t.Files} {
		*list = slices.Clone(*list)
	}
	if !t.templated() {
		return t, nil
	}
	fail := func(what string, err error) (Task, error) {
		return t, fmt.Errorf("task %s %s: %w", t.Name, what, err)
	}

	vars, err := renderValue(t.Vars, templateData(server, nil))
	if err != nil {
		return fail("vars", err)
	}
	t.Vars, _ = vars.(map[string]any)
	data := templateData(server, t.Vars)
	render := func(text string) (string, error) { return renderText(t.Name, text, data) }

	if t.hasCommand() {
		cmd, err := t.CommandFor(server)
		if err != nil {
			return t, err
		}
		t.Cmd, t.CmdFile = cmd, ""
	}
	if t.Check, err = render(t.Check); err != nil {
		return fail("check", err)
	}
	if t.Verify, err = render(t.Verify); err != nil {
		return fail("verify", err)
	}
	if len(t.EnvVars) > 0 {
		env := make(map[string]string, len(t.EnvVars))
		for _, k := range slices.Sorted(maps.Keys(t.EnvVars)) {
			if env[k], err = render(t.EnvVars[k]); err != nil {
				return fail("envvar "+k, err)
			}
		}
		t.EnvVars = env
	}
	for _, list := range []*[]Copy{&t.Copy, &t.Copies, &t.Upload, &t.Files} {
		for i := range *list {
			c := &(*list)[i]
			if c.Raw {
				continue
			}
			for _, field := range []*string{&c.Dst, &c.DstPath, &c.Dest, &c.Dir, &c.Content, &c.Mode, &c.Perm, &c.Owner, &c.Group} {
				if *field, err = render(*field); err != nil {
					return fail("copy", err)
				}
			}
			if strings.TrimSpace(cmp.Or(c.Dst, c.DstPath, c.Dest, c.Dir)) == "" {
				return fail("copy", errors.New("its destination is empty once filled in"))
			}
			if _, _, err := c.modeOf(); err != nil {
				return fail("copy", err)
			}
		}
	}
	t.Rendered = true
	return t, nil
}

// hasCommand reports whether the task runs a command (as opposed to only copying files).
func (t Task) hasCommand() bool {
	return cmp.Or(t.commandSources()...) != ""
}

// CommandFor returns the task's command for server (a Server, a *Server or nil): cmd, or the
// contents of cmd_file, filled in as a template unless the task is raw or already filled in.
func (t *Task) CommandFor(server any) (string, error) {
	text := t.Cmd
	if text == "" && t.CmdFile != "" {
		content, err := os.ReadFile(t.CmdFile)
		if err != nil {
			return "", fmt.Errorf("failed to read command file %s: %w", t.CmdFile, err)
		}
		text = string(content)
	}
	if text == "" {
		return "", fmt.Errorf("task %s has no command specified", t.Name)
	}
	if !t.templated() {
		return text, nil
	}
	out, err := renderText(t.Name, text, t.templateData(server))
	if err != nil {
		return "", fmt.Errorf("task %s command: %w", t.Name, err)
	}
	return out, nil
}

// renderText fills in text as a template with data. Text without "{{" is returned as it is.
func renderText(name, text string, data map[string]any) (string, error) {
	if !strings.Contains(text, "{{") {
		return text, nil
	}
	tmpl, err := template.New(name).Parse(text)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderValue fills in every text in v, a vars value, as a template with data: a string, or
// the strings inside a table or a list. Other values are kept. Tables and lists are copied.
func renderValue(v any, data map[string]any) (any, error) {
	switch v := v.(type) {
	case string:
		return renderText("vars", v, data)
	case map[string]any:
		if v == nil {
			return v, nil
		}
		out := make(map[string]any, len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			r, err := renderValue(v[k], data)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			r, err := renderValue(item, data)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	case []map[string]any:
		out := make([]map[string]any, len(v))
		for i, item := range v {
			r, err := renderValue(item, data)
			if err != nil {
				return nil, err
			}
			out[i], _ = r.(map[string]any)
		}
		return out, nil
	}
	return v, nil
}

// templateData is what the task's templates see: the server's data, and the task's vars as
// .Vars and .vars.
func (t *Task) templateData(server any) map[string]any {
	var s *Server
	switch v := server.(type) {
	case *Server:
		s = v
	case Server:
		s = &v
	}
	return templateData(s, t.Vars)
}

// templateData is what every template sees: every exported field of the server at the top
// level ({{.Name}}, {{.Hostname}}, ...), the server itself as .Server, and vars (a task's) as
// .Vars and .vars. Envvars are expanded from gorun's environment, so templates see the resolved
// values and not the "$VAR" references.
func templateData(s *Server, vars map[string]any) map[string]any {
	data := map[string]any{}
	if s != nil {
		expanded := *s
		if s.EnvVars != nil {
			expanded.EnvVars = make(map[string]string, len(s.EnvVars))
			for k, v := range s.EnvVars {
				expanded.EnvVars[k] = ExpandEnv(v)
			}
		}
		fields := reflect.ValueOf(expanded)
		for i := range fields.NumField() {
			if f := fields.Type().Field(i); f.IsExported() {
				data[f.Name] = fields.Field(i).Interface()
			}
		}
		data["Server"] = &expanded
		data["IsLocal"] = expanded.IsLocal()
		data["Address"] = expanded.Address()
	}
	if vars == nil {
		vars = map[string]any{}
	}
	data["Vars"] = vars
	data["vars"] = vars
	return data
}

// validate checks the task can run: its settings are in range, it has one command or at least
// one copy, and the files it names exist.
func (t Task) validate() error {
	switch t.Rerun {
	case "", RerunSafe, RerunUnsafe:
	default:
		return fmt.Errorf("task %s rerun must be %q or %q, got %q", t.Name, RerunSafe, RerunUnsafe, t.Rerun)
	}
	switch {
	case t.Timeout < 0:
		return fmt.Errorf("task %s timeout must not be negative", t.Name)
	case t.Retries < 0:
		return fmt.Errorf("task %s retries must not be negative", t.Name)
	case t.RetryDelay != nil && *t.RetryDelay < 0:
		return fmt.Errorf("task %s retry_delay must not be negative", t.Name)
	}

	commands := 0
	for _, source := range t.commandSources() {
		if source != "" {
			commands++
		}
	}
	copies := t.copies()
	switch {
	case commands > 1:
		return fmt.Errorf("task %s cannot have both cmd and cmd_file", t.Name)
	case commands == 0 && len(copies) == 0:
		return fmt.Errorf("task %s has no command specified", t.Name)
	case missing(t.CmdFile):
		return fmt.Errorf("command file %s does not exist", t.CmdFile)
	case t.CheckCopies && len(copies) == 0:
		return fmt.Errorf("task %s sets check_copies but copies nothing", t.Name)
	}

	for _, c := range copies {
		switch {
		case c.Content != "":
			if c.Render {
				return fmt.Errorf("task %s copy sets render with content: render fills in a src file, and content is filled in already", t.Name)
			}
		case c.Src == "":
			return fmt.Errorf("task %s copy requires src", t.Name)
		case missing(c.Src):
			return fmt.Errorf("copy source %s does not exist", c.Src)
		}
		if c.Dst == "" && c.Dir == "" {
			return fmt.Errorf("task %s copy requires dst or dir", t.Name)
		}
	}
	return nil
}

// missing reports whether path is set but names nothing on disk.
func missing(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}
