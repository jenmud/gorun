package websrv

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestTaskAddAndEditFromTheTasksPage(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)

	rec := post(t, a, "/tasks/add", url.Values{"name": {"restart"}, "cmd": {"echo restart"}, "file": {path}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad || !strings.Contains(msg, "Added task restart") {
		t.Fatalf("add flash = %q (bad %v)", msg, bad)
	}
	if _, ok := a.cfg.Tasks["restart"]; !ok {
		t.Fatal("added task is not in the reloaded config")
	}

	rec = post(t, a, "/tasks/edit", url.Values{"name": {"hello"}, "cmd": {"echo changed"}, "description": {"says hi"}, "sudo": {"on"}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatalf("edit flash = %q", msg)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `cmd = "echo changed"`) || !strings.Contains(string(data), "sudo = true") {
		t.Errorf("edit not written:\n%s", data)
	}
	if task := a.cfg.Tasks["hello"]; task.Cmd != "echo changed" || !task.Sudo {
		t.Errorf("reloaded task = %+v", task)
	}
}

func TestTaskAddRefusesAnEmptyCommand(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	before, _ := os.ReadFile(path)

	rec := post(t, a, "/tasks/add", url.Values{"name": {"empty"}, "file": {path}})
	if _, bad := flashOf(t, rec.Header().Get("Location")); !bad {
		t.Error("a task with no command was added")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("the file changed after a refused add")
	}
}

func TestBundleAddAndEditFromTheBundlesPage(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)

	rec := post(t, a, "/bundles/add", url.Values{"name": {"base"}, "tasks": {"hello"}, "file": {path}})
	if _, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatal("bundle add was refused")
	}
	if _, ok := a.cfg.Bundles["base"]; !ok {
		t.Fatal("added bundle is not in the reloaded config")
	}

	rec = post(t, a, "/bundles/edit", url.Values{"name": {"base"}, "tasks": {"hello, @base"}, "description": {"edited"}})
	if _, bad := flashOf(t, rec.Header().Get("Location")); !bad {
		t.Error("a bundle that includes itself was saved")
	}
	rec = post(t, a, "/bundles/edit", url.Values{"name": {"base"}, "tasks": {"hello"}, "description": {"edited"}})
	if _, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatal("bundle edit was refused")
	}
	if b := a.cfg.Bundles["base"]; b.Description != "edited" {
		t.Errorf("reloaded bundle = %+v", b)
	}
}

func TestEditThatBreaksTheFileIsRolledBack(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	before, _ := os.ReadFile(path)

	// a bundle naming a task that doesn't exist saves fine but no longer loads: it must not stick
	rec := post(t, a, "/bundles/add", url.Values{"name": {"broken"}, "tasks": {"no-such-task"}, "file": {path}})
	if _, bad := flashOf(t, rec.Header().Get("Location")); !bad {
		t.Fatal("a bundle with an unknown task was accepted")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("the file was left broken:\n%s", after)
	}
	if _, ok := a.cfg.Bundles["broken"]; ok {
		t.Error("the broken bundle is in the running config")
	}
}

func TestAddAndEditForms(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)

	tasks := get(t, a, "/tasks").Body.String()
	for _, want := range []string{`action="/tasks/add"`, "Add a task"} {
		if !strings.Contains(tasks, want) {
			t.Errorf("tasks page missing %q", want)
		}
	}
	if !strings.Contains(get(t, a, "/tasks").Body.String(), `href="/tasks?edit=hello"`) {
		t.Error("tasks page has no Edit link")
	}
	if body := get(t, a, "/tasks?edit=hello").Body.String(); !strings.Contains(body, `action="/tasks/edit"`) || !strings.Contains(body, "echo hi") {
		t.Error("task edit panel does not show the command")
	}

	post(t, a, "/bundles/add", url.Values{"name": {"base"}, "tasks": {"hello"}, "file": {path}})
	if body := get(t, a, "/bundles?edit=base").Body.String(); !strings.Contains(body, `action="/bundles/edit"`) {
		t.Error("bundle edit panel missing")
	}
	if body := get(t, a, "/bundles").Body.String(); !strings.Contains(body, `action="/bundles/add"`) {
		t.Error("bundles page has no add form")
	}
	if body := get(t, a, "/schedules").Body.String(); !strings.Contains(body, `action="/schedules/add"`) {
		t.Error("schedules page has no add form")
	}
}

// Task and bundle names link to their cards, and the cards have the matching ids.
func TestTaskAndBundleNamesLinkToTheirCards(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	post(t, a, "/bundles/add", url.Values{"name": {"base"}, "tasks": {"hello"}, "file": {path}})
	post(t, a, "/schedules/add", url.Values{
		"name": {"nightly"}, "target": {"@base"}, "cron": {"0 2 * * *"}, "missed": {"skip"}, "file": {path},
	})

	cases := map[string][]string{
		"/schedules": {`href="/bundles#bundle-base"`},
		"/bundles":   {`href="/tasks#task-hello"`, `id="bundle-base"`},
		"/tasks":     {`id="task-hello"`},
	}
	for page, wants := range cases {
		body := get(t, a, page).Body.String()
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %s", page, want)
			}
		}
	}
}

// A bundle named "@x" in a task's list links to the card with id "bundle-x".
func TestBundleLinkFromATaskOpensTheBundleCard(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	post(t, a, "/bundles/add", url.Values{"name": {"extra"}, "tasks": {"hello"}, "file": {path}})
	body := get(t, a, "/tasks").Body.String()
	if !strings.Contains(body, `href="/bundles#bundle-extra"`) || strings.Contains(body, "#bundle-@") {
		t.Errorf("bundle link from a task does not point at the bundle's card")
	}
	if !strings.Contains(get(t, a, "/bundles").Body.String(), `id="bundle-extra"`) {
		t.Error("bundle card id is wrong")
	}
}
