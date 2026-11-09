package gorun

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"
)

// CopyProgress is how far one copy of a task has got: a file, or a whole directory.
type CopyProgress struct {
	// File is where the copy goes on the server, as the task's copy names it.
	File string
	// Bytes is how many bytes have been sent so far, out of Total.
	Bytes int64
	Total int64
	// Elapsed is how long the copy has been running.
	Elapsed time.Duration
	// Done is true on the last report, once the server has acknowledged every byte.
	Done bool
}

// Percent is how much of the copy has been sent, from 0 to 100. An empty copy is 100 once done.
func (p CopyProgress) Percent() float64 {
	if p.Total <= 0 {
		if p.Done {
			return 100
		}
		return 0
	}
	return float64(p.Bytes) * 100 / float64(p.Total)
}

// BytesPerSecond is the average upload rate so far.
func (p CopyProgress) BytesPerSecond() float64 {
	if p.Elapsed <= 0 {
		return 0
	}
	return float64(p.Bytes) / p.Elapsed.Seconds()
}

// CopyProgressFunc receives the progress of a copy. It is called from the goroutine doing the copy,
// so it should return quickly.
type CopyProgressFunc func(CopyProgress)

// progressInterval is the least time between two progress reports of one copy.
const progressInterval = 250 * time.Millisecond

// copyTracker counts the bytes of one copy and reports them at most every progressInterval.
type copyTracker struct {
	ctx         context.Context
	fn          CopyProgressFunc
	file        string
	total, sent int64
	start, last time.Time
}

// newCopyTracker starts tracking a copy of total bytes to file, and reports it at 0%.
// With a nil fn it reports nothing, and only stops the copy when ctx is cancelled.
func newCopyTracker(ctx context.Context, fn CopyProgressFunc, file string, total int64) *copyTracker {
	if fn == nil {
		return &copyTracker{ctx: ctx}
	}
	now := time.Now()
	t := &copyTracker{ctx: ctx, fn: fn, file: file, total: total, start: now, last: now}
	t.report(false)
	return t
}

// report tells the callback how far the copy has got.
func (t *copyTracker) report(done bool) {
	if t.fn == nil {
		return
	}
	t.fn(CopyProgress{File: t.file, Bytes: t.sent, Total: t.total, Elapsed: time.Since(t.start), Done: done})
}

// add counts n more bytes, and reports them if enough time has passed since the last report.
func (t *copyTracker) add(n int64) {
	t.sent += n
	if t.fn == nil || t.sent >= t.total {
		return // the final report waits for the server's acknowledgement
	}
	if now := time.Now(); now.Sub(t.last) >= progressInterval {
		t.last = now
		t.report(false)
	}
}

// done reports the copy as finished.
func (t *copyTracker) done() { t.report(true) }

// wrap counts the bytes read from r, and stops the copy when the context is cancelled.
func (t *copyTracker) wrap(r io.Reader) io.Reader { return &progressReader{t: t, r: r} }

// progressReader counts what is read through it and fails once its tracker's context is done.
type progressReader struct {
	t *copyTracker
	r io.Reader
}

// Read reads from the wrapped reader, counting the bytes.
func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.t.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	p.t.add(int64(n))
	return n, err
}

// dirSize adds up the sizes of the regular files under dir, following symlinks as the copy does.
func dirSize(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			return 0, err
		}
		switch {
		case info.IsDir():
			n, err := dirSize(filepath.Join(dir, e.Name()))
			if err != nil {
				return 0, err
			}
			total += n
		case info.Mode().IsRegular():
			total += info.Size()
		}
	}
	return total, nil
}
