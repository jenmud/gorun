package websrv

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jenmud/gorun"
)

// sshOnlyGorunfile has one SSH server, so nothing can run on this machine yet.
const sshOnlyGorunfile = `[tasks.deploy]
cmd = "echo deploy"

[servers]
tasks = ["deploy"]

[servers.hosts.web-1]
hostname = "web-1.example.com"
    [servers.hosts.web-1.ssh]
    username = "deploy"
`

func loadText(t *testing.T, body string) (*app, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Gorunfile")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp(t)
	if err := a.loadConfig(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopScheduler)
	return a, path
}

func localServers(c *gorun.Config) []gorun.Server {
	var out []gorun.Server
	for _, s := range c.Servers {
		if s.IsLocal() {
			out = append(out, s)
		}
	}
	return out
}

func TestAddingALocalTaskAddsALocalServer(t *testing.T) {
	a, path := loadText(t, sshOnlyGorunfile)

	rec := post(t, a, "/tasks/add", url.Values{"name": {"datetime"}, "cmd": {"date"}, "local": {"on"}, "file": {path}})
	msg, bad := flashOf(t, rec.Header().Get("Location"))
	if bad || !strings.Contains(msg, "Added task datetime.") || !strings.Contains(msg, "Added a local server, localhost") {
		t.Fatalf("flash = %q (bad %v)", msg, bad)
	}
	local := localServers(a.cfg)
	if len(local) != 1 || local[0].Name != "localhost" {
		t.Fatalf("local servers = %+v, want localhost", local)
	}
	// its own empty list keeps the [servers] default list (deploy) off this machine
	if len(local[0].Tasks) != 0 {
		t.Errorf("the new local server runs %v, want nothing of its own", local[0].Tasks)
	}
	if err := a.cfg.CanRun([]string{"datetime"}); err != nil {
		t.Errorf("the local task still can't run: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "[servers.hosts.localhost]\ntasks = []") {
		t.Errorf("file:\n%s", data)
	}

	// a second local task doesn't add another server
	post(t, a, "/tasks/add", url.Values{"name": {"uptime"}, "cmd": {"uptime"}, "local": {"on"}, "file": {path}})
	if n := len(localServers(a.cfg)); n != 1 {
		t.Errorf("%d local servers after a second local task, want 1", n)
	}
}

func TestAddingARemoteTaskAddsNoServer(t *testing.T) {
	a, path := loadText(t, sshOnlyGorunfile)
	rec := post(t, a, "/tasks/add", url.Values{"name": {"restart"}, "cmd": {"echo restart"}, "file": {path}})
	if msg, _ := flashOf(t, rec.Header().Get("Location")); strings.Contains(msg, "local server") {
		t.Errorf("flash = %q", msg)
	}
	if n := len(localServers(a.cfg)); n != 0 {
		t.Errorf("%d local servers, want none", n)
	}
}

func TestEditingATaskToLocalAddsALocalServer(t *testing.T) {
	a, _ := loadText(t, sshOnlyGorunfile+`
[tasks.datetime]
cmd = "date"
`)
	rec := post(t, a, "/tasks/edit", url.Values{"name": {"datetime"}, "cmd": {"date"}, "local": {"on"}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad || !strings.Contains(msg, "Added a local server") {
		t.Fatalf("flash = %q (bad %v)", msg, bad)
	}
	if n := len(localServers(a.cfg)); n != 1 {
		t.Errorf("%d local servers, want 1", n)
	}
}

func TestAScheduleForALocalTaskAddsALocalServer(t *testing.T) {
	// the server called localhost is an SSH server here, so the new one is named local
	a, path := loadText(t, strings.ReplaceAll(sshOnlyGorunfile, "web-1", "localhost")+`
[tasks.datetime]
cmd = "date"
local = true
`)
	rec := post(t, a, "/schedules/add", url.Values{"name": {"datetime"}, "target": {"datetime"}, "cron": {"* * * * *"}, "file": {path}})
	if msg, bad := flashOf(t, rec.Header().Get("Location")); bad || !strings.Contains(msg, "Added a local server, local,") {
		t.Fatalf("flash = %q (bad %v)", msg, bad)
	}
	if err := a.cfg.CanRun([]string{"datetime"}); err != nil {
		t.Errorf("the schedule's task still can't run: %v", err)
	}
}

func TestALocalServerIsNotAddedTwice(t *testing.T) {
	a, path := loadText(t, sshOnlyGorunfile+"\n[servers.hosts.box]\n")
	before := len(a.cfg.Servers)
	post(t, a, "/tasks/add", url.Values{"name": {"datetime"}, "cmd": {"date"}, "local": {"on"}, "file": {path}})
	if len(a.cfg.Servers) != before {
		t.Errorf("servers went from %d to %d; the Gorunfile already had a local server", before, len(a.cfg.Servers))
	}
}
