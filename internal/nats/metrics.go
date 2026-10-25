package nats

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	tracer                  = otel.Tracer("gorun/nats")
	meter                   = otel.Meter("gorun")
	subscriptionDuration, _ = meter.Float64Histogram("gorun.nats.subscription.duration",
		metric.WithDescription("NATS subscription duration"),
		metric.WithUnit("s"),
	)
	connectionErrors, _ = meter.Int64Counter(
		"gorun.nats.connection.errors",
		metric.WithDescription("Number of NATS connection errors"),
		metric.WithUnit("{error}"),
	)
	eventProcessingDuration, _ = meter.Float64Histogram("gorun.nats.event.processing.duration",
		metric.WithDescription("NATS event processing duration"),
		metric.WithUnit("s"),
	)
	eventTypes, _ = meter.Int64Counter(
		"gorun.nats.events.by_type",
		metric.WithDescription("Number of events by type"),
		metric.WithUnit("{event}"),
	)
	messageUnmarshalErrors, _ = meter.Int64Counter(
		"gorun.nats.message.unmarshal.errors",
		metric.WithDescription("Number of message unmarshal errors"),
		metric.WithUnit("{error}"),
	)
	hubApplyDuration, _ = meter.Float64Histogram("gorun.nats.hub.apply.duration",
		metric.WithDescription("Hub apply duration"),
		metric.WithUnit("s"),
	)
	databaseUpdateDuration, _ = meter.Float64Histogram("gorun.nats.database.update.duration",
		metric.WithDescription("Database update duration"),
		metric.WithUnit("s"),
	)
	broadcastDuration, _ = meter.Float64Histogram("gorun.nats.broadcast.duration",
		metric.WithDescription("Broadcast duration"),
		metric.WithUnit("s"),
	)
	handlerErrors, _ = meter.Int64Counter(
		"gorun.nats.handler.errors",
		metric.WithDescription("Number of handler errors"),
		metric.WithUnit("{error}"),
	)
)
