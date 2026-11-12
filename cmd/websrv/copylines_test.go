package websrv

import "testing"

// A copy whose destination is a template must survive the edit form: shown as its template,
// and saved back as dst_template rather than as a literal path.
func TestCopyLinesKeepADstTemplate(t *testing.T) {
	body := map[string]any{"copy": []map[string]any{
		{"src_template": "compose.yml.tmpl", "dst_template": "{{ .Server.Vars.config }}/docker-compose.yml"},
		{"src": "app.conf", "dst": "/etc/app.conf"},
	}}
	lines, _ := copyLines(body)
	want := "template compose.yml.tmpl -> {{ .Server.Vars.config }}/docker-compose.yml\napp.conf -> /etc/app.conf"
	if lines != want {
		t.Fatalf("copyLines = %q, want %q", lines, want)
	}
	copies, err := parseCopyLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if copies[0]["dst_template"] != "{{ .Server.Vars.config }}/docker-compose.yml" || copies[0]["dst"] != nil {
		t.Errorf("templated copy = %v, want dst_template only", copies[0])
	}
	if copies[1]["dst"] != "/etc/app.conf" || copies[1]["dst_template"] != nil {
		t.Errorf("plain copy = %v, want dst only", copies[1])
	}
}
