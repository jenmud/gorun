// Package natsserver starts an embedded NATS server when none is running, so the web service
// works out of the box. A NATS address given by the user always wins: no server is started.
package natsserver

import (
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsclient "github.com/nats-io/nats.go"
)

// Defaults for the embedded server, used when no config file is given. Loopback only, so a
// fresh install doesn't listen on the network.
const (
	DefaultHost = "127.0.0.1"
	DefaultPort = natsclient.DefaultPort
)

// Settings say how to find or start the server.
type Settings struct {
	ConfigPath string // a nats-server config file; empty uses the defaults
	ProbeURL   string // a server already running here is used, and none is started
	ClientURL  string // when set, the user named a server: it is used and none is started
	Skip       bool   // never start one (GORUN_SKIP_NATS)
}

// Ensure makes sure a NATS server is reachable and returns the URL clients should use, plus a
// stop function. The URL is empty when the existing configuration is already right: the user
// named a server, NATS is skipped, or a server answers at ProbeURL.
func Ensure(s Settings) (url string, stop func(), err error) {
	noop := func() {}
	if s.Skip {
		return "", noop, nil
	}
	if s.ClientURL != "" {
		slog.Info("nats: using the server given by GORUN_NATS_CLIENT_URL", "url", s.ClientURL)
		return "", noop, nil
	}
	if reachable(s.ProbeURL) {
		return "", noop, nil
	}

	opts, err := options(s.ConfigPath)
	if err != nil {
		return "", noop, err
	}
	srv, err := server.NewServer(opts)
	if err != nil {
		return "", noop, fmt.Errorf("nats server: %w", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		srv.Shutdown()
		return "", noop, fmt.Errorf("nats server did not start listening on %s", srv.ClientURL())
	}
	url = clientURL(opts)
	slog.Info("nats: started an embedded server", "url", url, "config", s.ConfigPath)
	return url, func() { srv.Shutdown(); srv.WaitForShutdown() }, nil
}

// clientURL is the URL this process connects to. A wildcard host (0.0.0.0, ::) listens on every
// address, but connecting to it is not portable, so the loopback address is used instead.
func clientURL(opts *server.Options) string {
	host := opts.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = DefaultHost
	}
	return "nats://" + net.JoinHostPort(host, fmt.Sprint(opts.Port))
}

// options returns the server options: the config file's, or the defaults. A config file
// overrides what it sets; a host or port it leaves out falls back to the defaults.
func options(configPath string) (*server.Options, error) {
	opts := &server.Options{Host: DefaultHost, Port: DefaultPort, NoLog: true, NoSigs: true}
	if configPath == "" {
		return opts, nil
	}
	fromFile, err := server.ProcessConfigFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("nats server config %s: %w", configPath, err)
	}
	if fromFile.Host != "" {
		opts.Host = fromFile.Host
	}
	if fromFile.Port != 0 {
		opts.Port = fromFile.Port
	}
	// the file's own settings are the rest of the options
	fromFile.Host, fromFile.Port = opts.Host, opts.Port
	fromFile.NoSigs = true
	return fromFile, nil
}

// reachable reports whether a NATS server answers at url.
func reachable(url string) bool {
	if url == "" {
		return false
	}
	nc, err := natsclient.Connect(url, natsclient.Timeout(time.Second), natsclient.MaxReconnects(0))
	if err != nil {
		return false
	}
	nc.Close()
	return true
}
