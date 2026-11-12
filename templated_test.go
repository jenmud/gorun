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

// The case the templated keys exist for: a directory named by a server var, created when
// missing, and a file copied into it, both skipped once they are in place.
func TestTemplatedCheckVerifyDstAndEnvvars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "conf")
	vars := map[string]any{"config": dir}
	tasks := []Task{
		{
			Name:           "config-dir",
			CheckTemplate:  "test -d {{ .Server.Vars.config }}",
			CmdTemplate:    "mkdir -p {{ .Server.Vars.config }}",
			VerifyTemplate: "test -d {{ .Server.Vars.config }}",
		},
		{
			Name:            "compose",
			DependsOn:       []string{"config-dir"},
			CheckCopies:     true,
			Copy:            []Copy{{Content: "services: {}\n", DstTpl: "{{ .Server.Vars.config }}/docker-compose.yml"}},
			EnvVarsTemplate: map[string]string{"compose_file": "{{ .Server.Vars.config }}/docker-compose.yml"},
			Cmd:             `cat "$COMPOSE_FILE"`,
		},
	}

	events, err := runOnServer(t, vars, tasks...)
	if err != nil {
		ev := final(events, "compose")
		t.Fatalf("first run: %v (%v, stderr %q)", err, ev.Err, ev.Stderr)
	}
	ev := final(events, "config-dir")
	if ev.State != TaskDone || ev.Check == nil || ev.Check.Command != "test -d "+dir || ev.Verify == nil || ev.Verify.Command != "test -d "+dir {
		t.Fatalf("config-dir = state %d, check %+v, verify %+v; want it run with rendered check and verify", ev.State, ev.Check, ev.Verify)
	}
	if ev := final(events, "compose"); ev.State != TaskDone || ev.Stdout != "services: {}\n" {
		t.Fatalf("compose = state %d, stdout %q; want the copied file read through $COMPOSE_FILE", ev.State, ev.Stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		t.Fatalf("copy did not land at the rendered dst: %v", err)
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

func TestForServerRendersPerServerWithoutSharingState(t *testing.T) {
	task := Task{
		Name:            "t",
		Cmd:             "true",
		CheckTemplate:   "check {{ .Name }}",
		VerifyTemplate:  "verify {{ .Server.Vars.v }}",
		Copy:            []Copy{{Content: "x", DstTpl: "/srv/{{ .Name }}/x"}, {Content: "y", Dst: "/plain"}},
		EnvVars:         map[string]string{"a": "1"},
		EnvVarsTemplate: map[string]string{"b": "{{ .Name }}"},
	}
	one, err := task.ForServer(&Server{Name: "one", Vars: map[string]any{"v": 1}})
	if err != nil {
		t.Fatal(err)
	}
	two, err := task.ForServer(&Server{Name: "two", Vars: map[string]any{"v": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if one.Check != "check one" || one.Verify != "verify 1" || one.Copy[0].Dst != "/srv/one/x" || one.Copy[1].Dst != "/plain" {
		t.Errorf("one = check %q, verify %q, copies %+v", one.Check, one.Verify, one.Copy)
	}
	if two.Check != "check two" || two.Copy[0].Dst != "/srv/two/x" || two.EnvVars["B"] != "two" || two.EnvVars["A"] != "1" {
		t.Errorf("two = check %q, copies %+v, env %v", two.Check, two.Copy, two.EnvVars)
	}
	if one.CheckTemplate != "" || one.Copy[0].DstTpl != "" || one.EnvVarsTemplate != nil {
		t.Errorf("rendered task still carries templates: %+v", one)
	}
	if task.Copy[0].Dst != "" || task.Copy[0].DstTpl == "" || len(task.EnvVars) != 1 {
		t.Errorf("ForServer changed the task it was called on: %+v", task)
	}
	// rendering again is a no-op, so a planned task handed to NewTaskExec is unchanged
	again, err := one.ForServer(&Server{Name: "other"})
	if err != nil || again.Check != "check one" || again.Copy[0].Dst != "/srv/one/x" {
		t.Errorf("second render = %+v, %v", again, err)
	}
}

func TestForServerRefusesAKeyAndItsTemplateTogether(t *testing.T) {
	for name, task := range map[string]Task{
		"check":   {Name: "t", Check: "a", CheckTemplate: "b"},
		"verify":  {Name: "t", Verify: "a", VerifyTemplate: "b"},
		"dst":     {Name: "t", Copy: []Copy{{Content: "x", Dst: "/a", DstTpl: "/b"}}},
		"envvars": {Name: "t", EnvVars: map[string]string{"x": "1"}, EnvVarsTemplate: map[string]string{"X": "2"}},
		"empty":   {Name: "t", Copy: []Copy{{Content: "x", DstTpl: "{{ if false }}x{{ end }}"}}},
		"parse":   {Name: "t", CheckTemplate: "{{ .Name"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := task.ForServer(&Server{Name: "s"}); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestTemplatedTaskThatCannotRenderFailsTheRun(t *testing.T) {
	_, err := runOnServer(t, nil, Task{Name: "bad", Cmd: "true", VerifyTemplate: "{{ .Name.Field }}"})
	if err == nil || !strings.Contains(err.Error(), "verify template") {
		t.Fatalf("err = %v, want the verify template named", err)
	}
}
