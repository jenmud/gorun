package websrv

import (
	"testing"
	"time"
)

// TestModelDownloadStartsAgainAfterOneEnds starts downloads that fail at once (the repository
// name is invalid, so nothing is fetched), one after the other, and reads their state between.
func TestModelDownloadStartsAgainAfterOneEnds(t *testing.T) {
	var d modelDownload
	dir := t.TempDir()
	for i := range 2 {
		if err := d.start(dir, "not a repository", "x.gguf"); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for d.view().Running {
			if time.Now().After(deadline) {
				t.Fatalf("download %d never ended", i)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if v := d.view(); v.Error == "" || v.Done {
			t.Errorf("download %d = %+v, want it failed", i, v)
		}
	}
	d.stop() // nothing running: no-op
}
