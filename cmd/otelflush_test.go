package main

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"
)

type flushingProvider struct {
	noop.TracerProvider
	flushed int
}

func (p *flushingProvider) ForceFlush(context.Context) error { p.flushed++; return nil }

func TestFlushTelemetryFlushesTheTracerProvider(t *testing.T) {
	old := otel.GetTracerProvider()
	defer otel.SetTracerProvider(old)

	p := &flushingProvider{}
	otel.SetTracerProvider(p)
	flushTelemetry()
	if p.flushed != 1 {
		t.Errorf("ForceFlush called %d times, want 1", p.flushed)
	}
}
