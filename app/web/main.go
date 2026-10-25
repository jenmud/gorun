package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
	"gorun/app/web/ui"
	"gorun/internal/envvars"
)

type hub struct {
	mu      sync.Mutex
	state   ui.DashboardState
	clients map[chan string]struct{}
}

func newHub() *hub {
	return &hub{clients: make(map[chan string]struct{})}
}

func (h *hub) add(c chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *hub) remove(c chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

func (h *hub) render() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var buf bytes.Buffer
	if err := ui.Dashboard(h.state).Render(context.Background(), &buf); err != nil {
		return ""
	}
	return buf.String()
}

func (h *hub) apply(ev ui.Event) {
	h.mu.Lock()
	h.state.Apply(ev)
	h.mu.Unlock()
}

func (h *hub) clearFinished() {
	h.mu.Lock()
	h.state.ClearFinished()
	h.mu.Unlock()
}

func (h *hub) broadcast(html string) {
	msg := "event: datastar-patch-elements\ndata: mode inner\ndata: selector #dashboard\n"
	for _, line := range strings.Split(html, "\n") {
		msg += "data: elements " + line + "\n"
	}
	msg += "\n"
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- msg:
		default:
		}
	}
}

func main() {
	h := newHub()

	url := envvars.Get("GORUN_NATS_CLIENT_URL")
	if url == "" {
		url = nats.DefaultURL
	}
	topic := envvars.Get("GORUN_EVENT_TOPIC")
	if topic == "" {
		topic = "gorun.events"
	}

	nc, err := nats.Connect(url)
	if err != nil {
		log.Printf("gorun web: NATS disabled: %v", err)
	} else {
		defer nc.Close()
		_, err := nc.Subscribe(topic, func(m *nats.Msg) {
			var ev ui.Event
			if err := json.Unmarshal(m.Data, &ev); err != nil {
				return
			}
			h.apply(ev)
			html := h.render()
			if html != "" {
				h.broadcast(html)
			}
		})
		if err != nil {
			log.Printf("gorun web: subscribe failed: %v", err)
		} else {
			log.Printf("gorun web: subscribed to %s on %s", topic, url)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := ui.Index().Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.clearFinished()
		html := h.render()
		// Build the SSE patch once.
		var msg strings.Builder
		msg.WriteString("event: datastar-patch-elements\n")
		msg.WriteString("data: mode inner\n")
		msg.WriteString("data: selector #dashboard\n")
		for _, line := range strings.Split(html, "\n") {
			msg.WriteString("data: elements ")
			msg.WriteString(line)
			msg.WriteString("\n")
		}
		msg.WriteString("\n")
		// Update all connected clients.
		h.broadcast(msg.String())
		// Also return it to the caller so Datastar processes the response.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, msg.String())
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		ch := make(chan string, 64)
		h.add(ch)
		defer h.remove(ch)
		// send current state on connect
		if html := h.render(); html != "" {
			msg := "event: datastar-patch-elements\ndata: mode inner\ndata: selector #dashboard\n"
			for _, line := range strings.Split(html, "\n") {
				msg += "data: elements " + line + "\n"
			}
			msg += "\n"
			_, _ = fmt.Fprint(w, msg)
			flusher.Flush()
		}
		for {
			select {
			case msg := <-ch:
				_, _ = fmt.Fprint(w, msg)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	addr := ":8080"
	if v := envvars.Get("GORUN_ADDRESS"); v != "" {
		addr = v
	}
	fmt.Printf("gorun web listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
