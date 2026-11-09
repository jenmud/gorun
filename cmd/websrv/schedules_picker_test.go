package websrv

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestOneOffTimeIsSavedInUTC(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)

	// 02:00 on 1 Dec in Sydney (AEDT, UTC+11) is 15:00 UTC on 30 Nov
	rec := post(t, a, "/schedules/add", url.Values{
		"name": {"once-syd"}, "target": {"hello"}, "cron": {""}, "at_local": {"2099-12-01T02:00"},
		"timezone": {"Australia/Sydney"}, "missed": {"skip"}, "file": {path},
	})
	if _, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatal("add was refused")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `at = "2099-11-30T15:00:00Z"`) {
		t.Errorf("one-off time not saved in UTC:\n%s", data)
	}
	if got := a.cfg.Schedules[len(a.cfg.Schedules)-1].At; got != "2099-11-30T15:00:00Z" {
		t.Errorf("reloaded at = %q", got)
	}
}

func TestOneOffEditShowsTheTimeInItsZone(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	post(t, a, "/schedules/add", url.Values{
		"name": {"once-syd"}, "target": {"hello"}, "at_local": {"2099-12-01T02:00"},
		"timezone": {"Australia/Sydney"}, "missed": {"skip"}, "file": {path},
	})

	body := get(t, a, "/schedules?edit=once-syd").Body.String()
	if !strings.Contains(body, `value="2099-12-01T02:00"`) {
		t.Errorf("edit form does not show the Sydney time it was saved from")
	}
	if !strings.Contains(body, `type="datetime-local"`) {
		t.Error("the one-off time is not a date and time picker")
	}

	// saving the edit unchanged keeps the same UTC instant
	rec := post(t, a, "/schedules/edit", url.Values{
		"name": {"once-syd"}, "target": {"hello"}, "at_local": {"2099-12-01T02:00"},
		"timezone": {"Australia/Sydney"}, "missed": {"skip"},
	})
	if _, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatal("edit was refused")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `at = "2099-11-30T15:00:00Z"`) {
		t.Errorf("edit changed the instant:\n%s", data)
	}
}

func TestOneOffRefusesAnUnknownZone(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	rec := post(t, a, "/schedules/add", url.Values{
		"name": {"bad-zone"}, "target": {"hello"}, "at_local": {"2099-12-01T02:00"},
		"timezone": {"Mars/Olympus"}, "file": {path},
	})
	if _, bad := flashOf(t, rec.Header().Get("Location")); !bad {
		t.Error("a one-off time in an unknown zone was accepted")
	}
}
