package gorun

import (
	"context"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/jenmud/gorun/internal/envvars"
	"github.com/jenmud/gorun/internal/origin"
)

// Origin is who started a run, from where, and with what: the user, the machine, gorun's
// version, the command line, the CI job and the Gorunfile's git commit. Run fills it in and
// sends it on the process_started event, and the history saves it with the run. A program can
// set fields on Config.Origin first (Version, Trigger, Client, Labels); Run keeps them.
type Origin = origin.Origin

// GitInfo is the state of the repository a Gorunfile is in.
type GitInfo = origin.GitInfo

// originGitTimeout bounds the git commands that read the Gorunfile's commit, so a slow or stuck
// repository never holds up a run.
const originGitTimeout = 2 * time.Second

// origin is c.Origin with every field it doesn't set filled in from this process.
func (c *Config) origin() *Origin {
	o := c.Origin
	o.Labels = maps.Clone(o.Labels)
	if u, err := user.Current(); err == nil {
		o.User = cmpOr(o.User, u.Username)
		o.UID = cmpOr(o.UID, u.Uid)
	}
	o.User = cmpOr(o.User, envvars.Get("USER"), envvars.Get("USERNAME"))
	host, _ := os.Hostname()
	o.Hostname = cmpOr(o.Hostname, host)
	o.OS = cmpOr(o.OS, runtime.GOOS)
	o.Arch = cmpOr(o.Arch, runtime.GOARCH)
	if o.PID == 0 {
		o.PID = os.Getpid()
	}
	o.Version = cmpOr(o.Version, moduleVersion())
	if c.Schedule != "" {
		o.Trigger = "schedule"
	}
	o.Trigger = cmpOr(o.Trigger, "library")
	wd, _ := os.Getwd()
	o.WorkDir = cmpOr(o.WorkDir, wd)
	if o.Gorunfile == "" && c.Path != "" {
		o.Gorunfile, _ = filepath.Abs(c.Path)
	}
	if o.Command == nil {
		o.Command = os.Args
	}
	if conn := strings.Fields(envvars.Get("SSH_CONNECTION")); len(conn) > 0 {
		o.SSHClient = cmpOr(o.SSHClient, conn[0])
	}
	if o.CI == "" {
		o.CI, o.CIUser, o.CIURL = detectCI()
	}
	if o.Git == nil && o.Gorunfile != "" {
		o.Git = gitInfo(filepath.Dir(o.Gorunfile))
	}
	for k, v := range parseLabels(envvars.Get("GORUN_LABELS")) {
		if _, set := o.Labels[k]; !set {
			if o.Labels == nil {
				o.Labels = map[string]string{}
			}
			o.Labels[k] = v
		}
	}
	return &o
}

// cmpOr returns the first value that isn't empty.
func cmpOr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// moduleVersion is the gorun module's version from the build info: a tagged release's
// version, or empty for a build from a checkout.
func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if info.Main.Path == "github.com/jenmud/gorun" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/jenmud/gorun" {
			return dep.Version
		}
	}
	return ""
}

// detectCI names the CI system gorun runs in, the user who started the job, and a link to it.
func detectCI() (name, who, link string) {
	get := envvars.Get
	switch {
	case get("GITHUB_ACTIONS") == "true":
		link = get("GITHUB_SERVER_URL") + "/" + get("GITHUB_REPOSITORY") + "/actions/runs/" + get("GITHUB_RUN_ID")
		if get("GITHUB_SERVER_URL") == "" || get("GITHUB_RUN_ID") == "" {
			link = ""
		}
		return "github-actions", get("GITHUB_ACTOR"), link
	case get("GITLAB_CI") != "":
		return "gitlab", get("GITLAB_USER_LOGIN"), get("CI_JOB_URL")
	case get("JENKINS_URL") != "":
		return "jenkins", get("BUILD_USER_ID"), get("BUILD_URL")
	case get("BUILDKITE") == "true":
		return "buildkite", cmpOr(get("BUILDKITE_BUILD_CREATOR_EMAIL"), get("BUILDKITE_BUILD_CREATOR")), get("BUILDKITE_BUILD_URL")
	case get("CIRCLECI") == "true":
		return "circleci", get("CIRCLE_USERNAME"), get("CIRCLE_BUILD_URL")
	case get("CI") != "" && get("CI") != "false" && get("CI") != "0":
		return "ci", "", ""
	}
	return "", "", ""
}

