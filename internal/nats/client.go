package nats

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	natsclient "github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"gorun/internal/envvars"
)

// Client is an alias for the NATS client connection.
type Client = natsclient.Conn

// DefaultURL is the default NATS URL.
const DefaultURL = natsclient.DefaultURL

// NewClient creates a new NATS client with tracing and metrics.
func NewClient() (*Client, error) {
	startTime := time.Now()
	_, span := tracer.Start(context.Background(), "gorun/nats.NewClient")
	defer span.End()

	options := natsclient.GetDefaultOptions()

	name := envvars.Get("GORUN_NATS_CLIENT_NAME")
	if name == "" {
		name = envvars.Get("GORUN_NAME")
		if name == "" {
			name = "gorun"
		}
	}

	options.Name = name
	if v := envvars.Get("GORUN_NATS_CLIENT_ALLOW_RECONNECT"); v != "" {
		options.AllowReconnect = envvars.Bool("GORUN_NATS_CLIENT_ALLOW_RECONNECT")
	}

	url := envvars.Get("GORUN_NATS_CLIENT_URL")
	if url == "" {
		url = DefaultURL
	}

	options.Url = url

	reconnectWaitDuration := envvars.Get("GORUN_NATS_CLIENT_RECONNECT_WAIT")
	if reconnectWaitDuration != "" {
		d, err := time.ParseDuration(reconnectWaitDuration)
		if err != nil {
			slog.Error("gorun nats: error creating client", "error", err)
			span.RecordError(err)
			span.SetStatus(codes.Error, fmt.Sprintf("error creating NATS client, parsing GORUN_NATS_CLIENT_RECONNECT_WAIT: %v", err))
			return nil, fmt.Errorf("error creating NATS client, parsing GORUN_NATS_CLIENT_RECONNECT_WAIT: %w", err)
		}
		options.ReconnectWait = d
	}

	timeoutDuration := envvars.Get("GORUN_NATS_CLIENT_TIMEOUT")
	if timeoutDuration != "" {
		d, err := time.ParseDuration(timeoutDuration)
		if err != nil {
			slog.Error("gorun nats: error creating client", "error", err)
			span.RecordError(err)
			span.SetStatus(codes.Error, fmt.Sprintf("error creating NATS client, parsing GORUN_NATS_CLIENT_TIMEOUT: %v", err))
			return nil, fmt.Errorf("error creating NATS client, parsing GORUN_NATS_CLIENT_TIMEOUT: %w", err)
		}
		options.Timeout = d
	}

	conn, err := options.Connect()
	if err != nil {
		slog.Error("gorun nats: error connecting to NATS", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "error connecting to NATS")
		connectionErrors.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("url", url),
		))
		return nil, fmt.Errorf("error connecting to NATS: %w", err)
	}

	subscriptionDuration.Record(context.Background(), float64(time.Since(startTime)), metric.WithAttributes(
		attribute.String("url", url),
		attribute.String("name", name),
	))

	slog.Info("gorun nats: connected to server", "url", url, "name", name)
	return conn, nil
}
