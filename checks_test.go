package gorun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// runChecks runs tasks on a local server and returns every task event, in order.
func runChecks(t *testing.T, force bool, tasks ...Task) ([]TaskEvent, ProcessFinishedEvent, error) {
	t.Helper()
	cfg := &Config{Tasks: map[string]Task{}, Force: force}
	names := []string{}
	for _, task := range tasks {
		task.Local = true
		cfg.Tasks[task.Name] = task
		names = append(names, task.Name)
	}
	cfg.Servers = []Server{{Name: "local", TaskNames: names}}
	var mu sync.Mutex
	var events []TaskEvent
	var finished ProcessFinishedEvent
	cfg.OnEvent = func(event any) {
		mu.Lock()
		defer mu.Unlock()
		switch ev := event.(type) {
		case TaskEvent:
			events = append(events, ev)
		case ProcessFinishedEvent:
			finished = ev
		}
	}
	err := cfg.Run(context.Background())
	return events, finished, err
}

// final returns the last event of the named task.
func final(events []TaskEvent, name string) TaskEvent {
	var last TaskEvent
	for _, ev := range events {
		if ev.Task == name {
			last = ev
		}
	}
	return last
}

// phases lists the phases of the named task's running events.
func phases(events []TaskEvent, name string) []Phase {
	var out []Phase
	for _, ev := range events {
		if ev.Task == name && ev.State == TaskRunning {
			out = append(out, ev.Phase)
		}
	}
	return out
}

