package main

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// navAction is a navigation key's meaning on every screen. Vim keys and their plain equivalents
// map to the same action, so each screen handles actions, not keys.
type navAction int

const (
	navNone navAction = iota
	navUp
	navDown
	navLeft  // collapse, or go back
	navRight // expand, or go in
	navTop
	navBottom
	navPageUp
	navPageDown
	navHalfUp
	navHalfDown
	navPaneLeft
	navPaneRight
	navPaneNext
	navTabNext
	navTabPrev
	navTab1 // navTab1+n jumps to tab n+1
	navTab2
	navTab3
	navTab4
	navTab5
	navFilter
	navHelp
)

// keyChord remembers the first key of a two-key motion (g in gg, gt, gT; ctrl+w in ctrl+w h/l/w)
// until the second key arrives.
type keyChord struct {
	pending string
}

// nav returns the action for key. consumed is true when the key was used: an action, or the first
// half of a two-key motion still waiting for its second key. A key that means nothing returns
// navNone and false, so the screen can use it for its own purpose.
func (c *keyChord) nav(key string) (act navAction, consumed bool) {
	switch c.pending {
	case "g":
		c.pending = ""
		switch key {
		case "g":
			return navTop, true
		case "t":
			return navTabNext, true
		case "T":
			return navTabPrev, true
		}
		return navNone, true // vim drops an unknown second key
	case "ctrl+w":
		c.pending = ""
		switch key {
		case "h", "left", "ctrl+h":
			return navPaneLeft, true
		case "l", "right", "ctrl+l":
			return navPaneRight, true
		case "w", "ctrl+w":
			return navPaneNext, true
		}
		return navNone, true
	}
	switch key {
	case "g", "ctrl+w":
		c.pending = key
		return navNone, true
	case "j", "down":
		return navDown, true
	case "k", "up":
		return navUp, true
	case "h", "left":
		return navLeft, true
	case "l", "right":
		return navRight, true
	case "home":
		return navTop, true
	case "G", "end":
		return navBottom, true
	case "pgup":
		return navPageUp, true
	case "pgdown":
		return navPageDown, true
	case "ctrl+u":
		return navHalfUp, true
	case "ctrl+d":
		return navHalfDown, true
	case "H":
		return navPaneLeft, true
	case "L":
		return navPaneRight, true
	case "1", "2", "3", "4", "5":
		return navTab1 + navAction(key[0]-'1'), true
	case "/":
		return navFilter, true
	case "?":
		return navHelp, true
	}
	return navNone, false
}

// keyFor is the plain key a screen's existing code understands for an action, so a screen can
// translate a vim key into the arrow or page key it already handles.
func keyFor(act navAction) string {
	switch act {
	case navUp:
		return "up"
	case navDown:
		return "down"
	case navLeft:
		return "left"
	case navRight:
		return "right"
	case navTop:
		return "home"
	case navBottom:
		return "end"
	case navPageUp:
		return "pgup"
	case navPageDown:
		return "pgdown"
	}
	return ""
}

// keysHelp is the navigation every screen shares, for the help screens: each line gives the vim
// key, then the plain key, then what it does.
var keysHelp = [][3]string{
	{"j / k", "↓ / ↑", "move down / up"},
	{"h / l", "← / →", "collapse or back / expand or open"},
	{"gg / G", "home / end", "go to the top / bottom"},
	{"ctrl+u / ctrl+d", "pgup / pgdn", "half a page up / down (pgup/pgdn: a whole page)"},
	{"ctrl+w h/l, H / L", "tab", "move to the left / right pane"},
	{"ctrl+w w", "tab", "move to the next pane"},
	{"gt / gT", "shift+tab", "next / previous tab"},
	{"1 – 5", "ctrl+s/t/b/g/h", "go to the 1st – 5th tab (Servers, Tasks, Bundles, Groups, History)"},
	{"/", "/", "filter: type, enter keeps it, esc clears it"},
	{"?", "ctrl+k", "show / hide this help"},
}

// keyMsg is the key press for a plain key name, so a vim key can be handed on to code that
// handles the plain key.
func keyMsg(name string) tea.KeyPressMsg {
	codes := map[string]rune{"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "tab": tea.KeyTab}
	if code, ok := codes[name]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	if ctrl, ok := strings.CutPrefix(name, "ctrl+"); ok && len(ctrl) == 1 {
		return tea.KeyPressMsg{Code: rune(ctrl[0]), Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(name)[0], Text: name}
}

// editQuery applies a key typed into a filter and returns the new query. done is true when the
// key ends typing: enter keeps the query, esc clears it.
func editQuery(query string, msg tea.KeyPressMsg) (q string, done bool) {
	switch msg.String() {
	case "enter":
		return query, true
	case "esc":
		return "", true
	case "backspace":
		if r := []rune(query); len(r) > 0 {
			return string(r[:len(r)-1]), false
		}
	case "ctrl+w":
		query = strings.TrimRightFunc(query, unicode.IsSpace)
		return strings.TrimRightFunc(query, func(r rune) bool { return !unicode.IsSpace(r) }), false
	case "ctrl+u":
		return "", false
	default:
		key := msg.Key()
		if key.Text != "" && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
			return query + key.Text, false
		}
	}
	return query, false
}

// navHelpLines are the keysHelp rows laid out for a help screen, under a heading. Without tabs,
// the rows that switch tabs are left out.
func navHelpLines(tabs bool) []string {
	lines := []string{fmt.Sprintf("%-20s %-16s %s", "vim", "other keys", "")}
	for _, row := range keysHelp {
		if !tabs && (row[0] == "gt / gT" || row[0] == "1 – 5") {
			continue
		}
		lines = append(lines, fmt.Sprintf("%-20s %-16s %s", row[0], row[1], row[2]))
	}
	return lines
}
