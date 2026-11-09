package websrv

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// scheduleFields are the keys of a schedule's table that the forms set, in the order they show.
// The one-off time is not among them: the forms send it as a date and time, see setOnceAt.
var scheduleFields = []string{"description", "target", "cron", "timezone", "missed"}

// pickerLayout is the value of a date and time picker: a wall clock time with no zone.
const pickerLayout = "2006-01-02T15:04"

// newScheduleBody builds a schedule's table from an add form.
func newScheduleBody(form url.Values) (map[string]any, error) {
	body := map[string]any{}
	for _, k := range scheduleFields {
		setIf(body, k, form.Get(k))
	}
	if err := setOnceAt(body, form); err != nil {
		return nil, err
	}
	return body, nil
}

// applyScheduleEdit writes an edit form's fields over body. A cleared field removes the key.
func applyScheduleEdit(body map[string]any, form url.Values) error {
	for _, k := range scheduleFields {
		if v := strings.TrimSpace(form.Get(k)); v != "" {
			body[k] = v
		} else {
			delete(body, k)
		}
	}
	return setOnceAt(body, form)
}

// setOnceAt stores the one-off time from a form's date and time picker. The picker's time is
// read in the schedule's zone and saved in UTC, so the file means the same moment whatever the
// zone. A form without the picker keeps its own at text, and an empty picker clears the time.
func setOnceAt(body map[string]any, form url.Values) error {
	if !form.Has("at_local") {
		if v := strings.TrimSpace(form.Get("at")); v != "" {
			body["at"] = v
		} else {
			delete(body, "at")
		}
		return nil
	}
	local := strings.TrimSpace(form.Get("at_local"))
	if local == "" {
		delete(body, "at")
		return nil
	}
	zone := strings.TrimSpace(form.Get("timezone"))
	loc, err := time.LoadLocation(zoneOr(zone))
	if err != nil {
		return fmt.Errorf("time zone %q is not known", zone)
	}
	t, err := time.ParseInLocation(pickerLayout, local, loc)
	if err != nil {
		return fmt.Errorf("the date and time %q is not valid", local)
	}
	body["at"] = t.UTC().Format(time.RFC3339)
	return nil
}

// atForPicker shows a stored one-off time in the schedule's zone, for the date and time picker.
func atForPicker(at, zone string) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return at // written without an offset: already a wall clock time in the schedule's zone
	}
	loc, err := time.LoadLocation(zoneOr(zone))
	if err != nil {
		return t.UTC().Format(pickerLayout)
	}
	return t.In(loc).Format(pickerLayout)
}

// scheduleFromBody reads the schedule a table describes, so it can be checked the way the
// loader checks it.
func scheduleFromBody(name string, body map[string]any) gorun.Schedule {
	str := func(k string) string {
		s, _ := body[k].(string)
		return s
	}
	return gorun.Schedule{
		Name:        name,
		Description: str("description"),
		Target:      str("target"),
		Cron:        str("cron"),
		At:          str("at"),
		Timezone:    str("timezone"),
		Missed:      str("missed"),
	}
}

// checkChange reports why a schedule change would not load, against the loaded config. Other
// kinds are checked when the file is saved and reloaded.
func (a *app) checkChange(ch gorun.Change) error {
	return checkChangeIn(a.cfg, ch)
}

// checkChangeIn is checkChange against a given config.
func checkChangeIn(cfg *gorun.Config, ch gorun.Change) error {
	if ch.Kind != kindSchedule {
		return nil
	}
	return cfg.CheckSchedule(scheduleFromBody(ch.Name, ch.Body))
}

// scheduleBodyIn returns the table of the named schedule in a file.
func scheduleBodyIn(file, name string) (map[string]any, error) {
	var doc map[string]any
	if _, err := toml.DecodeFile(file, &doc); err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	tables, _ := doc["schedule"].([]map[string]any)
	for _, t := range tables {
		if n, _ := t["name"].(string); n == name {
			body := cloneMap(t)
			delete(body, "name")
			return body, nil
		}
	}
	return nil, fmt.Errorf("no schedule named %s in %s", name, file)
}

// scheduleEntityRows lists the loaded schedules for the Config page.
func scheduleEntityRows(cfg *gorun.Config, root string) []ui.EntityRow {
	out := make([]ui.EntityRow, 0, len(cfg.Schedules))
	for _, s := range cfg.Schedules {
		when := "once at " + s.At
		if s.Cron != "" {
			when = "cron " + s.Cron
		}
		zone := s.Timezone
		if zone == "" {
			zone = "UTC"
		}
		out = append(out, ui.EntityRow{
			Name:   s.Name,
			Detail: fmt.Sprintf("%s · %s · %s", s.Target, when, zone),
			Source: rel(root, s.Source),
		})
	}
	return out
}

// targetNames lists what a schedule can target: every task, and every bundle as "@name".
func targetNames(cfg *gorun.Config) []string {
	names := slices.Sorted(maps.Keys(cfg.Tasks))
	for _, b := range slices.Sorted(maps.Keys(cfg.Bundles)) {
		names = append(names, "@"+b)
	}
	return names
}
