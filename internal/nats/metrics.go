package nats

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	tracer = otel.Tracer("gorun/nats")
	meter  = otel.Meter("gorun")

	connectDuration, _ = meter.Float64Histogram("gorun.nats.subscription.duration",
		metric.WithDescription("Time taken to connect to NATS"), metric.WithUnit("s"))
	connectionErrors, _ = meter.Int64Counter("gorun.nats.connection.errors",
		metric.WithDescription("Number of NATS connection errors"), metric.WithUnit("{error}"))
)
