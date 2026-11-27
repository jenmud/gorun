package websrv

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jenmud/gorun"
)

// gateEngine answers once open is closed, and records how many answers ran at once.
type gateEngine struct {
	open        chan struct{}
	running     atomic.Int32
	most, total atomic.Int32
}

func (e *gateEngine) Chat(ctx context.Context, _ []gorun.ChatMessage) (string, error) {
	n := e.running.Add(1)
	defer e.running.Add(-1)
	for m := e.most.Load(); n > m && !e.most.CompareAndSwap(m, n); m = e.most.Load() {
	}
	e.total.Add(1)
	select {
	case <-e.open:
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Many people asking at once share one model: it answers one at a time, a full line turns the
// next away, and someone who leaves the line doesn't hold it up.
func TestManyUsersShareTheModel(t *testing.T) {
	a, _ := newTestApp(t)
	t.Cleanup(a.stopScheduler)
	e := &gateEngine{open: make(chan struct{})}
	a.gen = &generator{assistant: gorun.Assistant{Engine: e}}

	ask := func(ctx context.Context) error {
		release, err := a.genQ.acquire(ctx, nil)
		if err != nil {
			return err
		}
		defer release()
		_, err = a.gen.assistant.Chat(ctx, nil)
		return err
	}

	// one answering, maxWaiting in line
	var wg sync.WaitGroup
	errs := make(chan error, maxWaiting+1)
	for range maxWaiting + 1 {
		wg.Go(func() { errs <- ask(context.Background()) })
	}
	waitFor(t, func() bool { return a.genQ.waiting.Load() == maxWaiting })

	var busy errModelBusy
	if err := ask(context.Background()); !errors.As(err, &busy) {
		t.Errorf("a question past a full line = %v, want it turned away", err)
	}

	close(e.open)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a question in line failed: %v", err)
		}
	}
	if got := e.most.Load(); got != 1 {
		t.Errorf("%d answers ran at once, want 1", got)
	}
	if got := e.total.Load(); got != maxWaiting+1 {
		t.Errorf("the model answered %d questions, want %d", got, maxWaiting+1)
	}
}

// Someone whose page closes leaves the line, and their place is given back.
func TestALeaverGivesBackItsPlace(t *testing.T) {
	var q modelQueue
	release, err := q.acquire(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	left := make(chan error, 1)
	var status string
	go func() {
		_, err := q.acquire(ctx, func(s string) { status = s })
		left <- err
	}()
	waitFor(t, func() bool { return q.waiting.Load() == 1 })
	cancel()
	if err := <-left; !errors.Is(err, context.Canceled) {
		t.Errorf("acquire() after the page closed = %v", err)
	}
	if q.waiting.Load() != 0 {
		t.Errorf("waiting = %d after the only one waiting left", q.waiting.Load())
	}
	if status != "Waiting for the model to finish another answer; yours is next" {
		t.Errorf("status = %q", status)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
