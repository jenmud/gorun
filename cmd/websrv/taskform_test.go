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
