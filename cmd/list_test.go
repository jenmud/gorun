package main

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"gorun"
)

const listGorunfile = `
[tasks.packages]
cmd = "true"
description = "Install packages"

[tasks.timezone]
cmd = "true"

[tasks.verify]
cmd = "true"

[bundles.base]
description = "Baseline"
tasks = ["packages", "timezone"]

[bundles.full]
tasks = ["@base", "verify"]

[server_groups.prod]
tags = ["prod"]

[server_groups.staging]
tags = ["staging"]

[servers.hosts.web-1]
groups = ["prod"]
tags = ["web"]

[servers.hosts.web-2]
tags = ["web"]

[servers.hosts.db-1]
groups = ["prod"]
`

func loadList(t *testing.T) gorun.Config {
	t.Helper()
	var cfg gorun.Config
	if err := cfg.Load(strings.NewReader(listGorunfile)); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestPrintTags(t *testing.T) {
	cfg := loadList(t)

	var b strings.Builder
	printTags(&b, &cfg, cfg.Servers, false)
	want := `prod (2): web-1, db-1
staging (no servers)
web (2): web-1, web-2
`
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Errorf("tags mismatch (-want +got):\n%s", diff)
	}

	// filtered: only the selected servers' tags, no unused group tags
	selected, err := cfg.SelectServers(gorun.ServerFilter{Names: []string{"web-*"}})
	if err != nil {
		t.Fatal(err)
	}
	b.Reset()
	printTags(&b, &cfg, selected, true)
	want = `prod (1): web-1
web (2): web-1, web-2
`
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Errorf("filtered tags mismatch (-want +got):\n%s", diff)
	}

	b.Reset()
	printTags(&b, &gorun.Config{}, nil, false)
	if b.String() != "no tags\n" {
		t.Errorf("got %q, want %q", b.String(), "no tags\n")
	}
}

func TestPrintBundles(t *testing.T) {
	cfg := loadList(t)

	var b strings.Builder
	printBundles(&b, &cfg)
	want := `@base — Baseline
    ├── packages — Install packages
    └── timezone

@full
    ├── @base
    │   ├── packages — Install packages
    │   └── timezone
    └── verify
    = packages, timezone, verify
`
	if diff := cmp.Diff(want, b.String()); diff != "" {
		t.Errorf("bundles mismatch (-want +got):\n%s", diff)
	}

	b.Reset()
	printBundles(&b, &gorun.Config{})
	if b.String() != "no bundles\n" {
		t.Errorf("got %q, want %q", b.String(), "no bundles\n")
	}
}
