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

func TestLoadTaskMetadata(t *testing.T) {
	var c Config
	err := c.Load(strings.NewReader(`
[tasks.deploy]
cmd = "deploy"
description = "Deploy the app"
details = """
Stops the service, swaps the binary and starts it again.
"""
rerun = "unsafe"
`))
	if err != nil {
		t.Fatal(err)
	}

	task := c.Tasks["deploy"]
	if task.Description != "Deploy the app" {
		t.Errorf("description = %q, want %q", task.Description, "Deploy the app")
	}
	if !strings.Contains(task.Details, "swaps the binary") {
		t.Errorf("details = %q, want the long description", task.Details)
	}
	if task.Rerun != RerunUnsafe {
		t.Errorf("rerun = %q, want %q", task.Rerun, RerunUnsafe)
	}
}

func TestTaskValidateRerun(t *testing.T) {
	tests := []struct {
		name    string
		rerun   string
		wantErr bool
	}{
		{name: "unset", rerun: ""},
		{name: "safe", rerun: RerunSafe},
		{name: "unsafe", rerun: RerunUnsafe},
		{name: "typo", rerun: "save", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Name: "task", Cmd: "true", Rerun: tt.rerun}
			err := task.validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "rerun must be") {
				t.Errorf("error = %q, want it to explain the rerun values", err)
			}
		})
	}
}

