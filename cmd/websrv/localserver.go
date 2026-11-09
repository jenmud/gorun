package websrv

import (
	"fmt"

	"github.com/jenmud/gorun"
)

// localServerNote is added to a page's message when a save also added a local server.
const localServerNote = " Added a local server, %s, so it can run on this machine: it has no tasks of its own, so only schedules and --task reach it."

// localServerChange returns a change that adds a local server to the root Gorunfile when the
// saved changes need one and cfg, already reloaded, has none: a task marked local = true can
// only run on a local server, so a schedule for it would otherwise fail on every run. The
// server gets an empty task list, so the [servers] default list never runs on this machine.
func localServerChange(cfg *gorun.Config, saved []gorun.Change) (gorun.Change, bool) {
	if cfg == nil || cfg.Path == "" {
		return gorun.Change{}, false
	}
	for _, s := range cfg.Servers {
		if s.IsLocal() {
			return gorun.Change{}, false
		}
	}
	if !needsLocal(cfg, saved) {
		return gorun.Change{}, false
	}
	return gorun.Change{
		Kind: "host",
		Name: freeHostName(cfg),
		File: cfg.Path,
		Body: map[string]any{"tasks": []string{}},
	}, true
}

// needsLocal reports whether a saved task is marked local = true, or a saved schedule's target
// includes one.
func needsLocal(cfg *gorun.Config, saved []gorun.Change) bool {
	for _, ch := range saved {
		if ch.Remove {
			continue
		}
		switch ch.Kind {
		case "task":
			if cfg.Tasks[ch.Name].Local {
				return true
			}
		case gorun.KindSchedule:
			for _, sc := range cfg.Schedules {
				if sc.Name != ch.Name {
					continue
				}
				names, err := cfg.ExpandTaskRefs(sc.Target)
				if err != nil {
					continue
				}
				for _, n := range names {
					if cfg.Tasks[n].Local {
						return true
					}
				}
			}
		}
	}
	return false
}

// freeHostName is "localhost", or the first of "local", "local-2", ... that no server uses.
func freeHostName(cfg *gorun.Config) string {
	used := map[string]bool{}
	for _, s := range cfg.Servers {
		used[s.Name] = true
	}
	for _, name := range []string{"localhost", "local"} {
		if !used[name] {
			return name
		}
	}
	for i := 2; ; i++ {
		if name := fmt.Sprintf("local-%d", i); !used[name] {
			return name
		}
	}
}

// addLocalServerIfNeeded adds a local server after a save that needs one, and returns the note
// for the page's message, or "" when nothing was added. The caller holds a.saveMu.
func (a *app) addLocalServerIfNeeded(saved []gorun.Change) (string, error) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	ch, ok := localServerChange(cfg, saved)
	if !ok {
		return "", nil
	}
	if err := a.saveDirect(ch.File, []gorun.Change{ch}); err != nil {
		return "", fmt.Errorf("could not add a local server for it: %w", err)
	}
	return fmt.Sprintf(localServerNote, ch.Name), nil
}
