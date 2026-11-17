package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jenmud/gorun/internal/store"
)

func TestKeyMsgStringsRoundTrip(t *testing.T) {
	for _, name := range []string{"up", "down", "left", "right", "home", "end", "pgup", "pgdown", "tab",
		"/", "?", "G", "ctrl+u", "ctrl+d", "ctrl+w", "e"} {
		if got := keyMsg(name).String(); got != name {
			t.Errorf("keyMsg(%q).String() = %q", name, got)
		}
	}
}

func TestKeyChord(t *testing.T) {
	cases := []struct {
		keys     []string
		want     navAction
		consumed bool
	}{
		{[]string{"j"}, navDown, true},
		{[]string{"down"}, navDown, true},
		{[]string{"g", "g"}, navTop, true},
		{[]string{"home"}, navTop, true},
		{[]string{"G"}, navBottom, true},
		{[]string{"g", "t"}, navTabNext, true},
		{[]string{"g", "T"}, navTabPrev, true},
		{[]string{"ctrl+w", "l"}, navPaneRight, true},
		{[]string{"ctrl+w", "h"}, navPaneLeft, true},
		{[]string{"ctrl+w", "w"}, navPaneNext, true},
		{[]string{"L"}, navPaneRight, true},
		{[]string{"3"}, navTab3, true},
		{[]string{"ctrl+d"}, navHalfDown, true},
		{[]string{"/"}, navFilter, true},
		{[]string{"?"}, navHelp, true},
		{[]string{"g", "x"}, navNone, true}, // vim drops an unknown second key
		{[]string{"x"}, navNone, false},     // left for the screen's own keys
	}
	for _, c := range cases {
		var chord keyChord
		var act navAction
		var consumed bool
		for _, k := range c.keys {
			act, consumed = chord.nav(k)
		}
		if act != c.want || consumed != c.consumed {
			t.Errorf("%v: got %d %v, want %d %v", c.keys, act, consumed, c.want, c.consumed)
		}
		if chord.pending != "" {
			t.Errorf("%v: the chord should be finished, still waiting after %q", c.keys, chord.pending)
		}
	}
}

func keys(m serverSelectionModel, names ...string) serverSelectionModel {
	for _, k := range names {
		m, _ = send(m, keyMsg(k))
	}
	return m
}

func TestServerSelectionModel_VimKeys(t *testing.T) {
	m := newServerSelectionModel("test", pipelineServers())
	if m = keys(m, "j"); m.cursor == 0 {
		t.Fatal("j should move down")
	}
	if m = keys(m, "G"); m.cursor != len(m.rows)-1 {
		t.Errorf("G should go to the last row, got %d of %d", m.cursor, len(m.rows))
	}
	top := newServerSelectionModel("test", pipelineServers()).cursor
	if m = keys(m, "g", "g"); m.cursor != top {
		t.Errorf("gg should go back to the top, got %d", m.cursor)
	}
	if m = keys(m, "?"); !m.helpOpen {
		t.Fatal("? should open the help")
	}
	if help := ansi.Strip(m.helpContent()); !strings.Contains(help, "gg / G") || !strings.Contains(help, "home / end") {
		t.Errorf("the help should give the vim and the plain keys, got:\n%s", help)
	}
	if m = keys(m, "x"); m.helpOpen {
		t.Error("a key should close the help")
	}
}

func TestServerSelectionModel_SlashFilter(t *testing.T) {
	m := newServerSelectionModel("test", pipelineServers())
	m = keys(m, "w", "e")
	if m.query != "" {
		t.Fatalf("letters must not filter before /, got query %q", m.query)
	}
	m = keys(m, "/", "w", "e", "b")
	if !m.filtering || m.query != "web" {
		t.Fatalf("/ then web should filter, got filtering=%v query %q", m.filtering, m.query)
	}
	if !strings.Contains(ansi.Strip(m.statusLine()), "/ web") {
		t.Errorf("the status line should show the filter, got %q", ansi.Strip(m.statusLine()))
	}
	m = keys(m, "enter", "j")
	if m.filtering || m.query != "web" {
		t.Errorf("enter should keep the filter and go back to moving, got filtering=%v query %q", m.filtering, m.query)
	}
	m, cmd := send(m, keyMsg("esc"))
	if m.query != "" || m.quitting || cmd != nil {
		t.Errorf("esc should clear the kept filter before it quits, got query %q quitting=%v", m.query, m.quitting)
	}
}

