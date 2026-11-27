package docs

import (
	"strings"
	"testing"
)

func TestSearchFindsTheSectionThatAnswers(t *testing.T) {
	tests := map[string]string{
		"Is the web UI safe to expose?":          "safe to expose",
		"how do I run it in Docker?":             "Docker",
		"How do environment variables work?":     "Environment variables",
		"which model does the assistant use?":    "assistant",
		"where can I learn more about gorun":     "learn gorun",
		"what does missed = run do in schedules": "Schedules",
	}
	for q, want := range tests {
		got := Search(q, 2, 2000)
		if len(got) == 0 || !strings.Contains(got[0].Title, want) {
			t.Errorf("Search(%q) = %v, want %q first", q, titles(got), want)
		}
	}
}

func TestSearchKeepsToItsBudget(t *testing.T) {
	if got := Search("tasks servers schedules bundles copies", 5, 1000); len(got) > 1 && len(got[0].Text)+len(got[1].Text) > 1000 {
		t.Errorf("Search() returned %d characters over a 1000 budget", len(got[0].Text)+len(got[1].Text))
	}
	if got := Search("the and of", 3, 2000); len(got) != 0 {
		t.Errorf("Search() of stop words = %v, want none", titles(got))
	}
}

func titles(s []Section) []string {
	var out []string
	for _, x := range s {
		out = append(out, x.Title)
	}
	return out
}
