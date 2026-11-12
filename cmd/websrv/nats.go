package websrv

import (
	"errors"
	"net/url"
	"regexp"

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
		return natsState{mode: "embedded", url: withoutUserinfo(nc.ConnectedUrl()), emb: emb}
	case nc != nil:
		return natsState{mode: "external", url: withoutUserinfo(nc.ConnectedUrl())}
	case skipped:
		return natsState{mode: "off", reason: "GORUN_SKIP_NATS is set"}
	case connectErr != nil:
		var urlErr *url.Error
		if errors.As(connectErr, &urlErr) {
			// a url.Error quotes the URL, credentials and all, and may not parse well enough to scrub
			return natsState{mode: "off", reason: "GORUN_NATS_CLIENT_URL is not a valid URL"}
		}
		return natsState{mode: "off", reason: scrubUserinfo(connectErr.Error())}
	}
	return natsState{mode: "off", reason: "no NATS server answered"}
}

// withoutUserinfo drops the user, password or token from a NATS URL, so the URL can be shown
// on stdout and in the pages. ConnectedUrlRedacted is not enough: it keeps a token or user name.
func withoutUserinfo(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	return u.String()
}

// userinfoRE matches the userinfo of any URL in a piece of text, up to its last @, so a password
// with an unencoded / is caught too.
var userinfoRE = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s"]*@`)

// scrubUserinfo removes userinfo from every URL in s, such as one echoed in a connect error.
func scrubUserinfo(s string) string { return userinfoRE.ReplaceAllString(s, "$1") }

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
