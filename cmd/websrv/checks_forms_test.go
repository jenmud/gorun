package websrv

import (
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

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

	// the card shows both commands, folded like the task's command
	page = get(t, a, "/tasks").Body.String()
	if strings.Contains(page, `<details open class="group/step"`) {
		t.Error("a card's check or verify starts open; it should start folded")
	}
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

// The task forms add and edit a task's copies, dependencies, timeout and retries, and the card
// lists the copies.
func TestTaskFormsEditCopiesAndRunSettings(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)

	page := get(t, a, "/tasks").Body.String()
	for _, want := range []string{`name="copies"`, `name="depends_on"`, `name="timeout"`, `name="retries"`, `name="check_copies"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the add form has no %s field", want)
		}
	}
	rec := post(t, a, "/tasks/add", url.Values{"name": {"compose-up"}, "cmd": {"docker compose up -d"}, "file": {path},
		"copies":     {"scripts/stack-ok.sh -> /home/ubuntu/stack-ok.sh\r\n\r\ntemplate templates/app.conf.tmpl -> /etc/app.conf"},
		"depends_on": {"hello"}, "timeout": {"10m"}, "retries": {"2"}, "check_copies": {"on"}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatalf("add refused: %q", msg)
	}
	task := a.cfg.Tasks["compose-up"]
	if len(task.Copy) != 2 || !strings.HasSuffix(task.Copy[0].Src, "scripts/stack-ok.sh") || task.Copy[0].Dst != "/home/ubuntu/stack-ok.sh" ||
		!strings.HasSuffix(task.Copy[1].SrcTpl, "templates/app.conf.tmpl") || task.Copy[1].Dst != "/etc/app.conf" {
		t.Errorf("copies = %+v", task.Copy)
	}
	if !slices.Equal(task.DependsOn, []string{"hello"}) || task.Timeout != 10*time.Minute || task.Retries != 2 || !task.CheckCopies {
		t.Errorf("settings = depends %v timeout %v retries %d check_copies %v", task.DependsOn, task.Timeout, task.Retries, task.CheckCopies)
	}

	// the card lists each copy
	page = get(t, a, "/tasks").Body.String()
	if !strings.Contains(page, "stack-ok.sh → /home/ubuntu/stack-ok.sh") || !strings.Contains(page, "copied to the server before the command runs") {
		t.Error("the card doesn't list the copies")
	}

	// the edit form shows them as written, and saving an edited list replaces them
	edit := get(t, a, "/tasks?edit=compose-up").Body.String()
	for _, want := range []string{"scripts/stack-ok.sh -&gt; /home/ubuntu/stack-ok.sh", "template templates/app.conf.tmpl -&gt; /etc/app.conf", `value="10m"`, `value="2"`, `value="hello"`} {
		if !strings.Contains(edit, want) {
			t.Errorf("the edit form is missing %s", want)
		}
	}
	rec = post(t, a, "/tasks/edit", url.Values{"name": {"compose-up"}, "cmd": {"docker compose up -d"},
		"copies": {"scripts/stack-ok.sh -> /opt/stack-ok.sh"}, "depends_on": {""}, "timeout": {""}, "retries": {"0"}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatalf("edit refused: %q", msg)
	}
	task = a.cfg.Tasks["compose-up"]
	if len(task.Copy) != 1 || task.Copy[0].Dst != "/opt/stack-ok.sh" || len(task.DependsOn) != 0 || task.Timeout != 0 || task.Retries != 0 {
		t.Errorf("after the edit: %+v", task)
	}

	// a line without -> is refused, and the file is left as it was
	before, _ := os.ReadFile(path)
	rec = post(t, a, "/tasks/edit", url.Values{"name": {"compose-up"}, "cmd": {"docker compose up -d"}, "copies": {"just-a-path"}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); !bad || !strings.Contains(msg, "local/path -> /path/on/server") {
		t.Errorf("a bad copy line: flash %q (bad %v)", msg, bad)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("the file changed after a refused edit")
	}
}

// Editing the copies keeps a task's inline-text copies, which the form can't show.
func TestEditKeepsInlineCopies(t *testing.T) {
	a, path := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	data, _ := os.ReadFile(path)
	extra := "\n[tasks.motd]\ncmd = \"true\"\ncopy = [{ content = \"hello\", dst = \"/etc/motd\" }, { src = \"a.sh\", dst = \"/opt/a.sh\" }]\n"
	if err := os.WriteFile(path, append(data, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.reloadConfig(); err != nil {
		t.Fatal(err)
	}
	edit := get(t, a, "/tasks?edit=motd").Body.String()
	if !strings.Contains(edit, "a.sh -&gt; /opt/a.sh") || !strings.Contains(edit, "Plus 1 copy of inline text") {
		t.Error("the edit form doesn't show the file copy and the kept inline copy")
	}
	rec := post(t, a, "/tasks/edit", url.Values{"name": {"motd"}, "cmd": {"true"}, "copies": {"b.sh -> /opt/b.sh"}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad {
		t.Fatalf("edit refused: %q", msg)
	}
	got := a.cfg.Tasks["motd"].CopySummaries()
	if len(got) != 2 || got[0] != "inline text (5 bytes) → /etc/motd" || !strings.HasSuffix(got[1], "b.sh → /opt/b.sh") {
		t.Errorf("copies after the edit: %q", got)
	}
}

// The Config page's edit dialog has the same fields.
func TestConfigEditDialogHasCopies(t *testing.T) {
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	edit := get(t, a, "/config/edit?kind=task&name=hello").Body.String()
	for _, want := range []string{`name="copies"`, `name="depends_on"`, `name="timeout"`, `name="retries"`} {
		if !strings.Contains(edit, want) {
			t.Errorf("the Config edit dialog has no %s", want)
		}
	}
}
