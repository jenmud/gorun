package gorun

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const groupsGorunfile = `
[tasks.packages]
cmd = "true"

[tasks.timezone]
cmd = "true"
depends_on = ["packages"]

[tasks.users]
cmd = "true"

[tasks.app-files]
cmd = "true"

[tasks.deploy-app]
cmd = "true"

[tasks.deploy-db]
cmd = "true"

[tasks.monitoring]
cmd = "true"

[bundles.base]
description = "Every server's baseline"
tasks = ["timezone", "users"]

[bundles.deploy]
tasks = ["@base", "deploy-app"]

[server_groups.prod]
tags = ["prod"]
tasks = ["@base"]
log_level = "warn"
envvars = { APP_ENV = "prod", REGION = "eu" }

[server_groups.prod.ssh]
username = "deploy"
identityfile = "./keys/prod"
port = 2200

[server_groups.monitored]
tags = ["prod", "metrics"]
tasks = ["monitoring", "users"]
log_level = "debug"

[server_groups.monitored.ssh]
port = 2201

[servers]
tasks = ["packages"]

[servers.hosts.web-01]
hostname = "web-01.internal"
groups = ["prod", "monitored"]
tags = ["web"]
add_tasks = ["app-files"]
skip_tasks = ["timezone"]
envvars = { REGION = "us" }

[servers.hosts.web-02]
hostname = "web-02.internal"
groups = ["prod"]
tags = ["web"]
log_level = "error"
tasks = ["@deploy"]
add_tasks = ["deploy-app", "@base"]

[servers.hosts.db-01]
hostname = "db-01.internal"
groups = ["prod"]
tags = ["db"]
add_tasks = ["deploy-db"]
skip_tasks = ["@base"]
    [servers.hosts.db-01.ssh]
    username = "postgres"

[servers.hosts.canary-01]
hostname = "canary-01.internal"
tags = ["web"]

[servers.hosts.local]
`

func loadGroups(t *testing.T) Config {
	t.Helper()
	var c Config
	if err := c.Load(strings.NewReader(groupsGorunfile)); err != nil {
		t.Fatal(err)
	}
	return c
}

