package websrv

import "testing"

// A file filled in as a template, and a destination with a template in it, survive the edit
// form: shown as "template src -> dst", and saved back with render = true and dst as written.
func TestCopyLinesKeepTemplates(t *testing.T) {
	body := map[string]any{"copy": []map[string]any{
		{"src": "compose.yml.tmpl", "render": true, "dst": "{{ .Server.Vars.config }}/docker-compose.yml"},
		{"src": "app.conf", "dst": "/etc/app.conf"},
		{"src": "keep.sh", "dst": "/opt/{{ x }}", "raw": true},
	}}
	lines, kept := copyLines(body)
	want := "template compose.yml.tmpl -> {{ .Server.Vars.config }}/docker-compose.yml\napp.conf -> /etc/app.conf"
	if lines != want || kept != 1 {
		t.Fatalf("copyLines = %q, %d kept; want %q, the raw copy kept", lines, kept, want)
	}
	copies, err := parseCopyLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if copies[0]["src"] != "compose.yml.tmpl" || copies[0]["render"] != true || copies[0]["dst"] != "{{ .Server.Vars.config }}/docker-compose.yml" {
		t.Errorf("template copy = %v", copies[0])
	}
	if copies[1]["dst"] != "/etc/app.conf" || copies[1]["render"] != nil {
		t.Errorf("plain copy = %v", copies[1])
	}
}

// A copy's mode, owner and group are shown after its destination, and the form reads them back
// from the end of the line; a destination with spaces in it is kept whole.
func TestCopyLinesKeepModeOwnerAndGroup(t *testing.T) {
	body := map[string]any{"copy": []map[string]any{
		{"src": "app.conf", "dst": "/etc/my app/app.conf", "mode": "0640", "owner": "nobody", "group": "nogroup"},
		{"src": "tool.sh", "dst": "/usr/local/bin/tool", "perm": "0755"},
	}}
	lines, _ := copyLines(body)
	want := "app.conf -> /etc/my app/app.conf mode=0640 owner=nobody group=nogroup\ntool.sh -> /usr/local/bin/tool mode=0755"
	if lines != want {
		t.Fatalf("copyLines = %q, want %q", lines, want)
	}
	copies, err := parseCopyLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if copies[0]["dst"] != "/etc/my app/app.conf" || copies[0]["mode"] != "0640" || copies[0]["owner"] != "nobody" || copies[0]["group"] != "nogroup" {
		t.Errorf("copy with options = %v", copies[0])
	}
	if copies[1]["mode"] != "0755" || copies[1]["dst"] != "/usr/local/bin/tool" {
		t.Errorf("perm copy = %v", copies[1])
	}
}

// A bad mode in the form is refused with the line it is on.
func TestCopyLinesRefuseABadMode(t *testing.T) {
	if _, err := parseCopyLines("a.conf -> /etc/a.conf mode=rwx"); err == nil {
		t.Fatal("a symbolic mode was accepted")
	}
}
