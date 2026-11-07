package gorun

import (
	"strings"
	"testing"
)

func TestAlertsLoadFromIncludesAndRejectBadRules(t *testing.T) {
	c, err := loadTree(t, map[string]string{
		"Gorunfile":       "include = [\"alerts/*.toml\"]\n\n[tasks.hello]\ncmd = \"echo hi\"\n",
		"alerts/web.toml": "[[alerts]]\nname = \"deploy\"\nkind = \"failure_streak\"\ntask = \"deploy\"\ncount = 2\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Alerts) != 1 || c.Alerts[0].Name != "deploy" || c.Alerts[0].Count != 2 {
		t.Fatalf("alerts from an include were not merged: %+v", c.Alerts)
	}

	_, err = loadTree(t, map[string]string{
		"Gorunfile": "[[alerts]]\nname = \"typo\"\nkind = \"failure_streaks\"\n",
	})
	if err == nil || !strings.Contains(err.Error(), "failure_streaks") {
		t.Fatalf("a bad alert kind should stop the load, got %v", err)
	}
}
