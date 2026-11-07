package alerts

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// TaskStat summarises one task over its most recent outcomes. Durations come from successful
// runs only, since failures often stop early and would drag the numbers down.
type TaskStat struct {
	Server     string
	Task       string
	Runs       int
	Succeeded  int
	Median     time.Duration
	P95        time.Duration
	Last       time.Time
	LastFailed bool
}

// Summarize returns one TaskStat per task, using each task's newest window outcomes. The result
// is sorted by server, then task.
func Summarize(history []Outcome, window int) []TaskStat {
	series, keys := groupByTask(history)
	out := make([]TaskStat, 0, len(keys))
	for _, k := range keys {
		runs := series[k]
		if window > 0 && len(runs) > window {
			runs = runs[:window]
		}
		s := TaskStat{Server: k.server, Task: k.name, Runs: len(runs), Last: runs[0].At, LastFailed: runs[0].Failed}
		var durations []time.Duration
		for _, o := range runs {
			if !o.Failed {
				s.Succeeded++
				durations = append(durations, o.Duration)
			}
		}
		if len(durations) > 0 {
			s.Median = percentile(durations, 0.5)
			s.P95 = percentile(durations, 0.95)
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(x, y TaskStat) int {
		if c := cmp.Compare(x.Server, y.Server); c != 0 {
			return c
		}
		return cmp.Compare(x.Task, y.Task)
	})
	return out
}

// percentile returns the nearest-rank p (0 to 1) of values, which it does not modify.
func percentile(values []time.Duration, p float64) time.Duration {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	rank := max(int(math.Ceil(p*float64(len(sorted)))), 1) // p95 of 10 samples is the 10th
	return sorted[rank-1]
}
