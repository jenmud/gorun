package websrv

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// schedulesPage lists the loaded Gorunfile's schedules with their status and next run. Next
// runs are sent as UTC instants; the page shows them in the viewer's zone.
func (a *app) schedulesPage(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	s := a.sched
	a.mu.Unlock()

	msg, bad := flashFrom(r)
	d := ui.SchedulesPageData{Error: r.URL.Query().Get("error")}
	if bad {
		d.Error = msg
	} else {
		d.Notice = msg
	}
	if cfg != nil {
		d.Loaded, d.Path = true, cfg.Path
		d.Running = s != nil
		d.Rows = scheduleRows(cfg, s, time.Now())
		d.Files = fileOptions(cfg)
		d.Targets = targetNames(cfg)
	}
	if edit := r.URL.Query().Get("edit"); edit != "" && cfg != nil && d.Error == "" {
		a.mu.Lock()
		pending := slices.Clone(a.pending)
		a.mu.Unlock()
		if msg := pendingConflict(pending, kindSchedule, edit); msg != "" {
			d.Error = msg
		} else if body, err := currentBody(cfg, pending, kindSchedule, edit); err != nil {
			d.Error = err.Error()
		} else {
			d.EditingName = edit
			d.Edit = editFields(body)
			d.Targets = targetNames(cfg)
		}
	}
	a.page(w, "Schedules", "schedules", ui.SchedulesPage(d))
}

// schedulePause, scheduleResume and scheduleSkip are the POST actions on the schedules page.
func (a *app) schedulePause(w http.ResponseWriter, r *http.Request) {
	a.scheduleControl(w, r, (*gorun.Scheduler).Pause)
}

// scheduleResume resumes a paused schedule.
func (a *app) scheduleResume(w http.ResponseWriter, r *http.Request) {
	a.scheduleControl(w, r, (*gorun.Scheduler).Resume)
}

// scheduleSkip skips a schedule's next run.
func (a *app) scheduleSkip(w http.ResponseWriter, r *http.Request) {
	a.scheduleControl(w, r, (*gorun.Scheduler).Skip)
}

// scheduleControl applies one action to the named schedule, then returns to the page. A
// failure comes back as an error banner rather than an error page.
func (a *app) scheduleControl(w http.ResponseWriter, r *http.Request, op func(*gorun.Scheduler, string) error) {
	a.mu.Lock()
	s := a.sched
	a.mu.Unlock()

	var err error
	if s == nil {
		err = fmt.Errorf("the scheduler is not running")
	} else {
		err = op(s, r.FormValue("schedule"))
	}
	target := "/schedules"
	if err != nil {
		target += "?error=" + url.QueryEscape(err.Error())
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// recordScheduleEvent writes skipped, missed and failed occurrences to history. A run that
// starts is recorded by its own events; a run that fails before it starts, such as a target no
// server can run, has none, so the failure is written here.
func (a *app) recordScheduleEvent(ev gorun.ScheduleEvent) {
	failed := ev.Kind == gorun.ScheduleFailed
	if a.store == nil || (ev.Kind != gorun.ScheduleSkipped && ev.Kind != gorun.ScheduleMissed && !failed) {
		return
	}
	ctx := context.Background()
	host, _ := os.Hostname()
	id := fmt.Sprintf("schedule-%s-%d", ev.Schedule, time.Now().UnixNano())
	title := fmt.Sprintf("schedule %s %s: %s", ev.Schedule, ev.Kind, ev.Reason)
	if err := a.store.CreateRun(ctx, id, title, host, os.Getpid(), ev.At); err != nil {
		log.Printf("gorun web: record schedule %s: %v", ev.Schedule, err)
		return
	}
	if err := a.store.SetRunSchedule(ctx, id, ev.Schedule); err != nil {
		log.Printf("gorun web: record schedule %s: %v", ev.Schedule, err)
	}
	if err := a.store.FinishRun(ctx, id, !failed, 0, 0, 0, 0, ev.At); err != nil {
		log.Printf("gorun web: finish schedule %s: %v", ev.Schedule, err)
	}
}

// scheduleRows builds one row per schedule. A running scheduler supplies the live status;
// otherwise the next run is worked out from the schedule itself.
func scheduleRows(cfg *gorun.Config, s *gorun.Scheduler, now time.Time) []ui.ScheduleRow {
	live := map[string]gorun.ScheduleStatus{}
	if s != nil {
		for _, st := range s.Status() {
			live[st.Name] = st
		}
	}

	rows := make([]ui.ScheduleRow, 0, len(cfg.Schedules))
	for _, sc := range cfg.Schedules {
		row := ui.ScheduleRow{
			Name:        sc.Name,
			Description: sc.Description,
			Target:      sc.Target,
			When:        scheduleWhen(sc),
			Zone:        scheduleZone(sc),
			Status:      "not running",
		}
		next, found := time.Time{}, false
		if st, ok := live[sc.Name]; ok {
			next, found = st.Next, !st.Next.IsZero()
			row.Status = liveStatus(st)
			row.Missed = st.Missed
			row.Paused = st.Paused
			row.SkipPending = st.SkipNext
			row.CanSkip = found && !st.SkipNext
			row.Finished = !found
		} else if n, ok, err := sc.Next(now); err == nil && ok {
			next, found = n, true
		}
		if names, err := cfg.ExpandTaskRefs(sc.Target); err == nil {
			if err := cfg.CanRun(names); err != nil {
				row.Problem = strings.TrimPrefix(err.Error(), "no tasks to run: ")
			}
		}
		if found {
			row.NextUTC = next.UTC().Format(time.RFC3339)
			row.NextText = next.UTC().Format("2006-01-02 15:04 MST")
		} else if row.Status == "not running" {
			row.Status = "finished"
		}
		rows = append(rows, row)
	}
	return rows
}

// liveStatus names what the scheduler is doing with a schedule.
func liveStatus(st gorun.ScheduleStatus) string {
	switch {
	case st.Running:
		return "running"
	case st.Paused:
		return "paused"
	case st.SkipNext:
		return "next run skipped"
	case st.Next.IsZero():
		return "finished"
	default:
		return "scheduled"
	}
}

// scheduleWhen describes how a schedule is timed.
func scheduleWhen(sc gorun.Schedule) string {
	if sc.Cron != "" {
		return fmt.Sprintf("cron %q", sc.Cron)
	}
	return "once at " + sc.At
}

// scheduleZone is the zone a schedule is written in, UTC when none is set.
func scheduleZone(sc gorun.Schedule) string {
	if sc.Timezone == "" {
		return "UTC"
	}
	return sc.Timezone
}

// pendingConflict explains why an item can't be edited here, or returns "" when it can. An
// item with unsaved changes on the Config page waits until those are saved or discarded, so
// the two edits can't disagree.
func pendingConflict(pending []gorun.Change, kind, name string) string {
	for _, p := range pending {
		if p.Kind == kind && p.Name == name {
			return "Save or discard the unsaved changes to " + name + " on the Config page first."
		}
	}
	return ""
}

// reloadConfig reads the saved Gorunfile again and restarts the scheduler. Unsaved changes on
// the Config page are kept, unlike loading a file there.
func (a *app) reloadConfig() error {
	a.mu.Lock()
	path := a.cfgPath
	a.mu.Unlock()
	cfg := &gorun.Config{}
	if err := cfg.LoadFile(path); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	a.startScheduler(cfg)
	return nil
}
