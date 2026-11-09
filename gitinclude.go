package gorun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jenmud/gorun/internal/envvars"
)

// gitPrefix marks an include as a git repository, the way Terraform and go-getter spell it.
const gitPrefix = "git::"

// gitTimeout bounds each git command, so an unreachable host fails instead of hanging.
const gitTimeout = 2 * time.Minute

// IncludeSource is a git include as resolved for this load: where it came from, the ref the
// Gorunfile asked for, and the commit that was used.
type IncludeSource struct {
	Source string `json:"source"` // the repository URL
	Ref    string `json:"ref"`    // the ?ref= value: a tag, branch or commit
	Commit string `json:"commit"` // the commit the files were read from
	Dir    string `json:"-"`      // the local checkout the files were read from
}

// gitSpec is a parsed git include: git::<repo>//<path>?ref=<ref>.
type gitSpec struct {
	repo string // the repository URL, as git takes it
	path string // the files inside the repository, a glob; defaults to *.toml
	ref  string
}

var commitHash = regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`)

// isGitInclude reports whether an include pattern names a git repository.
func isGitInclude(pattern string) bool {
	return strings.HasPrefix(pattern, gitPrefix)
}

// parseGitInclude reads a git include: git::<repo>//<path>?ref=<ref>. The repository may be an
// https://, ssh:// or file:// URL, or the scp-like user@host:path form. The ref is required, so
// a Gorunfile always says which version of the files it wants.
func parseGitInclude(pattern string) (gitSpec, error) {
	rest := strings.TrimPrefix(pattern, gitPrefix)
	bad := func(format string, args ...any) (gitSpec, error) {
		return gitSpec{}, fmt.Errorf("include %q: %s", pattern, fmt.Sprintf(format, args...))
	}

	var query string
	if i := strings.LastIndex(rest, "?"); i >= 0 {
		rest, query = rest[:i], rest[i+1:]
	}
	params, err := url.ParseQuery(query)
	if err != nil {
		return bad("bad query: %v", err)
	}
	for key := range params {
		if key != "ref" {
			return bad("unknown option %q: only ref is supported", key)
		}
	}
	ref := params.Get("ref")
	if ref == "" {
		return bad("pin a version with ?ref=<tag, branch or commit>, for example ?ref=v1.2.0")
	}

	// the path inside the repository follows a double slash, after the URL's own scheme://
	start := 0
	if i := strings.Index(rest, "://"); i >= 0 {
		start = i + len("://")
	}
	repo, path := rest, ""
	if i := strings.Index(rest[start:], "//"); i >= 0 {
		repo, path = rest[:start+i], rest[start+i+2:]
	}
	if repo == "" {
		return bad("no repository")
	}

	switch {
	case strings.HasPrefix(repo, "git://"):
		return bad("the git:// protocol is unauthenticated and unencrypted; use https:// or ssh://")
	case strings.HasPrefix(repo, "https://"), strings.HasPrefix(repo, "http://"),
		strings.HasPrefix(repo, "ssh://"), strings.HasPrefix(repo, "file://"):
	case strings.Contains(repo, "@") && strings.Contains(repo, ":"): // git@github.com:org/repo.git
	default:
		return bad("repository %q should be an https://, ssh:// or file:// URL, or user@host:path", repo)
	}

	if path == "" {
		path = "*.toml"
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return bad("path %q must stay inside the repository", path)
	}
	return gitSpec{repo: repo, path: clean, ref: ref}, nil
}

// lockEntry is one line of the lock file: the commit a source and ref resolved to.
type lockEntry struct {
	Source string `toml:"source"`
	Ref    string `toml:"ref"`
	Commit string `toml:"commit"`
}

// lockFile is the layout of a Gorunfile's .lock file.
type lockFile struct {
	Include []lockEntry `toml:"include"`
}

const lockHeader = `# This file records the commit each git include in the Gorunfile resolved to, so every run
# uses the same commands. Commit it with the Gorunfile. Run gorun with -update-includes to move
# tags and branches to their latest commits.

