package ui

import (
	"strings"
	"testing"
)

// A task's filter text holds its command, copies, check and dependencies, so searching for any
// of them finds it.
func TestTaskFilterHoldsWhatTheTaskDoes(t *testing.T) {
	task := TaskCard{Name: "deploy", Description: "Ship it", Badges: []string{"sudo"}, Command: "apt-get install nginx",
		Copies: []string{"site.conf → /etc/nginx/site.conf"}, Check: "dpkg -s nginx", DependsOn: []string{"base"}}
	text := taskFilter(task)
	for _, want := range []string{"deploy", "ship it", "sudo", "nginx", "/etc/nginx/site.conf", "dpkg -s nginx", "base"} {
		if !strings.Contains(text, want) {
			t.Errorf("taskFilter = %q, missing %q", text, want)
		}
	}
	if text != strings.ToLower(text) {
		t.Error("the filter text is not lower case, so the page's filter couldn't match it")
	}
}

// A bundle's filter text holds the tasks it expands to and the bundles that use it.
func TestBundleFilterHoldsItsTasks(t *testing.T) {
	text := bundleFilter(BundleCard{Name: "base", Description: "every server", Expanded: []string{"hello", "show-env"}, UsedBy: []string{"web"}})
	for _, want := range []string{"base", "every server", "show-env", "web"} {
		if !strings.Contains(text, want) {
			t.Errorf("bundleFilter = %q, missing %q", text, want)
		}
	}
}
