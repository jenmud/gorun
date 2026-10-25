package ui

import (
	"time"

	"gorun/internal/store"
)

// runServerHasFailed reports whether any task on the server failed.
func runServerHasFailed(run store.RunDetail, server string) bool {
	for _, t := range run.Tasks {
		if t.Server == server && t.State == 3 {
			return true
		}
	}
	return false
}

// serverCardClass returns the CSS classes for a server card based on failure state.
func serverCardClass(failed bool) string {
	if failed {
		return "rounded-2xl border border-red-900/50 bg-red-950/20 p-4"
	}
	return "rounded-2xl border border-zinc-800 bg-zinc-900/40 p-4"
}

// serverNameClass returns the CSS classes for a server name based on failure state.
func serverNameClass(failed bool) string {
	if failed {
		return "font-bold text-red-400"
	}
	return "font-bold text-zinc-100"
}

// serverStats returns total, completed, succeeded, failed counts and total duration for a server.
func serverStats(run store.RunDetail, server string) (total, done, ok, failed int, duration time.Duration) {
	for _, t := range run.Tasks {
		if t.Server != server {
			continue
		}
		total++
		if t.State >= 2 {
			done++
		}
		if t.State == 2 {
			ok++
		}
		if t.State == 3 {
			failed++
		}
		duration += t.Duration
	}
	return
}
