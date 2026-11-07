package websrv

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jenmud/gorun/internal/alerts"
)

// failedRun saves a finished run whose only task, deploy on web-1, failed or passed.
func failedRun(t *testing.T, a *app, id string, started time.Time, state int) {
	t.Helper()
	ctx := context.Background()
	if err := a.store.CreateRun(ctx, id, "deploy", "box", 1, started); err != nil {
		t.Fatal(err)
	}
	if err := a.store.AddServer(ctx, id, "web-1", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.UpsertTask(ctx, id, "web-1", "deploy", state, "make deploy", "", "boom", 1, time.Second); err != nil {
		t.Fatal(err)
	}
	passed := state == 2
	failed := 0
	if !passed {
		failed = 1
	}
	if err := a.store.FinishRun(ctx, id, passed, 1, failed, 0, time.Second, started.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateSavesEachAlertOnce(t *testing.T) {
	a, _ := newTestApp(t)
	a.cfg.Alerts = []alerts.Rule{{Name: "deploy keeps failing", Kind: alerts.KindFailureStreak, Task: "deploy", Count: 2}}
	base := time.Now().Add(-time.Hour)
	failedRun(t, a, "r1", base, 3)
	failedRun(t, a, "r2", base.Add(time.Minute), 3)

	ctx := context.Background()
	a.evaluateAlerts(ctx, "r2", true)
	a.evaluateAlerts(ctx, "r2", true) // a second evaluation must not duplicate it

	saved, err := a.store.ListAlerts(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Rule != "deploy keeps failing" || saved[0].RunID != "r2" || saved[0].Name != "deploy" {
		t.Fatalf("want one saved alert for r2, got %+v", saved)
	}

	page := get(t, a, "/alerts").Body.String()
	for _, want := range []string{"deploy keeps failing", "failed 2 runs in a row", "web-1/deploy", "/history/r2", "Task stats"} {
		if !strings.Contains(page, want) {
			t.Errorf("alerts page does not contain %q", want)
		}
	}
}

func TestEvaluateStaysQuietWithoutRulesOrAfterAPass(t *testing.T) {
	a, _ := newTestApp(t)
	base := time.Now().Add(-time.Hour)
	failedRun(t, a, "r1", base, 3)
	ctx := context.Background()

	a.evaluateAlerts(ctx, "r1", true) // no rules loaded
	a.cfg.Alerts = []alerts.Rule{{Name: "streak", Kind: alerts.KindFailureStreak, Count: 1}}
	failedRun(t, a, "r2", base.Add(time.Minute), 2)
	a.evaluateAlerts(ctx, "r2", false) // the newest run passed, so the streak is broken

	if saved, _ := a.store.ListAlerts(ctx, 10); len(saved) != 0 {
		t.Fatalf("want no alerts, got %+v", saved)
	}
}

func TestAlertsPageExplainsMissingRulesAndHistory(t *testing.T) {
	a, _ := newTestApp(t)
	page := get(t, a, "/alerts").Body.String()
	if !strings.Contains(page, "No alert rules loaded") || !strings.Contains(page, "No alerts have fired yet") {
		t.Error("an empty alerts page should say what is missing")
	}

	a.store = nil
	rec := get(t, a, "/alerts")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "History is off") {
		t.Errorf("without history the page should say so (status %d)", rec.Code)
	}
}