func TestServerSelectionModel_PanesAndTabs(t *testing.T) {
	m := newServerSelectionModel("test", pipelineServers())
	if m = keys(m, "L"); m.focus != paneTasks {
		t.Fatal("L should move to the tasks pane")
	}
	if m = keys(m, "ctrl+w", "h"); m.focus != paneServers {
		t.Fatal("ctrl+w h should move back to the servers pane")
	}
	if m = keys(m, "ctrl+w", "w"); m.focus != paneTasks {
		t.Fatal("ctrl+w w should move to the next pane")
	}

	h := historyPicker(t)
	if h = keys(h, "g", "t"); h.tab != tabHistory {
		t.Fatalf("gt should go to the next tab, got %d", h.tab)
	}
	if h = keys(h, "g", "T"); h.tab != tabServers {
		t.Fatalf("gT should go to the previous tab, got %d", h.tab)
	}
	if h = keys(h, "2"); h.tab != tabHistory {
		t.Fatalf("2 should go to the second tab, got %d", h.tab)
	}
	if h = keys(h, "1"); h.tab != tabServers {
		t.Fatalf("1 should go to the first tab, got %d", h.tab)
	}
}

func TestHistoryTab_Filter(t *testing.T) {
	m := keys(historyPicker(t), "2")
	m = keys(m, "/", "n", "o", "p", "e")
	if m.history.query != "nope" || m.query != "" {
		t.Fatalf("/ on the History tab should filter the runs, got history %q servers %q", m.history.query, m.query)
	}
	if view := ansi.Strip(m.historyContent()); strings.Contains(view, "nightly deploy") || !strings.Contains(view, "No runs match") {
		t.Errorf("a filter that matches nothing should hide the run, got:\n%s", view)
	}
	m = keys(m, "ctrl+u", "n", "i", "g", "h", "t", "enter")
	if view := ansi.Strip(m.historyContent()); !strings.Contains(view, "nightly deploy") {
		t.Errorf("the run should match nightly, got:\n%s", view)
	}
	if m = keys(m, "l"); m.history.state != historyDetail {
		t.Fatal("l should open the run")
	}
	if m.history.query != "" {
		t.Errorf("an opened run starts unfiltered, got %q", m.history.query)
	}
	if m = keys(m, "esc"); m.history.state != historyList || m.history.query != "night" {
		t.Errorf("esc should go back to the runs with their filter, got state=%d query %q", m.history.state, m.history.query)
	}
	if m = keys(m, "esc"); m.history.query != "" || m.tab != tabHistory {
		t.Errorf("esc should clear the filter first, got query %q tab %d", m.history.query, m.tab)
	}
}

func TestHistoryModel_StandaloneVimKeys(t *testing.T) {
	m := newHistoryModel(nil)
	for _, id := range []string{"a", "b", "c", "d"} {
		m.runs = append(m.runs, store.RunSummary{ID: "run-" + id, Title: "deploy " + id})
	}
	press := func(names ...string) {
		for _, k := range names {
			next, _ := m.Update(keyMsg(k))
			m = next.(historyModel)
		}
	}
	if press("j", "j"); m.cursor != 2 {
		t.Errorf("j j should move to the third run, got %d", m.cursor)
	}
	if press("g", "g"); m.cursor != 0 {
		t.Errorf("gg should go to the first run, got %d", m.cursor)
	}
	if press("G"); m.cursor != 3 {
		t.Errorf("G should go to the last run, got %d", m.cursor)
	}
	press("/", "b", "enter")
	if r, ok := m.runAtCursor(); !ok || r.ID != "run-b" || len(m.shownRuns()) != 1 {
		t.Errorf("the filter should keep only run-b under the cursor, got %+v", r)
	}
	if press("?"); !m.helpOpen || !strings.Contains(ansi.Strip(m.View().Content), "ctrl+u / ctrl+d") {
		t.Errorf("? should show the help with the vim keys")
	}
}

