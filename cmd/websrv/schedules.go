package websrv

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// schedulesPage lists the loaded Gorunfile's schedules with their status and next run. Next
// runs are sent as UTC instants; the page shows them in the viewer's zone.
func (a *app) schedulesPage(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	cfg := a.cfg
	s := a.sched
	a.mu.Unlock()

	d := ui.SchedulesPageData{}
	if cfg != nil {
		d.Loaded, d.Path = true, cfg.Path
		d.Running = s != nil
		d.Rows = scheduleRows(cfg, s, time.Now())
	}
	a.page(w, "Schedules", "schedules", ui.SchedulesPage(d))
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
