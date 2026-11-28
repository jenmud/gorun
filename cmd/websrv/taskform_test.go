package websrv

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

// A browser sends a textarea's lines ending in \r\n; a \r left in a command breaks it in sh
// ("set: Illegal option -").
func TestTaskFormsSaveUnixLineBreaks(t *testing.T) {
	a, path := loadText(t, sshOnlyGorunfile)
	script := "set -e\r\necho one\r\necho two\r\n"
	check := "test -f /tmp/a\r\ntest -f /tmp/b"

	post(t, a, "/tasks/add", url.Values{"name": {"report"}, "cmd": {script}, "check": {check}, "verify": {check}, "file": {path}})
	if got := a.cfg.Tasks["report"]; got.Cmd != "set -e\necho one\necho two" || got.Check != "test -f /tmp/a\ntest -f /tmp/b" || got.Verify != got.Check {
		t.Errorf("added task = cmd %q, check %q, verify %q", got.Cmd, got.Check, got.Verify)
	}

	post(t, a, "/tasks/edit", url.Values{"name": {"deploy"}, "cmd": {script}, "check": {check}, "verify": {check}})
	if got := a.cfg.Tasks["deploy"]; got.Cmd != "set -e\necho one\necho two" || got.Check != "test -f /tmp/a\ntest -f /tmp/b" || got.Verify != got.Check {
		t.Errorf("edited task = cmd %q, check %q, verify %q", got.Cmd, got.Check, got.Verify)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "\r") || strings.Contains(string(data), `\r`) {
		t.Errorf("the Gorunfile kept a \\r:\n%s", data)
	}
}

func TestTaskFormsSaveVarsAndEnvVars(t *testing.T) {
	a, path := loadText(t, sshOnlyGorunfile)

	post(t, a, "/tasks/add", url.Values{"name": {"web"}, "cmd": {"echo {{ .vars.port }}"}, "file": {path},
		"vars": {"port = 8080\r\ntag = \"2.4.1\""}, "envvars": {"APP_ENV = \"production\"\nTOKEN = \"${REGISTRY_TOKEN}\""}})
	got := a.cfg.Tasks["web"]
	if got.Vars["port"] != int64(8080) || got.Vars["tag"] != "2.4.1" {
		t.Errorf("added vars = %#v", got.Vars)
	}
	// the secret stays a reference in the Gorunfile; it is expanded only when the task runs
	if got.EnvVars["APP_ENV"] != "production" || got.EnvVars["TOKEN"] != "${REGISTRY_TOKEN}" {
		t.Errorf("added envvars = %#v", got.EnvVars)
	}

	// the edit form shows them, and a nested table keeps its shape
	body, err := currentBody(a.cfg, a.pending, kindTask, "web")
	if err != nil {
		t.Fatal(err)
	}
	if f := editFields(body); !strings.Contains(f.Vars, "port = 8080") || !strings.Contains(f.EnvVars, `TOKEN = "${REGISTRY_TOKEN}"`) {
		t.Errorf("edit form vars %q, envvars %q", f.Vars, f.EnvVars)
	}
	post(t, a, "/tasks/edit", url.Values{"name": {"web"}, "cmd": {"echo {{ .vars.db.port }}"}, "vars": {"[db]\nport = 5432"}, "envvars": {""}})
	got = a.cfg.Tasks["web"]
	if db, _ := got.Vars["db"].(map[string]any); db["port"] != int64(5432) || got.Vars["port"] != nil {
		t.Errorf("edited vars = %#v", got.Vars)
	}
	if len(got.EnvVars) != 0 {
		t.Errorf("a cleared envvars field kept %#v", got.EnvVars)
	}

	// an env var must be a string, and bad TOML is refused
	for _, bad := range []url.Values{{"envvars": {"PORT = 8080"}}, {"vars": {"port = "}}} {
		form := url.Values{"name": {"web"}, "cmd": {"true"}}
		for k, v := range bad {
			form[k] = v
		}
		rec := post(t, a, "/tasks/edit", form)
		if msg, isBad := flashOf(t, rec.Header().Get("Location")); !isBad {
			t.Errorf("%v was saved (flash %q)", bad, msg)
		}
	}
	if a.cfg.Tasks["web"].Cmd != "echo {{ .vars.db.port }}" {
		t.Errorf("a refused edit changed the task: %q", a.cfg.Tasks["web"].Cmd)
	}
}
