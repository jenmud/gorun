package nats

import (
	"fmt"
	"gorun/internal/envvars"
	"time"

	"github.com/nats-io/nats.go"
)

// NewClient creates a new NATS client.
func NewClient() (*nats.Conn, error) {
	options := nats.GetDefaultOptions()

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
		url = nats.DefaultURL
	}

	options.Url = url

	reconnectWaitDuration := envvars.Get("GORUN_NATS_CLIENT_RECONNECT_WAIT")
	if reconnectWaitDuration != "" {
		d, err := time.ParseDuration(reconnectWaitDuration)
		if err != nil {
			return nil, fmt.Errorf("error creating NATS client, parsing GORUN_NATS_CLIENT_RECONNECT_WAIT: %w", err)
		}
		options.ReconnectWait = d
	}

	timeoutDuration := envvars.Get("GORUN_NATS_CLIENT_TIMEOUT")
	if timeoutDuration != "" {
		d, err := time.ParseDuration(timeoutDuration)
		if err != nil {
			return nil, fmt.Errorf("error creating NATS client, parsing GORUN_NATS_CLIENT_TIMEOUT: %w", err)
		}
		options.Timeout = d
	}

	return options.Connect()
}
