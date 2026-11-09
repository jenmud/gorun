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

// Embedded is a NATS server this process started.
type Embedded struct {
	URL        string // where clients connect
	ConfigPath string // the config file it was started with; empty for the defaults
	srv        *server.Server
}

// Connections is how many clients are connected to the server right now.
func (e *Embedded) Connections() int {
	return e.srv.NumClients()
}

// Stop shuts the server down and waits for it to finish.
func (e *Embedded) Stop() {
	e.srv.Shutdown()
	e.srv.WaitForShutdown()
}

// Ensure makes sure a NATS server is reachable. It returns the server it started, or nil when
// none was needed: the user named a server, NATS is skipped, or a server already answers at
// ProbeURL.
func Ensure(s Settings) (*Embedded, error) {
	if s.Skip {
		return nil, nil
	}
	if s.ClientURL != "" {
		slog.Info("nats: using the server given by GORUN_NATS_CLIENT_URL", "url", s.ClientURL)
		return nil, nil
	}
	if reachable(s.ProbeURL) {
		return nil, nil
	}

	opts, err := options(s.ConfigPath)
	if err != nil {
		return nil, err
	}
	srv, err := server.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("nats server: %w", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		srv.Shutdown()
		return nil, fmt.Errorf("nats server did not start listening on %s", clientURL(opts))
	}
	e := &Embedded{URL: clientURL(opts), ConfigPath: s.ConfigPath, srv: srv}
	slog.Info("nats: started an embedded server", "url", e.URL, "config", s.ConfigPath)
	return e, nil
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
