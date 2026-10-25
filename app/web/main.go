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

func (h *hub) broadcast(msg string) {
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

	// Connect to NATS and subscribe to gorun events.
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
			var buf bytes.Buffer
			if err := ui.EventRow(ev).Render(context.Background(), &buf); err != nil {
				return
			}
			html := buf.String()
			// Datastar SSE patch-elements: one "data: elements" line per HTML line.
			var b strings.Builder
			b.WriteString("event: datastar-patch-elements\n")
			b.WriteString("data: mode append\n")
			b.WriteString("data: selector #event-log\n")
			for _, line := range strings.Split(html, "\n") {
				b.WriteString("data: elements ")
				b.WriteString(line)
				b.WriteString("\n")
			}
			b.WriteString("\n")
			h.broadcast(b.String())
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
