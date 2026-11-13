package gorun

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// noCI clears the variables CI systems set, so a test sees the same thing on a laptop and in
// GitHub Actions.
func noCI(t *testing.T) {
	for _, k := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "JENKINS_URL", "BUILDKITE", "CIRCLECI"} {
		t.Setenv(k, "")
	}
}

// originConfig is a Gorunfile with one local task, saved in a git repository, ready to run.
func originConfig(t *testing.T) (*Config, *gitRepo) {
	t.Helper()
	text := "[tasks.hello]\ncmd = \"true\"\nlocal = true\n\n[servers]\ntasks = [\"hello\"]\n[servers.hosts.local]\n"
	repo := newGitRepo(t, map[string]string{"Gorunfile": text})
	repo.run("remote", "add", "origin", "https://deploy:s3cr3t-token@git.example.com/ops/infra.git")
	cfg := loadConfig(t, text)
	cfg.Path = filepath.Join(repo.dir, "Gorunfile")
	return cfg, repo
}

// startedEvent runs cfg and returns its process_started event.
func startedEvent(t *testing.T, cfg *Config) ProcessStartedEvent {
	t.Helper()
	var mu sync.Mutex
	var started ProcessStartedEvent
	cfg.OnEvent = func(event any) {
		if ev, ok := event.(ProcessStartedEvent); ok {
			mu.Lock()
			started = ev
			mu.Unlock()
		}
	}
	if err := cfg.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if started.Origin == nil {
		t.Fatal("the started event has no origin")
	}
	return started
}

