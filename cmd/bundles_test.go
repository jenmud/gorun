package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jenmud/gorun"
)

func TestSelectBundles(t *testing.T) {
	var cfg gorun.Config
	if err := cfg.Load(strings.NewReader(`
[tasks.keep]
cmd = "true"

[tasks.alpha]
cmd = "true"

[tasks.beta]
cmd = "true"

[bundles.base]
tasks = ["alpha"]

[bundles.extra]
tasks = ["beta"]

[servers.hosts.web]
hostname = "web.example.com"
tasks = ["keep"]
  [servers.hosts.web.ssh]
  username = "deploy"
`)); err != nil {
		t.Fatal(err)
	}
	cfg.SelectedTasks = []string{"keep"}

	tasks, err := selectBundles(&cfg, "ba*", "@extra")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tasks, []string{"alpha", "beta"}) {
		t.Fatalf("selected tasks = %v, want [alpha beta]", tasks)
	}
	if len(cfg.SelectedTasks) != 0 {
		t.Errorf("selected task filter = %v, want it cleared", cfg.SelectedTasks)
	}

	pipeline, err := cfg.ServerPipeline(cfg.Servers[0])
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(pipeline.Tasks))
	for _, task := range pipeline.Tasks {
		got = append(got, task.Name)
	}
	if !reflect.DeepEqual(got, tasks) {
		t.Errorf("server pipeline = %v, want selected bundle tasks %v", got, tasks)
	}
}

func TestSelectBundlesUnknownBundleDoesNotChangeConfig(t *testing.T) {
	var cfg gorun.Config
	if err := cfg.Load(strings.NewReader(`
[tasks.keep]
cmd = "true"

[bundles.base]
tasks = ["keep"]

[servers.hosts.web]
tasks = ["keep"]
  [servers.hosts.web.ssh]
  username = "deploy"
`)); err != nil {
		t.Fatal(err)
	}

	if _, err := selectBundles(&cfg, "missing"); err == nil {
		t.Fatal("selectBundles should reject an unknown bundle")
	}
	if !reflect.DeepEqual(cfg.Servers[0].TaskNames, []string{"keep"}) {
		t.Errorf("server tasks changed after invalid selection: %v", cfg.Servers[0].TaskNames)
	}
}
