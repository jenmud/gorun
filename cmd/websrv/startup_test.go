package websrv

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	inats "github.com/jenmud/gorun/internal/nats"
	"github.com/nats-io/nats-server/v2/server"
)

// message renders the startup message for info, as one string.
func message(info startupInfo) string {
	var b strings.Builder
	startupMessage(&b, info)
	return b.String()
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

// lineFor returns the message line with the given label, or "".
func lineFor(msg, label string) string {
	for _, l := range strings.Split(msg, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), label+" ") {
			return l
		}
	}
	return ""
}

func TestStartupMessageAllInterfaces(t *testing.T) {
	for _, listen := range []string{"[::]:8080", "0.0.0.0:8080", ":8080"} {
		first := firstLine(message(startupInfo{listen: listen}))
		if !strings.HasPrefix(first, "gorun is serving on http://localhost:8080 ") {
			t.Errorf("%s: first line = %q, want the localhost URL", listen, first)
		}
		if !strings.Contains(first, "all interfaces") {
			t.Errorf("%s: first line = %q, want all interfaces", listen, first)
		}
	}
}

func TestStartupMessageSpecificHost(t *testing.T) {
	first := firstLine(message(startupInfo{listen: "127.0.0.1:9000"}))
	if want := "gorun is serving on http://127.0.0.1:9000 (listening on 127.0.0.1:9000)"; first != want {
		t.Errorf("first line = %q, want %q", first, want)
	}
	if strings.Contains(first, "all interfaces") {
		t.Errorf("a specific host should have no all interfaces note: %q", first)
	}
}

func TestStartupMessageGorunfile(t *testing.T) {
	for _, tc := range []struct {
		name string
		info startupInfo
		want []string
	}{
		{"schedules", startupInfo{cfgPath: "/abs/Gorunfile", schedules: 2, schedulerOn: true},
			[]string{"/abs/Gorunfile", "2 schedules", "scheduler running"}},
		{"no schedules", startupInfo{cfgPath: "/abs/Gorunfile"}, []string{"/abs/Gorunfile", "no schedules"}},
		{"no file", startupInfo{}, []string{"none, load one on the Config page"}},
		{"load error", startupInfo{cfgErr: errors.New("bad toml")}, []string{"not loaded: bad toml"}},
	} {
		line := lineFor(message(tc.info), "Gorunfile")
		for _, w := range tc.want {
			if !strings.Contains(line, w) {
				t.Errorf("%s: Gorunfile line = %q, want %q", tc.name, line, w)
			}
		}
	}
}

func TestStartupMessageNATS(t *testing.T) {
	for _, tc := range []struct {
		n    natsState
		want []string
	}{
		{natsState{mode: "embedded", url: "nats://127.0.0.1:4222"},
			[]string{"embedded server at nats://127.0.0.1:4222", "events on gorun.events"}},
		{natsState{mode: "external", url: "nats://nats.internal:4222"},
			[]string{"external server at nats://nats.internal:4222", "events on gorun.events"}},
		{natsState{mode: "off", reason: "GORUN_SKIP_NATS is set"},
			[]string{"off (GORUN_SKIP_NATS is set)", "live updates and runs are off"}},
	} {
		line := lineFor(message(startupInfo{nats: tc.n, topic: "gorun.events"}), "NATS")
		for _, w := range tc.want {
			if !strings.Contains(line, w) {
				t.Errorf("%s: NATS line = %q, want %q", tc.n.mode, line, w)
			}
		}
	}
}

