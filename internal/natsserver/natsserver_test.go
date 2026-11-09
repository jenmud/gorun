package natsserver

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsclient "github.com/nats-io/nats.go"
)

// freePort returns a loopback port nothing is listening on.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func writeConf(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nats.conf")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnsureStartsNothingWhenToldNotTo(t *testing.T) {
	if e, err := Ensure(Settings{Skip: true, ProbeURL: "nats://127.0.0.1:1"}); e != nil || err != nil {
		t.Errorf("skip: started %v err %v", e, err)
	}
	if e, err := Ensure(Settings{ClientURL: "nats://elsewhere:4222", ProbeURL: "nats://127.0.0.1:1"}); e != nil || err != nil {
		t.Errorf("given address: started %v err %v", e, err)
	}
}

func TestEnsureUsesAServerThatIsAlreadyRunning(t *testing.T) {
	port := freePort(t)
	conf := writeConf(t, fmt.Sprintf("host: 127.0.0.1\nport: %d\n", port))
	first, err := Ensure(Settings{ConfigPath: conf, ProbeURL: "nats://127.0.0.1:1"})
	if err != nil || first == nil {
		t.Fatalf("starting the first server: %v err %v", first, err)
	}
	defer first.Stop()

	// a second Ensure finds the first and starts nothing
	if e, err := Ensure(Settings{ProbeURL: first.URL}); e != nil || err != nil {
		t.Errorf("second Ensure: started %v err %v, want the running server reused", e, err)
	}
}

func TestEnsureStartsAServerFromItsConfig(t *testing.T) {
	port := freePort(t)
	conf := writeConf(t, fmt.Sprintf("host: 127.0.0.1\nport: %d\nmax_payload: 2048\n", port))

	e, err := Ensure(Settings{ConfigPath: conf, ProbeURL: fmt.Sprintf("nats://127.0.0.1:%d", freePort(t))})
	if err != nil || e == nil {
		t.Fatalf("Ensure: %v err %v", e, err)
	}
	defer e.Stop()
	if want := fmt.Sprintf("nats://127.0.0.1:%d", port); e.URL != want {
		t.Fatalf("url = %q, want %q", e.URL, want)
	}
	if e.Connections() != 0 {
		t.Errorf("connections = %d before anyone connects", e.Connections())
	}

	nc, err := natsclient.Connect(e.URL, natsclient.Timeout(2*time.Second))
	if err != nil {
		t.Fatalf("connect to the embedded server: %v", err)
	}
	defer nc.Close()
	got := make(chan string, 1)
	sub, err := nc.Subscribe("gorun.test", func(m *natsclient.Msg) { got <- string(m.Data) })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	if err := nc.Publish("gorun.test", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		if msg != "hello" {
			t.Errorf("got %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no message from the embedded server")
	}
}

func TestClientURLNeverDialsAWildcardAddress(t *testing.T) {
	for host, want := range map[string]string{
		"0.0.0.0":  "nats://127.0.0.1:4222",
		"::":       "nats://127.0.0.1:4222",
		"10.1.2.3": "nats://10.1.2.3:4222",
	} {
		if got := clientURL(&server.Options{Host: host, Port: 4222}); got != want {
			t.Errorf("host %s: url %q, want %q", host, got, want)
		}
	}
}

func TestOptionsDefaultsAndConfigOverrides(t *testing.T) {
	opts, err := options("")
	if err != nil {
		t.Fatal(err)
	}
	if opts.Host != DefaultHost || opts.Port != DefaultPort {
		t.Errorf("defaults = %s:%d, want %s:%d", opts.Host, opts.Port, DefaultHost, DefaultPort)
	}

	// a config that sets only the port keeps the default host
	opts, err = options(writeConf(t, "port: 5555\n"))
	if err != nil {
		t.Fatal(err)
	}
	if opts.Host != DefaultHost || opts.Port != 5555 {
		t.Errorf("partial config = %s:%d, want %s:5555", opts.Host, opts.Port, DefaultHost)
	}

	if _, err := options(filepath.Join(t.TempDir(), "missing.conf")); err == nil {
		t.Error("a missing config file was accepted")
	}
}
