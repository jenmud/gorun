// Package alerts checks finished runs against rules from a Gorunfile. It works on plain task
// history, so it needs no model, network or third party. Each rule fires only for the run that
// just finished, so an old failure is not raised again on every later run.
package alerts

import (
	"fmt"
	"path"
	"time"
)

// Rule kinds.
const (
	KindFailureStreak = "failure_streak" // a task failed the last Count runs in a row
	KindSlow          = "slow"           // a task ran longer than Factor times its recent median
	KindRunFailed     = "run_failed"     // the whole run failed
)

// Defaults for rule fields left at zero.
const (
	defaultCount  = 3
	defaultFactor = 2.0
	defaultWindow = 20
	minBaseline   = 3 // fewer successful runs than this and a slow rule stays quiet
)

// Rule is one [[alerts]] table in a Gorunfile.
type Rule struct {
	Name   string  `toml:"name"`
	Kind   string  `toml:"kind"`
	Server string  `toml:"server"` // glob, empty matches every server
	Task   string  `toml:"task"`   // glob, empty matches every task
	Count  int     `toml:"count"`  // failure_streak: runs in a row, default 3
	Factor float64 `toml:"factor"` // slow: multiple of the median, default 2
	Window int     `toml:"window"` // slow: successful runs used for the median, default 20
}

// Outcome is one finished task on one server, as recorded in history.
type Outcome struct {
	RunID    string
	Server   string
	Task     string
	Failed   bool
	Duration time.Duration
	At       time.Time
}

// Alert is a rule that fired for a run. Server and Task are empty for run_failed.
type Alert struct {
	Rule    string
	RunID   string
	Server  string
	Task    string
	Message string
	At      time.Time
}

// Validate reports the first rule that cannot be evaluated. It is called when a Gorunfile loads,
// so a bad rule fails the load instead of being skipped silently later.
func Validate(rules []Rule) error {
	names := map[string]bool{}
	for i, r := range rules {
		if r.Name == "" {
			return fmt.Errorf("alert %d: name is required", i+1)
		}
		if names[r.Name] {
			return fmt.Errorf("duplicate alert %q", r.Name)
		}
		names[r.Name] = true
		if err := r.check(); err != nil {
			return fmt.Errorf("alert %q: %w", r.Name, err)
		}
	}
	return nil
}

// check reports why a single rule is malformed.
func (r Rule) check() error {
	switch r.Kind {
	case KindFailureStreak, KindSlow, KindRunFailed:
	default:
		return fmt.Errorf("kind must be %s, %s or %s, not %q", KindFailureStreak, KindSlow, KindRunFailed, r.Kind)
	}
	for _, pattern := range []string{r.Server, r.Task} {
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("bad pattern %q: %w", pattern, err)
		}
	}
	if r.Count < 0 {
		return fmt.Errorf("count must not be negative")
	}
	if r.Factor != 0 && r.Factor <= 1 {
		return fmt.Errorf("factor must be above 1")
	}
	if r.Window != 0 && r.Window < minBaseline {
		return fmt.Errorf("window must be at least %d", minBaseline)
	}
	return nil
}

// Evaluate returns the alerts the rules raise for the run runID. history holds finished task
// outcomes, newest first. A task only counts if its newest outcome belongs to runID.
func Evaluate(rules []Rule, runID string, runFailed bool, history []Outcome) []Alert {
	series, keys := groupByTask(history)
	var out []Alert
	for _, r := range rules {
		if r.Kind == KindRunFailed {
			if runFailed {
				out = append(out, Alert{Rule: r.Name, RunID: runID, Message: "the run failed"})
			}
			continue
		}
		for _, k := range keys {
			runs := series[k]
			if !matches(r, k) || runs[0].RunID != runID {
				continue
			}
			if msg, ok := fire(r, runs); ok {
				out = append(out, Alert{
					Rule: r.Name, RunID: runID, Server: k.server, Task: k.name,
					Message: msg, At: runs[0].At,
				})
			}
		}
	}
	return out
}

// fire reports whether a task rule matches its newest outcome, and why.
func fire(r Rule, runs []Outcome) (string, bool) {
	switch r.Kind {
	case KindFailureStreak:
		count := or(r.Count, defaultCount)
		streak := 0
		for _, o := range runs {
			if !o.Failed {
				break
			}
			streak++
		}
		if streak >= count {
			return fmt.Sprintf("failed %d runs in a row", streak), true
		}
	case KindSlow:
		if runs[0].Failed {
			return "", false
		}
		window := or(r.Window, defaultWindow)
		var base []time.Duration
		for _, o := range runs[1:] {
			if len(base) == window {
				break
			}
			if !o.Failed {
				base = append(base, o.Duration)
			}
		}
		if len(base) < minBaseline {
			return "", false
		}
		med := percentile(base, 0.5)
		factor := r.Factor
		if factor == 0 {
			factor = defaultFactor
		}
		if med > 0 && float64(runs[0].Duration) > factor*float64(med) {
			return fmt.Sprintf("took %s, %.1fx the median %s of the last %d successful runs",
				runs[0].Duration.Round(time.Millisecond), float64(runs[0].Duration)/float64(med),
				med.Round(time.Millisecond), len(base)), true
		}
	}
	return "", false
}

// taskKey identifies one task on one server.
type taskKey struct{ server, name string }

// groupByTask splits history per task, keeping each task's outcomes newest first. keys lists
// the tasks in the order they first appear.
func groupByTask(history []Outcome) (map[taskKey][]Outcome, []taskKey) {
	series := map[taskKey][]Outcome{}
	var keys []taskKey
	for _, o := range history {
		k := taskKey{o.Server, o.Task}
		if _, ok := series[k]; !ok {
			keys = append(keys, k)
		}
		series[k] = append(series[k], o)
	}
	return series, keys
}

// matches reports whether a rule's server and task globs match the task. An empty glob matches
// anything.
func matches(r Rule, k taskKey) bool {
	return globMatch(r.Server, k.server) && globMatch(r.Task, k.name)
}

func globMatch(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	ok, _ := path.Match(pattern, name) // patterns are checked by Validate
	return ok
}

func or(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

// Describe returns a short reading of when the rule fires, with defaults filled in.
func (r Rule) Describe() string {
	switch r.Kind {
	case KindFailureStreak:
		return fmt.Sprintf("failed %d runs in a row", or(r.Count, defaultCount))
	case KindSlow:
		factor := r.Factor
		if factor == 0 {
			factor = defaultFactor
		}
		return fmt.Sprintf("takes over %.1fx the median of its last %d passing runs", factor, or(r.Window, defaultWindow))
	case KindRunFailed:
		return "the whole run fails"
	}
	return r.Kind
}
