package gorun

import (
	"context"
	"strings"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("GORUN_TEST_TOKEN", "s3cret")

	tests := map[string]string{
		"${GORUN_TEST_TOKEN}":      "s3cret",
		"$GORUN_TEST_TOKEN":        "s3cret",
		"pre-${GORUN_TEST_TOKEN}!": "pre-s3cret!",
		"${GORUN_TEST_UNSET}":      "",
		"cost $$5":                 "cost $5",
		"plain":                    "plain",
	}
	for in, want := range tests {
		if got := expandEnv(in); got != want {
			t.Errorf("expandEnv(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnvLine(t *testing.T) {
	got, err := envLine(map[string]string{"token": "it's $HOME", "A": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `export A='1' TOKEN='it'\''s $HOME'`; got != want {
		t.Errorf("envLine = %q, want %q", got, want)
	}

	for _, vars := range []map[string]string{{"A": "two\nlines"}, {"BAD-NAME": "x"}, {"1A": "x"}} {
		if _, err := envLine(vars); err == nil {
			t.Errorf("envLine(%v) succeeded, want an error", vars)
		}
	}
}

func TestLocalExecute_EnvVars(t *testing.T) {
	secret := `it's "$HOME" & ; $(echo no)`
	task := Task{
		Name:    "env",
		Cmd:     `printf '%s|%s' "$TOKEN" "$OTHER"`,
		EnvVars: map[string]string{"token": secret, "OTHER": "x"},
	}

	execs, err := NewTaskExec(nil, task)
	if err != nil {
		t.Fatal(err)
	}
	if err := execs[0].Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v (stderr %q)", err, execs[0].stderr)
	}

	if got, want := string(execs[0].stdout), secret+"|x"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if strings.Contains(execs[0].command, "it's") || strings.Contains(execs[0].command, "__gorun_env") {
		t.Errorf("logged command leaks the envvars: %q", execs[0].command)
	}
}

func TestLocalExecute_EnvVarsCustomShell(t *testing.T) {
	tests := []struct {
		name  string
		shell string
		cmd   string
	}{
		{"xtrace shell does not trace the values", "bash -x -c", `printenv TOKEN`},
		{"non-sh shell", "python3 -c", `import os, sys; sys.stdout.write(os.environ["TOKEN"])`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Name: "env", Shell: tt.shell, Cmd: tt.cmd, EnvVars: map[string]string{"TOKEN": "s3cret"}}
			execs, err := NewTaskExec(nil, task)
			if err != nil {
				t.Fatal(err)
			}
			if err := execs[0].Execute(context.Background()); err != nil {
				t.Fatalf("Execute: %v (stderr %q)", err, execs[0].stderr)
			}
			if got := strings.TrimSpace(string(execs[0].stdout)); got != "s3cret" {
				t.Errorf("stdout = %q, want %q", got, "s3cret")
			}
			if strings.Contains(string(execs[0].stderr), "s3cret") {
				t.Errorf("stderr leaks the value: %q", execs[0].stderr)
			}
		})
	}
}

func TestConfig_Run_TaskEnvVarsWinRegardlessOfCase(t *testing.T) {
	cfg := Config{}
	err := cfg.Load(strings.NewReader(`
envvars = { token = "global" }

[tasks.show]
cmd = 'printf "%s" "$TOKEN"'
[tasks.show.envvars]
TOKEN = "task"

[servers]
tasks = ["show"]
[servers.hosts.local]
`))
	if err != nil {
		t.Fatal(err)
	}

	var got string
	cfg.OnEvent = func(event any) {
		if ev, ok := event.(TaskEvent); ok && ev.State == TaskDone {
			got = ev.Stdout
		}
	}
	t.Setenv("GORUN_SKIP_NATS", "1")
	if err := cfg.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "task" {
		t.Errorf("TOKEN = %q, want the task value %q", got, "task")
	}
}
