package ui

import (
	"time"

	"gorun/internal/store"
)

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