func serverByName(t *testing.T, c Config, name string) Server {
	t.Helper()
	for _, s := range c.Servers {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no server %q", name)
	return Server{}
}

func TestConfig_Load_Groups(t *testing.T) {
	c := loadGroups(t)

	if got := c.Bundles["base"].Name; got != "base" {
		t.Errorf("bundle name should come from its key, got %q", got)
	}
	if got := c.Groups["prod"].Name; got != "prod" {
		t.Errorf("group name should come from its key, got %q", got)
	}

	tests := []struct {
		server string
		want   Server
	}{
		{
			// global, then each group in order, then add_tasks, minus skip_tasks; the
			// later group's port and log level win, the host's REGION wins
			server: "web-01",
			want: Server{
				Name:      "web-01",
				Hostname:  "web-01.internal",
				LogLevel:  "debug",
				Groups:    []string{"prod", "monitored"},
				Tags:      []string{"prod", "metrics", "web"},
				TaskNames: []string{"packages", "users", "monitoring", "app-files"},
				AddTasks:  []string{"app-files"},
				SkipTasks: []string{"timezone"},
				EnvVars:   map[string]string{"APP_ENV": "prod", "REGION": "us"},
				SSH:       SSH{User: "deploy", Identityfile: "./keys/prod", Port: 2201},
			},
		},
		{
			// the host's own tasks replace the global and group lists; repeats are dropped
			server: "web-02",
			want: Server{
				Name:      "web-02",
				Hostname:  "web-02.internal",
				LogLevel:  "error",
				Groups:    []string{"prod"},
				Tags:      []string{"prod", "web"},
				TaskNames: []string{"timezone", "users", "deploy-app"},
				AddTasks:  []string{"deploy-app", "@base"},
				EnvVars:   map[string]string{"APP_ENV": "prod", "REGION": "eu"},
				SSH:       SSH{User: "deploy", Identityfile: "./keys/prod", Port: 2200},
			},
		},
		{
			// skipping a bundle skips its tasks; the host's ssh username wins
			server: "db-01",
			want: Server{
				Name:      "db-01",
				Hostname:  "db-01.internal",
				LogLevel:  "warn",
				Groups:    []string{"prod"},
				Tags:      []string{"prod", "db"},
				TaskNames: []string{"packages", "deploy-db"},
				AddTasks:  []string{"deploy-db"},
				SkipTasks: []string{"@base"},
				EnvVars:   map[string]string{"APP_ENV": "prod", "REGION": "eu"},
				SSH:       SSH{User: "postgres", Identityfile: "./keys/prod", Port: 2200},
			},
		},
		{
			// no groups: unchanged, and still local
			server: "local",
			want:   Server{Name: "local", TaskNames: []string{"packages"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.server, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, serverByName(t, c, tt.server)); diff != "" {
				t.Errorf("server mismatch (-want +got):\n%s", diff)
			}
		})
	}

	// group identity files resolve relative to the config file like the host's own
	c.ResolvePaths("/etc/gorun")
	if got := serverByName(t, c, "web-01").SSH.Identityfile; got != "/etc/gorun/keys/prod" {
		t.Errorf("group identityfile should resolve relative to the config dir, got %q", got)
	}
}

func TestConfig_Load_GroupsErrors(t *testing.T) {
	const library = `
[tasks.packages]
cmd = "true"

[tasks.timezone]
cmd = "true"
depends_on = ["packages"]

[tasks.users]
cmd = "true"
`

	tests := []struct {
		name      string
		gorunfile string
		wantErr   string
	}{
		{
			name:      "unknown group",
			gorunfile: "[server_groups.prod]\n[servers.hosts.a]\ngroups = [\"prdo\"]\n",
			wantErr:   `server "a": unknown server group "prdo" (did you mean "prod"?)`,
		},
		{
			name:      "unknown add_tasks",
			gorunfile: "[servers.hosts.a]\nadd_tasks = [\"user\"]\n",
			wantErr:   `server "a": add_tasks: unknown task "user" (did you mean "users"?)`,
		},
		{
			name:      "unknown skip_tasks",
			gorunfile: "[servers.hosts.a]\nskip_tasks = [\"nope\"]\n",
			wantErr:   `server "a": skip_tasks: unknown task "nope"`,
		},
		{
			name:      "unknown bundle in a host list",
			gorunfile: "[bundles.base]\ntasks = [\"users\"]\n[servers.hosts.a]\ntasks = [\"@bsae\"]\n",
			wantErr:   `server "a": unknown bundle "@bsae" (did you mean "@base"?)`,
		},
		{
			name:      "unused bundle with an unknown task",
			gorunfile: "[bundles.base]\ntasks = [\"userz\"]\n",
			wantErr:   `bundle "base" lists unknown task "userz" (did you mean "users"?)`,
		},
		{
			name:      "bundle including an unknown bundle",
			gorunfile: "[bundles.base]\ntasks = [\"@nope\"]\n",
			wantErr:   `bundle "base" includes unknown bundle "@nope"`,
		},
		{
			name:      "bundle cycle",
			gorunfile: "[bundles.a]\ntasks = [\"@b\"]\n[bundles.b]\ntasks = [\"users\", \"@a\"]\n",
			wantErr:   `cyclic bundle detected: @a -> @b -> @a`,
		},
		{
			name:      "unused group with an unknown task",
			gorunfile: "[server_groups.prod]\ntasks = [\"nope\"]\n",
			wantErr:   `server group "prod": unknown task "nope"`,
		},
		{
			name:      "bundle named with the prefix",
			gorunfile: "[bundles.\"@base\"]\ntasks = [\"users\"]\n",
			wantErr:   `bundle "@base": write the name without "@"`,
		},
		{
			name:      "task named with the prefix",
			gorunfile: "[tasks.\"@x\"]\ncmd = \"true\"\n",
			wantErr:   `task "@x": task names must not start with "@"`,
		},
		{
			name:      "skipping a dependency",
			gorunfile: "[servers.hosts.a]\ntasks = [\"timezone\"]\nskip_tasks = [\"packages\"]\n",
			wantErr:   `server "a": skip_tasks: "packages" can't be skipped because task "timezone" depends on it`,
		},
		{
			name:      "typo in a group",
			gorunfile: "[server_groups.prod]\ntag = [\"x\"]\n",
			wantErr:   "unknown keys: server_groups.prod.tag",
		},
		{
			name:      "typo in a bundle",
			gorunfile: "[bundles.base]\ntask = [\"users\"]\n",
			wantErr:   "unknown keys: bundles.base.task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c Config
			err := c.Load(strings.NewReader(library + tt.gorunfile))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Load_UnknownHostTaskIsDeferred(t *testing.T) {
	// a typo in one host's list must not stop runs on the other hosts, so it is
	// reported when that host's pipeline is built rather than by Load
	var c Config
	err := c.Load(strings.NewReader(`
[tasks.timezone]
cmd = "true"

[servers.hosts.good]
tasks = ["timezone"]

[servers.hosts.bad]
tasks = ["timezon"]
skip_tasks = ["timezone"]
`))
	if err != nil {
		t.Fatalf("Load should accept an unknown host task, got %v", err)
	}

	if _, err := c.ServerPipeline(serverByName(t, c, "good")); err != nil {
		t.Errorf("good server: %v", err)
	}
	_, err = c.ServerPipeline(serverByName(t, c, "bad"))
	if want := `unknown task "timezon" (did you mean "timezone"?)`; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("got error %v, want it to contain %q", err, want)
	}
}

func TestResolvePathsExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	c := loadGroups(t)
	for i := range c.Servers {
		c.Servers[i].SSH.Identityfile = "~/.ssh/id_ed25519"
	}
	c.ResolvePaths("/etc/gorun")

	if got, want := c.Servers[0].SSH.Identityfile, home+"/.ssh/id_ed25519"; got != want {
		t.Errorf("identityfile = %q, want %q", got, want)
	}
}

func TestExpandTaskPatterns(t *testing.T) {
	c := loadGroups(t)

	tests := []struct {
		name     string
		patterns []string
		want     []string
		wantErr  string
	}{
		{name: "names keep their order", patterns: []string{"users", "packages"}, want: []string{"users", "packages"}},
		{name: "bundle", patterns: []string{"@base"}, want: []string{"timezone", "users"}},
		{name: "nested bundle", patterns: []string{"@deploy"}, want: []string{"timezone", "users", "deploy-app"}},
		{name: "glob", patterns: []string{"deploy-*"}, want: []string{"deploy-app", "deploy-db"}},
		{name: "bundle glob", patterns: []string{"@d*"}, want: []string{"timezone", "users", "deploy-app"}},
		{name: "repeats are dropped", patterns: []string{"users", "@base"}, want: []string{"users", "timezone"}},
		{name: "unknown name suggests", patterns: []string{"timezon"}, wantErr: `unknown task "timezon" (did you mean "timezone"?)`},
		{name: "unknown bundle suggests", patterns: []string{"@bsae"}, wantErr: `unknown bundle "@bsae" (did you mean "@base"?)`},
		{name: "glob without matches", patterns: []string{"nope-*"}, wantErr: `no tasks match "nope-*"`},
		{name: "bundle glob without matches", patterns: []string{"@nope-*"}, wantErr: `no bundles match "@nope-*"`},
		{name: "bad glob", patterns: []string{"[deploy"}, wantErr: `invalid pattern "[deploy"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.ExpandTaskPatterns(tt.patterns...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got error %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("tasks mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSelectServers(t *testing.T) {
	c := loadGroups(t)

	tests := []struct {
		name    string
		filter  ServerFilter
		want    []string
		wantErr string
	}{
		{name: "no filter", filter: ServerFilter{}, want: []string{"web-01", "web-02", "db-01", "canary-01", "local"}},
		{name: "tag", filter: ServerFilter{Tags: []string{"web"}}, want: []string{"web-01", "web-02", "canary-01"}},
		{name: "tags are and-ed", filter: ServerFilter{Tags: []string{"web", "prod"}}, want: []string{"web-01", "web-02"}},
		{name: "group tags count", filter: ServerFilter{Tags: []string{"metrics"}}, want: []string{"web-01"}},
		{name: "groups are and-ed", filter: ServerFilter{Groups: []string{"prod", "monitored"}}, want: []string{"web-01"}},
		{name: "tag and group", filter: ServerFilter{Tags: []string{"db"}, Groups: []string{"prod"}}, want: []string{"db-01"}},
		{name: "names are or-ed", filter: ServerFilter{Names: []string{"local", "db-01"}}, want: []string{"db-01", "local"}},
		{name: "glob", filter: ServerFilter{Names: []string{"web-*"}}, want: []string{"web-01", "web-02"}},
		{name: "exclude glob", filter: ServerFilter{Tags: []string{"web"}, Exclude: []string{"canary-*"}}, want: []string{"web-01", "web-02"}},
		{name: "exclude glob without matches", filter: ServerFilter{Exclude: []string{"nope-*"}}, want: []string{"web-01", "web-02", "db-01", "canary-01", "local"}},
		{name: "unknown server", filter: ServerFilter{Names: []string{"web-1"}}, wantErr: `unknown server "web-1" (did you mean "web-01"?)`},
		{name: "unknown excluded server", filter: ServerFilter{Exclude: []string{"lcoal"}}, wantErr: `unknown server "lcoal" (did you mean "local"?)`},
		{name: "glob without matches", filter: ServerFilter{Names: []string{"nope-*"}}, wantErr: `no servers match "nope-*"`},
		{name: "unknown tag", filter: ServerFilter{Tags: []string{"prdo"}}, wantErr: `unknown tag "prdo" (did you mean "prod"?)`},
		{name: "unknown group", filter: ServerFilter{Groups: []string{"monitor"}}, wantErr: `unknown server group "monitor" (did you mean "monitored"?)`},
		{name: "nothing left", filter: ServerFilter{Tags: []string{"db", "web"}}, wantErr: "no servers match the selection"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			servers, err := c.SelectServers(tt.filter)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got error %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, s := range servers {
				got = append(got, s.Name)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("servers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRunServerEnvVars(t *testing.T) {
	var c Config
	err := c.Load(strings.NewReader(`
envvars = { GLOBAL = "global", SHARED = "global", TASKWINS = "global" }

[tasks.env]
cmd = "echo $GLOBAL $SHARED $TASKWINS $GROUP"
envvars = { TASKWINS = "task" }

[server_groups.g]
envvars = { SHARED = "group", GROUP = "group", TASKWINS = "group" }

[servers.hosts.a]
groups = ["g"]
tasks = ["env"]

[servers.hosts.b]
tasks = ["env"]
`))
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	stdout := map[string]string{}
	c.OnEvent = func(event any) {
		if ev, ok := event.(TaskEvent); ok && ev.State == TaskDone {
			mu.Lock()
			stdout[ev.Server] = strings.TrimSpace(ev.Stdout)
			mu.Unlock()
		}
	}

	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// task envvars win, then the server's (and its groups'), then the global ones;
	// a group's envvars don't leak to servers outside it
	want := map[string]string{
		"a": "global group task group",
		"b": "global global task",
	}
	if diff := cmp.Diff(want, stdout); diff != "" {
		t.Errorf("stdout mismatch (-want +got):\n%s", diff)
	}
}

func TestServerPipelineExpandsBundles(t *testing.T) {
	c := loadGroups(t)

	server, err := c.ServerPipeline(Server{Name: "x", TaskNames: []string{"@base"}})
	if err != nil {
		t.Fatal(err)
	}

	got := []string{}
	for _, task := range server.Tasks {
		got = append(got, task.Name)
	}
	if diff := cmp.Diff([]string{"packages", "timezone", "users"}, got); diff != "" {
		t.Errorf("pipeline mismatch (-want +got):\n%s", diff)
	}
}