func TestTUIModel_FilterAndVimKeys(t *testing.T) {
	m := newTUIModel("deploy", false, func() {})
	for _, name := range []string{"web-1", "web-2", "db-1"} {
		m.servers = append(m.servers, &serverView{name: name})
	}
	press := func(names ...string) {
		for _, k := range names {
			next, _ := m.Update(keyMsg(k))
			m = next.(tuiModel)
		}
	}
	if press("G"); m.selectedServer() != 2 {
		t.Errorf("G should select the last server, got %d", m.selectedServer())
	}
	if press("g", "g"); m.selectedServer() != 0 {
		t.Errorf("gg should select the first server, got %d", m.selectedServer())
	}
	press("/", "d", "b")
	if len(m.shown()) != 1 || m.shown()[0].name != "db-1" {
		t.Fatalf("/db should keep only db-1, got %d servers", len(m.shown()))
	}
	if !strings.Contains(ansi.Strip(m.footer()), "/ db") {
		t.Errorf("the footer should show the filter, got %q", ansi.Strip(m.footer()))
	}
	press("enter", "q")
	if !m.quitting {
		t.Error("after enter keeps the filter, q should quit as before")
	}
	m.quitting = false
	press("?")
	if help := ansi.Strip(m.View().Content); !m.helpOpen || !strings.Contains(help, "ctrl+u / ctrl+d") || strings.Contains(help, "gt / gT") {
		t.Errorf("? should open the help with the vim keys and no tab keys, got:\n%s", help)
	}
}

func TestQueryPositions(t *testing.T) {
	if got := queryPositions("Web-web", "WEB"); !slices.Equal(got, []int{0, 1, 2, 4, 5, 6}) {
		t.Errorf("both places should match, ignoring case, got %v", got)
	}
	if queryPositions("db-1", "") != nil || queryPositions("db-1", "web") != nil {
		t.Error("no query, or no match, has no positions")
	}
}

func TestHighlightSelectedRowKeepsMatches(t *testing.T) {
	line := "  " + highlightQuery("web-1", "web", serverStyle) + mutedStyle.Render(" local")
	plain, matched := splitMatches(line)
	if plain != "  web-1 local" {
		t.Fatalf("got plain %q", plain)
	}
	want := []bool{false, false, true, true, true, false, false, false, false, false, false, false, false}
	if !slices.Equal(matched, want) {
		t.Errorf("only web should be marked, got %v", matched)
	}
	row := highlightSelectedRow(line, 20)
	if !strings.Contains(row, selectedMatchStyle.Render("web")) {
		t.Errorf("the selected row should keep the match picked out, got %q", row)
	}
	if w := ansi.StringWidth(row); w != 20 {
		t.Errorf("the selected row should fill 20 cells, got %d", w)
	}
	if strings.Contains(highlightSelectedRow(mutedStyle.Render("db-1"), 10), matchPrefix) {
		t.Error("a row without matches should be drawn as before")
	}
}

func TestFilterHighlightsOnEveryScreen(t *testing.T) {
	hasMatch := func(s string) bool { return strings.Contains(s, matchPrefix) || strings.Contains(s, selectedMatchStyle.Render("w")) }

	// live view
	lv := newTUIModel("deploy", false, func() {})
	lv.servers = append(lv.servers, &serverView{name: "web-1"})
	lv.query = "web"
	if !hasMatch(lv.serverSummary(lv.servers[0])) {
		t.Error("the live view should pick out the match in a server's name")
	}

	// history runs
	h := newHistoryModel(nil)
	h.runs = []store.RunSummary{{ID: "run-1", Title: "nightly deploy"}}
	h.query = "night"
	if !strings.Contains(h.View().Content, selectedMatchStyle.Render("night")) {
		t.Error("the history should pick out the match in a run's title")
	}
}
