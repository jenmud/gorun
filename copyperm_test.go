package gorun

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// A copy's mode is an octal permission; anything else is refused when the task is filled in.
func TestCopyModeIsOctal(t *testing.T) {
	for in, want := range map[string]os.FileMode{"0644": 0o644, "644": 0o644, "0600": 0o600, "0755": 0o755, "4755": 0o755} {
		got, ok, err := Copy{Mode: in}.modeOf()
		if err != nil || !ok || got != want {
			t.Errorf("modeOf(%q) = %o, %v, %v; want %o", in, got, ok, err, want)
		}
	}
	if got, ok, _ := (Copy{Perm: "0640"}).modeOf(); !ok || got != 0o640 {
		t.Errorf("perm = %o, %v; want 0640 (perm is another spelling of mode)", got, ok)
	}
	if _, ok, _ := (Copy{}).modeOf(); ok {
		t.Error("a copy without a mode sets one")
	}
	for _, bad := range []string{"rwx", "0999", "u=rw", "10000"} {
		if _, _, err := (Copy{Mode: bad}).modeOf(); err == nil {
			t.Errorf("modeOf(%q) accepted a bad mode", bad)
		}
	}
}

// ls -l's permission characters become the permission bits, with setuid and friends ignored.
func TestPermOctalReadsLsPermissions(t *testing.T) {
	for in, want := range map[string]int{
		"rw-r--r--": 0o644, "rwxr-x---": 0o750, "rwsr-xr-x": 0o755, "rwSr--r--": 0o644, "rwxrwxrwt": 0o777, "---------": 0,
	} {
		got, ok := permOctal(in)
		if !ok || got != want {
			t.Errorf("permOctal(%q) = %o, %v; want %o", in, got, ok, want)
		}
	}
	if _, ok := permOctal("rw-r--r"); ok {
		t.Error("a short permission string was read")
	}
}

// The check says what differs, so a changed mode, owner or group is named.
func TestPermMismatchNamesWhatDiffers(t *testing.T) {
	c := Copy{Mode: "0600", Owner: "deploy", Group: "www"}
	if got := permMismatch(c, permLine{"rw-------", "deploy", "www"}, true); got != "" {
		t.Errorf("a matching target differs: %q", got)
	}
	if got := permMismatch(c, permLine{"rw-r--r--", "root", "www"}, true); !strings.Contains(got, "mode 0600 wanted, rw-r--r-- found") || !strings.Contains(got, "owner deploy wanted, root found") {
		t.Errorf("mismatch = %q", got)
	}
	if got := permMismatch(c, permLine{}, false); got != "is missing" {
		t.Errorf("missing = %q", got)
	}
}

// A local copy with a mode gets that mode, not the source's.
func TestLocalCopySetsTheCopyMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.conf")
	if err := os.WriteFile(src, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "app.conf")
	task := &Task{Name: "copy", Copy: []Copy{{Src: src, Dst: dst, Mode: "0600"}}}
	if err := CopyFiles(nil, &Server{Name: "local"}, task); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dst); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode of the copy = %v, %v; want 0600", info.Mode().Perm(), err)
	}
}

// A folder's mode applies to the files inside it.
func TestLocalFolderCopySetsTheModeOfEachFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "site")
	if err := os.MkdirAll(filepath.Join(src, "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "css", "a.css"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "www")
	task := &Task{Name: "copy", Copy: []Copy{{Src: src, Dst: dst, Mode: "0640"}}}
	if err := CopyFiles(nil, &Server{Name: "local"}, task); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dst, "css", "a.css")); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("file mode = %v, %v; want 0640", info.Mode().Perm(), err)
	}
}

// Setting the owner to the login user needs no root, so it runs here and the owner is kept.
func TestLocalCopySetsTheOwner(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip("no current user:", err)
	}
	group, err := user.LookupGroupId(me.Gid)
	if err != nil {
		t.Skip("no group name:", err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	task := &Task{Name: "copy", Copy: []Copy{{Src: src, Dst: dst, Owner: me.Username, Group: group.Name}}}
	if err := CopyFiles(nil, &Server{Name: "local"}, task); err != nil {
		t.Skip("chown to the login user isn't allowed here:", err)
	}
	got, ok := lsLocal(context.Background(), dst)
	if !ok || got.user != me.Username || got.group != group.Name {
		t.Errorf("owner = %+v, %v; want %s:%s", got, ok, me.Username, group.Name)
	}
}

// The check catches a copy whose content is right but whose mode is wrong, and passes once the
// copy has set it.
func TestCheckSeesAWrongModeLocally(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(dst, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := Task{Name: "copy", CheckCopies: true, Copy: []Copy{{Src: src, Dst: dst, Mode: "0600"}}}
	te := &TaskExec{task: task}
	res, err := te.compareCopies(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 1 || !strings.Contains(res.Stdout, "mode 0600 wanted") {
		t.Fatalf("check = exit %d, %q; want a changed mode", res.ExitCode, res.Stdout)
	}
	if err := CopyFiles(nil, &Server{Name: "local"}, &task); err != nil {
		t.Fatal(err)
	}
	res, err = te.compareCopies(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Errorf("after the copy the check says %q", res.Stdout)
	}
}

// A mode that isn't octal stops the task before it copies anything.
func TestCopyModeIsCheckedWhenTheTaskIsFilled(t *testing.T) {
	task := Task{Name: "copy", Copy: []Copy{{Src: "a", Dst: "b", Mode: "rwx"}}}
	if _, err := task.ForServer(&Server{Name: "web-1"}); err == nil || !strings.Contains(err.Error(), "octal") {
		t.Errorf("ForServer error = %v; want it to say the mode is not octal", err)
	}
}

// The commands that set a copy's mode and owner: plain, under sudo, for a folder, and nothing for
// a copy that sets neither.
func TestPermCommands(t *testing.T) {
	c := Copy{Mode: "0640", Owner: "deploy", Group: "www"}
	for _, tc := range []struct {
		dir, sudo bool
		want      string
	}{
		{false, false, "chmod 0640 '/srv/app.conf' && chown 'deploy:www' '/srv/app.conf'"},
		{false, true, "sudo -n " + shWrap("chmod 0640 '/srv/app.conf' && chown 'deploy:www' '/srv/app.conf'")},
		{true, false, "find '/srv/app.conf' -type f -exec chmod 0640 {} + && chown -R 'deploy:www' '/srv/app.conf'"},
	} {
		got, err := permCommand(c, "/srv/app.conf", tc.dir, tc.sudo)
		if err != nil || got != tc.want {
			t.Errorf("dir=%v sudo=%v: command = %q, %v; want %q", tc.dir, tc.sudo, got, err, tc.want)
		}
	}
	if got, _ := permCommand(Copy{Group: "www"}, "/srv/x", false, false); got != "chown ':www' '/srv/x'" {
		t.Errorf("group only = %q", got)
	}
	if got, _ := permCommand(Copy{}, "/srv/x", false, true); got != "" {
		t.Errorf("a copy with no mode or owner runs %q", got)
	}
}
