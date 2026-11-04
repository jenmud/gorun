package websrv

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jenmud/gorun/cmd/web/ui"
)

// hub fans server-sent events out to every open page.
type hub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

// add registers a new page and returns the channel its messages arrive on.
func (h *hub) add() chan string {
	c := make(chan string, 32)
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

// remove unregisters a page and closes its channel.
func (h *hub) remove(c chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c)
	}
}

// broadcast sends msg to every open page. A page too slow to keep up misses the message,
// and the next update puts it right, because every update carries the whole panel.
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

// patch builds a Datastar event that replaces the inside of selector with html.
func patch(selector, html string) string {
	var b strings.Builder
	b.WriteString("event: datastar-patch-elements\n")
	b.WriteString("data: mode inner\n")
	b.WriteString("data: selector " + selector + "\n")
	for _, line := range strings.Split(html, "\n") {
		b.WriteString("data: elements " + line + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// events is the stream the run page listens to. It sends the current state first, then every
// change as it happens.
func (a *app) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")

	msgs := a.hub.add()
	defer a.hub.remove(msgs)

	a.mu.Lock()
	dash := render(ui.Dashboard(a.dash))
	a.mu.Unlock()
	stats := render(ui.StatsPanel(a.stats(r.Context())))
	if _, err := io.WriteString(w, patch("#dashboard", dash)+patch("#stats", stats)); err != nil {
		return
	}
	flusher.Flush()

	// A comment every so often keeps proxies from closing a quiet stream.
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-msgs:
			if _, err := io.WriteString(w, msg); err != nil {
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
