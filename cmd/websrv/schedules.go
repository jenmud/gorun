package websrv

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
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
	}
	if edit := r.URL.Query().Get("edit"); edit != "" && cfg != nil && d.Error == "" {
		a.mu.Lock()
		pending := slices.Clone(a.pending)
		a.mu.Unlock()
		if msg := pendingConflict(pending, edit); msg != "" {
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

func (a *app) scheduleResume(w http.ResponseWriter, r *http.Request) {
	a.scheduleControl(w, r, (*gorun.Scheduler).Resume)
}

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

// recordScheduleEvent writes skipped and missed occurrences to history. Runs that start are
// recorded by the run's own events, so they are not written here.
func (a *app) recordScheduleEvent(ev gorun.ScheduleEvent) {
	if a.store == nil || (ev.Kind != gorun.ScheduleSkipped && ev.Kind != gorun.ScheduleMissed) {
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
	if err := a.store.FinishRun(ctx, id, true, 0, 0, 0, 0, ev.At); err != nil {
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

// scheduleEdit saves an edit made on the schedules page straight to the schedule's file. A
// schedule with unsaved changes on the Config page is left alone until those are saved or
// discarded, so the two edits can't disagree.
func (a *app) scheduleEdit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWith(w, r, "/schedules", "Could not read the form.", true)
		return
	}
	name := r.PostFormValue("name")
	back := "/schedules?" + url.Values{"edit": {name}}.Encode()

	a.mu.Lock()
	cfg, pending := a.cfg, slices.Clone(a.pending)
	if cfg == nil {
		a.mu.Unlock()
		redirectWith(w, r, "/schedules", "Load a Gorunfile first.", true)
		return
	}
	if msg := pendingConflict(pending, name); msg != "" {
		a.mu.Unlock()
		redirectWith(w, r, back, msg, true)
		return
	}
	body, err := currentBody(cfg, pending, kindSchedule, name)
	if err == nil {
		applyScheduleEdit(body, r.PostForm)
		err = a.checkChange(gorun.Change{Kind: kindSchedule, Name: name, Body: body})
	}
	if err == nil {
		file := sourceOf(cfg, pending, kindSchedule, name)
		err = gorun.SaveChanges([]gorun.Change{
			{Kind: kindSchedule, Name: name, File: file, Remove: true},
			{Kind: kindSchedule, Name: name, File: file, Body: body},
		})
	}
	a.mu.Unlock()
	if err != nil {
		redirectWith(w, r, back, err.Error(), true)
		return
	}
	if err := a.reloadConfig(); err != nil {
		redirectWith(w, r, "/schedules", err.Error(), true)
		return
	}
	redirectWith(w, r, "/schedules", "Updated schedule "+name+".", false)
}

// pendingConflict explains why a schedule can't be edited here, or returns "" when it can.
func pendingConflict(pending []gorun.Change, name string) string {
	for _, p := range pending {
		if p.Kind == kindSchedule && p.Name == name {
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