`

// gitIncludes resolves the git includes of one load, against the lock file next to the root
// Gorunfile.
type gitIncludes struct {
	lockPath string
	update   bool // re-resolve refs instead of using the lock
	locked   map[string]string
	used     map[string]lockEntry
	changed  bool
	roots    []string // checkout directories, resolved through symlinks
	sources  []IncludeSource
}

func lockKey(source, ref string) string { return source + "\x00" + ref }

// newGitIncludes reads the lock file for the root Gorunfile at root, if there is one.
func newGitIncludes(root string, update bool) (*gitIncludes, error) {
	g := &gitIncludes{lockPath: root + ".lock", update: update, locked: map[string]string{}, used: map[string]lockEntry{}}
	var lf lockFile
	if _, err := toml.DecodeFile(g.lockPath, &lf); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(g.lockPath), err)
	}
	for _, e := range lf.Include {
		g.locked[lockKey(e.Source, e.Ref)] = e.Commit
	}
	return g, nil
}

// files fetches the repository at the pinned commit, if it isn't cached already, and returns the
// files the include names.
func (g *gitIncludes) files(pattern string) ([]string, error) {
	spec, err := parseGitInclude(pattern)
	if err != nil {
		return nil, err
	}
	commit, err := g.commit(spec)
	if err != nil {
		return nil, fmt.Errorf("include %q: %w", pattern, err)
	}
	dir, err := checkout(spec, commit)
	if err != nil {
		return nil, fmt.Errorf("include %q: %w", pattern, err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("include %q: %w", pattern, err)
	}
	if !slices.Contains(g.roots, real) {
		g.roots = append(g.roots, real)
		g.sources = append(g.sources, IncludeSource{Source: spec.repo, Ref: spec.ref, Commit: commit, Dir: real})
	}

	matches, err := filepath.Glob(filepath.Join(real, spec.path))
	if err != nil {
		return nil, fmt.Errorf("include %q: %w", pattern, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("include %q: no files match %s in %s at %s", pattern, filepath.ToSlash(spec.path), spec.repo, short(commit))
	}
	sort.Strings(matches)
	return matches, nil
}

// commit is the commit an include uses: the ref itself when it is a commit hash, else the locked
// commit, else what the ref points to now (which is then locked).
func (g *gitIncludes) commit(spec gitSpec) (string, error) {
	key := lockKey(spec.repo, spec.ref)
	commit := ""
	switch {
	case commitHash.MatchString(spec.ref):
		commit = strings.ToLower(spec.ref)
	case !g.update && g.locked[key] != "":
		commit = g.locked[key]
	default:
		var err error
		if commit, err = lsRemote(spec.repo, spec.ref); err != nil {
			return "", err
		}
		slog.Info("git include resolved", "source", spec.repo, "ref", spec.ref, "commit", commit)
	}
	if g.locked[key] != commit {
		g.changed = true
	}
	g.used[key] = lockEntry{Source: spec.repo, Ref: spec.ref, Commit: commit}
	return commit, nil
}

// within returns the checkout a file belongs to, or "" when it is not from a git include.
func (g *gitIncludes) within(file string) string {
	if g == nil {
		return ""
	}
	for _, root := range g.roots {
		if file == root || strings.HasPrefix(file, root+string(filepath.Separator)) {
			return root
		}
	}
	return ""
}

// writeLock saves the commits used, when they differ from the lock file. Without -update-includes
// entries this load didn't use are kept, so commenting an include out doesn't lose its pin.
func (g *gitIncludes) writeLock() error {
	if g == nil || !g.changed {
		return nil
	}
	entries := map[string]lockEntry{}
	if !g.update {
		for key, commit := range g.locked {
			source, ref, _ := strings.Cut(key, "\x00")
			entries[key] = lockEntry{Source: source, Ref: ref, Commit: commit}
		}
	}
	for key, e := range g.used {
		entries[key] = e
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var lf lockFile
	for _, key := range keys {
		lf.Include = append(lf.Include, entries[key])
	}

	var buf bytes.Buffer
	buf.WriteString(lockHeader)
	if err := toml.NewEncoder(&buf).Encode(lf); err != nil {
		return err
	}
	if err := writeFileAtomic(g.lockPath, buf.Bytes()); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(g.lockPath), err)
	}
	return nil
}

// lsRemote finds the commit a ref points to: a tag (peeled to its commit), a branch, or a full
// ref name such as refs/heads/main.
func lsRemote(repo, ref string) (string, error) {
	out, err := git("", "ls-remote", repo, ref, "refs/tags/"+ref+"^{}")
	if err != nil {
		return "", err
	}
	found := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		hash, name, ok := strings.Cut(line, "\t")
		if ok {
			found[name] = hash
		}
	}
	for _, name := range []string{"refs/tags/" + ref + "^{}", "refs/tags/" + ref, "refs/heads/" + ref, ref} {
		if hash := found[name]; hash != "" {
			return hash, nil
		}
	}
	return "", fmt.Errorf("ref %q not found in %s", ref, repo)
}

// cacheDir is where checkouts are kept: GORUN_CACHE_DIR, or gorun's folder in the user's cache
// directory (~/.cache/gorun on Linux).
func cacheDir() (string, error) {
	if dir := envvars.Get("GORUN_CACHE_DIR"); dir != "" {
		return dir, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no cache directory: set GORUN_CACHE_DIR: %w", err)
	}
	return filepath.Join(dir, "gorun"), nil
}

// checkout returns a directory holding the repository's files at commit, fetching them only when
// they aren't cached. A cached checkout needs no network, so runs work offline.
func checkout(spec gitSpec, commit string) (string, error) {
	base, err := cacheDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(spec.repo))
	dir := filepath.Join(base, "git", hex.EncodeToString(sum[:8]), commit)
	if _, err := os.Stat(filepath.Join(dir, ".gorun-complete")); err == nil {
		return dir, nil
	}

	slog.Info("fetching git include", "source", spec.repo, "ref", spec.ref, "commit", commit)
	tmp := fmt.Sprintf("%s.tmp-%d", dir, os.Getpid())
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if _, err := git(tmp, "init", "-q"); err != nil {
		return "", err
	}
	if err := fetchCommit(tmp, spec, commit); err != nil {
		return "", err
	}
	if _, err := git(tmp, "-c", "advice.detachedHead=false", "checkout", "-q", "--detach", commit); err != nil {
		return "", err
	}
	// the files are all that's needed; dropping .git keeps the cache small and plain
	if err := os.RemoveAll(filepath.Join(tmp, ".git")); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, ".gorun-complete"), []byte(spec.repo+" "+commit+"\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, statErr := os.Stat(filepath.Join(dir, ".gorun-complete")); statErr == nil {
			return dir, nil // another gorun fetched it first
		}
		return "", err
	}
	return dir, nil
}

// fetchCommit fetches one commit into the repository at dir. It asks for the commit directly,
// which hosts such as GitHub, GitLab and Bitbucket allow. Otherwise it fetches the ref and checks
// it still points at the locked commit, so a moved tag is reported instead of used silently.
func fetchCommit(dir string, spec gitSpec, commit string) error {
	if _, err := git(dir, "fetch", "-q", "--depth", "1", spec.repo, commit); err == nil {
		return nil
	}
	if commitHash.MatchString(spec.ref) {
		// a server that refuses fetching by hash: take every branch and tag, then the commit
		if _, err := git(dir, "fetch", "-q", "--tags", spec.repo, "+refs/heads/*:refs/remotes/origin/*"); err != nil {
			return err
		}
		return nil
	}
	if _, err := git(dir, "fetch", "-q", "--depth", "1", spec.repo, spec.ref); err != nil {
		return err
	}
	got, err := git(dir, "rev-parse", "FETCH_HEAD^{commit}")
	if err != nil {
		return err
	}
	if got = strings.TrimSpace(got); got != commit {
		return fmt.Errorf("%s at %s is now %s, not the locked %s; run gorun with -update-includes to use the new commit", spec.repo, spec.ref, short(got), short(commit))
	}
	return nil
}

// git runs a git command in dir. Prompts are turned off, as Go does, so a repository that needs
// credentials fails with git's message instead of waiting for input; git's own credential
// helpers, SSH agent and keys are used as usual.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=")
	// ssh prompts on its own (host keys, passphrases); stop it as Go does, unless the user set it up
	if os.Getenv("GIT_SSH") == "" && os.Getenv("GIT_SSH_COMMAND") == "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND=ssh -o ControlMaster=no -o BatchMode=yes")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("git includes need the git command, which is not installed")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// short is a commit hash as people read it.
func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
