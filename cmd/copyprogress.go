package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/jenmud/gorun"
)

// formatBytes shows a size the way scp does: 1024-based, with one decimal from KB up.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTP"[exp])
}

// copySizes is "440.0 MB / 1.0 GB · 12.3 MB/s" for a copy in progress.
func copySizes(ev gorun.CopyProgressEvent) string {
	s := formatBytes(ev.Bytes) + " / " + formatBytes(ev.Total)
	if ev.BytesPerSecond > 0 {
		s += " · " + formatBytes(int64(ev.BytesPerSecond)) + "/s"
	}
	return s
}

// copyName is the file a copy writes, without its directory.
func copyName(ev gorun.CopyProgressEvent) string {
	return filepath.Base(ev.File)
}

// copyFinished is "sent release.tar.gz (1.0 GB in 10.2s, 98.0 MB/s)".
func copyFinished(ev gorun.CopyProgressEvent) string {
	rate := ""
	if ev.BytesPerSecond > 0 {
		rate = ", " + formatBytes(int64(ev.BytesPerSecond)) + "/s"
	}
	return fmt.Sprintf("sent %s (%s in %s%s)", copyName(ev), formatBytes(ev.Total), ev.Elapsed.Round(100*time.Millisecond), rate)
}
