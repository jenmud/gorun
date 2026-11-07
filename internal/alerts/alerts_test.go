package alerts

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// pass and fail make one outcome in run for server/task.
func pass(run, server, task string, d time.Duration) Outcome {
	return Outcome{RunID: run, Server: server, Task: task, Duration: d}
}

func fail(run, server, task string, d time.Duration) Outcome {
	return Outcome{RunID: run, Server: server, Task: task, Failed: true, Duration: d}
}

func TestValidateRejectsBadRules(t *testing.T) {
	ok := Rule{Name: "a", Kind: KindFailureStreak}
	bad := map[string]Rule{
		"no name":      {Kind: KindRunFailed},
		"unknown kind": {Name: "a", Kind: "flaky"},
		"bad glob":     {Name: "a", Kind: KindFailureStreak, Task: "[x"},
		"negative":     {Name: "a", Kind: KindFailureStreak, Count: -1},
		"factor 1":     {Name: "a", Kind: KindSlow, Factor: 1},
		"small window": {Name: "a", Kind: KindSlow, Window: 2},
	}
	if err := Validate([]Rule{ok}); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
	for what, r := range bad {
		if err := Validate([]Rule{r}); err == nil {
			t.Errorf("%s: want an error", what)
		}
	}
	if err := Validate([]Rule{ok, ok}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("duplicate names: got %v", err)
	}
}

func TestFailureStreakFiresOnTheRunThatCompletesIt(t *testing.T) {
	rules := []Rule{{Name: "streak", Kind: KindFailureStreak, Task: "deploy", Count: 3}}
	h := []Outcome{
		fail("r3", "web", "deploy", time.Second),
		fail("r2", "web", "deploy", time.Second),
		fail("r1", "web", "deploy", time.Second),
		pass("r0", "web", "deploy", time.Second),
	}
	got := Evaluate(rules, "r3", false, h)
	if len(got) != 1 || got[0].Message != "failed 3 runs in a row" || got[0].Server != "web" {
		t.Fatalf("r3: got %+v", got)
	}
	if got := Evaluate(rules, "r2", false, h); len(got) != 0 {
		t.Errorf("an older run should not raise the alert again: %+v", got)
	}
	if got := Evaluate([]Rule{{Name: "s", Kind: KindFailureStreak, Count: 4}}, "r3", false, h); len(got) != 0 {
		t.Errorf("streak of 3 with count 4 should stay quiet: %+v", got)
	}
}

func TestSlowComparesWithThePassingMedian(t *testing.T) {
	rules := []Rule{{Name: "slow", Kind: KindSlow, Factor: 2}}
	h := []Outcome{
		pass("now", "web", "backup", 10*time.Second),
		fail("f", "web", "backup", 100*time.Second), // failures do not form the baseline
		pass("p1", "web", "backup", time.Second),
		pass("p2", "web", "backup", time.Second),
		pass("p3", "web", "backup", 2*time.Second),
		pass("p4", "web", "backup", time.Second),
		pass("p5", "web", "backup", time.Second),
	}
	if got := Evaluate(rules, "now", false, h); len(got) != 1 {
		t.Fatalf("10s against a 1s median should fire: %+v", got)
	}

	h[0].Duration = 1500 * time.Millisecond
	if got := Evaluate(rules, "now", false, h); len(got) != 0 {
		t.Errorf("1.5s is under twice the median: %+v", got)
	}

	short := []Outcome{pass("now", "web", "backup", time.Hour), pass("p1", "web", "backup", time.Second)}
	if got := Evaluate(rules, "now", false, short); len(got) != 0 {
		t.Errorf("too few runs to compare against should stay quiet: %+v", got)
	}
}

func TestRunFailedAndGlobs(t *testing.T) {
	h := []Outcome{fail("r1", "web-1", "deploy", time.Second), pass("r1", "db", "build", time.Second)}

	got := Evaluate([]Rule{{Name: "any", Kind: KindRunFailed}}, "r1", true, h)
	if len(got) != 1 || got[0].Server != "" || got[0].Task != "" {
		t.Fatalf("run_failed: got %+v", got)
	}
	if got := Evaluate([]Rule{{Name: "any", Kind: KindRunFailed}}, "r1", false, h); len(got) != 0 {
		t.Errorf("a passing run should not fire run_failed: %+v", got)
	}

	streak := []Rule{{Name: "web", Kind: KindFailureStreak, Server: "web-*", Count: 1}}
	if got := Evaluate(streak, "r1", false, h); len(got) != 1 || got[0].Task != "deploy" {
		t.Errorf("glob on server should match web-1 only: %+v", got)
	}
}

func TestSummarizeGivesPercentilesFromPassingRuns(t *testing.T) {
	var h []Outcome
	for i := 10; i >= 1; i-- { // newest first: 10s down to 1s, all passing
		h = append(h, pass(fmt.Sprintf("r%d", i), "web", "build", time.Duration(i)*time.Second))
	}
	h = append([]Outcome{fail("new", "web", "build", time.Second)}, h...)
	h = append(h, pass("old", "db", "migrate", 3*time.Second))

	stats := Summarize(h, 0)
	if len(stats) != 2 || stats[0].Server != "db" || stats[1].Task != "build" {
		t.Fatalf("want db/migrate then web/build, got %+v", stats)
	}
	s := stats[1]
	if s.Runs != 11 || s.Succeeded != 10 || !s.LastFailed {
		t.Errorf("counts: %+v", s)
	}
	if s.Median != 5*time.Second || s.P95 != 10*time.Second {
		t.Errorf("median %s p95 %s, want 5s and 10s", s.Median, s.P95)
	}
	if got := Summarize(h, 3); got[1].Runs != 3 {
		t.Errorf("window of 3: got %d runs", got[1].Runs)
	}
}
