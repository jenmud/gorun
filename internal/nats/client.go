package nats

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jenmud/gorun/internal/envvars"
	natsclient "github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

// Client is an alias for the NATS client connection.
type Client = natsclient.Conn

// DefaultURL is the default NATS URL.
const DefaultURL = natsclient.DefaultURL

// NewClient connects to NATS, configured by the GORUN_NATS_CLIENT_* environment variables.
func NewClient() (*Client, error) {
	start := time.Now()
	_, span := tracer.Start(context.Background(), "gorun/nats.NewClient")
	defer span.End()
	fail := func(err error) (*Client, error) {
		slog.Error("gorun nats: error creating client", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	options := natsclient.GetDefaultOptions()
	options.Name = cmp.Or(envvars.Get("GORUN_NATS_CLIENT_NAME"), envvars.Get("GORUN_NAME"), "gorun")
	options.Url = cmp.Or(envvars.Get("GORUN_NATS_CLIENT_URL"), DefaultURL)
	if envvars.Get("GORUN_NATS_CLIENT_ALLOW_RECONNECT") != "" {
		options.AllowReconnect = envvars.Bool("GORUN_NATS_CLIENT_ALLOW_RECONNECT")
	}
	for key, dst := range map[string]*time.Duration{
		"GORUN_NATS_CLIENT_RECONNECT_WAIT": &options.ReconnectWait,
		"GORUN_NATS_CLIENT_TIMEOUT":        &options.Timeout,
	} {
		if v := envvars.Get(key); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fail(fmt.Errorf("error creating NATS client, parsing %s: %w", key, err))
			}
			*dst = d
		}
	}

	conn, err := options.Connect()
	if err != nil {
		connectionErrors.Add(context.Background(), 1, metric.WithAttributes(attribute.String("url", options.Url)))
		return fail(fmt.Errorf("error connecting to NATS: %w", err))
	}
	connectDuration.Record(context.Background(), time.Since(start).Seconds(), metric.WithAttributes(
		attribute.String("url", options.Url),
		attribute.String("name", options.Name),
	))
	slog.Info("gorun nats: connected to server", "url", options.Url, "name", options.Name)
	return conn, nil
}
