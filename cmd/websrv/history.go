package websrv

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/jenmud/gorun/cmd/web/ui"
	"github.com/jenmud/gorun/internal/store"
)

// stateFailed is the task state the store records for a failed task.
const stateFailed = 3

// keepRuns is how many runs "Trim history" keeps.
const keepRuns = 10

// runList lists the saved runs, newest first. The store must be open.
func (a *app) runList(ctx context.Context) ([]store.RunSummary, error) {
	runs, err := a.store.ListRuns(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(runs, func(x, y store.RunSummary) int { return y.StartedAt.Compare(x.StartedAt) })
	return runs, nil
}

// stats summarises the saved runs for the stats panel.
func (a *app) stats(ctx context.Context) ui.StatsView {
	var v ui.StatsView
	if a.store == nil {
		return v
	}
	runs, err := a.runList(ctx)
	if err != nil {
		log.Printf("gorun web: stats: %v", err)
		return v
	}
	v.Runs = len(runs)

	var finished []store.RunSummary
	var total time.Duration
	for _, r := range runs {
		if !r.Finished {
			continue
		}
		finished = append(finished, r)
		v.Finished++
		if r.Success {
			v.Succeeded++
		}
		v.TasksRun += r.TotalTasks
		v.TasksFailed += r.Failed
		total += r.Duration
	}
	if v.Finished > 0 {
		v.SuccessRate = v.Succeeded * 100 / v.Finished
		v.AvgDuration = total / time.Duration(v.Finished)
	}

	// The 20 most recent finished runs, oldest first, so the newest bar is on the right.
	recent := finished
	if len(recent) > 20 {
		recent = recent[:20]
	}
	for i := len(recent) - 1; i >= 0; i-- {
		r := recent[i]
		v.Recent = append(v.Recent, ui.Bar{
			Label: r.StartedAt.Format("15:04"),
			Value: r.Duration.Seconds(),
			Tip:   fmt.Sprintf("%s · %s", r.Title, r.Duration.Round(time.Millisecond)),
			Bad:   !r.Success,
		})
	}
	v.Slowest = a.slowest(ctx, finished)
	return v
}

// slowest ranks the tasks of the ten most recent finished runs by their average duration.
func (a *app) slowest(ctx context.Context, finished []store.RunSummary) []ui.SlowTask {
	type key struct{ server, name string }
	type agg struct {
		total time.Duration
		runs  int
	}
	sums := map[key]*agg{}
	for i, r := range finished {
		if i == 10 {
			break
		}
		d, err := a.store.GetRun(ctx, r.ID)
		if err != nil || d == nil {
			continue
		}
		for _, t := range d.Tasks {
			if t.Duration <= 0 {
				continue
			}
			k := key{t.Server, t.Name}
			g := sums[k]
			if g == nil {
				g = &agg{}
				sums[k] = g
			}
			g.total += t.Duration
			g.runs++
		}
	}

	out := make([]ui.SlowTask, 0, len(sums))
	for k, g := range sums {
		out = append(out, ui.SlowTask{Name: k.name, Server: k.server, Average: g.total / time.Duration(g.runs), Runs: g.runs})
	}
	slices.SortFunc(out, func(x, y ui.SlowTask) int {
		if c := cmp.Compare(y.Average, x.Average); c != 0 {
			return c
		}
		return cmp.Compare(x.Name, y.Name)
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// runRow turns a saved run into a row for the history list.
func runRow(s store.RunSummary) ui.RunRow {
	state := "ok"
	switch {
	case !s.Finished:
		state = "running"
	case !s.Success:
		state = "failed"
	}
	return ui.RunRow{
		ID:         s.ID,
		Title:      s.Title,
		Hostname:   s.Hostname,
		StartedAt:  s.StartedAt,
		Duration:   s.Duration,
		State:      state,
		Servers:    s.Servers,
		TotalTasks: s.TotalTasks,
		Failed:     s.Failed,
		Skipped:    s.Skipped,
	}
}

// historyPage lists the saved runs.
func (a *app) historyPage(w http.ResponseWriter, r *http.Request) {
	msg, _ := flashFrom(r)
	rows := []ui.RunRow{}
	if a.store == nil {
		msg = "History is off, because the database could not be opened."
	} else {
		runs, err := a.runList(r.Context())
		if err != nil {
			msg = "Could not load history: " + err.Error()
		}
		for _, s := range runs {
			rows = append(rows, runRow(s))
		}
	}
	a.page(w, "History", "history", ui.HistoryPage(ui.HistoryPageData{Runs: rows, Flash: msg}))
}

// historyDetail shows one saved run with each server's tasks and their output.
func (a *app) historyDetail(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		http.NotFound(w, r)
		return
	}
	d, err := a.store.GetRun(r.Context(), r.PathValue("id"))
	if err != nil || d == nil {
		http.NotFound(w, r)
		return
	}
	data := ui.RunDetailData{Run: *d, Summary: runRow(d.RunSummary), Servers: serverBlocks(d)}
	a.page(w, d.Title, "history", ui.HistoryDetailPage(data))
}

// serverBlocks groups a run's tasks by the server they ran on. A server that could not be
// reached still gets a block, with the error that stopped it.
func serverBlocks(d *store.RunDetail) []ui.ServerBlock {
	bySrv := map[string][]store.TaskDetail{}
	for _, t := range d.Tasks {
		bySrv[t.Server] = append(bySrv[t.Server], t)
	}

	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, n := range d.Servers {
		add(n)
	}
	for _, n := range slices.Sorted(maps.Keys(d.ServerErrors)) {
		add(n)
	}
	for _, n := range slices.Sorted(maps.Keys(bySrv)) {
		add(n)
	}

	out := make([]ui.ServerBlock, 0, len(names))
	for _, n := range names {
		b := ui.ServerBlock{
			Name:    n,
			Address: d.ServerAddress[n],
			Error:   d.ServerErrors[n],
			Tasks:   bySrv[n],
		}
		for _, t := range b.Tasks {
			b.Duration += t.Duration
			if t.State == stateFailed {
				b.Failed = true
			}
		}
		if b.Error != "" {
			b.Failed = true
		}
		out = append(out, b)
	}
	return out
}

// deleteRun removes one saved run from the history.
func (a *app) deleteRun(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		redirectWith(w, r, "/history", "History is off, so there is nothing to delete.", true)
		return
	}
	if err := a.store.DeleteRun(r.Context(), r.PathValue("id")); err != nil {
		redirectWith(w, r, "/history", "Could not delete the run: "+err.Error(), true)
		return
	}
	a.pushStats(r.Context())
	redirectWith(w, r, "/history", "Run deleted.", false)
}

// truncateRuns keeps only the newest saved runs.
func (a *app) truncateRuns(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		redirectWith(w, r, "/history", "History is off, so there is nothing to trim.", true)
		return
	}
	if err := a.store.TruncateRuns(r.Context(), keepRuns); err != nil {
		redirectWith(w, r, "/history", "Could not trim history: "+err.Error(), true)
		return
	}
	a.pushStats(r.Context())
	redirectWith(w, r, "/history", fmt.Sprintf("Kept the newest %d runs.", keepRuns), false)
}
