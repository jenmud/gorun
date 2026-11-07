package websrv

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jenmud/gorun/cmd/web/ui"
	"github.com/jenmud/gorun/internal/alerts"
	"github.com/jenmud/gorun/internal/envvars"
	"github.com/jenmud/gorun/internal/store"
)

const (
	// historyLimit caps the task outcomes read for one evaluation or page view.
	historyLimit = 5000
	// statsWindow is how many recent runs per task the stats table summarises.
	statsWindow = 20
)

// alertTopic is the NATS subject fired alerts are published to.
func alertTopic() string {
	if t := envvars.Get("GORUN_ALERT_TOPIC"); t != "" {
		return t
	}
	return "gorun.alerts"
}

// alertRules returns the alert rules of the loaded Gorunfile.
func (a *app) alertRules() []alerts.Rule {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil {
		return nil
	}
	return a.cfg.Alerts
}

// outcomes reads the recent finished task outcomes from the history.
func (a *app) outcomes(ctx context.Context) ([]alerts.Outcome, error) {
	rows, err := a.store.TaskOutcomes(ctx, historyLimit)
	if err != nil {
		return nil, err
	}
	out := make([]alerts.Outcome, len(rows))
	for i, r := range rows {
		out[i] = alerts.Outcome{RunID: r.RunID, Server: r.Server, Task: r.Name, Failed: r.Failed, Duration: r.Duration, At: r.At}
	}
	return out, nil
}

// evaluateAlerts checks the loaded rules against a run that just finished. Each alert that is
// new is saved, logged and published to NATS, so other tools can act on it.
func (a *app) evaluateAlerts(ctx context.Context, runID string, runFailed bool) {
	rules := a.alertRules()
	if a.store == nil || len(rules) == 0 {
		return
	}
	history, err := a.outcomes(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "gorun web: alerts: read history", "error", err)
		return
	}
	for _, f := range alerts.Evaluate(rules, runID, runFailed, history) {
		if f.At.IsZero() {
			f.At = time.Now()
		}
		added, err := a.store.AddAlert(ctx, store.AlertRow{
			Rule: f.Rule, RunID: f.RunID, Server: f.Server, Name: f.Task, Message: f.Message, CreatedAt: f.At,
		})
		if err != nil {
			slog.ErrorContext(ctx, "gorun web: alerts: save", "rule", f.Rule, "error", err)
			continue
		}
		if !added {
			continue
		}
		slog.WarnContext(ctx, "gorun alert", "rule", f.Rule, "run_id", f.RunID, "server", f.Server, "task", f.Task, "message", f.Message)
		a.publishAlert(ctx, f)
	}
}

// publishAlert sends one fired alert to NATS, when NATS is connected.
func (a *app) publishAlert(ctx context.Context, f alerts.Alert) {
	if a.nc == nil {
		return
	}
	data, err := json.Marshal(map[string]any{
		"type": "alert", "rule": f.Rule, "run_id": f.RunID, "server": f.Server,
		"task": f.Task, "message": f.Message, "at": f.At,
	})
	if err != nil {
		return
	}
	if err := a.nc.Publish(alertTopic(), data); err != nil {
		slog.ErrorContext(ctx, "gorun web: alerts: publish", "rule", f.Rule, "error", err)
	}
}

// alertsPage lists the loaded rules, the alerts that fired, and per-task stats.
func (a *app) alertsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	msg, bad := flashFrom(r)
	d := ui.AlertsPageData{Flash: msg, FlashBad: bad, HasHistory: a.store != nil}
	for _, rule := range a.alertRules() {
		d.Rules = append(d.Rules, ui.AlertRuleRow{
			Name: rule.Name, Kind: rule.Kind, Target: ruleTarget(rule), Threshold: rule.Describe(),
		})
	}
	if a.store == nil {
		d.Flash, d.FlashBad = "History is off, so alerts and stats are not recorded.", true
		a.page(w, "Alerts", "alerts", ui.AlertsPage(d))
		return
	}

	saved, err := a.store.ListAlerts(ctx, 100)
	if err != nil {
		d.Flash, d.FlashBad = "Could not load alerts: "+err.Error(), true
	}
	for _, s := range saved {
		d.Alerts = append(d.Alerts, ui.AlertItem{
			When:    s.CreatedAt.Local().Format("Mon 2 Jan 15:04"),
			Rule:    s.Rule,
			Target:  taskTarget(s.Server, s.Name),
			Message: s.Message,
			RunID:   s.RunID,
		})
	}

	history, err := a.outcomes(ctx)
	if err != nil {
		d.Flash, d.FlashBad = "Could not load history: "+err.Error(), true
	}
	for _, s := range alerts.Summarize(history, statsWindow) {
		d.Stats = append(d.Stats, ui.TaskStatRow{
			Target:      taskTarget(s.Server, s.Task),
			Runs:        fmt.Sprint(s.Runs),
			SuccessRate: fmt.Sprint(s.Succeeded * 100 / s.Runs),
			Median:      s.Median.Round(time.Millisecond).String(),
			P95:         s.P95.Round(time.Millisecond).String(),
			Last:        s.Last.Local().Format("Mon 2 Jan 15:04"),
			LastFailed:  s.LastFailed,
		})
	}
	a.page(w, "Alerts", "alerts", ui.AlertsPage(d))
}

// ruleTarget names the tasks a rule applies to.
func ruleTarget(r alerts.Rule) string {
	if r.Kind == alerts.KindRunFailed {
		return "any run"
	}
	return taskTarget(orDefault(r.Server, "*"), orDefault(r.Task, "*"))
}

// taskTarget formats a task as server/task.
func taskTarget(server, name string) string {
	return server + "/" + name
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
