package websrv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"gorun/cmd/web/ui"
	"gorun/internal/envvars"
	inats "gorun/internal/nats"
	"gorun/internal/store"
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

func Run() {
	h := newHub()

	dbPath := envvars.Get("GORUN_DB_PATH")
	if dbPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir := filepath.Join(home, ".gorun")
			_ = os.MkdirAll(dir, 0755)
			dbPath = filepath.Join(dir, "gorun.db")
		} else {
			dbPath = "gorun.db"
		}
	} else if len(dbPath) > 0 && dbPath[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			dbPath = filepath.Join(home, dbPath[1:])
		}
	}
	st, err := store.Open(dbPath)
	if err != nil {
		log.Printf("gorun web: store disabled: %v", err)
		st = nil
	} else {
		defer func() { _ = st.Close() }()
		log.Printf("gorun web: store opened at %s", dbPath)
	}

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
			var ev inats.Event
			if err := json.Unmarshal(m.Data, &ev); err != nil {
				return
			}
			h.apply(ui.Event{
				Type:       ev.Type,
				GorunUUID:  ev.GorunUUID,
				Title:      ev.Title,
				Servers:    ev.Servers,
				StartedAt:  ev.StartedAt,
				Hostname:   ev.Hostname,
				PID:        ev.PID,
				Success:    ev.Success,
				Duration:   ev.Duration,
				TotalTasks: ev.TotalTasks,
				Failed:     ev.Failed,
				Skipped:    ev.Skipped,
				FinishedAt: ev.FinishedAt,
				Server:     ev.Server,
				Address:    ev.Address,
				Tasks:      ev.Tasks,
				Task:       ev.Task,
				State:      ev.State,
				Stdout:     ev.Stdout,
				Stderr:     ev.Stderr,
				Command:    ev.Command,
				ExitCode:   ev.ExitCode,
			})
			if st != nil {
				inats.UpdateDatabase(context.Background(), st, &ev)
			}
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
		h.broadcast(msg.String())
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

	mux.HandleFunc("/runs", func(w http.ResponseWriter, r *http.Request) {
		if st == nil {
			http.Error(w, "store disabled", http.StatusServiceUnavailable)
			return
		}
		runs, err := st.ListRuns(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := ui.RunsList(runs).Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if st == nil {
			http.Error(w, "store disabled", http.StatusServiceUnavailable)
			return
		}
		id := r.PathValue("id")
		run, err := st.GetRun(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := ui.RunDetail(*run).Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/runs/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if st == nil {
			http.Error(w, "store disabled", http.StatusServiceUnavailable)
			return
		}
		id := r.PathValue("id")
		if err := st.DeleteRun(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/runs", http.StatusSeeOther)
	})
	mux.HandleFunc("/runs/truncate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if st == nil {
			http.Error(w, "store disabled", http.StatusServiceUnavailable)
			return
		}
		if err := st.TruncateRuns(r.Context(), 10); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/runs", http.StatusSeeOther)
	})

	addr := ":8080"
	if v := envvars.Get("GORUN_ADDRESS"); v != "" {
		addr = v
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	fmt.Printf("gorun web listening on %s\n", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
