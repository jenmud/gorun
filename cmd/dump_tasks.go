package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jenmud/gorun"
)

// dumpTasks writes one file per task into dir. The file is named after the task
// and holds the (merged) environment as exports followed by the command the task
// would run on the remote host.
func dumpTasks(cfg *gorun.Config, dir string, servers map[string]gorun.Server, selectedTasks []string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("dump-tasks requires a directory")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	allowed := map[string]bool{}
	if len(selectedTasks) > 0 {
		for _, name := range selectedTasks {
			allowed[name] = true
		}
	}

	seen := map[string]bool{}

	serverNames := make([]string, 0, len(servers))
	for name := range servers {
		serverNames = append(serverNames, name)
	}
	// Prefer remote hosts over the local machine when rendering "the command the
	// task would execute on the remote host".
	sort.Slice(serverNames, func(i, j int) bool {
		if servers[serverNames[i]].IsLocal() != servers[serverNames[j]].IsLocal() {
			return !servers[serverNames[i]].IsLocal()
		}
		return serverNames[i] < serverNames[j]
	})

	for _, sname := range serverNames {
		server := servers[sname]

		if len(cfg.Vars) > 0 {
			if server.Vars == nil {
				server.Vars = map[string]any{}
			}
			for k, v := range cfg.Vars {
				if _, ok := server.Vars[k]; !ok {
					server.Vars[k] = v
				}
			}
		}
		if len(cfg.EnvVars) > 0 {
			if server.EnvVars == nil {
				server.EnvVars = map[string]string{}
			}
			for k, v := range cfg.EnvVars {
				if _, ok := server.EnvVars[k]; !ok {
					server.EnvVars[k] = v
				}
			}
		}

		for i := range server.Tasks {
			task := server.Tasks[i]
			if seen[task.Name] {
				continue
			}
			if len(allowed) > 0 && !allowed[task.Name] {
				continue
			}
			seen[task.Name] = true

			if err := writeDumpedTask(cfg, dir, task, server, true); err != nil {
				return err
			}
		}
	}

	// Tasks that are not part of any selected server's pipeline (for example
	// unassigned library tasks) are still dumped, using the first available server
	// as the rendering context when there is one.
	for name := range cfg.Tasks {
		if seen[name] {
			continue
		}
		if len(allowed) > 0 && !allowed[name] {
			continue
		}
		task := cfg.Tasks[name]

		var server gorun.Server
		haveServer := false
		// render it for a server of its own kind: local = true tasks only run locally
		for _, sname := range serverNames {
			if servers[sname].IsLocal() == task.Local {
				server = servers[sname]
				haveServer = true
				break
			}
		}
		if haveServer {
			if len(cfg.Vars) > 0 {
				if server.Vars == nil {
					server.Vars = map[string]any{}
				}
				for k, v := range cfg.Vars {
					if _, ok := server.Vars[k]; !ok {
						server.Vars[k] = v
					}
				}
			}
			if len(cfg.EnvVars) > 0 {
				if server.EnvVars == nil {
					server.EnvVars = map[string]string{}
				}
				for k, v := range cfg.EnvVars {
					if _, ok := server.EnvVars[k]; !ok {
						server.EnvVars[k] = v
					}
				}
			}
		}

		if err := writeDumpedTask(cfg, dir, task, server, haveServer); err != nil {
			return err
		}
		seen[name] = true
	}

	return nil
}

// writeDumpedTask writes one task to dir as a runnable shell script, with its command rendered for server when there is one.
func writeDumpedTask(cfg *gorun.Config, dir string, task gorun.Task, server gorun.Server, haveServer bool) error {
	var target any // nil renders templates without a server
	var srv *gorun.Server
	if haveServer {
		target, srv = server, &server
	}
	task, err := task.ForServer(srv) // filled in for the server, so the script is what runs
	if err != nil {
		return err
	}
	cmd, err := task.CommandFor(target)
	if err != nil {
		if !strings.Contains(err.Error(), "has no command") {
			return err
		}
		cmd = ""
	}

	env := map[string]string{}
	layers := []map[string]string{cfg.EnvVars, task.EnvVars}
	if haveServer {
		layers = []map[string]string{cfg.EnvVars, server.EnvVars, task.EnvVars}
	}
	for _, layer := range layers {
		for k, v := range layer {
			env[strings.ToUpper(k)] = gorun.ExpandEnv(v)
		}
	}

	var b strings.Builder
	if len(env) > 0 {
		line, err := gorun.EnvLine(env)
		if err != nil {
			return err
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if cmd != "" {
		b.WriteString(cmd)
		if !strings.HasSuffix(cmd, "\n") {
			b.WriteByte('\n')
		}
	}

	path := filepath.Join(dir, task.Name)
	return os.WriteFile(path, []byte(b.String()), 0755)
}
