package main

import (
	"os"
	"runtime/debug"
	"strings"
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

// Every release build must stamp its tag, or -version reports whatever the build falls back to.
func TestReleaseBuildsStampTheVersion(t *testing.T) {
	data, err := os.ReadFile("../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// the builds section runs until the next top-level key
	section := strings.SplitN(string(data), "\nbuilds:\n", 2)[1]
	var lines []string
	for _, l := range strings.Split(section, "\n") {
		if l != "" && l[0] != ' ' {
			break
		}
		lines = append(lines, l)
	}
	builds := strings.Split(strings.Join(lines, "\n"), "  - id:")[1:]
	if len(builds) < 2 {
		t.Fatalf("expected the gorun and gorun-otel builds in .goreleaser.yaml, found %d", len(builds))
	}
	for _, b := range builds {
		if !strings.Contains(b, "-X main.version={{.Tag}}") {
			t.Errorf("release build %q doesn't stamp the version", strings.TrimSpace(strings.SplitN(b, "\n", 2)[0]))
		}
	}
}

// The install script and make build stamp the version too.
func TestOtherBuildsStampTheVersion(t *testing.T) {
	for file, want := range map[string]string{
		"../scripts/install.sh": "-X main.version=",
		"../Makefile":           "-X main.version=",
	} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s doesn't stamp the version", file)
		}
	}
}
