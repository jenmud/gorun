package gorun

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/google/go-cmp/cmp"
)

// equateEmpty treats nil and empty slices and maps as equal: Encode leaves both out.
var equateEmpty = cmp.FilterValues(func(x, y any) bool {
	vx, vy := reflect.ValueOf(x), reflect.ValueOf(y)
	if x == nil || y == nil || vx.Type() != vy.Type() {
		return false
	}
	k := vx.Kind()
	return (k == reflect.Slice || k == reflect.Map) && vx.Len() == 0 && vy.Len() == 0
}, cmp.Comparer(func(_, _ any) bool { return true }))

// tricky are values that must come back exactly as written: template syntax, shell syntax,
// quotes, TOML string delimiters, escapes, control characters and non-ASCII text.
var tricky = []string{
	"{{.Name}} {{ unterminated",
	"$HOME ${USER} $(id -u) `date`",
	`she said "hi" and 'bye'`,
	"line one\nline two\r\n\ttabbed",
	`back\slash \n not a newline`,
	`""" and ''' delimiters`,
	"bell \a escape \x1b del \x7f",
	"héllo wörld, 日本語, emoji 🚀",
}

// richConfig uses every part of a Gorunfile. Vars hold the types TOML decodes to, so they
// compare equal after a load.
func richConfig() Config {
	delay := time.Duration(0)
	return Config{
		Title:           "Rich " + tricky[2],
		LogLevel:        "debug",
		MaxParallel:     3,
		MaxTaskParallel: -1,
		Raw:             true,
		EnvVars:         map[string]string{"TRICKY": tricky[1], "UNICODE": tricky[7]},
		Vars: map[string]any{
			"template": tricky[0],
			"count":    int64(4),
			"ratio":    0.5,
			"on":       true,
			"ports":    []any{int64(80), int64(443)},
			"db":       map[string]any{"host": "db.local", "port": int64(5432)},
			"users":    []map[string]any{{"name": "ann"}, {"name": "bob"}},
		},
		SSH: SSH{User: "deploy", Port: 2222, Identityfile: "keys/id", ConfigFile: "ssh_config"},
		Tasks: map[string]Task{
			"base": {
				Name:        "base",
				Cmd:         tricky[3],
				Description: tricky[5],
				Rerun:       RerunSafe,
				Timeout:     90 * time.Second,
				Retries:     2,
				RetryDelay:  &delay,
				Shell:       "/bin/bash",
				LogLevel:    "warn",
			},
			"deploy app": {
				Name:        "deploy app",
				Sudo:        true,
				CmdFile:     "scripts/deploy.sh",
				DependsOn:   []string{"base"},
				Check:       tricky[0],
				Verify:      tricky[4],
				CheckCopies: true,
				Details:     tricky[6],
				Copies: []Copy{
					{Src: "app.tar", Dst: "/opt/app.tar", Mode: "0640", Owner: "app", Group: "app"},
					{Src: "conf.tmpl", Dir: "/etc/app", Render: true},
				},
				Copy:    []Copy{{Content: tricky[0], Dst: "/etc/motd", Raw: true}},
				Upload:  []Copy{{SrcFile: "a.txt", DstPath: "/tmp/a.txt", Perm: "0600"}},
				Files:   []Copy{{Src: "b.txt", Dest: "/tmp/b.txt"}},
				EnvVars: map[string]string{"QUOTED": tricky[2]},
				Vars:    map[string]any{"version": "1.2.3", "raw": tricky[1]},
				Raw:     true,
			},
			`x.y "z" [1]`: {Name: `x.y "z" [1]`, Cmd: "true"},
			"local-echo": {
				Name:        "local-echo",
				Cmd:         "echo {{.Name}}",
				Local:       true,
				RetryUnsafe: true,
				Rerun:       RerunUnsafe,
			},
		},
		Bundles: map[string]Bundle{
			"all":  {Name: "all", Description: "everything", Tasks: []string{"@base", "deploy app"}, Vars: map[string]any{"from": "bundle"}},
			"base": {Name: "base", Tasks: []string{"base"}, Vars: map[string]any{"t": tricky[0]}, Raw: true},
		},
		Groups: map[string]ServerGroup{
			"web": {
				Name:     "web",
				LogLevel: "info",
				Tags:     []string{"web", "prod"},
				Tasks:    []string{"@all"},
				EnvVars:  map[string]string{"ROLE": "web"},
				Vars:     map[string]any{"workers": int64(4)},
				SSH:      SSH{User: "www", Config: "group_ssh_config"},
			},
			"quiet": {Name: "quiet", Tags: []string{"quiet"}, Vars: map[string]any{"q": tricky[0]}, Raw: true},
		},
		Schedules: []Schedule{
			{Name: "nightly", Description: tricky[7], Target: "@all", Cron: "0 2 * * *", Timezone: "Australia/Sydney", Missed: MissedRun},
			{Name: "once", Target: "base", At: "2099-01-02T03:04"},
		},
		Servers: []Server{
			{
				Name:            "web-02",
				Hostname:        "10.0.0.2",
				LogLevel:        "error",
				MaxTaskParallel: 2,
				SSH:             SSH{User: "root", Password: "$WEB_PASSWORD", Port: 22, Identityfile: "~/.ssh/id"},
				TaskNames:       []string{"deploy app"},
				Groups:          []string{"web"},
				Tags:            []string{"blue"},
				EnvVars:         map[string]string{"TRICKY": tricky[3]},
				Vars:            map[string]any{"template": tricky[0], "n": int64(7)},
				Raw:             true,
				AddTasks:        []string{"base"},
			},
			{Name: "local", TaskNames: []string{"local-echo"}},
			{Name: "a.b", Hostname: "dotted", SSH: SSH{User: "x"}, TaskNames: []string{}, Groups: []string{"web", "quiet"}, SkipTasks: []string{"deploy app"}},
			{Name: "web-01", Hostname: "10.0.0.1", SSH: SSH{User: "deploy"}, TaskNames: []string{"base"}, Vars: map[string]any{"who": "{{.Name}}"}},
			{Name: `odd "name" [x].y`, SSH: SSH{User: "o"}, TaskNames: []string{`x.y "z" [1]`}},
		},
	}
}

