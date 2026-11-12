package main

import (
	"bufio"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestServeFlagStartsWebService(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"-serve"}, true},
		{[]string{"-web"}, true},
		{[]string{"-serve", "-web"}, true},
		{[]string{"Gorunfile", "-serve"}, true},
		{[]string{"Gorunfile", "-web"}, true},
		{[]string{"Gorunfile", "-r"}, false},
		{nil, false},
	} {
		fs := flag.NewFlagSet("gorun", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		if got := parseArgs(fs, tc.args).serve; got != tc.want {
			t.Errorf("parseArgs(%q).serve = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestUsageDocumentsServeAndWebAlias(t *testing.T) {
	var b strings.Builder
	writeUsage(&b)
	out := b.String()
	if !strings.Contains(out, "-serve [Gorunfile]  start the web service (scheduler, NATS, web UI); -web is an alias") {
		t.Errorf("usage should document -serve with -web as an alias, got:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "-web") && !strings.Contains(line, "alias") {
			t.Errorf("usage mentions -web outside the alias note: %q", line)
		}
	}
}

// TestDocsUseServeFlag also scans the recipes' Gorunfiles, whose comments name the flag.
func TestDocsUseServeFlag(t *testing.T) {
	files := []string{"../README.md", "../Makefile", "../AGENTS.md", "Gorunfile.empty"}
	docs, _ := filepath.Glob("../docs/*.md")
	files = append(files, docs...)
	_ = filepath.WalkDir("../recipes", func(path string, d os.DirEntry, err error) error {
		if err == nil && (d.Name() == "README.md" || d.Name() == "Gorunfile") {
			files = append(files, path)
		}
		return nil
	})
	old := regexp.MustCompile(`(gorun|\./cmd) -web\b`)
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		for n := 1; sc.Scan(); n++ {
			if line := sc.Text(); old.MatchString(line) && !strings.Contains(line, "alias") {
				t.Errorf("%s:%d uses -web, want -serve: %s", name, n, line)
			}
		}
		_ = f.Close()
	}
}

func TestCompletionListsServe(t *testing.T) {
	data, err := os.ReadFile("../docs/integrations.md")
	if err != nil {
		t.Fatal(err)
	}
	var words []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "compgen -W") {
			words = strings.Fields(strings.SplitN(line, `"`, 3)[1])
		}
	}
	if words == nil {
		t.Fatal("no bash completion word list in docs/integrations.md")
	}
	for _, want := range []string{"-serve", "--serve", "-web", "--web"} {
		found := false
		for _, w := range words {
			found = found || w == want
		}
		if !found {
			t.Errorf("completion word list is missing %s", want)
		}
	}
}