// Every run says who started it, on which machine, how, with which gorun, from which
// Gorunfile and commit, and with which labels.
func TestRunSaysWhoStartedItAndWhere(t *testing.T) {
	noCI(t)
	t.Setenv("GORUN_LABELS", "team=ops, ticket=CHG-42,,=nokey")
	t.Setenv("SSH_CONNECTION", "10.0.0.9 51234 10.0.0.1 22")
	cfg, repo := originConfig(t)
	cfg.Origin = Origin{Version: "v9.9.9", Labels: map[string]string{"team": "platform"}}

	o := startedEvent(t, cfg).Origin
	host, _ := os.Hostname()
	wd, _ := os.Getwd()
	checks := map[string][2]string{
		"hostname":   {o.Hostname, host},
		"os":         {o.OS, runtime.GOOS},
		"arch":       {o.Arch, runtime.GOARCH},
		"version":    {o.Version, "v9.9.9"},
		"trigger":    {o.Trigger, "library"},
		"workdir":    {o.WorkDir, wd},
		"gorunfile":  {o.Gorunfile, filepath.Join(repo.dir, "Gorunfile")},
		"ssh client": {o.SSHClient, "10.0.0.9"},
		"label team": {o.Labels["team"], "platform"}, // what the program set wins
		"ticket":     {o.Labels["ticket"], "CHG-42"},
		"commit":     {o.Git.Commit, repo.run("rev-parse", "HEAD")},
		"branch":     {o.Git.Branch, "main"},
		"remote":     {o.Git.Remote, "https://git.example.com/ops/infra.git"},
		"ci":         {o.CI, ""},
	}
	for what, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %q, want %q", what, c[0], c[1])
		}
	}
	if o.User == "" || o.PID != os.Getpid() || !slices.Equal(o.Command, os.Args) || o.Git.Dirty || len(o.Labels) != 2 {
		t.Errorf("origin = %+v", o)
	}
	if cfg.Origin.Labels["ticket"] != "" {
		t.Error("the run changed the program's Config.Origin.Labels")
	}

	// uncommitted changes to the Gorunfile's repository show
	if err := os.WriteFile(filepath.Join(repo.dir, "Gorunfile"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if g := gitInfo(repo.dir); g == nil || !g.Dirty {
		t.Errorf("git = %+v, want it dirty", g)
	}
	if g := gitInfo(t.TempDir()); g != nil {
		t.Errorf("a directory outside git gave %+v", g)
	}
}

// A run started by a schedule says so, whatever the program set.
func TestScheduledRunsHaveTheScheduleTrigger(t *testing.T) {
	noCI(t)
	cfg, _ := originConfig(t)
	cfg.Schedule, cfg.Origin.Trigger = "nightly", "web"
	if got := startedEvent(t, cfg).Origin.Trigger; got != "schedule" {
		t.Errorf("trigger = %q, want schedule", got)
	}
}

// In CI the run names the CI system, who started the job and a link to it.
func TestCIRunsSayWhichJobAndWho(t *testing.T) {
	noCI(t)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_ACTOR", "jen")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "jenmud/gorun")
	t.Setenv("GITHUB_RUN_ID", "42")
	if name, who, link := detectCI(); name != "github-actions" || who != "jen" || link != "https://github.com/jenmud/gorun/actions/runs/42" {
		t.Errorf("github: %q %q %q", name, who, link)
	}
	noCI(t)
	t.Setenv("GITLAB_CI", "true")
	t.Setenv("GITLAB_USER_LOGIN", "sam")
	t.Setenv("CI_JOB_URL", "https://gitlab.example.com/x/-/jobs/7")
	if name, who, link := detectCI(); name != "gitlab" || who != "sam" || link != "https://gitlab.example.com/x/-/jobs/7" {
		t.Errorf("gitlab: %q %q %q", name, who, link)
	}
	noCI(t)
	t.Setenv("CI", "true")
	if name, _, _ := detectCI(); name != "ci" {
		t.Errorf("plain CI = %q, want ci", name)
	}
	noCI(t)
	if name, _, _ := detectCI(); name != "" {
		t.Errorf("outside CI = %q", name)
	}
}

// A git remote's password or token is never recorded.
func TestRemoteCredentialsAreNotRecorded(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:token@github.com/a/b.git": "https://github.com/a/b.git",
		"https://ghp_abc@github.com/a/b.git":    "https://github.com/a/b.git",
		"ssh://git@host:22/a/b.git":             "ssh://host:22/a/b.git",
		"git@github.com:a/b.git":                "git@github.com:a/b.git",
		"https://github.com/a/b.git":            "https://github.com/a/b.git",
	} {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// recorder is a tracer provider that keeps every span's name and attributes.
type recorder struct {
	noop.TracerProvider
	mu    sync.Mutex
	spans []*recSpan
}

type recTracer struct {
	noop.Tracer
	r *recorder
}

type recSpan struct {
	noop.Span
	r     *recorder
	name  string
	attrs map[attribute.Key]attribute.Value
}

func (r *recorder) Tracer(string, ...trace.TracerOption) trace.Tracer { return recTracer{r: r} }

func (t recTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	s := &recSpan{r: t.r, name: name, attrs: map[attribute.Key]attribute.Value{}}
	cfg := trace.NewSpanStartConfig(opts...)
	s.SetAttributes(cfg.Attributes()...)
	t.r.mu.Lock()
	t.r.spans = append(t.r.spans, s)
	t.r.mu.Unlock()
	return trace.ContextWithSpan(ctx, s), s
}

func (s *recSpan) SetAttributes(kv ...attribute.KeyValue) {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	for _, a := range kv {
		s.attrs[a.Key] = a.Value
	}
}

// span returns the first span with the given name.
func (r *recorder) span(t *testing.T, name string) map[attribute.Key]attribute.Value {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.spans {
		if s.name == name {
			return s.attrs
		}
	}
	t.Fatalf("no %s span", name)
	return nil
}

// The run's trace carries who started it and where: all of it on the gorun.run span, and the
// main fields on each server's span too.
func TestRunTraceCarriesWhoAndWhere(t *testing.T) {
	noCI(t)
	t.Setenv("GORUN_LABELS", "ticket=CHG-42")
	rec := &recorder{}
	otel.SetTracerProvider(rec)
	cfg, repo := originConfig(t)
	cfg.Title = "deploy"
	cfg.Origin = Origin{Version: "v9.9.9", Trigger: "cli"}
	o := startedEvent(t, cfg).Origin

	run := rec.span(t, "gorun.run")
	for key, want := range map[attribute.Key]string{
		"gorun.run.id": cfg.GorunUUID, "gorun.title": "deploy", "user.name": o.User, "host.name": o.Hostname,
		"os.type": runtime.GOOS, "gorun.version": "v9.9.9", "gorun.trigger": "cli",
		"process.command_line": strings.Join(os.Args, " "), "gorun.gorunfile": o.Gorunfile,
		"vcs.ref.head.revision": repo.run("rev-parse", "HEAD"), "vcs.ref.head.name": "main",
		"vcs.repository.url.full": "https://git.example.com/ops/infra.git", "gorun.label.ticket": "CHG-42",
	} {
		if got := run[key].AsString(); got != want {
			t.Errorf("gorun.run %s = %q, want %q", key, got, want)
		}
	}
	if run["process.pid"].AsInt64() != int64(os.Getpid()) || run["gorun.tasks.total"].AsInt64() != 1 {
		t.Errorf("gorun.run pid/tasks = %v/%v", run["process.pid"], run["gorun.tasks.total"])
	}

	server := rec.span(t, "server.run")
	for _, key := range []attribute.Key{"gorun.run.id", "user.name", "host.name", "gorun.trigger", "gorun.version", "gorun.label.ticket"} {
		if server[key] != run[key] {
			t.Errorf("server.run %s = %v, want %v", key, server[key], run[key])
		}
	}
	if _, ok := server["process.command_line"]; ok {
		t.Error("server.run repeats the whole origin; it should carry only the main fields")
	}
}