// encodeAndLoad encodes cfg into a Gorunfile in a temporary directory and loads it back.
func encodeAndLoad(t *testing.T, cfg *Config) (Config, []byte) {
	t.Helper()
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var buf bytes.Buffer
	if err := cfg.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Fatal("Encode and Marshal wrote different bytes")
	}
	path := filepath.Join(t.TempDir(), "Gorunfile")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := got.LoadFile(path); err != nil {
		t.Fatalf("LoadFile: %v\n%s", err, data)
	}
	return got, data
}

// unsourced returns cfg without what LoadFile records about the files it read.
func unsourced(cfg Config) Config {
	cfg.Path, cfg.Files, cfg.Includes = "", nil, nil
	clear := func(m any) {
		v := reflect.ValueOf(m)
		for _, k := range v.MapKeys() {
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(v.MapIndex(k))
			e.FieldByName("Source").SetString("")
			v.SetMapIndex(k, e)
		}
	}
	cfg.Tasks, cfg.Bundles, cfg.Groups = cloneMap(cfg.Tasks), cloneMap(cfg.Bundles), cloneMap(cfg.Groups)
	clear(cfg.Tasks)
	clear(cfg.Bundles)
	clear(cfg.Groups)
	cfg.Schedules = append([]Schedule(nil), cfg.Schedules...)
	for i := range cfg.Schedules {
		cfg.Schedules[i].Source = ""
	}
	cfg.Servers = append([]Server(nil), cfg.Servers...)
	for i := range cfg.Servers {
		cfg.Servers[i].Source = ""
	}
	return cfg
}

func cloneMap[V any](m map[string]V) map[string]V {
	if m == nil {
		return nil
	}
	out := make(map[string]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestEncodeRoundTrip(t *testing.T) {
	cfg := richConfig()
	got, data := encodeAndLoad(t, &cfg)

	// LoadFile composes servers, so the expected servers are the originals composed the same way.
	want := cfg
	want.Servers = nil
	for _, s := range cfg.Servers {
		if err := cfg.composeServer(&s, nil, true); err != nil {
			t.Fatalf("compose %s: %v", s.Name, err)
		}
		want.Servers = append(want.Servers, s)
	}
	if diff := cmp.Diff(want, unsourced(got), equateEmpty); diff != "" {
		t.Fatalf("round trip changed the config (-want +got):\n%s\n%s", diff, data)
	}

	// Each server is written as it is in the config, not as composed: check the raw tables.
	var raw configFile
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, key := range md.Keys() {
		if len(key) == 3 && key[0] == "servers" && key[1] == "hosts" {
			order = append(order, key[2])
		}
	}
	if diff := cmp.Diff([]string{"web-02", "local", "a.b", "web-01", `odd "name" [x].y`}, order); diff != "" {
		t.Errorf("servers out of order (-want +got):\n%s", diff)
	}
	for _, s := range cfg.Servers {
		decoded := raw.Servers.Hosts[s.Name]
		decoded.Name = s.Name
		if diff := cmp.Diff(s, decoded, equateEmpty); diff != "" {
			t.Errorf("server %s written differently (-want +got):\n%s", s.Name, diff)
		}
	}
	if !md.IsDefined("servers", "hosts", "a.b", "tasks") {
		t.Error("a.b's empty task list was left out, so it would run its groups' tasks")
	}
}

func TestEncodeIsDeterministic(t *testing.T) {
	cfg := richConfig()
	first, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		again := richConfig()
		next, err := again.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, next) {
			t.Fatalf("encoding changed between runs:\n%s\n---\n%s", first, next)
		}
	}
}

