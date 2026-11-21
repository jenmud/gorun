package main

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	logglobal "go.opentelemetry.io/otel/log/global"
)

// flushTelemetry sends spans, metrics and logs still buffered in the OpenTelemetry SDK, which the
// instrumented build only does on a signal. Without it a short run exits before its last batch.
func flushTelemetry() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, p := range []any{otel.GetTracerProvider(), otel.GetMeterProvider(), logglobal.GetLoggerProvider()} {
		if f, ok := p.(interface{ ForceFlush(context.Context) error }); ok {
			_ = f.ForceFlush(ctx)
		}
	}
}

// exit flushes telemetry, then exits.
func exit(code int) {
	flushTelemetry()
	os.Exit(code)
}
