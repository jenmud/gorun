package gorun

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

// Copy is one file copy of a task. A source is one of Src (a file or directory), SrcTpl (a
// template rendered for each server) or Content (inline text); the target is Dst, or Dir to keep
// the source's name. The other fields are alternative spellings of those keys.
type Copy struct {
	Src      string `toml:"src"`
	SrcFile  string `toml:"src_file,omitempty"`
	SrcTpl   string `toml:"src_template,omitempty"`
	SrcTmpl  string `toml:"src_tmpl,omitempty"`
	Template string `toml:"template,omitempty"`
	Content  string `toml:"content,omitempty"`
	Dst      string `toml:"dst"`
	DstPath  string `toml:"dst_path,omitempty"`
	Dir      string `toml:"dir,omitempty"`
	Dest     string `toml:"dest,omitempty"`
	Mode     string `toml:"mode,omitempty"`
	Perm     string `toml:"perm,omitempty"`
}

// target is where the copy goes on the server: Dst, or Dir with the source's name.
func (c Copy) target() string {
	if c.Dst == "" && c.Dir != "" && c.Src != "" {
		return filepath.Join(c.Dir, filepath.Base(c.Src))
	}
	return c.Dst
}

// normalized returns the copy with its alternative keys folded into Src, SrcTpl and Dst.
func (c Copy) normalized() Copy {
	c.Src = cmp.Or(c.Src, c.SrcFile)
	c.SrcTpl = cmp.Or(c.SrcTpl, c.Template, c.SrcTmpl)
	c.Dst = cmp.Or(c.Dst, c.DstPath, c.Dest)
	return c
}

// Task is a task to execute on one or more servers.
type Task struct {
	LogLevel        string            `toml:"log_level"`
	Name            string            `toml:"-"` // the <name> in [tasks.<name>]
	Source          string            `toml:"-"` // the file that defines it
	Sudo            bool              `toml:"sudo"`
	Cmd             string            `toml:"cmd,omitempty"`
	CmdFile         string            `toml:"cmd_file,omitempty"`
	CmdTemplate     string            `toml:"cmd_template,omitempty"`
	CmdTemplateFile string            `toml:"cmd_template_file,omitempty"`
	Copy            []Copy            `toml:"copy,omitempty"`
	Copies          []Copy            `toml:"copies,omitempty"`
	Upload          []Copy            `toml:"upload,omitempty"`
	Files           []Copy            `toml:"files,omitempty"`
	EnvVars         map[string]string `toml:"envvars"`
	Vars            map[string]any    `toml:"vars,omitempty"`
	DependsOn       []string          `toml:"depends_on"`
	Shell           string            `toml:"shell"`
	Timeout         time.Duration     `toml:"timeout,omitempty"`

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
// copied (a file or folder, a template rendered for the server, or inline text) and where it
// goes on the server, e.g. "scripts/ok.sh → /opt/app/ok.sh". Copies run before the command.
func (t Task) CopySummaries() []string {
	var out []string
	for _, c := range t.copies() {
		from := c.Src
		switch {
		case c.Content != "":
			from = fmt.Sprintf("inline text (%d bytes)", len(c.Content))
		case c.SrcTpl != "":
			from = "template " + c.SrcTpl
		}
		out = append(out, from+" → "+c.target())
	}
	return out
}

// commandSources are the task's four ways to give a command, of which at most one may be set.
func (t Task) commandSources() []string {
	return []string{t.Cmd, t.CmdFile, t.CmdTemplate, t.CmdTemplateFile}
}

// hasCheck reports whether the task checks whether there is anything to do before it runs.
func (t Task) hasCheck() bool {
	return t.Check != "" || t.CheckCopies
}

// hasCommand reports whether the task runs a command (as opposed to only copying files).
func (t Task) hasCommand() bool {
	return cmp.Or(t.commandSources()...) != ""
}

// CommandFor returns the task's command: cmd as written, the contents of cmd_file, or
// cmd_template / cmd_template_file rendered for server (a Server, a *Server or nil).
func (t *Task) CommandFor(server any) (string, error) {
	switch {
	case t.CmdTemplate != "":
		return t.render("command template for task "+t.Name, t.CmdTemplate, server)
	case t.CmdTemplateFile != "":
		content, err := os.ReadFile(t.CmdTemplateFile)
		if err != nil {
			return "", fmt.Errorf("failed to read command template file %s: %w", t.CmdTemplateFile, err)
		}
		return t.render("command template file "+t.CmdTemplateFile, string(content), server)
	case t.Cmd != "":
		return t.Cmd, nil
	case t.CmdFile != "":
		content, err := os.ReadFile(t.CmdFile)
		if err != nil {
			return "", fmt.Errorf("failed to read command file %s: %w", t.CmdFile, err)
		}
		return string(content), nil
	}
	return "", fmt.Errorf("task %s has no command specified", t.Name)
}

// render executes a template with the task's template data. what names it in errors.
func (t *Task) render(what, text string, server any) (string, error) {
	tmpl, err := template.New(t.Name).Parse(text)
	if err != nil {
		return "", fmt.Errorf("failed to parse %s: %w", what, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, t.templateData(server)); err != nil {
		return "", fmt.Errorf("failed to execute %s: %w", what, err)
	}
	return buf.String(), nil
}

// templateData is what cmd_template and copy templates see: every exported field of the server
// at the top level ({{.Name}}, {{.Hostname}}, ...), the server itself as .Server, and the task's
// vars as .Vars and .vars. Envvars are expanded from gorun's environment, so templates see the
// resolved values and not the "$VAR" references.
func (t *Task) templateData(server any) map[string]any {
	data := map[string]any{}
	var s *Server
	switch v := server.(type) {
	case *Server:
		s = v
	case Server:
		s = &v
	}
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
	vars := t.Vars
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
		return fmt.Errorf("task %s cannot have more than one of cmd, cmd_file, cmd_template, cmd_template_file specified", t.Name)
	case commands == 0 && len(copies) == 0:
		return fmt.Errorf("task %s has no command specified", t.Name)
	case missing(t.CmdFile):
		return fmt.Errorf("command file %s does not exist", t.CmdFile)
	case missing(t.CmdTemplateFile):
		return fmt.Errorf("command template file %s does not exist", t.CmdTemplateFile)
	case t.CheckCopies && len(copies) == 0:
		return fmt.Errorf("task %s sets check_copies but copies nothing", t.Name)
	}

	for _, c := range copies {
		switch {
		case c.Content != "":
		case c.SrcTpl != "":
			if missing(c.SrcTpl) {
				return fmt.Errorf("copy template %s does not exist", c.SrcTpl)
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
