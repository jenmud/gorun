package main

import (
	"runtime/debug"
	"testing"
)

func TestBuildVersion(t *testing.T) {
	defer func(v string) { version = v }(version)

	version = "v1.2.3" // set by the release's ldflags
	if got := buildVersion(); got != "v1.2.3" {
		t.Errorf("a release's version should win, got %q", got)
	}

	version = "" // a test binary has no module version, so it is a devel build
	if got := buildVersion(); got == "" || got == "0.1.0" {
		t.Errorf("an unset version should come from the build, got %q", got)
	}
}

func TestDevelVersion(t *testing.T) {
	for _, c := range []struct {
		settings []debug.BuildSetting
		want     string
	}{
		{nil, "devel"},
		{[]debug.BuildSetting{{Key: "vcs.revision", Value: "91fb74e842aad3ad70410fd8"}}, "devel-91fb74e842aa"},
		{[]debug.BuildSetting{{Key: "vcs.revision", Value: "91fb74e842aad3ad70410fd8"}, {Key: "vcs.modified", Value: "true"}}, "devel-91fb74e842aa-dirty"},
	} {
		if got := develVersion(c.settings); got != c.want {
			t.Errorf("develVersion(%v) = %q, want %q", c.settings, got, c.want)
		}
	}
}
