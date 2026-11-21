package gorun

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
)

type recordingProvider struct {
	embedded.LoggerProvider
	records []otellog.Record
}

func (p *recordingProvider) Logger(string, ...otellog.LoggerOption) otellog.Logger {
	return &recordingLogger{p: p}
}

type recordingLogger struct {
	embedded.Logger
	p *recordingProvider
}

func (l *recordingLogger) Emit(_ context.Context, r otellog.Record) {
	l.p.records = append(l.p.records, r)
}
func (l *recordingLogger) Enabled(context.Context, otellog.EnabledParameters) bool { return true }

func attrs(r otellog.Record) map[string]otellog.Value {
	m := map[string]otellog.Value{}
	r.WalkAttributes(func(kv otellog.KeyValue) bool { m[kv.Key] = kv.Value; return true })
	return m
}

func TestOTelHandlerEmitsRecords(t *testing.T) {
	p := &recordingProvider{}
	logger := slog.New(newOTelHandler(p, slog.LevelInfo)).With("name", "deploy").WithGroup("server")

	logger.Debug("too quiet")
	logger.Error("task failed", slog.Int("exitcode", 13), slog.String("user", "dev"))

	if len(p.records) != 1 {
		t.Fatalf("got %d records, want 1 (debug is below the level)", len(p.records))
	}
	r := p.records[0]
	if r.Body().AsString() != "task failed" || r.Severity() != otellog.SeverityError {
		t.Errorf("got body %q severity %v", r.Body().AsString(), r.Severity())
	}
	got := attrs(r)
	if got["name"].AsString() != "deploy" || got["server.exitcode"].AsInt64() != 13 || got["server.user"].AsString() != "dev" {
		t.Errorf("attributes = %v", got)
	}
}

func TestNewLoggerKeepsWritingText(t *testing.T) {
	var out bytes.Buffer
	NewLogger("INFO", &out, false).Info("hello")
	if got := out.String(); got == "" {
		t.Error("the text log is empty")
	}
}
