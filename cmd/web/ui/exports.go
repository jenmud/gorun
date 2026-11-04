package ui

import "github.com/a-h/templ"

// StatsPanel is the stats panel on its own, so the server can push it when the history changes.
func StatsPanel(s StatsView) templ.Component {
	return statsPanel(s)
}
