package websrv

import (
	"github.com/jenmud/gorun/cmd/web/ui"
	inats "github.com/jenmud/gorun/internal/nats"
	"github.com/jenmud/gorun/internal/natsserver"
)

// natsState is how the web service reaches NATS. It is set once at startup and read by every page.
type natsState struct {
	mode   string // "embedded", "external" or "off"
	url    string // the address clients connect to, when NATS is on
	reason string // why NATS is off
	emb    *natsserver.Embedded
}

// natsStartState works out how the service reaches NATS once it has tried to connect.
func natsStartState(emb *natsserver.Embedded, nc *inats.Client, connectErr error, skipped bool) natsState {
	switch {
	case emb != nil && nc != nil:
		return natsState{mode: "embedded", url: nc.ConnectedUrl(), emb: emb}
	case nc != nil:
		return natsState{mode: "external", url: nc.ConnectedUrl()}
	case skipped:
		return natsState{mode: "off", reason: "GORUN_SKIP_NATS is set"}
	case connectErr != nil:
		return natsState{mode: "off", reason: connectErr.Error()}
	}
	return natsState{mode: "off", reason: "no NATS server answered"}
}

// natsStatus is the NATS state for the pages, with an embedded server's live client count.
func (a *app) natsStatus() ui.NATSStatus {
	if a.nats.mode == "" {
		return ui.NATSStatus{Mode: "off", Reason: "NATS was not set up"}
	}
	n := ui.NATSStatus{Mode: a.nats.mode, URL: a.nats.url, Reason: a.nats.reason}
	if a.nats.emb != nil {
		n.ConfigPath = a.nats.emb.ConfigPath
		n.Connections = a.nats.emb.Connections()
	}
	return n
}
