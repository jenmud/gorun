package gorun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// runOnServer runs tasks on one local server with the given vars and returns every task event.
func runOnServer(t *testing.T, vars map[string]any, tasks ...Task) ([]TaskEvent, error) {
	t.Helper()
	cfg := &Config{Tasks: map[string]Task{}}
	names := []string{}
	for _, task := range tasks {
		task.Local = true
		cfg.Tasks[task.Name] = task
		names = append(names, task.Name)
	}
	cfg.Servers = []Server{{Name: "local", TaskNames: names, Vars: vars}}
	var mu sync.Mutex
	var events []TaskEvent
	cfg.OnEvent = func(event any) {
		if ev, ok := event.(TaskEvent); ok {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		}
	}
	return events, cfg.Run(context.Background())
}

// Every setting is a template: a directory named by a server var, created when missing, and a
// file copied into it, both skipped once they are in place.
func TestEverySettingIsATemplate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "conf")
	vars := map[string]any{"config": dir}
	tasks := []Task{
		{
			Name:   "config-dir",
			Check:  "test -d {{ .Server.Vars.config }}",
			Cmd:    "mkdir -p {{ .Server.Vars.config }}",
			Verify: "test -d {{ .Server.Vars.config }}",
		},
		{
			Name:        "compose",
			DependsOn:   []string{"config-dir"},
			CheckCopies: true,
			Copy:        []Copy{{Content: "services: {} # {{ .Name }}\n", Dst: "{{ .Server.Vars.config }}/docker-compose.yml"}},
			EnvVars:     map[string]string{"compose_file": "{{ .Server.Vars.config }}/docker-compose.yml"},
			Cmd:         `cat "$COMPOSE_FILE"`,
		},
	}

	events, err := runOnServer(t, vars, tasks...)
	if err != nil {
		ev := final(events, "compose")
		t.Fatalf("first run: %v (%v, stderr %q)", err, ev.Err, ev.Stderr)
	}
	ev := final(events, "config-dir")
	if ev.State != TaskDone || ev.Check == nil || ev.Check.Command != "test -d "+dir || ev.Verify == nil || ev.Verify.Command != "test -d "+dir {
		t.Fatalf("config-dir = state %d, check %+v, verify %+v; want it run with filled-in check and verify", ev.State, ev.Check, ev.Verify)
	}
	if ev := final(events, "compose"); ev.State != TaskDone || ev.Stdout != "services: {} # local\n" {
		t.Fatalf("compose = state %d, stdout %q; want the filled-in file read through $COMPOSE_FILE", ev.State, ev.Stdout)
	}

	events, err = runOnServer(t, vars, tasks...)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, name := range []string{"config-dir", "compose"} {
		if ev := final(events, name); ev.State != TaskUnchanged {
			t.Errorf("second run: %s state %d, want unchanged", name, ev.State)
		}
	}
}

