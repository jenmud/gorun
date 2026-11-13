// Package origin is the Origin type: who started a run, from where, and with what. It has no
// dependencies, so the gorun library and the history store can share it.
package origin

// Origin is who started a run, from where, and with what: it travels on the process_started
// event and is saved with the run. Every field is optional; gorun fills in what it can find.
type Origin struct {
	User     string `json:"user,omitempty"`     // the login name gorun runs as
	UID      string `json:"uid,omitempty"`      // that user's id (a SID on Windows)
	Hostname string `json:"hostname,omitempty"` // the machine gorun runs on
	OS       string `json:"os,omitempty"`       // linux, darwin, windows
	Arch     string `json:"arch,omitempty"`     // amd64, arm64
	PID      int    `json:"pid,omitempty"`
	Version  string `json:"version,omitempty"` // gorun's version

	// Trigger is how the run was started: cli (gorun -r -plain), tui, web, schedule, or
	// library for a program built on the gorun package.
	Trigger string `json:"trigger,omitempty"`

	WorkDir   string   `json:"workdir,omitempty"`   // the directory gorun ran in
	Gorunfile string   `json:"gorunfile,omitempty"` // the Gorunfile's path
	Command   []string `json:"command,omitempty"`   // gorun's command line

	// SSHClient is the address the user is logged in from, when gorun runs in an SSH session.
	SSHClient string `json:"ssh_client,omitempty"`
	// Client is the browser's address, for a run started from the web UI, and WebUser the
	// user an auth proxy in front of the web UI says is signed in (X-Forwarded-User,
	// X-Auth-Request-User or Remote-User).
	Client  string `json:"client,omitempty"`
	WebUser string `json:"web_user,omitempty"`

	// CI is the CI system the run is part of (github-actions, gitlab, jenkins, buildkite,
	// circleci, or ci), with the user who started the job and a link to it.
	CI     string `json:"ci,omitempty"`
	CIUser string `json:"ci_user,omitempty"`
	CIURL  string `json:"ci_url,omitempty"`

	// Git is the commit of the repository the Gorunfile is in, when it is in one.
	Git *GitInfo `json:"git,omitempty"`

	// Labels are extra key=value pairs: from GORUN_LABELS ("team=ops,ticket=CHG-42"), or set
	// by a program on Config.Origin.
	Labels map[string]string `json:"labels,omitempty"`
}

// GitInfo is the state of the repository a Gorunfile is in.
type GitInfo struct {
	Commit string `json:"commit,omitempty"`
	Branch string `json:"branch,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`  // it has uncommitted changes
	Remote string `json:"remote,omitempty"` // origin's URL, without any password or token
}

// Where is the user and machine as user@host, or whichever of them is known.
func (o *Origin) Where() string {
	if o == nil {
		return ""
	}
	switch {
	case o.User != "" && o.Hostname != "":
		return o.User + "@" + o.Hostname
	case o.User != "":
		return o.User
	default:
		return o.Hostname
	}
}

// Bits is the short form of who started a run and where, as tags: user@host, how it
// was started, the CI system, gorun's version and the Gorunfile's commit.
func (o *Origin) Bits() []string {
	if o == nil {
		return nil
	}
	var bits []string
	if w := o.Where(); w != "" {
		bits = append(bits, w)
	}
	if o.WebUser != "" {
		bits = append(bits, "web user "+o.WebUser)
	}
	if o.Trigger != "" {
		bits = append(bits, o.Trigger)
	}
	if o.CI != "" {
		ci := o.CI
		if o.CIUser != "" {
			ci += " · " + o.CIUser
		}
		bits = append(bits, ci)
	}
	if o.Version != "" {
		bits = append(bits, o.Version)
	}
	if g := o.Git.Short(); g != "" {
		bits = append(bits, g)
	}
	return bits
}

// Short is the commit as branch@abc1234, with a * when there were uncommitted changes.
func (g *GitInfo) Short() string {
	if g == nil || g.Commit == "" {
		return ""
	}
	s := g.Commit
	if len(s) > 7 {
		s = s[:7]
	}
	if g.Branch != "" {
		s = g.Branch + "@" + s
	}
	if g.Dirty {
		s += "*"
	}
	return s
}
