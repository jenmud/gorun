package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/jenmud/gorun"
)

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 1536: "1.5 KB",
		440 << 20: "440.0 MB", 1 << 30: "1.0 GB", 5 << 40: "5.0 TB",
	} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

// copyEvents is a 1 GB upload reported every 5%, then done.
func copyEvents() []gorun.CopyProgressEvent {
	const total = 1 << 30
	var out []gorun.CopyProgressEvent
	for pct := 0; pct < 100; pct += 5 {
		bytes := int64(total) * int64(pct) / 100
		out = append(out, gorun.CopyProgressEvent{
			Type: "copy_progress", Server: "web-1", Task: "upload", File: "/srv/app/release.tar.gz",
			Bytes: bytes, Total: total, Percent: float64(pct), BytesPerSecond: 100 << 20,
		})
	}
	return append(out, gorun.CopyProgressEvent{
		Type: "copy_progress", Server: "web-1", Task: "upload", File: "/srv/app/release.tar.gz",
		Bytes: total, Total: total, Percent: 100, BytesPerSecond: 100 << 20, Elapsed: 10240 * time.Millisecond, Done: true,
	})
}

func TestPlainPrintsCopyProgressEveryTenPercent(t *testing.T) {
	var buf bytes.Buffer
	p := &plainPrinter{w: &buf}
	for _, ev := range copyEvents() {
		p.event(ev)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 11 { // the start, 10% to 90%, and done
		t.Fatalf("got %d lines, want 11:\n%s", len(lines), buf.String())
	}
	if want := "web-1: upload sending release.tar.gz (1.0 GB)"; lines[0] != want {
		t.Errorf("first line = %q, want %q", lines[0], want)
	}
	if want := "web-1: upload sending release.tar.gz 40% (409.6 MB / 1.0 GB · 100.0 MB/s)"; lines[4] != want {
		t.Errorf("40%% line = %q, want %q", lines[4], want)
	}
	if want := "web-1: upload sent release.tar.gz (1.0 GB in 10.2s, 100.0 MB/s)"; lines[10] != want {
		t.Errorf("last line = %q, want %q", lines[10], want)
	}
}

func TestPlainSmallCopyPrintsNothing(t *testing.T) {
	var buf bytes.Buffer
	p := &plainPrinter{w: &buf}
	p.event(gorun.CopyProgressEvent{Server: "web-1", Task: "conf", File: "/etc/app.conf", Total: 300})
	p.event(gorun.CopyProgressEvent{Server: "web-1", Task: "conf", File: "/etc/app.conf", Bytes: 300, Total: 300, Percent: 100, Done: true})
	if got := buf.String(); got != "" {
		t.Fatalf("output = %q, want nothing for a small copy", got)
	}
}

func TestTUIShowsCopyProgress(t *testing.T) {
	m := newTUIModel("test", false, func() {})
	m.width, m.height = 120, 40
	m = m.applyEvent(gorun.PlanEvent{Server: "web-1", Address: "web-1:22", Tasks: []string{"upload"}})
	m = m.applyEvent(gorun.TaskEvent{Server: "web-1", Task: "upload", State: gorun.TaskRunning})
	events := copyEvents()
	m = m.applyEvent(events[8]) // 40%

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "upload ↑") || !strings.Contains(view, "40%") || !strings.Contains(view, "409.6 MB / 1.0 GB") {
		t.Fatalf("the compact view does not show the upload's progress:\n%s", view)
	}
	if got := ansi.Strip(m.taskStatus(m.servers[0].tasks[0])); !strings.Contains(got, "↑ release.tar.gz") || !strings.Contains(got, "40%") {
		t.Fatalf("task row = %q, want the file and its progress", got)
	}

	m = m.applyEvent(events[len(events)-1])
	if m.servers[0].tasks[0].copy != nil {
		t.Fatal("a finished copy still shows progress")
	}
	m = m.applyEvent(events[8])
	m = m.applyEvent(gorun.TaskEvent{Server: "web-1", Task: "upload", State: gorun.TaskDone})
	if m.servers[0].tasks[0].copy != nil {
		t.Fatal("a finished task still shows its copy")
	}
}
