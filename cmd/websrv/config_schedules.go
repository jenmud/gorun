package websrv

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/cmd/web/ui"
)

// scheduleFields are the keys of a schedule's table that the forms set, in the order they show.
var scheduleFields = []string{"description", "target", "cron", "at", "timezone", "missed"}

// newScheduleBody builds a schedule's table from an add form.
func newScheduleBody(form url.Values) map[string]any {
	body := map[string]any{}
	for _, k := range scheduleFields {
		setIf(body, k, form.Get(k))
	}
	return body
}

// applyScheduleEdit writes an edit form's fields over body. A cleared field removes the key.
func applyScheduleEdit(body map[string]any, form url.Values) {
	for _, k := range scheduleFields {
		if v := strings.TrimSpace(form.Get(k)); v != "" {
			body[k] = v
		} else {
			delete(body, k)
		}
	}
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