func TestStartupMessageHistoryAndLog(t *testing.T) {
	msg := message(startupInfo{dbPath: "/home/u/.gorun/gorun.db", logPath: "/home/u/.gorun/gorun.log"})
	if l := lineFor(msg, "history"); !strings.HasSuffix(l, "/home/u/.gorun/gorun.db") {
		t.Errorf("history line = %q", l)
	}
	if l := lineFor(msg, "log"); !strings.HasSuffix(l, "/home/u/.gorun/gorun.log") {
		t.Errorf("log line = %q", l)
	}

	msg = message(startupInfo{dbPath: "/x.db", dbErr: errors.New("disk full")})
	if l := lineFor(msg, "history"); !strings.HasSuffix(l, "off (disk full)") {
		t.Errorf("history line = %q, want off (disk full)", l)
	}
	if l := lineFor(msg, "log"); l != "" {
		t.Errorf("an empty log path should leave out the log line, got %q", l)
	}
	if !strings.HasSuffix(msg, "Press ctrl+c to stop.\n") {
		t.Errorf("message should end with how to stop, got:\n%s", msg)
	}
}

func TestRunReportsBindError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	addr := ln.Addr().String()

	var out strings.Builder
	err = listenAndServe(&http.Server{}, addr, func(a net.Addr) { startupMessage(&out, startupInfo{listen: a.String()}) })
	if err == nil {
		t.Fatal("binding a used address should fail")
	}
	if !strings.HasPrefix(err.Error(), "listen "+addr+": ") {
		t.Errorf("error = %q, want it to start with listen %s", err, addr)
	}
	if out.Len() != 0 {
		t.Errorf("nothing should be printed when binding fails, got %q", out.String())
	}
}

func TestStartupMessageSchedulerNotStarted(t *testing.T) {
	line := lineFor(message(startupInfo{cfgPath: "/abs/Gorunfile", schedules: 1}), "Gorunfile")
	if !strings.HasSuffix(line, "/abs/Gorunfile, 1 schedule, scheduler not started, see the log") {
		t.Errorf("Gorunfile line = %q", line)
	}
}

func TestStartupMessageHidesNATSCredentials(t *testing.T) {
	for _, tc := range []struct{ user, pass string }{{"alice", "s3cret"}, {"tok3n", ""}} {
		opts := &server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true}
		if tc.pass != "" {
			opts.Username, opts.Password = tc.user, tc.pass
		} else {
			opts.Authorization = tc.user
		}
		srv, err := server.NewServer(opts)
		if err != nil {
			t.Fatal(err)
		}
		go srv.Start()
		if !srv.ReadyForConnections(5 * time.Second) {
			t.Fatal("nats server did not start")
		}
		creds := tc.user
		if tc.pass != "" {
			creds += ":" + tc.pass
		}
		addr := srv.Addr().String()
		t.Setenv("GORUN_NATS_CLIENT_URL", "nats://"+creds+"@"+addr)
		nc, err := inats.NewClient()
		if err != nil {
			srv.Shutdown()
			t.Fatal(err)
		}
		msg := message(startupInfo{nats: natsStartState(nil, nc, nil, false), topic: "gorun.events"})
		nc.Close()
		srv.Shutdown()

		line := lineFor(msg, "NATS")
		if !strings.Contains(line, "external server at nats://"+addr) {
			t.Errorf("NATS line = %q, want the URL without credentials", line)
		}
		for _, secret := range []string{tc.user, tc.pass} {
			if secret != "" && strings.Contains(msg, secret) {
				t.Errorf("startup message leaks %q:\n%s", secret, msg)
			}
		}
	}
}

func TestNATSOffReasonHidesCredentials(t *testing.T) {
	err := errors.New("dial nats://alice:s3cret@nats.internal:4222 failed; also nats://tok3n@other:4222")
	line := lineFor(message(startupInfo{nats: natsStartState(nil, nil, err, false)}), "NATS")
	for _, secret := range []string{"alice", "s3cret", "tok3n"} {
		if strings.Contains(line, secret) {
			t.Errorf("off reason leaks %q: %s", secret, line)
		}
	}
	if !strings.Contains(line, "nats://nats.internal:4222") || !strings.Contains(line, "nats://other:4222") {
		t.Errorf("off reason should keep the hosts: %s", line)
	}
}
