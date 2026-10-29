package main

import (
	"context"
	"encoding/json"
	"time"

	inats "gorun/internal/nats"
	"gorun/internal/store"
)

// recordEvent saves an event to the history database. Writes use a context
// detached from the run context: quitting a run cancels that context, but the
// final task and process_finished events still need to be recorded so the
// history doesn't stay stuck on "running".
func recordEvent(ctx context.Context, st *store.Store, event any) {
	if st == nil {
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	var ev inats.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	inats.UpdateDatabase(ctx, st, &ev)
}
