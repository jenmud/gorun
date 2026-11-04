package gorun

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func localRuleConfig() Config {
	return Config{
		Tasks: map[string]Task{
			"remote":       {Name: "remote", Cmd: "true"},
			"here":         {Name: "here", Cmd: "true", Local: true},
			"here-after":   {Name: "here-after", Cmd: "true", Local: true, DependsOn: []string{"remote"}},
			"remote-after": {Name: "remote-after", Cmd: "true", DependsOn: []string{"here"}},
		},
	}
}

func pipelineNames(t *testing.T, c Config, server Server) []string {
	t.Helper()
	server, err := c.ServerPipeline(server)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, task := range server.Tasks {
		got = append(got, task.Name)
	}
	return got
}

func TestServerPipelineLocalServerRunsOnlyLocalTasks(t *testing.T) {
	c := localRuleConfig()

	got := pipelineNames(t, c, Server{Name: "local", TaskNames: []string{"remote", "here"}})
	if diff := cmp.Diff([]string{"here"}, got); diff != "" {
		t.Errorf("pipeline mismatch (-want +got):\n%s", diff)
	}
}

func TestServerPipelineLocalServerDropsDependentsOfRemoteTasks(t *testing.T) {
	c := localRuleConfig()

	got := pipelineNames(t, c, Server{Name: "local", TaskNames: []string{"here-after", "here"}})
	if diff := cmp.Diff([]string{"here"}, got); diff != "" {
		t.Errorf("pipeline mismatch (-want +got):\n%s", diff)
	}
}

func TestServerPipelineSSHServerRunsOnlyRemoteTasks(t *testing.T) {
	c := localRuleConfig()

	// never dialled: ServerPipeline only builds the task list
	server := Server{
		Name:      "web",
		Hostname:  "web.internal",
		SSH:       SSH{User: "deploy", Port: 2222},
		TaskNames: []string{"remote", "here", "remote-after"},
	}
	got := pipelineNames(t, c, server)
	if diff := cmp.Diff([]string{"remote"}, got); diff != "" {
		t.Errorf("pipeline mismatch (-want +got):\n%s", diff)
	}
}

func TestRunTaskRemoteTaskWithOnlyLocalTargets(t *testing.T) {
	c := Config{Servers: []Server{{Name: "local"}}}

	err := c.RunTask(context.Background(), Task{Name: "remote", Cmd: "true"})
	if err == nil {
		t.Fatal("expected RunTask to fail")
	}
	if !strings.Contains(err.Error(), "local = true") {
		t.Errorf("error = %q, want it to mention local = true", err)
	}
}

func TestServerPipelineDropsDependentsAcrossLevels(t *testing.T) {
	c := Config{
		Tasks: map[string]Task{
			"a":    {Name: "a", Cmd: "true"},
			"b":    {Name: "b", Cmd: "true", Local: true, DependsOn: []string{"a"}},
			"c":    {Name: "c", Cmd: "true", Local: true, DependsOn: []string{"b"}},
			"here": {Name: "here", Cmd: "true", Local: true},
		},
	}

	got := pipelineNames(t, c, Server{Name: "local", TaskNames: []string{"c", "here"}})
	if diff := cmp.Diff([]string{"here"}, got); diff != "" {
		t.Errorf("pipeline mismatch (-want +got):\n%s", diff)
	}
}

func TestServerPipelineGroupSSHMakesServerRemote(t *testing.T) {
	c := Config{}
	err := c.Load(strings.NewReader(`
[tasks.remote]
cmd = "true"
[tasks.here]
cmd = "true"
local = true

[server_groups.app]
tasks = ["remote", "here"]
    [server_groups.app.ssh]
    username = "deploy"

[servers.hosts.web]
hostname = "web.internal"
groups = ["app"]
`))
	if err != nil {
		t.Fatal(err)
	}
	servers, err := c.ServersPipeline()
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, task := range servers["web"].Tasks {
		got = append(got, task.Name)
	}
	if diff := cmp.Diff([]string{"remote"}, got); diff != "" {
		t.Errorf("pipeline mismatch (-want +got):\n%s", diff)
	}
}

func TestRunTaskLocalTaskWithOnlySSHTargets(t *testing.T) {
	c := Config{Servers: []Server{{Name: "web", Hostname: "web.internal", SSH: SSH{User: "deploy"}}}}

	err := c.RunTask(context.Background(), Task{Name: "here", Cmd: "true", Local: true})
	if err == nil || !strings.Contains(err.Error(), "only runs locally") {
		t.Errorf("error = %v, want it to say the task only runs locally", err)
	}
}

func TestRunLocalWithOnlyRemoteTasksReturnsErrNoTasks(t *testing.T) {
	c := localRuleConfig()

	if err := c.RunLocal(context.Background(), "remote"); !errors.Is(err, ErrNoTasks) {
		t.Errorf("error = %v, want ErrNoTasks", err)
	}
}

// TestRunSkipsServersWithNoTasks checks a server left with no tasks of its kind is
// never dialled: the SSH server points at a closed port, so dialling it would fail.
func TestRunSkipsServersWithNoTasks(t *testing.T) {
	c := localRuleConfig()

	unreachable := Server{Name: "web", Hostname: "127.0.0.1", SSH: SSH{User: "deploy", Port: 1}, TaskNames: []string{"here"}}
	local := Server{Name: "local", TaskNames: []string{"here"}}
	if err := c.Run(context.Background(), unreachable, local); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
