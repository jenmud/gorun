package websrv

import (
	"fmt"
	"io"
	"net"
)

// startupInfo is what the startup message reports about a service that is listening.
type startupInfo struct {
	listen      string // the bound listener address, e.g. "[::]:8080"
	cfgPath     string // the loaded Gorunfile, empty when none was given
	cfgErr      error  // why the Gorunfile did not load
	schedules   int
	schedulerOn bool
	nats        natsState
	topic       string
	dbPath      string
	dbErr       error // why the history store did not open
	logPath     string
}

// startupMessage writes the lines that tell the user the service is up, where to open it and
// what it started.
func startupMessage(w io.Writer, info startupInfo) {
	url, where := serveURL(info.listen)
	fmt.Fprintf(w, "gorun is serving on %s (listening on %s)\n", url, where)
	line := func(label, value string) { fmt.Fprintf(w, "  %-11s%s\n", label, value) }
	line("Gorunfile", gorunfileLine(info))
	line("NATS", natsLine(info.nats, info.topic))
	if info.dbErr != nil {
		line("history", fmt.Sprintf("off (%v)", info.dbErr))
	} else {
		line("history", info.dbPath)
	}
	if info.logPath != "" {
		line("log", info.logPath)
	}
	fmt.Fprintln(w, "Press ctrl+c to stop.")
}

// serveURL turns a bound address into the URL to open and the listening note. A wildcard host
// shows as localhost, since a browser can't open 0.0.0.0 or [::] everywhere.
func serveURL(listen string) (url, where string) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://" + listen, listen
	}
	switch host {
	case "", "0.0.0.0", "::":
		return "http://" + net.JoinHostPort("localhost", port), ":" + port + ", all interfaces"
	}
	hp := net.JoinHostPort(host, port)
	return "http://" + hp, hp
}

// gorunfileLine says which Gorunfile is loaded and whether its schedules are running.
func gorunfileLine(info startupInfo) string {
	switch {
	case info.cfgErr != nil:
		return fmt.Sprintf("not loaded: %v", info.cfgErr)
	case info.cfgPath == "":
		return "none, load one on the Config page"
	case info.schedules == 0:
		return info.cfgPath + ", no schedules"
	}
	noun := "schedules"
	if info.schedules == 1 {
		noun = "schedule"
	}
	state := "scheduler running"
	if !info.schedulerOn {
		state = "scheduler not started, see the log"
	}
	return fmt.Sprintf("%s, %d %s, %s", info.cfgPath, info.schedules, noun, state)
}

// natsLine says how the service reaches NATS, and what that means for events.
func natsLine(n natsState, topic string) string {
	switch n.mode {
	case "embedded":
		return fmt.Sprintf("embedded server at %s, events on %s", n.url, topic)
	case "external":
		return fmt.Sprintf("external server at %s, events on %s", n.url, topic)
	}
	return fmt.Sprintf("off (%s), live updates and runs are off", n.reason)
}
