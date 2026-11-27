package websrv

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// maxWaiting is how many questions may wait for the model while it answers another. More are
// turned away, so a crowd can't pile up work the model would take many minutes to get through.
const maxWaiting = 8

// modelQueue gives the model to one request at a time, in the order they came. The model is
// loaded once and answers one question at a time, whoever asks, so many users never cost more
// memory, only a wait. Its zero value is ready to use.
type modelQueue struct {
	once    sync.Once
	slot    chan struct{} // holds a token while a request uses the model
	waiting atomic.Int32
	last    atomic.Int64 // when a question last finished with the model, in Unix nanoseconds
}

// errModelBusy is returned when maxWaiting questions are waiting already.
type errModelBusy int

func (e errModelBusy) Error() string {
	return fmt.Sprintf("the assistant is busy with %d other questions; try again in a minute", int(e))
}

// acquire waits for the model, telling status where the request is in the line, and returns the
// func that hands it back. It gives up when ctx ends, such as when the page is closed, or at once
// when too many requests are waiting.
func (q *modelQueue) acquire(ctx context.Context, status func(string)) (func(), error) {
	q.once.Do(func() { q.slot = make(chan struct{}, 1) })
	release := func() {
		q.last.Store(time.Now().UnixNano())
		<-q.slot
	}
	select {
	case q.slot <- struct{}{}:
		return release, nil
	default:
	}
	ahead := q.waiting.Add(1)
	defer q.waiting.Add(-1)
	if ahead > maxWaiting {
		return nil, errModelBusy(ahead)
	}
	switch {
	case status == nil:
	case ahead == 1:
		status("Waiting for the model to finish another answer; yours is next")
	case ahead == 2:
		status("Waiting for the model: it is answering another question, and 1 more is ahead of yours")
	default:
		status(fmt.Sprintf("Waiting for the model: it is answering another question, and %d more are ahead of yours", ahead-1))
	}
	// blocked senders are woken in the order they came
	select {
	case q.slot <- struct{}{}:
		return release, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// errModelInUse is why claim refused: the model is answering, or was used too recently to tell
// whether someone's chat is still going.
type errModelInUse string

func (e errModelInUse) Error() string { return string(e) }

// claim takes the model at once for a change to it, such as switching models, and refuses when
// someone is using it: it is answering or has questions waiting, or one finished within quiet.
// Its release doesn't count as a use.
func (q *modelQueue) claim(quiet time.Duration) (func(), error) {
	q.once.Do(func() { q.slot = make(chan struct{}, 1) })
	select {
	case q.slot <- struct{}{}:
	default:
		return nil, errModelInUse("The assistant is answering a question now.")
	}
	release := func() { <-q.slot }
	if n := q.waiting.Load(); n > 0 {
		release()
		return nil, errModelInUse("Questions are waiting for the assistant.")
	}
	if last := q.last.Load(); last != 0 {
		if ago := time.Since(time.Unix(0, last)); ago < quiet {
			release()
			return nil, errModelInUse(fmt.Sprintf("Someone used the assistant %s ago, so their chat may still be going.", agoText(ago)))
		}
	}
	return release, nil
}

// agoText is a short duration as people say it: "less than a minute", "3 minutes".
func agoText(d time.Duration) string {
	switch m := int(d.Minutes()); {
	case m < 1:
		return "less than a minute"
	case m == 1:
		return "a minute"
	default:
		return fmt.Sprintf("%d minutes", m)
	}
}
