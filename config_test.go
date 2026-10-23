package main

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestResolveTasks(t *testing.T) {
	library := map[string]Task{
		"a":       {Name: "a", DependsOn: []string{"b"}},
		"b":       {Name: "b", DependsOn: []string{"c"}},
		"c":       {Name: "c"},
		"d":       {Name: "d", DependsOn: []string{"c", "b"}},
		"e":       {Name: "e"},
		"loop-1":  {Name: "loop-1", DependsOn: []string{"loop-2"}},
		"loop-2":  {Name: "loop-2", DependsOn: []string{"loop-1"}},
		"missing": {Name: "missing", DependsOn: []string{"nope"}},
	}

	tests := []struct {
		name    string
		tasks   []string
		want    []string
		wantErr string
	}{
		{name: "empty", tasks: nil, want: []string{}},
		{name: "no dependencies keep list order", tasks: []string{"e", "c"}, want: []string{"e", "c"}},
		{name: "dependencies are included", tasks: []string{"a"}, want: []string{"c", "b", "a"}},
		{name: "shared dependencies run once", tasks: []string{"a", "d"}, want: []string{"c", "b", "a", "d"}},
		{name: "listed dependency runs once", tasks: []string{"c", "a", "b"}, want: []string{"c", "b", "a"}},
		{name: "unknown task", tasks: []string{"zzz"}, wantErr: `unknown task "zzz"`},
		{name: "unknown dependency", tasks: []string{"missing"}, wantErr: `task "missing" depends on unknown task "nope"`},
		{name: "cycle", tasks: []string{"loop-1"}, wantErr: "cyclic dependency detected: loop-1 -> loop-2 -> loop-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks, err := resolveTasks(library, tt.tasks...)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("got error %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := []string{}
			for _, task := range tasks {
				got = append(got, task.Name)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("task order mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConfig_Load(t *testing.T) {
	gorunfile := `
title = "test"

[tasks.packages]
cmd = "apk add curl"

[tasks.timezone]
cmd = "echo UTC > /etc/timezone"
depends_on = ["packages"]

[tasks.smoke-test]
cmd_file = "./scripts/smoke.sh"

[servers]
tasks = ["timezone"]

[servers.hosts.zeta]
hostname = "zeta.internal"

[servers.hosts.alpha]
hostname = "alpha.internal"
ssh = { port = 2222, username = "deploy" }
tasks = ["smoke-test"]

[servers.hosts.nothing]
tasks = []
`

	var c Config
	if err := c.Load(strings.NewReader(gorunfile)); err != nil {
		t.Fatal(err)
	}

	if got := c.Tasks["timezone"].Name; got != "timezone" {
		t.Errorf("task name should come from its key, got %q", got)
	}

	want := []Server{
		{Name: "zeta", Hostname: "zeta.internal", TaskNames: []string{"timezone"}},
		{Name: "alpha", Hostname: "alpha.internal", SSH: SSH{Port: 2222, User: "deploy"}, TaskNames: []string{"smoke-test"}},
		{Name: "nothing", TaskNames: []string{}},
	}
	if diff := cmp.Diff(want, c.Servers); diff != "" {
		t.Errorf("servers mismatch, should be in file order and inherit the global tasks unless overridden (-want +got):\n%s", diff)
	}

	// inherited tasks pull in their dependencies
	zeta, err := c.ServerPipeline(c.Servers[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(zeta.Tasks) != 2 || zeta.Tasks[0].Name != "packages" || zeta.Tasks[1].Name != "timezone" {
		t.Errorf("zeta should run packages then timezone, got %+v", zeta.Tasks)
	}

	c.ResolvePaths("/etc/gorun")
	if got := c.Tasks["smoke-test"].CmdFile; got != "/etc/gorun/scripts/smoke.sh" {
		t.Errorf("cmd_file should resolve relative to the config dir, got %q", got)
	}
}

func TestConfig_Load_Errors(t *testing.T) {
	tests := []struct {
		name      string
		gorunfile string
		wantErr   string
	}{
		{
			name:      "typo in a task",
			gorunfile: "[tasks.a]\ncmd_flie = \"x.sh\"\n",
			wantErr:   "unknown keys: tasks.a.cmd_flie",
		},
		{
			name:      "old array layout",
			gorunfile: "[[servers]]\nname = \"server1\"\n",
			wantErr:   "error with loading config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c Config
			err := c.Load(strings.NewReader(tt.gorunfile))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
