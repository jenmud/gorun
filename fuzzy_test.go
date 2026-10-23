package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"
)

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		pattern   string
		text      string
		ok        bool
		positions []int
	}{
		{pattern: "", text: "server1", ok: true},
		{pattern: "srv", text: "server1", ok: true, positions: []int{0, 2, 3}},
		{pattern: "SRV", text: "server1", ok: true, positions: []int{0, 2, 3}},
		{pattern: "wp2", text: "web-prod-2", ok: true, positions: []int{0, 4, 9}},
		{pattern: "dbr", text: "db-replica-eu", ok: true, positions: []int{0, 1, 3}},
		{pattern: "vrs", text: "server1", ok: false},
		{pattern: "server12", text: "server1", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"/"+tt.text, func(t *testing.T) {
			_, positions, ok := fuzzyMatch(tt.pattern, tt.text)
			if ok != tt.ok {
				t.Fatalf("fuzzyMatch(%q, %q) ok = %v, want %v", tt.pattern, tt.text, ok, tt.ok)
			}
			if diff := cmp.Diff(tt.positions, positions); diff != "" {
				t.Errorf("positions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFuzzyMatch_Ranking(t *testing.T) {
	// each pair: the first text should score higher than the second for the pattern
	tests := []struct {
		pattern       string
		better, worse string
	}{
		{pattern: "web", better: "web-prod-1", worse: "worker-eu-b"},  // consecutive beats scattered
		{pattern: "db", better: "db-primary", worse: "dashboard-1"},   // word start beats a gap
		{pattern: "pr", better: "web-prod-1", worse: "deploy-runner"}, // boundary + consecutive
		{pattern: "eu", better: "worker-eu-a", worse: "queue-1"},      // word start beats mid-word
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			better, _, ok := fuzzyMatch(tt.pattern, tt.better)
			if !ok {
				t.Fatalf("%q should match %q", tt.pattern, tt.better)
			}
			worse, _, ok := fuzzyMatch(tt.pattern, tt.worse)
			if !ok {
				t.Fatalf("%q should match %q", tt.pattern, tt.worse)
			}
			if better <= worse {
				t.Errorf("%q: score(%q)=%d should beat score(%q)=%d", tt.pattern, tt.better, better, tt.worse, worse)
			}
		})
	}
}

func TestServerSelectionModel_Filter(t *testing.T) {
	servers := []Server{
		{Name: "worker-eu-b", Hostname: "localhost"},
		{Name: "web-prod-1", Hostname: "localhost"},
		{Name: "db-primary", Hostname: "db.internal"},
	}

	m := newServerSelectionModel("test", servers)
	if len(m.matches) != 3 {
		t.Fatalf("empty filter should show every server, got %d", len(m.matches))
	}

	m.query = "web"
	m.filter()
	got := []string{}
	for _, match := range m.matches {
		got = append(got, servers[match.index].Name)
	}
	if diff := cmp.Diff([]string{"web-prod-1", "worker-eu-b"}, got); diff != "" {
		t.Errorf("filter mismatch (-want +got):\n%s", diff)
	}

	// falls back to matching the hostname
	m.query = "internal"
	m.filter()
	if len(m.matches) != 1 || m.matches[0].onName {
		t.Fatalf("expected a single hostname match, got %+v", m.matches)
	}

	// enter with nothing selected picks the server under the cursor
	if diff := cmp.Diff([]Server{servers[2]}, m.chosen()); diff != "" {
		t.Errorf("chosen mismatch (-want +got):\n%s", diff)
	}
}

func TestServerSelectionModel_Mouse(t *testing.T) {
	servers := []Server{{Name: "server1"}, {Name: "server2"}, {Name: "server3"}}
	m := newServerSelectionModel("test", servers)

	// clicking the second server row moves the cursor to it and selects it
	l := m.layout()
	next, _ := m.Update(tea.MouseClickMsg{X: 5, Y: l.listTop + 1, Button: tea.MouseLeft})
	m = next.(serverSelectionModel)
	if m.cursor != 1 || !m.selected["server2"] {
		t.Fatalf("expected server2 selected under the cursor, got cursor=%d selected=%v", m.cursor, m.selected)
	}

	// clicking the run button confirms the selection
	next, cmd := m.Update(tea.MouseClickMsg{X: 1, Y: m.layout().buttonY, Button: tea.MouseLeft})
	m = next.(serverSelectionModel)
	if !m.confirmed || cmd == nil {
		t.Fatal("clicking the run button should confirm and quit")
	}
	if diff := cmp.Diff([]Server{servers[1]}, m.chosen()); diff != "" {
		t.Errorf("chosen mismatch (-want +got):\n%s", diff)
	}
}
