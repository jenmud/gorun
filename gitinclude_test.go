package gorun

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo is a local repository standing in for one on GitHub or Bitbucket, reached by file://.
type gitRepo struct {
	t   *testing.T
	dir string
}

func newGitRepo(t *testing.T, files map[string]string) *gitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	r := &gitRepo{t: t, dir: t.TempDir()}
	r.run("init", "-q", "-b", "main")
	r.commit("first", files)
	return r
}

func (r *gitRepo) run(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes files and commits them, returning the commit hash.
func (r *gitRepo) commit(msg string, files map[string]string) string {
	r.t.Helper()
	writeFiles(r.t, r.dir, files)
	r.run("add", "-A")
	r.run("commit", "-q", "-m", msg)
	return r.run("rev-parse", "HEAD")
}

func (r *gitRepo) url() string { return "file://" + filepath.ToSlash(r.dir) }

// library is a task library: tasks and a bundle, with a script the task runs.
var library = map[string]string{
	"tasks/web.toml": `
[tasks.hello-lib]
description = "Say hello from the library"
cmd_file = "scripts/hello.sh"
local = true

[bundles.web-baseline]
tasks = ["hello-lib"]
`,
	"scripts/hello.sh": "echo hello from the library v1\n",
}

func gitTree(t *testing.T, include string) (string, map[string]string) {
	t.Helper()
	t.Setenv("GORUN_CACHE_DIR", t.TempDir())
	return t.TempDir(), map[string]string{"Gorunfile": `include = ['` + include + `']

[servers]
tasks = ["@web-baseline"]

[servers.hosts.localhost]
`}
}

func loadGit(t *testing.T, dir string, files map[string]string, update bool) (*Config, error) {
	t.Helper()
	writeFiles(t, dir, files)
	c := &Config{UpdateIncludes: update}
	return c, c.LoadFile(filepath.Join(dir, "Gorunfile"))
}

func TestGitIncludeLoadsAndLocksALibrary(t *testing.T) {
	repo := newGitRepo(t, library)
	first := repo.run("rev-parse", "HEAD")
	repo.run("tag", "v1.0.0")

	dir, files := gitTree(t, "git::"+repo.url()+"//tasks/*.toml?ref=v1.0.0")
	c, err := loadGit(t, dir, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Tasks["hello-lib"]; !ok {
		t.Fatalf("library task not loaded: %v", c.Tasks)
	}
	if len(c.Includes) != 1 || c.Includes[0].Commit != first || c.Includes[0].Ref != "v1.0.0" {
		t.Errorf("Includes = %+v, want v1.0.0 at %s", c.Includes, first)
	}

	lock, err := os.ReadFile(filepath.Join(dir, "Gorunfile.lock"))
	if err != nil {
		t.Fatalf("no lock file: %v", err)
	}
	if !strings.Contains(string(lock), first) || !strings.Contains(string(lock), `ref = "v1.0.0"`) {
		t.Errorf("lock file does not pin v1.0.0 to %s:\n%s", first, lock)
	}

	// the library's cmd_file is relative to its repository, not to the Gorunfile
	c.ResolvePaths(dir)
	if got := c.Tasks["hello-lib"].CmdFile; !strings.HasSuffix(got, filepath.Join(first, "scripts", "hello.sh")) {
		t.Errorf("cmd_file = %q, want the script in the checkout", got)
	}
	var out string
	c.OnEvent = func(event any) {
		if ev, ok := event.(TaskEvent); ok && ev.State == TaskDone {
			out = ev.Stdout
		}
	}
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello from the library v1") {
		t.Errorf("library task printed %q", out)
	}
}

func TestGitIncludeStaysOnTheLockedCommitUntilUpdated(t *testing.T) {
	repo := newGitRepo(t, library)
	first := repo.run("rev-parse", "HEAD")
	repo.run("tag", "v1.0.0")
	dir, files := gitTree(t, "git::"+repo.url()+"//tasks/*.toml?ref=v1.0.0")
	if _, err := loadGit(t, dir, files, false); err != nil {
		t.Fatal(err)
	}

	// the tag is moved to a new commit, as happens when someone retags a release
	second := repo.commit("second", map[string]string{"scripts/hello.sh": "echo hello from the library v2\n"})
	repo.run("tag", "-f", "v1.0.0")

	c, err := loadGit(t, dir, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Includes[0].Commit != first {
		t.Errorf("without -update-includes the include moved to %s, want the locked %s", c.Includes[0].Commit, first)
	}

	c, err = loadGit(t, dir, files, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Includes[0].Commit != second {
		t.Errorf("with -update-includes the include is at %s, want %s", c.Includes[0].Commit, second)
	}
	lock, _ := os.ReadFile(filepath.Join(dir, "Gorunfile.lock"))
	if !strings.Contains(string(lock), second) || strings.Contains(string(lock), first) {
		t.Errorf("lock file not updated to %s:\n%s", second, lock)
	}
}

func TestGitIncludeWorksOfflineFromTheCache(t *testing.T) {
	repo := newGitRepo(t, library)
	dir, files := gitTree(t, "git::"+repo.url()+"//tasks/*.toml?ref=main")
	if _, err := loadGit(t, dir, files, false); err != nil {
		t.Fatal(err)
	}
	// the repository goes away: the lock file and the cache are enough
	if err := os.RemoveAll(repo.dir); err != nil {
		t.Fatal(err)
	}
	c, err := loadGit(t, dir, files, false)
	if err != nil {
		t.Fatalf("offline load: %v", err)
	}
	if _, ok := c.Tasks["hello-lib"]; !ok {
		t.Error("library task missing when loading from the cache")
	}
}

func TestGitIncludeByCommitHash(t *testing.T) {
	repo := newGitRepo(t, library)
	first := repo.run("rev-parse", "HEAD")
	repo.commit("second", map[string]string{"scripts/hello.sh": "echo v2\n"})

	dir, files := gitTree(t, "git::"+repo.url()+"//tasks/*.toml?ref="+first)
	c, err := loadGit(t, dir, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Includes[0].Commit != first {
		t.Errorf("commit = %s, want %s", c.Includes[0].Commit, first)
	}
}

func TestGitIncludeRefusals(t *testing.T) {
	repo := newGitRepo(t, map[string]string{
		"tasks/web.toml":   "[tasks.ok]\ncmd = \"true\"\n",
		"bad/servers.toml": "[servers.hosts.sneaky]\nhostname = \"evil.example.com\"\n",
		"bad/envvars.toml": "envvars = { PATH = \"/tmp\" }\n",
		"bad/escape.toml":  "include = [\"../../*.toml\"]\n",
	})
	cases := map[string]struct{ include, want string }{
		"no ref":          {"git::" + repo.url() + "//tasks/*.toml", "?ref="},
		"git protocol":    {"git::git://github.com/x/y.git//tasks/*.toml?ref=v1", "unauthenticated"},
		"unknown option":  {"git::" + repo.url() + "//tasks/*.toml?ref=main&depth=1", "unknown option"},
		"path escapes":    {"git::" + repo.url() + "//../*.toml?ref=main", "inside the repository"},
		"no such ref":     {"git::" + repo.url() + "//tasks/*.toml?ref=v9.9.9", "not found"},
		"no such files":   {"git::" + repo.url() + "//nope/*.toml?ref=main", "no files match"},
		"defines servers": {"git::" + repo.url() + "//bad/servers.toml?ref=main", "only tasks and bundles"},
		"sets envvars":    {"git::" + repo.url() + "//bad/envvars.toml?ref=main", "only tasks and bundles"},
		"reaches outside": {"git::" + repo.url() + "//bad/escape.toml?ref=main", "outside its git repository"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir, _ := gitTree(t, tc.include)
			files := map[string]string{"Gorunfile": "include = ['" + tc.include + "']\n"}
			_, err := loadGit(t, dir, files, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestParseGitInclude(t *testing.T) {
	cases := map[string]gitSpec{
		"git::https://github.com/you/tasks.git//web/*.toml?ref=v1.2.0": {repo: "https://github.com/you/tasks.git", path: filepath.FromSlash("web/*.toml"), ref: "v1.2.0"},
		"git::ssh://git@bitbucket.org/you/tasks.git//a.toml?ref=main":  {repo: "ssh://git@bitbucket.org/you/tasks.git", path: "a.toml", ref: "main"},
		"git::git@github.com:you/tasks.git//web/*.toml?ref=v2":         {repo: "git@github.com:you/tasks.git", path: filepath.FromSlash("web/*.toml"), ref: "v2"},
		"git::https://gitlab.com/you/tasks.git?ref=v1":                 {repo: "https://gitlab.com/you/tasks.git", path: "*.toml", ref: "v1"},
	}
	for in, want := range cases {
		got, err := parseGitInclude(in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%s:\n got %+v\nwant %+v", in, got, want)
		}
	}
}

// A run says which commit of each task library it used, in its started event.
func TestRunReportsItsIncludeCommits(t *testing.T) {
	repo := newGitRepo(t, library)
	commit := repo.run("rev-parse", "HEAD")
	repo.run("tag", "v1.0.0")
	dir, files := gitTree(t, "git::"+repo.url()+"//tasks/*.toml?ref=v1.0.0")
	c, err := loadGit(t, dir, files, false)
	if err != nil {
		t.Fatal(err)
	}
	c.ResolvePaths(dir)

	var started ProcessStartedEvent
	c.OnEvent = func(event any) {
		if ev, ok := event.(ProcessStartedEvent); ok {
			started = ev
		}
	}
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(started.Includes) != 1 {
		t.Fatalf("started event includes = %+v", started.Includes)
	}
	if inc := started.Includes[0]; inc.Source != repo.url() || inc.Ref != "v1.0.0" || inc.Commit != commit {
		t.Errorf("include = %+v, want %s @ v1.0.0 -> %s", inc, repo.url(), commit)
	}
}

// A library task that depends on a task nobody defines fails the load, naming where it came from,
// even when nothing runs it.
func TestGitIncludeWithAnUnknownDependencyFailsToLoad(t *testing.T) {
	repo := newGitRepo(t, map[string]string{"tasks.toml": "[tasks.deploy]\ncmd = \"true\"\ndepends_on = [\"prepare\"]\n"})
	dir := t.TempDir()
	t.Setenv("GORUN_CACHE_DIR", t.TempDir())
	_, err := loadGit(t, dir, map[string]string{"Gorunfile": "include = ['git::" + repo.url() + "//tasks.toml?ref=main']\n"}, false)
	if err == nil {
		t.Fatal("a library with an unknown dependency loaded")
	}
	for _, want := range []string{`task "deploy"`, `unknown task "prepare"`, repo.url() + "@main", "tasks.toml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	// defining the task in the Gorunfile satisfies it
	files := map[string]string{"Gorunfile": "include = ['git::" + repo.url() + "//tasks.toml?ref=main']\n\n[tasks.prepare]\ncmd = \"true\"\n"}
	if _, err := loadGit(t, dir, files, false); err != nil {
		t.Errorf("with prepare defined: %v", err)
	}
}

// A local file's task with an unknown dependency names the file.
func TestUnknownDependencyNamesTheFile(t *testing.T) {
	_, err := loadTree(t, map[string]string{
		"Gorunfile":  "include = [\"tasks.toml\"]\n",
		"tasks.toml": "[tasks.deploy]\ncmd = \"true\"\ndepends_on = [\"prepare\"]\n",
	})
	if err == nil || !strings.Contains(err.Error(), `(in tasks.toml) depends on unknown task "prepare"`) {
		t.Fatalf("error = %v", err)
	}
}
