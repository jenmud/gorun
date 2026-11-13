package ui

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jenmud/gorun/internal/store"
)

// originBits is the short form of who started a run and where, as tags.
func originBits(o *store.Origin) []string { return o.Bits() }

// originRow is one line of the origin panel.
type originRow struct {
	Label, Value, Link string
}

// originRows are the lines of the origin panel, skipping what wasn't recorded.
func originRows(o *store.Origin) []originRow {
	if o == nil {
		return nil
	}
	var rows []originRow
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, originRow{Label: label, Value: value})
		}
	}
	user := o.User
	if o.UID != "" && user != "" {
		user += " (uid " + o.UID + ")"
	}
	add("User", user)
	machine := o.Hostname
	if o.OS != "" {
		machine = strings.TrimSpace(machine + " · " + o.OS + "/" + o.Arch)
	}
	if o.PID != 0 {
		machine += " · pid " + strconv.Itoa(o.PID)
	}
	add("Machine", strings.TrimPrefix(machine, " · "))
	add("Started from", o.Trigger)
	add("Web user", o.WebUser)
	add("Browser", o.Client)
	add("SSH from", o.SSHClient)
	add("gorun", o.Version)
	add("Directory", o.WorkDir)
	add("Gorunfile", o.Gorunfile)
	add("Command", strings.Join(o.Command, " "))
	if o.CI != "" {
		ci := o.CI
		if o.CIUser != "" {
			ci += " · started by " + o.CIUser
		}
		rows = append(rows, originRow{Label: "CI", Value: ci, Link: o.CIURL})
	}
	if g := o.Git; g != nil && g.Commit != "" {
		v := g.Commit
		if g.Branch != "" {
			v = g.Branch + " @ " + v
		}
		if g.Dirty {
			v += " (uncommitted changes)"
		}
		add("Git", v)
		add("Repository", g.Remote)
	}
	for _, k := range slices.Sorted(maps.Keys(o.Labels)) {
		add("Label "+k, o.Labels[k])
	}
	return rows
}

// originTagClass styles the origin line's tags: the first (user@host) stands out.
func originTagClass(i int) string {
	if i == 0 {
		return "rounded-full bg-zinc-800 px-2 py-0.5 font-mono text-zinc-200"
	}
	return "rounded-full px-2 py-0.5 font-mono text-zinc-400 ring-1 ring-zinc-800"
}

// runWhere finishes "Started …" with who and where: "by jen@laptop", or "on laptop" for a
// run saved before gorun recorded who started it.
func runWhere(r RunRow) string {
	if w := r.Origin.Where(); w != "" {
		return "by " + w
	}
	if r.Hostname != "" {
		return "on " + r.Hostname
	}
	return ""
}
