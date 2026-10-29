package gorun

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestTaskTimeoutDuration(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{name: "default", want: DefaultTaskTimeout},
		{name: "override", timeout: 2 * time.Minute, want: 2 * time.Minute},
		{name: "negative falls back to default", timeout: -time.Second, want: DefaultTaskTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Timeout: tt.timeout}
			if got := task.TimeoutDuration(); got != tt.want {
				t.Errorf("TimeoutDuration() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLoadTaskTimeout(t *testing.T) {
	var c Config
	err := c.Load(strings.NewReader(`
[tasks.slow]
cmd = "sleep 10"
timeout = "2m"

[tasks.default]
cmd = "true"
`))
	if err != nil {
		t.Fatal(err)
	}

	if got := c.Tasks["slow"].Timeout; got != 2*time.Minute {
		t.Errorf("slow timeout = %s, want 2m", got)
	}
	if got := c.Tasks["default"].Timeout; got != 0 {
		t.Errorf("default timeout = %s, want 0 (unset)", got)
	}
	if got := c.Tasks["default"].TimeoutDuration(); got != DefaultTaskTimeout {
		t.Errorf("default TimeoutDuration() = %s, want %s", got, DefaultTaskTimeout)
	}
}

func TestTaskValidateRejectsNegativeTimeout(t *testing.T) {
	task := Task{Name: "bad", Cmd: "true", Timeout: -time.Second}
	if err := task.validate(); err == nil {
		t.Fatal("expected an error for a negative timeout")
	}
}

func TestRunEmitsTimeout(t *testing.T) {
	old := logOutput
	SetLogOutput(io.Discard)
	t.Cleanup(func() { SetLogOutput(old) })

	var failed *TaskEvent
	cfg := Config{
		Tasks: map[string]Task{
			"slow": {Name: "slow", Cmd: "echo working >&2; sleep 5", Timeout: 50 * time.Millisecond},
		},
		Servers: []Server{{Name: "local", TaskNames: []string{"slow"}}},
		OnEvent: func(event any) {
			if ev, ok := event.(TaskEvent); ok && ev.State == TaskFailed {
				failed = &ev
			}
		},
	}

	if err := cfg.Run(context.Background()); err == nil {
		t.Fatal("expected Run to fail")
	}
	if failed == nil {
		t.Fatal("expected a failed task event")
	}
	if failed.ExitCode != 124 {
		t.Errorf("exit code = %d, want 124", failed.ExitCode)
	}
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "timed out after 50ms") {
		t.Errorf("error = %v, want it to mention the timeout", failed.Err)
	}
	if !strings.Contains(failed.Stderr, "timed out after 50ms") {
		t.Errorf("stderr = %q, want it to include the timeout reason", failed.Stderr)
	}
}

func TestLocalExecuteTimeout(t *testing.T) {
	old := logOutput
	SetLogOutput(io.Discard)
	t.Cleanup(func() { SetLogOutput(old) })

	tasks, err := NewTaskExec(nil, Task{Name: "slow", Cmd: "echo starting; sleep 5; echo done", Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	te := tasks[0]

	started := time.Now()
	err = te.LocalExecute(context.Background())
	if err == nil {
		t.Fatal("expected the task to time out")
	}
	if !strings.Contains(err.Error(), "timed out after 50ms") {
		t.Errorf("error = %q, want it to mention the timeout", err)
	}
	if te.exitCode != 124 {
		t.Errorf("exit code = %d, want 124", te.exitCode)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("task took %s to time out, want less than 2s", elapsed)
	}
}
