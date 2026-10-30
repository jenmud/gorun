// Package gorun runs tasks defined in a Gorunfile across one or more servers over SSH.
//
// A Gorunfile is TOML: a library of named tasks (a command, a command file or template,
// and optional file copies, sudo, envvars and dependencies) and the servers that run them.
// The gorun command in ./cmd drives this package with a TUI or plain output; use the package
// directly to run deployments from your own code:
//
//	cfg := gorun.Config{}
//	if err := cfg.Load(r); err != nil {
//		return err
//	}
//	cfg.ResolvePaths(filepath.Dir(path))
//	cfg.OnEvent = func(event any) { /* PlanEvent, TaskEvent, ... */ }
//	return cfg.Run(ctx)
package gorun