func TestCheckExitZeroLeavesTheTaskUnchanged(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	events, finished, err := runChecks(t, false,
		Task{Name: "deploy", Check: "echo up to date", Cmd: "touch " + marker, Verify: "false"},
		Task{Name: "after", Cmd: "echo after", DependsOn: []string{"deploy"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	ev := final(events, "deploy")
	if ev.State != TaskUnchanged || ev.ExitCode != 0 || ev.Phase != PhaseCheck {
		t.Errorf("deploy = state %d, exit %d, phase %q; want unchanged at the check", ev.State, ev.ExitCode, ev.Phase)
	}
	if ev.Check == nil || ev.Check.ExitCode != 0 || strings.TrimSpace(ev.Check.Stdout) != "up to date" || ev.Verify != nil {
		t.Errorf("check = %+v, verify = %+v; want the check's output and no verify", ev.Check, ev.Verify)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the command ran although the check found nothing to do")
	}
	if got := phases(events, "deploy"); len(got) != 1 || got[0] != PhaseCheck {
		t.Errorf("running phases = %v, want only the check", got)
	}
	// an unchanged task is a success: what depends on it still runs
	if after := final(events, "after"); after.State != TaskDone {
		t.Errorf("dependent task state = %d, want done", after.State)
	}
	if finished.Unchanged != 1 || finished.Failed != 0 || finished.Skipped != 0 || !finished.Success {
		t.Errorf("finished = %+v, want 1 unchanged and success", finished)
	}
}

func TestCheckExitOneRunsTheTaskThenVerifies(t *testing.T) {
	events, _, err := runChecks(t, false, Task{Name: "deploy", Check: "echo needed; exit 1", Cmd: "echo deployed", Verify: "echo verified"})
	if err != nil {
		t.Fatal(err)
	}
	ev := final(events, "deploy")
	if ev.State != TaskDone || ev.Phase != PhaseVerify || strings.TrimSpace(ev.Stdout) != "deployed" {
		t.Errorf("deploy = state %d, phase %q, stdout %q; want done after verify", ev.State, ev.Phase, ev.Stdout)
	}
	if ev.Check == nil || ev.Check.ExitCode != 1 || ev.Verify == nil || strings.TrimSpace(ev.Verify.Stdout) != "verified" {
		t.Errorf("check = %+v, verify = %+v", ev.Check, ev.Verify)
	}
	want := []Phase{PhaseCheck, PhaseRun, PhaseVerify}
	if got := phases(events, "deploy"); strings.Join(phaseStrings(got), ",") != strings.Join(phaseStrings(want), ",") {
		t.Errorf("running phases = %v, want %v", got, want)
	}
}

// grep exits 2 when its file is missing, which is the first run of a typical check: the task runs.
func TestAnyNonZeroCheckRunsTheTask(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	events, _, err := runChecks(t, false, Task{Name: "deploy", Check: "grep -q v2 " + missing, Cmd: "echo deployed"})
	if err != nil {
		t.Fatal(err)
	}
	ev := final(events, "deploy")
	if ev.State != TaskDone || ev.Check == nil || ev.Check.ExitCode != 2 || strings.TrimSpace(ev.Stdout) != "deployed" {
		t.Errorf("deploy = state %d, check %+v, stdout %q; want the task run after a check exiting 2", ev.State, ev.Check, ev.Stdout)
	}
}

// A check that can't finish is not a "yes": the task fails rather than running blind.
func TestCheckThatTimesOutFailsTheTask(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	events, _, err := runChecks(t, false, Task{Name: "deploy", Check: "sleep 5", Cmd: "touch " + marker, Timeout: 200 * time.Millisecond})
	if err == nil {
		t.Fatal("run succeeded with a check that timed out")
	}
	ev := final(events, "deploy")
	if ev.State != TaskFailed || ev.ExitCode != 124 || ev.Phase != PhaseCheck {
		t.Errorf("deploy = state %d, exit %d, phase %q; want failed at the check with a timeout", ev.State, ev.ExitCode, ev.Phase)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the command ran after its check timed out")
	}
}

func TestVerifyThatFailsFailsTheTask(t *testing.T) {
	events, _, err := runChecks(t, false,
		Task{Name: "deploy", Cmd: "echo deployed", Verify: "echo still old; exit 4"},
		Task{Name: "after", Cmd: "echo after", DependsOn: []string{"deploy"}},
	)
	if err == nil {
		t.Fatal("run succeeded with a failing verify")
	}
	ev := final(events, "deploy")
	if ev.State != TaskFailed || ev.ExitCode != 4 || ev.Phase != PhaseVerify || strings.TrimSpace(ev.Stdout) != "deployed" {
		t.Errorf("deploy = state %d, exit %d, phase %q, stdout %q; want failed at verify, keeping the command's output", ev.State, ev.ExitCode, ev.Phase, ev.Stdout)
	}
	if !strings.Contains(ev.Stderr, "verify failed") || !strings.Contains(ev.Stderr, "still old") {
		t.Errorf("stderr = %q, want the verify's failure and output", ev.Stderr)
	}
	if after := final(events, "after"); after.State != TaskSkipped {
		t.Errorf("dependent task state = %d, want skipped", after.State)
	}
}

func TestForceSkipsTheCheck(t *testing.T) {
	events, finished, err := runChecks(t, true, Task{Name: "deploy", Check: "true", Cmd: "echo deployed", Verify: "true"})
	if err != nil {
		t.Fatal(err)
	}
	ev := final(events, "deploy")
	if ev.State != TaskDone || ev.Check != nil || ev.Verify == nil || strings.TrimSpace(ev.Stdout) != "deployed" {
		t.Errorf("deploy = state %d, check %+v, verify %+v, stdout %q; want the command run and verified, no check", ev.State, ev.Check, ev.Verify, ev.Stdout)
	}
	if finished.Unchanged != 0 {
		t.Errorf("unchanged = %d, want 0 when forced", finished.Unchanged)
	}
}

func TestRetriesRunTheCheckAgain(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "n")
	// the check says a change is needed; the command fails the first time and works the second
	cmd := "n=$(cat " + counter + " 2>/dev/null || echo 0); echo $((n+1)) > " + counter + "; [ $n -ge 1 ]"
	events, _, err := runChecks(t, false, Task{Name: "deploy", Check: "exit 1", Cmd: cmd, Retries: 1, RetryDelay: new(time.Duration)})
	if err != nil {
		t.Fatal(err)
	}
	want := []Phase{PhaseCheck, PhaseRun, PhaseCheck, PhaseRun}
	if got := phases(events, "deploy"); strings.Join(phaseStrings(got), ",") != strings.Join(phaseStrings(want), ",") {
		t.Errorf("running phases = %v, want %v", got, want)
	}
	// the retry's first running event says why it is running again
	var retry TaskEvent
	for _, ev := range events {
		if ev.State == TaskRunning && ev.Attempt == 2 {
			retry = ev
			break
		}
	}
	if retry.Phase != PhaseCheck || retry.Err == nil {
		t.Errorf("retry's first event = %+v, want the check with the previous failure", retry)
	}
}

func TestStepFailureNamesTheStep(t *testing.T) {
	got := stepFailure(PhaseVerify, errors.New("verify failed: exit status 1"), nil, &StepResult{Stdout: "a\nlast line\n"})
	if got != "verify failed: exit status 1: last line" {
		t.Errorf("stepFailure = %q", got)
	}
}

func phaseStrings(ps []Phase) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return out
}
