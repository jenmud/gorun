package websrv

import (
	"errors"
	"strings"
	"testing"
)

func TestNATSStartStateNamesTheMode(t *testing.T) {
	if s := natsStartState(nil, nil, errors.New("refused"), true); s.mode != "off" || !strings.Contains(s.reason, "GORUN_SKIP_NATS") {
		t.Errorf("skipped: %+v", s)
	}
	if s := natsStartState(nil, nil, errors.New("refused"), false); s.mode != "off" || s.reason != "refused" {
		t.Errorf("connect failed: %+v", s)
	}
}

func TestPagesSayHowNATSIsReached(t *testing.T) {
	a, _ := newTestApp(t)

	// before startup has set anything, the pages say NATS was not set up
	if body := get(t, a, "/tasks").Body.String(); !strings.Contains(body, "NATS is off") {
		t.Error("header does not say NATS is off")
	}

	a.nats = natsState{mode: "external", url: "nats://nats.internal:4222"}
	body := get(t, a, "/config").Body.String()
	for _, want := range []string{"external, already running", "nats://nats.internal:4222"} {
		if !strings.Contains(body, want) {
			t.Errorf("config page missing %q for an external server", want)
		}
	}
	if strings.Contains(body, "Clients") {
		t.Error("an external server shows an embedded-only client count")
	}

	a.nats = natsState{mode: "off", reason: "GORUN_SKIP_NATS is set"}
	if body := get(t, a, "/tasks").Body.String(); !strings.Contains(body, "GORUN_SKIP_NATS is set") {
		t.Error("header does not give the reason NATS is off")
	}
}