// gitInfo reads the commit, branch, remote and whether there are uncommitted changes of the
// repository dir is in. It returns nil when dir isn't in one or git isn't installed.
func gitInfo(dir string) *GitInfo {
	ctx, cancel := context.WithTimeout(context.Background(), originGitTimeout)
	defer cancel()
	git := func(args ...string) (string, bool) {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
		return strings.TrimSpace(string(out)), err == nil
	}
	commit, ok := git("rev-parse", "HEAD")
	if !ok || commit == "" {
		return nil
	}
	g := &GitInfo{Commit: commit}
	if branch, ok := git("rev-parse", "--abbrev-ref", "HEAD"); ok && branch != "HEAD" {
		g.Branch = branch
	}
	status, ok := git("status", "--porcelain", "--untracked-files=no")
	g.Dirty = ok && status != ""
	if remote, ok := git("remote", "get-url", "origin"); ok {
		g.Remote = redactURL(remote)
	}
	return g
}

// redactURL drops any user name, password or token from a URL, so a remote such as
// https://user:token@host/repo.git is stored as https://host/repo.git.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil || u.Scheme == "" {
		return raw // not a URL with credentials, or an scp-style git@host:repo address
	}
	u.User = nil
	return u.String()
}

// parseLabels reads "key=value,key=value". Pairs without a key are ignored.
func parseLabels(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		k, v, _ := strings.Cut(pair, "=")
		if k = strings.TrimSpace(k); k != "" {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

// runAttributes are the OTEL span attributes of a run: the run itself and everything in its
// origin. They use OpenTelemetry's semantic convention names where there is one (user.name,
// host.name, process.command_line, vcs.*, cicd.*) and gorun.* for the rest.
func runAttributes(c *Config, o *Origin) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("gorun.run.id", c.GorunUUID),
		attribute.Bool("gorun.check_only", c.CheckOnly),
		attribute.Bool("gorun.force", c.Force),
	}
	str := func(key, value string) {
		if value != "" {
			attrs = append(attrs, attribute.String(key, value))
		}
	}
	str("gorun.title", c.Title)
	str("gorun.schedule", c.Schedule)
	str("user.name", o.User)
	str("user.id", o.UID)
	str("host.name", o.Hostname)
	str("os.type", o.OS)
	str("host.arch", o.Arch)
	if o.PID != 0 {
		attrs = append(attrs, attribute.Int("process.pid", o.PID))
	}
	str("gorun.version", o.Version)
	str("gorun.trigger", o.Trigger)
	str("process.working_directory", o.WorkDir)
	str("gorun.gorunfile", o.Gorunfile)
	if len(o.Command) > 0 {
		attrs = append(attrs, attribute.String("process.command_line", strings.Join(o.Command, " ")))
	}
	str("gorun.ssh_client", o.SSHClient)
	str("client.address", o.Client)
	str("gorun.web_user", o.WebUser)
	str("cicd.system", o.CI)
	str("gorun.ci.user", o.CIUser)
	str("cicd.pipeline.run.url.full", o.CIURL)
	if g := o.Git; g != nil {
		str("vcs.ref.head.revision", g.Commit)
		str("vcs.ref.head.name", g.Branch)
		str("vcs.repository.url.full", g.Remote)
		attrs = append(attrs, attribute.Bool("gorun.git.dirty", g.Dirty))
	}
	for _, k := range slices.Sorted(maps.Keys(o.Labels)) {
		attrs = append(attrs, attribute.String("gorun.label."+k, o.Labels[k]))
	}
	return attrs
}

// runSpanSummary is the part of runAttributes every server span repeats: which run, who and
// where.
func runSpanSummary(c *Config, o *Origin) []attribute.KeyValue {
	keep := map[attribute.Key]bool{"gorun.run.id": true, "gorun.title": true, "user.name": true,
		"host.name": true, "gorun.trigger": true, "gorun.version": true, "gorun.schedule": true,
		"gorun.web_user": true, "gorun.ci.user": true, "vcs.ref.head.revision": true}
	var out []attribute.KeyValue
	for _, a := range runAttributes(c, o) {
		if keep[a.Key] || strings.HasPrefix(string(a.Key), "gorun.label.") {
			out = append(out, a)
		}
	}
	return out
}
