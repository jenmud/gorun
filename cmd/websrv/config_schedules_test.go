package websrv

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jenmud/gorun"
)

// flashOf returns the message and whether it is an error, from a redirect's location.
func flashOf(t *testing.T, loc string) (string, bool) {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("flash"), u.Query().Get("bad") != ""
}

func scheduleForm(file string, extra url.Values) url.Values {
	form := url.Values{
		"kind":     {"schedule"},
		"name":     {"nightly"},
		"target":   {"hello"},
		"cron":     {"0 2 * * *"},
		"timezone": {"Australia/Sydney"},
		"missed":   {"run"},
		"file":     {file},
	}
	for k, v := range extra {
		form[k] = v
	}
	return form
}

func TestAddScheduleQueuesAndSaves(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)

	rec := post(t, a, "/config/add", scheduleForm(path, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add: status %d", rec.Code)
	}
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad || !strings.Contains(msg, "Add schedule nightly") {
		t.Fatalf("flash = %q (bad %v)", msg, bad)
	}
	if body := get(t, a, "/config").Body.String(); !strings.Contains(body, "Add schedule nightly") || !strings.Contains(body, "data-zone-picker") {
		t.Error("config page does not list the queued schedule or the zone dropdown")
	}

	post(t, a, "/config/save", nil)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[[schedule]]\nname = \"nightly\"") {
		t.Errorf("saved file has no [[schedule]] table:\n%s", data)
	}
	if len(a.cfg.Schedules) != 1 || a.cfg.Schedules[0].Name != "nightly" || a.cfg.Schedules[0].Timezone != "Australia/Sydney" {
		t.Errorf("reloaded schedules = %+v", a.cfg.Schedules)
	}
}

func TestAddScheduleRejectsBadSchedules(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)

	cases := map[string]url.Values{
		"both cron and at": scheduleForm(path, url.Values{"at": {"2026-12-01T02:00"}}),
		"unknown target":   scheduleForm(path, url.Values{"target": {"nope"}}),
		"bad zone":         scheduleForm(path, url.Values{"timezone": {"Mars/Olympus"}}),
	}
	for name, form := range cases {
		rec := post(t, a, "/config/add", form)
		if msg, bad := flashOf(t, rec.Header().Get("Location")); !bad || msg == "" {
			t.Errorf("%s: flash = %q (bad %v), want an error", name, msg, bad)
		}
	}
	if len(a.pending) != 0 {
		t.Errorf("pending = %d changes after rejected adds, want 0", len(a.pending))
	}
}

func TestEditScheduleKeepsWhatTheFormDoesNotShow(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	post(t, a, "/config/add", scheduleForm(path, nil))
	post(t, a, "/config/save", nil)

	page := get(t, a, "/config/edit?kind=schedule&name=nightly")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `value="0 2 * * *"`) {
		t.Fatalf("edit page: status %d, no cron in the form", page.Code)
	}

	rec := post(t, a, "/config/edit", url.Values{
		"kind": {"schedule"}, "name": {"nightly"},
		"target": {"hello"}, "cron": {"30 3 * * *"}, "at": {""}, "timezone": {"UTC"}, "missed": {"skip"},
	})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatalf("edit refused: %q", msg)
	}
	post(t, a, "/config/save", nil)

	c := &gorun.Config{}
	if err := c.LoadFile(path); err != nil {
		t.Fatalf("saved file does not load: %v", err)
	}
	if len(c.Schedules) != 1 || c.Schedules[0].Cron != "30 3 * * *" || c.Schedules[0].Missed != "skip" {
		t.Errorf("schedule after edit = %+v", c.Schedules)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "0 2 * * *") {
		t.Errorf("old cron still in the file:\n%s", data)
	}
}

func TestEditScheduleRejectsBothTimings(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	post(t, a, "/config/add", scheduleForm(path, nil))
	post(t, a, "/config/save", nil)

	rec := post(t, a, "/config/edit", url.Values{
		"kind": {"schedule"}, "name": {"nightly"},
		"target": {"hello"}, "cron": {"0 2 * * *"}, "at": {"2026-12-01T02:00"}, "missed": {"skip"},
	})
	if _, bad := flashOf(t, rec.Header().Get("Location")); !bad {
		t.Error("an edit with both cron and at was accepted")
	}
	if len(a.pending) != 0 {
		t.Errorf("pending = %d changes after a rejected edit, want 0", len(a.pending))
	}
}

func TestRemoveScheduleThenSave(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	post(t, a, "/config/add", scheduleForm(path, nil))
	post(t, a, "/config/save", nil)

	post(t, a, "/config/remove", url.Values{"kind": {"schedule"}, "name": {"nightly"}})
	post(t, a, "/config/save", nil)
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "nightly") {
		t.Errorf("removed schedule still in the file:\n%s", data)
	}
	if len(a.cfg.Schedules) != 0 {
		t.Errorf("schedules after remove = %+v", a.cfg.Schedules)
	}
}
