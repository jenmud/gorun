package websrv

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jenmud/gorun"
)

// The Tasks page adds and edits a task's check and verify, and its card shows them.
func TestTasksPageAddsAndEditsCheckAndVerify(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)

	page := get(t, a, "/tasks").Body.String()
	for _, want := range []string{`name="check"`, `name="verify"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the add form has no %s field", want)
		}
	}
	rec := post(t, a, "/tasks/add", url.Values{"name": {"nginx"}, "cmd": {"apt-get install -y nginx"},
		"check": {"dpkg -s nginx"}, "verify": {"systemctl is-active nginx"}, "file": {path}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatalf("add refused: %q", msg)
	}
	if task := a.cfg.Tasks["nginx"]; task.Check != "dpkg -s nginx" || task.Verify != "systemctl is-active nginx" {
		t.Fatalf("added task = %+v", task)
	}

	// the card shows both commands, open
	page = get(t, a, "/tasks").Body.String()
	for _, want := range []string{"dpkg -s nginx", "systemctl is-active nginx", "exit 0 means nothing to do", "a non-zero exit fails the task"} {
		if !strings.Contains(page, want) {
			t.Errorf("the task card is missing %q", want)
		}
	}

	// the edit form is filled in, and saving changes them
	edit := get(t, a, "/tasks?edit=nginx").Body.String()
	if !strings.Contains(edit, `value="dpkg -s nginx"`) || !strings.Contains(edit, `value="systemctl is-active nginx"`) {
		t.Error("the edit form doesn't show the check and verify")
	}
	rec = post(t, a, "/tasks/edit", url.Values{"name": {"nginx"}, "cmd": {"apt-get install -y nginx"},
		"check": {"command -v nginx"}, "verify": {""}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatalf("edit refused: %q", msg)
	}
	if task := a.cfg.Tasks["nginx"]; task.Check != "command -v nginx" || task.Verify != "" {
		t.Errorf("edited task = %+v (a cleared verify should be removed)", task)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `check = "command -v nginx"`) || strings.Contains(string(data), "verify") {
		t.Errorf("file after the edit:\n%s", data)
	}
}

// The Config page's add form and edit dialog carry the check and verify too, and editing a task
// keeps its check_copies.
func TestConfigPageAddsAndEditsCheckAndVerify(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)

	if page := get(t, a, "/config").Body.String(); !strings.Contains(page, `name="check"`) || !strings.Contains(page, `name="verify"`) {
		t.Error("the Config page's add task form has no check or verify")
	}
	rec := post(t, a, "/config/add", url.Values{"kind": {"task"}, "name": {"nginx"}, "cmd": {"apt-get install -y nginx"},
		"check": {"dpkg -s nginx"}, "verify": {"nginx -v"}, "file": {path}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatalf("add refused: %q", msg)
	}
	post(t, a, "/config/save", nil)
	c := &gorun.Config{}
	if err := c.LoadFile(path); err != nil {
		t.Fatal(err)
	}
	if task := c.Tasks["nginx"]; task.Check != "dpkg -s nginx" || task.Verify != "nginx -v" {
		t.Fatalf("saved task = %+v", task)
	}

	edit := get(t, a, "/config/edit?kind=task&name=nginx").Body.String()
	for _, want := range []string{`value="dpkg -s nginx"`, `value="nginx -v"`, `name="check_copies"`} {
		if !strings.Contains(edit, want) {
			t.Errorf("the edit dialog is missing %s", want)
		}
	}
	rec = post(t, a, "/config/edit", url.Values{"kind": {"task"}, "name": {"nginx"}, "cmd": {"apt-get install -y nginx"},
		"check": {"dpkg -s nginx"}, "verify": {"systemctl is-active nginx"}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatalf("edit refused: %q", msg)
	}
	post(t, a, "/config/save", nil)
	c = &gorun.Config{}
	if err := c.LoadFile(path); err != nil {
		t.Fatal(err)
	}
	if task := c.Tasks["nginx"]; task.Verify != "systemctl is-active nginx" {
		t.Errorf("edited task = %+v", task)
	}
}

// Editing a task that compares its copies keeps check_copies when the box stays ticked, and
// shows it ticked.
func TestEditKeepsCheckCopies(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	data, _ := os.ReadFile(path)
	src := t.TempDir() + "/site.conf"
	_ = os.WriteFile(src, []byte("x"), 0o644)
	extra := "\n[tasks.ship]\ncmd = \"echo ship\"\ncheck_copies = true\ncopy = [{ src = \"" + src + "\", dst = \"/tmp/site.conf\" }]\n"
	if err := os.WriteFile(path, append(data, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.reloadConfig(); err != nil {
		t.Fatal(err)
	}
	if edit := get(t, a, "/tasks?edit=ship").Body.String(); !strings.Contains(edit, `name="check_copies" checked`) {
		t.Error("check_copies isn't ticked in the edit form")
	}
	rec := post(t, a, "/tasks/edit", url.Values{"name": {"ship"}, "cmd": {"echo shipped"}, "check_copies": {"on"}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatalf("edit refused: %q", msg)
	}
	if task := a.cfg.Tasks["ship"]; !task.CheckCopies || task.Cmd != "echo shipped" || len(task.Copy) != 1 {
		t.Errorf("edited task = %+v", task)
	}
}
