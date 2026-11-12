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