func TestForServerFillsInPerServerWithoutSharingState(t *testing.T) {
	task := Task{
		Name:    "t",
		Cmd:     "echo {{ .vars.greeting }}",
		Check:   "check {{ .Name }}",
		Verify:  "verify {{ .Server.Vars.v }}",
		Copy:    []Copy{{Content: "x {{ .Name }}", Dst: "/srv/{{ .Name }}/x"}, {Content: "y", Dst: "/plain"}},
		EnvVars: map[string]string{"A": "1", "B": "{{ .Name }}"},
		Vars:    map[string]any{"greeting": "hi from {{ .Name }}", "port": 8080},
	}
	one, err := task.ForServer(&Server{Name: "one", Vars: map[string]any{"v": 1}})
	if err != nil {
		t.Fatal(err)
	}
	two, err := task.ForServer(&Server{Name: "two", Vars: map[string]any{"v": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if one.Check != "check one" || one.Verify != "verify 1" || one.Copy[0].Dst != "/srv/one/x" || one.Copy[0].Content != "x one" || one.Copy[1].Dst != "/plain" {
		t.Errorf("one = check %q, verify %q, copies %+v", one.Check, one.Verify, one.Copy)
	}
	if one.Cmd != "echo hi from one" || one.Vars["greeting"] != "hi from one" || one.Vars["port"] != 8080 {
		t.Errorf("one = cmd %q, vars %v; task vars are templates too, and see the server", one.Cmd, one.Vars)
	}
	if two.Check != "check two" || two.Copy[0].Dst != "/srv/two/x" || two.EnvVars["B"] != "two" || two.EnvVars["A"] != "1" {
		t.Errorf("two = check %q, copies %+v, env %v", two.Check, two.Copy, two.EnvVars)
	}
	if task.Copy[0].Dst != "/srv/{{ .Name }}/x" || task.Vars["greeting"] != "hi from {{ .Name }}" || task.EnvVars["B"] != "{{ .Name }}" {
		t.Errorf("ForServer changed the task it was called on: %+v", task)
	}
	// filling in again is a no-op, so a value that contains {{ once filled in stays as it is
	again, err := one.ForServer(&Server{Name: "other"})
	if err != nil || again.Check != "check one" || again.Copy[0].Dst != "/srv/one/x" {
		t.Errorf("second fill = %+v, %v", again, err)
	}
	if cmd, err := one.CommandFor(&Server{Name: "other"}); err != nil || cmd != "echo hi from one" {
		t.Errorf("command of a filled-in task = %q, %v", cmd, err)
	}
}

// raw = true keeps a task, or a copy, exactly as written: for commands that use {{ themselves.
func TestRawKeepsTextAsWritten(t *testing.T) {
	task := Task{
		Name:    "ps",
		Raw:     true,
		Cmd:     "docker ps --format '{{.Names}}'",
		Check:   "test {{ .Name }}",
		EnvVars: map[string]string{"F": "{{.ID}}"},
		Vars:    map[string]any{"v": "{{ x }}"},
		Copy:    []Copy{{Content: "{{ .Name }}", Dst: "/a/{{ .Name }}"}},
	}
	got, err := task.ForServer(&Server{Name: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmd != task.Cmd || got.Check != task.Check || got.EnvVars["F"] != "{{.ID}}" || got.Vars["v"] != "{{ x }}" || got.Copy[0].Dst != "/a/{{ .Name }}" {
		t.Errorf("a raw task was filled in: %+v", got)
	}
	if cmd, _ := task.CommandFor(&Server{Name: "s"}); cmd != task.Cmd {
		t.Errorf("raw command = %q", cmd)
	}

	// a raw copy keeps its own text, and the rest of the task is still filled in
	mixed := Task{Name: "m", Cmd: "echo {{ .Name }}", Copy: []Copy{{Raw: true, Content: "{{ keep }}", Dst: "/a/{{ keep }}"}}}
	got, err = mixed.ForServer(&Server{Name: "s"})
	if err != nil || got.Cmd != "echo s" || got.Copy[0].Content != "{{ keep }}" || got.Copy[0].Dst != "/a/{{ keep }}" {
		t.Errorf("raw copy = %+v, %v", got, err)
	}
}

// A cmd_file's script is a template too; the filled-in script becomes the task's cmd.
func TestCmdFileIsATemplate(t *testing.T) {
	script := filepath.Join(t.TempDir(), "deploy.sh")
	if err := os.WriteFile(script, []byte("echo deploying {{ .vars.release }} to {{ .Name }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := Task{Name: "deploy", CmdFile: script, Vars: map[string]any{"release": "2.4.0"}}
	got, err := task.ForServer(&Server{Name: "web-01"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmd != "echo deploying 2.4.0 to web-01\n" || got.CmdFile != "" {
		t.Errorf("cmd = %q, cmd_file = %q", got.Cmd, got.CmdFile)
	}
	task.Raw = true
	if got, _ := task.ForServer(&Server{Name: "web-01"}); got.CmdFile != script {
		t.Errorf("a raw task's cmd_file was read: %+v", got)
	}
}

// A copied file is sent as it is unless the copy says render = true: it may be a binary, an
// archive, or another tool's template.
func TestCopiedFilesRenderOnlyWhenAsked(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "app.conf")
	if err := os.WriteFile(src, []byte("name = {{ .Name }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := &Server{Name: "web-01"}
	for _, c := range []struct {
		copy Copy
		want string
	}{
		{Copy{Src: src, Dst: filepath.Join(dir, "plain.conf")}, "name = {{ .Name }}\n"},
		{Copy{Src: src, Dst: filepath.Join(dir, "rendered.conf"), Render: true}, "name = web-01\n"},
		{Copy{Src: src, Dst: filepath.Join(dir, "raw.conf"), Render: true, Raw: true}, "name = {{ .Name }}\n"},
	} {
		task := &Task{Name: "copy", Copy: []Copy{c.copy}}
		if err := CopyFiles(nil, server, task); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(c.copy.Dst); string(got) != c.want {
			t.Errorf("%s = %q, want %q", filepath.Base(c.copy.Dst), got, c.want)
		}
	}
}

func TestForServerErrors(t *testing.T) {
	for name, task := range map[string]Task{
		"empty dst": {Name: "t", Copy: []Copy{{Content: "x", Dst: "{{ if false }}x{{ end }}"}}},
		"parse":     {Name: "t", Cmd: "true", Check: "{{ .Name"},
		"vars":      {Name: "t", Cmd: "true", Vars: map[string]any{"a": "{{ .Name"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := task.ForServer(&Server{Name: "s"}); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestTaskThatCannotFillInFailsTheRun(t *testing.T) {
	_, err := runOnServer(t, nil, Task{Name: "bad", Cmd: "true", Verify: "{{ .Name.Field }}"})
	if err == nil || !strings.Contains(err.Error(), "task bad verify") {
		t.Fatalf("err = %v, want the task and its verify named", err)
	}
}

// Global, group, bundle and server vars and envvars are templates too, filled in for each
// server, each layer unless it is raw. Envvars see the vars; the layering is unchanged.
func TestServerGroupBundleAndGlobalVarsAreTemplates(t *testing.T) {
	cfg := loadConfig(t, `
vars    = { region = "eu", home = "/srv/{{ .Name }}" }
envvars = { NODE = "{{ .Name }}" }

[server_groups.web]
vars    = { config = "{{ .Server.Vars.home }}/conf", tagged = "{{ index .Tags 0 }}" }
envvars = { CONFIG = "{{ .Server.Vars.config }}", PORT = "{{ .Server.Vars.port }}" }

[server_groups.literal]
raw     = true
envvars = { FORMAT = "{{.Names}}" }

[bundles.base]
tasks = ["hello"]
vars  = { bundled = "for {{ .Name }}", region = "bundle loses" }

[tasks.hello]
cmd = "echo hello"

[servers.hosts.web-01]
hostname = "web-01.example.com"
groups   = ["web", "literal"]
tags     = ["canary"]
vars     = { port = 8080, home = "/opt/{{ .Hostname }}" }
envvars  = { ADDR = "{{ .Address }}" }
tasks    = ["@base"]
    [servers.hosts.web-01.ssh]
    username = "deploy"

[servers.hosts.raw-01]
raw     = true
vars    = { home = "{{ keep }}" }
tasks   = ["hello"]
    [servers.hosts.raw-01.ssh]
    username = "deploy"
`)
	web := serverNamed(t, cfg, "web-01")
	wantVars := map[string]any{
		"region":  "eu",
		"home":    "/opt/web-01.example.com", // the server's own layer wins, filled in for it
		"config":  "/srv/web-01/conf",        // a var sees the server's fields, not other vars
		"tagged":  "canary",
		"port":    int64(8080),
		"bundled": "for web-01",
	}
	for k, want := range wantVars {
		if got := web.Vars[k]; got != want {
			t.Errorf("web-01 var %s = %#v, want %#v", k, got, want)
		}
	}
	wantEnv := map[string]string{
		"NODE":   "web-01",
		"CONFIG": "/srv/web-01/conf", // envvars see the filled-in vars
		"PORT":   "8080",
		"FORMAT": "{{.Names}}", // a raw group keeps its text
		"ADDR":   "web-01.example.com:22",
	}
	for k, want := range wantEnv {
		if got := web.EnvVars[k]; got != want {
			t.Errorf("web-01 envvar %s = %q, want %q", k, got, want)
		}
	}
	raw := serverNamed(t, cfg, "raw-01")
	if raw.Vars["home"] != "{{ keep }}" || raw.EnvVars["NODE"] != "raw-01" {
		t.Errorf("raw-01 = vars %v, env %v: its own layer is raw, the global one isn't", raw.Vars, raw.EnvVars)
	}
}

// The removed _template keys stop the load with what to write instead.
func TestRemovedTemplateKeysSayWhatToWrite(t *testing.T) {
	for key, want := range map[string]string{
		"[tasks.a]\ncmd_template = \"echo\"":                                               "write cmd",
		"[tasks.a]\ncmd = \"x\"\ncheck_template = \"true\"":                                "write check",
		"[tasks.a]\ncmd = \"x\"\nenvvars_template = { A = \"b\" }":                         "write envvars",
		"[tasks.a]\ncmd = \"x\"\n[[tasks.a.copy]]\nsrc_template = \"f\"\ndst = \"/a\"":     "render = true",
		"[tasks.a]\ncmd = \"x\"\n[[tasks.a.copy]]\ncontent = \"f\"\ndst_template = \"/a\"": "write dst",
	} {
		var cfg Config
		err := cfg.Load(strings.NewReader(key))
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "removed in v0.20") {
			t.Errorf("%q: err = %v, want it to say %q", key, err, want)
		}
	}
}

// loadConfig loads a Gorunfile from text.
func loadConfig(t *testing.T, text string) *Config {
	t.Helper()
	var cfg Config
	if err := cfg.Load(strings.NewReader(text)); err != nil {
		t.Fatal(err)
	}
	return &cfg
}

// serverNamed returns the named server of cfg.
func serverNamed(t *testing.T, cfg *Config, name string) Server {
	t.Helper()
	for _, s := range cfg.Servers {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no server %s", name)
	return Server{}
}
