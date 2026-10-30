package main

import (
	"context"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorun"
)

// The template from -dump must load and run as-is.
func TestEmptyGorunfileRuns(t *testing.T) {
	old := gorun.LogOutput()
	gorun.SetLogOutput(io.Discard)
	t.Cleanup(func() { gorun.SetLogOutput(old) })

	var cfg gorun.Config
	if err := cfg.Load(strings.NewReader(emptyGorunfile)); err != nil {
		t.Fatalf("the dumped Gorunfile should load: %v", err)
	}

	var mu sync.Mutex
	stdout := map[string]string{}
	cfg.OnEvent = func(event any) {
		if ev, ok := event.(gorun.TaskEvent); ok && ev.State == gorun.TaskDone {
			mu.Lock()
			stdout[ev.Task] = strings.TrimSpace(ev.Stdout)
			mu.Unlock()
		}
	}
	if err := cfg.Run(context.Background()); err != nil {
		t.Fatalf("the dumped Gorunfile should run: %v", err)
	}

	if got, want := stdout["hello"], "hello from localhost, tags: #local #dev"; got != want {
		t.Errorf("hello printed %q, want %q", got, want)
	}
	if got, want := stdout["show-env"], "APP_ENV=development REGION=home"; got != want {
		t.Errorf("show-env printed %q, want %q (global and group envvars)", got, want)
	}
}

// Every config key should be documented in the template, so it keeps up with new options.
func TestEmptyGorunfileDocumentsEveryKey(t *testing.T) {
	// aliases of documented keys, left out to keep the template short
	aliases := map[string]bool{
		"copies": true, "upload": true, "files": true, // copy
		"src_file": true, "src_tmpl": true, "template": true, "dst_path": true, "dest": true, "perm": true,
		"config": true, // config_file
	}

	for _, v := range []any{gorun.Config{}, gorun.Task{}, gorun.Copy{}, gorun.Bundle{}, gorun.ServerGroup{}, gorun.Server{}, gorun.SSH{}} {
		typ := reflect.TypeOf(v)
		for i := range typ.NumField() {
			key, _, _ := strings.Cut(typ.Field(i).Tag.Get("toml"), ",")
			if key == "" || key == "-" || aliases[key] {
				continue
			}
			if !strings.Contains(emptyGorunfile, key+" =") && !strings.Contains(emptyGorunfile, "["+key) && !strings.Contains(emptyGorunfile, "."+key+"]") {
				t.Errorf("%s key %q is not documented in Gorunfile.empty", typ.Name(), key)
			}
		}
	}
}