func TestRunEmitsTaskMetadataInPlan(t *testing.T) {
	old := logOutput
	SetLogOutput(io.Discard)
	t.Cleanup(func() { SetLogOutput(old) })

	var plan *PlanEvent
	cfg := Config{
		Tasks: map[string]Task{
			"deploy": {
				Name:        "deploy",
				Cmd:         "true",
				Description: "Deploy the app",
				Details:     "Replaces the running binary.",
				Rerun:       RerunUnsafe,
			},
		},
		Servers: []Server{{Name: "local", TaskNames: []string{"deploy"}}},
		OnEvent: func(event any) {
			if ev, ok := event.(PlanEvent); ok {
				plan = &ev
			}
		},
	}

	if err := cfg.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if plan == nil {
		t.Fatal("expected a plan event")
	}
	if len(plan.TaskMeta) != 1 {
		t.Fatalf("task metadata = %v, want one entry", plan.TaskMeta)
	}
	meta := plan.TaskMeta[0]
	if meta.Name != "deploy" || meta.Description != "Deploy the app" || meta.Details != "Replaces the running binary." || meta.Rerun != RerunUnsafe {
		t.Errorf("task metadata = %+v, want the task's description, details and rerun value", meta)
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

func TestTaskAttempts(t *testing.T) {
	tests := []struct {
		name    string
		retries int
		want    int
	}{
		{name: "default", retries: 0, want: 1},
		{name: "two retries", retries: 2, want: 3},
		{name: "negative falls back to one", retries: -1, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Retries: tt.retries}
			if got := task.Attempts(); got != tt.want {
				t.Errorf("Attempts() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestLoadTaskRetries(t *testing.T) {
	var c Config
	if err := c.Load(strings.NewReader(`
[tasks.flaky]
cmd = "true"
retries = 2

[tasks.once]
cmd = "true"
`)); err != nil {
		t.Fatal(err)
	}

	if got := c.Tasks["flaky"].Retries; got != 2 {
		t.Errorf("flaky retries = %d, want 2", got)
	}
	if got := c.Tasks["flaky"].Attempts(); got != 3 {
		t.Errorf("flaky Attempts() = %d, want 3", got)
	}
	if got := c.Tasks["once"].Attempts(); got != 1 {
		t.Errorf("once Attempts() = %d, want 1", got)
	}
}

func TestTaskValidateRejectsNegativeRetries(t *testing.T) {
	task := Task{Name: "bad", Cmd: "true", Retries: -1}
	err := task.validate()
	if err == nil {
		t.Fatal("expected an error for negative retries")
	}
	if !strings.Contains(err.Error(), "retries must not be negative") {
		t.Errorf("error = %q, want it to mention retries", err)
	}
}

func TestRunRetriesUntilSuccess(t *testing.T) {
	old := logOutput
	SetLogOutput(io.Discard)
	t.Cleanup(func() { SetLogOutput(old) })

	dir := t.TempDir()
	counter := dir + "/count"
	cmd := strings.Join([]string{
		"n=0",
		"[ -f " + counter + " ] && n=$(cat " + counter + ")",
		"n=$((n+1))",
		"echo $n > " + counter,
		"echo attempt $n",
		"[ $n -ge 3 ]",
	}, "; ")

	var (
		running []TaskEvent
		final   *TaskEvent
		done    *ProcessFinishedEvent
	)
	cfg := Config{
		Tasks: map[string]Task{
			"flaky": {Name: "flaky", Cmd: cmd, Retries: 2},
		},
		Servers: []Server{{Name: "local", TaskNames: []string{"flaky"}}},
		OnEvent: func(event any) {
			switch ev := event.(type) {
			case TaskEvent:
				if ev.State == TaskRunning {
					running = append(running, ev)
				}
				if ev.State == TaskDone {
					final = &ev
				}
			case ProcessFinishedEvent:
				done = &ev
			}
		},
	}

	if err := cfg.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(running) != 3 {
		t.Fatalf("running events = %d, want 3 attempts", len(running))
	}
	for i, ev := range running {
		if ev.Attempt != i+1 || ev.Attempts != 3 {
			t.Errorf("running event %d = attempt %d/%d, want %d/3", i, ev.Attempt, ev.Attempts, i+1)
		}
	}
	if running[1].Err == nil || !strings.Contains(running[1].Err.Error(), "exit status 1") {
		t.Errorf("second running event error = %v, want the previous attempt's failure", running[1].Err)
	}
	if final == nil {
		t.Fatal("expected the task to succeed on the third attempt")
	}
	if final.Attempt != 3 || final.Attempts != 3 {
		t.Errorf("final attempt = %d/%d, want 3/3", final.Attempt, final.Attempts)
	}
	if done == nil || !done.Success || done.Failed != 0 {
		t.Errorf("process finished = %+v, want success with no failures", done)
	}
}

func TestRunRetriesExhaustedCountsOneFailure(t *testing.T) {
	old := logOutput
	SetLogOutput(io.Discard)
	t.Cleanup(func() { SetLogOutput(old) })

	var (
		running []TaskEvent
		final   *TaskEvent
		done    *ProcessFinishedEvent
	)
	cfg := Config{
		Tasks: map[string]Task{
			"broken": {Name: "broken", Cmd: "exit 7", Retries: 2},
		},
		Servers: []Server{{Name: "local", TaskNames: []string{"broken"}}},
		OnEvent: func(event any) {
			switch ev := event.(type) {
			case TaskEvent:
				if ev.State == TaskRunning {
					running = append(running, ev)
				}
				if ev.State == TaskFailed {
					final = &ev
				}
			case ProcessFinishedEvent:
				done = &ev
			}
		},
	}

	if err := cfg.Run(context.Background()); err == nil {
		t.Fatal("expected Run to fail")
	}
	if len(running) != 3 {
		t.Fatalf("running events = %d, want 3 attempts", len(running))
	}
	if final == nil {
		t.Fatal("expected a failed task event")
	}
	if final.Attempt != 3 || final.Attempts != 3 {
		t.Errorf("final attempt = %d/%d, want 3/3", final.Attempt, final.Attempts)
	}
	if final.ExitCode != 7 {
		t.Errorf("exit code = %d, want 7", final.ExitCode)
	}
	if done == nil || done.Success || done.Failed != 1 {
		t.Errorf("process finished = %+v, want one failed task regardless of attempts", done)
	}
}

func TestRunDoesNotRetryTimeouts(t *testing.T) {
	old := logOutput
	SetLogOutput(io.Discard)
	t.Cleanup(func() { SetLogOutput(old) })

	var (
		running []TaskEvent
		final   *TaskEvent
	)
	cfg := Config{
		Tasks: map[string]Task{
			"slow": {Name: "slow", Cmd: "sleep 5", Timeout: 50 * time.Millisecond, Retries: 3},
		},
		Servers: []Server{{Name: "local", TaskNames: []string{"slow"}}},
		OnEvent: func(event any) {
			if ev, ok := event.(TaskEvent); ok {
				switch ev.State {
				case TaskRunning:
					running = append(running, ev)
				case TaskFailed:
					final = &ev
				}
			}
		},
	}

	if err := cfg.Run(context.Background()); err == nil {
		t.Fatal("expected Run to fail")
	}
	if len(running) != 1 {
		t.Errorf("running events = %d, want 1 (timeouts are not retried)", len(running))
	}
	if final == nil {
		t.Fatal("expected a failed task event")
	}
	if final.ExitCode != 124 {
		t.Errorf("exit code = %d, want 124", final.ExitCode)
	}
	if final.Attempt != 1 || final.Attempts != 4 {
		t.Errorf("final attempt = %d/%d, want 1/4", final.Attempt, final.Attempts)
	}
}

func TestRunWithoutRetriesOmitsAttemptInfo(t *testing.T) {
	old := logOutput
	SetLogOutput(io.Discard)
	t.Cleanup(func() { SetLogOutput(old) })

	var final *TaskEvent
	cfg := Config{
		Tasks: map[string]Task{
			"once": {Name: "once", Cmd: "true"},
		},
		Servers: []Server{{Name: "local", TaskNames: []string{"once"}}},
		OnEvent: func(event any) {
			if ev, ok := event.(TaskEvent); ok && ev.State == TaskDone {
				final = &ev
			}
		},
	}

	if err := cfg.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if final == nil {
		t.Fatal("expected a done task event")
	}
	if final.Attempt != 0 || final.Attempts != 0 {
		t.Errorf("attempt = %d/%d, want both unset when retries are not configured", final.Attempt, final.Attempts)
	}
}

func TestTaskRetryDelayDuration(t *testing.T) {
	if got := (Task{}).RetryDelayDuration(); got != DefaultRetryDelay {
		t.Errorf("unset RetryDelayDuration() = %s, want %s", got, DefaultRetryDelay)
	}

	zero := time.Duration(0)
	if got := (Task{RetryDelay: &zero}).RetryDelayDuration(); got != 0 {
		t.Errorf("explicit zero RetryDelayDuration() = %s, want 0", got)
	}

	delay := 2 * time.Second
	if got := (Task{RetryDelay: &delay}).RetryDelayDuration(); got != delay {
		t.Errorf("RetryDelayDuration() = %s, want %s", got, delay)
	}
}

func TestLoadTaskRetryDelay(t *testing.T) {
	var c Config
	if err := c.Load(strings.NewReader(`
[tasks.delayed]
cmd = "true"
retries = 1
retry_delay = "1s"

[tasks.default]
cmd = "true"
`)); err != nil {
		t.Fatal(err)
	}

	delayed := c.Tasks["delayed"]
	if delayed.RetryDelay == nil || *delayed.RetryDelay != time.Second {
		t.Errorf("delayed retry_delay = %v, want 1s", delayed.RetryDelay)
	}
	if got := c.Tasks["default"].RetryDelayDuration(); got != DefaultRetryDelay {
		t.Errorf("default RetryDelayDuration() = %s, want %s", got, DefaultRetryDelay)
	}
}

func TestTaskValidateRejectsNegativeRetryDelay(t *testing.T) {
	delay := -time.Second
	task := Task{Name: "bad", Cmd: "true", RetryDelay: &delay}
	err := task.validate()
	if err == nil {
		t.Fatal("expected an error for a negative retry delay")
	}
	if !strings.Contains(err.Error(), "retry_delay must not be negative") {
		t.Errorf("error = %q, want it to mention retry_delay", err)
	}
}

func TestTaskWaitForRetry(t *testing.T) {
	delay := 5 * time.Second
	te := &TaskExec{task: Task{RetryDelay: &delay}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if te.waitForRetry(ctx) {
		t.Error("waitForRetry returned true for a canceled context")
	}

	zero := time.Duration(0)
	te = &TaskExec{task: Task{RetryDelay: &zero}}
	if !te.waitForRetry(context.Background()) {
		t.Error("waitForRetry returned false for an active context")
	}
}

func TestRunWaitsBetweenRetries(t *testing.T) {
	old := logOutput
	SetLogOutput(io.Discard)
	t.Cleanup(func() { SetLogOutput(old) })

	delay := 80 * time.Millisecond
	var starts []time.Time
	cfg := Config{
		Tasks: map[string]Task{
			"broken": {Name: "broken", Cmd: "exit 1", Retries: 1, RetryDelay: &delay},
		},
		Servers: []Server{{Name: "local", TaskNames: []string{"broken"}}},
		OnEvent: func(event any) {
			if ev, ok := event.(TaskEvent); ok && ev.State == TaskRunning {
				starts = append(starts, time.Now())
			}
		},
	}

	if err := cfg.Run(context.Background()); err == nil {
		t.Fatal("expected Run to fail")
	}
	if len(starts) != 2 {
		t.Fatalf("running events = %d, want 2 attempts", len(starts))
	}
	if gap := starts[1].Sub(starts[0]); gap < delay {
		t.Errorf("gap between attempts = %s, want at least %s", gap, delay)
	}
}

func TestTaskCanRetry(t *testing.T) {
	tests := []struct {
		name        string
		rerun       string
		retryUnsafe bool
		want        bool
	}{
		{name: "unset", rerun: "", want: true},
		{name: "safe", rerun: RerunSafe, want: true},
		{name: "unsafe", rerun: RerunUnsafe, want: false},
		{name: "unsafe with override", rerun: RerunUnsafe, retryUnsafe: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Rerun: tt.rerun, RetryUnsafe: tt.retryUnsafe}
			if got := task.CanRetry(); got != tt.want {
				t.Errorf("CanRetry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadTaskRetryUnsafe(t *testing.T) {
	var c Config
	if err := c.Load(strings.NewReader(`
[tasks.override]
cmd = "true"
rerun = "unsafe"
retries = 1
retry_unsafe = true
`)); err != nil {
		t.Fatal(err)
	}

	task := c.Tasks["override"]
	if !task.RetryUnsafe {
		t.Error("retry_unsafe = false, want true")
	}
	if !task.CanRetry() {
		t.Error("CanRetry() = false, want true with retry_unsafe set")
	}
}

func TestRunDoesNotRetryUnsafeTask(t *testing.T) {
	old := logOutput
	SetLogOutput(io.Discard)
	t.Cleanup(func() { SetLogOutput(old) })

	var (
		running []TaskEvent
		final   *TaskEvent
		done    *ProcessFinishedEvent
	)
	cfg := Config{
		Tasks: map[string]Task{
			"rotate": {
				Name:    "rotate",
				Cmd:     "echo boom >&2; exit 9",
				Rerun:   RerunUnsafe,
				Retries: 2,
			},
		},
		Servers: []Server{{Name: "local", TaskNames: []string{"rotate"}}},
		OnEvent: func(event any) {
			switch ev := event.(type) {
			case TaskEvent:
				if ev.State == TaskRunning {
					running = append(running, ev)
				}
				if ev.State == TaskFailed {
					final = &ev
				}
			case ProcessFinishedEvent:
				done = &ev
			}
		},
	}

	if err := cfg.Run(context.Background()); err == nil {
		t.Fatal("expected Run to fail")
	}
	if len(running) != 1 {
		t.Errorf("running events = %d, want 1 (unsafe tasks are not retried)", len(running))
	}
	if final == nil {
		t.Fatal("expected a failed task event")
	}
	if final.ExitCode != 9 {
		t.Errorf("exit code = %d, want 9", final.ExitCode)
	}
	if !strings.Contains(final.Stderr, "not retried: task is marked unsafe to rerun") {
		t.Errorf("stderr = %q, want it to explain why the task was not retried", final.Stderr)
	}
	if !strings.Contains(final.Stderr, "retry_unsafe = true") {
		t.Errorf("stderr = %q, want it to mention the retry_unsafe override", final.Stderr)
	}
	if done == nil || done.Success || done.Failed != 1 {
		t.Errorf("process finished = %+v, want one failed task", done)
	}
}

func TestRunRetriesUnsafeWhenOverridden(t *testing.T) {
	old := logOutput
	SetLogOutput(io.Discard)
	t.Cleanup(func() { SetLogOutput(old) })

	dir := t.TempDir()
	counter := dir + "/count"
	cmd := strings.Join([]string{
		"n=0",
		"[ -f " + counter + " ] && n=$(cat " + counter + ")",
		"n=$((n+1))",
		"echo $n > " + counter,
		"[ $n -ge 2 ]",
	}, "; ")

	var (
		running []TaskEvent
		final   *TaskEvent
	)
	zero := time.Duration(0)
	cfg := Config{
		Tasks: map[string]Task{
			"rotate": {
				Name:        "rotate",
				Cmd:         cmd,
				Rerun:       RerunUnsafe,
				Retries:     1,
				RetryDelay:  &zero,
				RetryUnsafe: true,
			},
		},
		Servers: []Server{{Name: "local", TaskNames: []string{"rotate"}}},
		OnEvent: func(event any) {
			if ev, ok := event.(TaskEvent); ok {
				if ev.State == TaskRunning {
					running = append(running, ev)
				}
				if ev.State == TaskDone {
					final = &ev
				}
			}
		},
	}

	if err := cfg.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(running) != 2 {
		t.Fatalf("running events = %d, want 2 attempts", len(running))
	}
	if final == nil || final.Attempt != 2 || final.Attempts != 2 {
		t.Errorf("final event = %+v, want success on attempt 2/2", final)
	}
}