func TestEncodeWritesOnlyGorunfileData(t *testing.T) {
	cfg := richConfig()
	cfg.GorunUUID = "secret-uuid"
	cfg.Path = "/secret/path/Gorunfile"
	cfg.Files = []string{"/secret/files"}
	cfg.Schedule = "secret-schedule"
	cfg.SelectedTasks = []string{"secret-selected"}
	cfg.Origin = Origin{User: "secret-user"}
	cfg.UpdateIncludes, cfg.Force, cfg.CheckOnly = true, true, true
	cfg.OnEvent = func(any) {}
	cfg.Includes = []IncludeSource{{Source: "secret-include"}}
	task := cfg.Tasks["base"]
	task.Source, task.Rendered = "secret-task-source", true
	cfg.Tasks["base"] = task
	cfg.Servers[0].Source = "secret-server-source"
	cfg.Servers[0].Tasks = []Task{{Name: "secret-pipeline", Cmd: "secret-cmd"}}

	data, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "secret") {
		t.Errorf("a field with no Gorunfile key was written:\n%s", text)
	}
	if strings.Contains(text, "rendered") || strings.Contains(text, "[servers.hosts.web-02.tasks") {
		t.Errorf("a toml:\"-\" field was written:\n%s", text)
	}

	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		path, ok := headerPath(line)
		if !ok {
			continue
		}
		next := ""
		if i+1 < len(lines) {
			next = lines[i+1]
		}
		child, isHeader := headerPath(next)
		if next == "" || (isHeader && !hasPrefix(child, path)) {
			t.Errorf("empty table %s:\n%s", line, text)
		}
	}
	// a.b's empty task list is the one empty value that changes what a load gives
	if n := strings.Count(text, "\ntasks = []\n"); n != 1 {
		t.Errorf("want only a.b's empty task list written, got %d:\n%s", n, text)
	}
	rest := strings.Replace(text, "\ntasks = []\n", "\n", 1)
	for _, empty := range []string{"= []", "= {}", `= ""`, "= 0\n", "= false"} {
		if strings.Contains(rest, empty) {
			t.Errorf("an empty value (%s) was written:\n%s", empty, text)
		}
	}
}

func TestEncodeEmptyAndInvalid(t *testing.T) {
	var empty Config
	data, err := empty.Marshal()
	if err != nil || len(data) != 0 {
		t.Errorf("empty config: got %q, %v; want no output", data, err)
	}

	tables := Config{Vars: map[string]any{"users": []map[string]any{{"name": "ann"}}}}
	if data, err := tables.Marshal(); err != nil || string(data) != "[[vars.users]]\nname = \"ann\"\n" {
		t.Errorf("vars holding only an array of tables: got %q, %v", data, err)
	}

	local := Config{Servers: []Server{{Name: "localhost"}}}
	if data, err := local.Marshal(); err != nil || string(data) != "[servers.hosts.localhost]\n" {
		t.Errorf("bare server: got %q, %v", data, err)
	}

	for name, cfg := range map[string]Config{
		"unnamed server":   {Servers: []Server{{Hostname: "x"}}},
		"duplicate server": {Servers: []Server{{Name: "a"}, {Name: "a"}}},
		"invalid UTF-8":    {Title: "bad \xff byte"},
	} {
		if _, err := cfg.Marshal(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// gitInclude matches an include of a git repository outside a comment; loading one needs the
// network, so those Gorunfiles are left out.
var gitInclude = regexp.MustCompile(`(?m)^[^#\n]*["']git::`)

func TestEncodeExampleGorunfiles(t *testing.T) {
	var files []string
	for _, pattern := range []string{"cmd/Gorunfile*", "recipes/Gorunfile", "recipes/*/Gorunfile", "examples/*/Gorunfile", "docs/*/Gorunfile"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) < 10 {
		t.Fatalf("found only %d example Gorunfiles: %v", len(files), files)
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if gitInclude.Match(src) {
				t.Skip("includes a git repository")
			}
			var first Config
			if err := first.LoadFile(file); err != nil {
				t.Fatal(err)
			}
			second, data := encodeAndLoad(t, &first)
			if diff := cmp.Diff(unsourced(first), unsourced(second), equateEmpty); diff != "" {
				t.Fatalf("loading the encoded file changed the config (-first +second):\n%s\n%s", diff, data)
			}
			again, err := second.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, again) {
				t.Errorf("encoding is not stable across a load:\n%s\n---\n%s", data, again)
			}
		})
	}
}
