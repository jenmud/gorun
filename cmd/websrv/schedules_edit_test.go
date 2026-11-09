package websrv

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

// editForm is what the schedules page's edit form posts for the named schedule.
func editForm(name string, extra url.Values) url.Values {
	form := url.Values{
		"name":        {name},
		"target":      {"check"},
		"cron":        {"30 3 * * *"},
		"at":          {""},
		"timezone":    {"Australia/Sydney"},
		"missed":      {"run"},
		"description": {"moved to 03:30"},
	}
	for k, v := range extra {
		form[k] = v
	}
	return form
}

func TestSchedulesPageShowsTheEditForm(t *testing.T) {
	a := loadSchedules(t)
	body := get(t, a, "/schedules?edit=nightly").Body.String()
	for _, want := range []string{`action="/schedules/edit"`, `value="0 2 * * *"`, "Edit schedule"} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form missing %q", want)
		}
	}
	if !strings.Contains(get(t, a, "/schedules").Body.String(), `href="/schedules?edit=nightly"`) {
		t.Error("the schedules table has no Edit link")
	}
}

func TestScheduleEditSavesToTheFileAndKeepsItsPause(t *testing.T) {
	a := loadSchedules(t)
	post(t, a, "/schedules/pause", url.Values{"schedule": {"nightly"}})

	rec := post(t, a, "/schedules/edit", editForm("nightly", nil))
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad || !strings.Contains(msg, "Updated schedule nightly") {
		t.Fatalf("edit flash = %q (bad %v)", msg, bad)
	}

	data, err := os.ReadFile(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `cron = "30 3 * * *"`) || strings.Contains(string(data), `cron = "0 2 * * *"`) {
		t.Errorf("file not updated:\n%s", data)
	}
	if len(a.pending) != 0 {
		t.Errorf("pending = %d changes, want none: the edit goes straight to the file", len(a.pending))
	}
	if st := statusOf(a, "nightly"); st != "paused" {
		t.Errorf("after edit, status = %q, want paused (the pause should survive the reload)", st)
	}
}

func TestScheduleEditRefusesUnsavedChangesOnConfig(t *testing.T) {
	a := loadSchedules(t)
	post(t, a, "/config/remove", url.Values{"kind": {kindSchedule}, "name": {"nightly"}})

	rec := post(t, a, "/schedules/edit", editForm("nightly", nil))
	if _, bad := flashOf(t, rec.Header().Get("Location")); !bad {
		t.Error("an edit went ahead over an unsaved change on the Config page")
	}
	data, _ := os.ReadFile(a.cfgPath)
	if !strings.Contains(string(data), `cron = "0 2 * * *"`) {
		t.Errorf("file changed despite the refusal:\n%s", data)
	}
	if body := get(t, a, "/schedules?edit=nightly").Body.String(); !strings.Contains(body, "Save or discard") {
		t.Error("the edit page does not explain the conflict")
	}
}

func TestScheduleEditRejectsBadTiming(t *testing.T) {
	a := loadSchedules(t)
	rec := post(t, a, "/schedules/edit", editForm("nightly", url.Values{"at": {"2026-12-01T02:00"}}))
	if _, bad := flashOf(t, rec.Header().Get("Location")); !bad {
		t.Error("a schedule with cron and at was saved")
	}
	data, _ := os.ReadFile(a.cfgPath)
	if strings.Contains(string(data), "30 3 * * *") {
		t.Errorf("file changed despite the refusal:\n%s", data)
	}
}
