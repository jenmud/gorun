package nats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	natsclient "github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"gorun/app/web/ui"
	"gorun/internal/store"
)

// hub manages the dashboard state and client connections.
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

// render renders the dashboard state to an io.Writer.
func (h *hub) render(ctx context.Context, w io.Writer) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var buf bytes.Buffer
	if err := ui.Dashboard(h.state).Render(ctx, &buf); err != nil {
		slog.Error("gorun web: failed to render dashboard", "error", err)
		return err
	}
	_, err := io.WriteString(w, buf.String())
	return err
}

func (h *hub) apply(ev ui.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state.Apply(ev)
}

func (h *hub) clearFinished() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state.ClearFinished()
}

func (h *hub) broadcast(msg string) {
	startTime := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- msg:
		default:
		}
	}
	broadcastDuration.Record(context.Background(), float64(time.Since(startTime)), metric.WithAttributes(
		attribute.Int("active_clients", len(h.clients)),
	))
	slog.Debug("gorun nats: broadcast to clients", "count", len(h.clients), "message_size", len(msg))
}

// eventHandler wraps the hub and implements NATS event handling.
type eventHandler struct {
	ctx    context.Context
	cancel context.CancelFunc
	hub    *hub
	store  *store.Store
	wg     sync.WaitGroup
}

// NewEventHandler creates a new event handler with NATS subscription.
func NewEventHandler(ctx context.Context, nc *Client, topic string, h *hub, st *store.Store) (*eventHandler, error) {
	eh := &eventHandler{
		ctx:    ctx,
		cancel: func() {},
		hub:    h,
		store:  st,
	}

	// Subscribe synchronously so callers can publish immediately after
	// NewEventHandler returns.
	eh.wg.Add(1)
	eh.run(nc, topic)

	return eh, nil
}

func (h *eventHandler) run(nc *Client, topic string) {
	defer h.wg.Done()

	_, span := tracer.Start(context.Background(), "gorun/nats/eventhandler.run")
	_, err := nc.Subscribe(topic, func(m *natsclient.Msg) {
		h.handleMessage(m)
	})
	if err != nil {
		slog.Error("gorun nats: subscribe failed", "topic", topic, "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, fmt.Sprintf("error subscribing to NATS topic: %v", err))
		handlerErrors.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("topic", topic),
		))
		return
	}

	span.SetAttributes(attribute.String("topic", topic))
	slog.Info("gorun nats: subscribed to topic", "topic", topic, "url", nc.ConnectedUrl())
}

func (h *eventHandler) handleMessage(m *natsclient.Msg) {
	startTime := time.Now()
	_, span := tracer.Start(context.Background(), "gorun/nats/eventhandler.handleMessage")
	defer span.End()

	var ev Event
	if err := json.Unmarshal(m.Data, &ev); err != nil {
		slog.Error("gorun nats: failed to unmarshal event", "error", err)
		handlerErrors.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("topic", m.Subject),
		))
		return
	}

	hubApplyDuration.Record(context.Background(), float64(time.Since(startTime)), metric.WithAttributes(
		attribute.String("type", ev.Type),
	))

	h.Apply(ev)

	startTime = time.Now()
	h.updateDatabase(&ev)

	startTime = time.Now()
	h.renderAndBroadcast()

	eventProcessingDuration.Record(context.Background(), float64(time.Since(startTime)), metric.WithAttributes(
		attribute.String("type", ev.Type),
		attribute.String("gorun_uuid", ev.GorunUUID),
	))
}

func (h *eventHandler) handle(ev Event) {
	h.Apply(ev)
	h.updateDatabase(&ev)
	h.renderAndBroadcast()
}

func (h *eventHandler) renderAndBroadcast() {
	startTime := time.Now()
	var buf bytes.Buffer
	if err := h.Render(h.ctx, &buf); err != nil {
		slog.Error("gorun nats: failed to render dashboard", "error", err)
		return
	}

	// Build the message from the buffer content
	var msg strings.Builder
	msg.WriteString("event: datastar-patch-elements\ndata: mode inner\ndata: selector #dashboard\n")
	for _, line := range strings.Split(buf.String(), "\n") {
		msg.WriteString("data: elements " + line + "\n")
	}
	msg.WriteString("\n")

	h.Broadcast(msg.String())

	broadcastDuration.Record(context.Background(), float64(time.Since(startTime)), metric.WithAttributes(
		attribute.Int("message_size", len(msg.String())),
	))
}

// Apply applies an event to the hub state.
func (h *eventHandler) Apply(event Event) {
	startTime := time.Now()
	h.hub.apply(ui.Event{
		Type:       event.Type,
		GorunUUID:  event.GorunUUID,
		Title:      event.Title,
		Servers:    event.Servers,
		StartedAt:  event.StartedAt,
		Hostname:   event.Hostname,
		PID:        event.PID,
		Success:    event.Success,
		Duration:   event.Duration,
		TotalTasks: event.TotalTasks,
		Failed:     event.Failed,
		Skipped:    event.Skipped,
		FinishedAt: event.FinishedAt,
		Server:     event.Server,
		Address:    event.Address,
		Tasks:      event.Tasks,
		Task:       event.Task,
		State:      event.State,
		Stdout:     event.Stdout,
		Stderr:     event.Stderr,
		Command:    event.Command,
		ExitCode:   event.ExitCode,
	})
	hubApplyDuration.Record(context.Background(), float64(time.Since(startTime)), metric.WithAttributes(
		attribute.String("type", event.Type),
	))
}

// Handle processes an event by applying it to the hub, updating the database,
// and broadcasting the updated state.
func (h *eventHandler) Handle(ev Event) {
	startTime := time.Now()
	h.Apply(ev)

	h.updateDatabase(&ev)

	startTime = time.Now()
	h.renderAndBroadcast()

	eventProcessingDuration.Record(context.Background(), float64(time.Since(startTime)), metric.WithAttributes(
		attribute.String("type", ev.Type),
	))
}

// Render writes the current HTML representation to the provided writer.
func (h *eventHandler) Render(ctx context.Context, w io.Writer) error {
	return h.hub.render(ctx, w)
}

// Broadcast sends an SSE message to all connected clients.
func (h *eventHandler) Broadcast(msg string) {
	h.hub.broadcast(msg)
}

// Stop stops the NATS subscription and closes connections.
func (h *eventHandler) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	h.wg.Wait()
}

// updateDatabase handles database updates based on event type.
func (h *eventHandler) updateDatabase(ev *Event) {
	startTime := time.Now()
	if h.store == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	UpdateDatabase(ctx, h.store, ev)

	databaseUpdateDuration.Record(context.Background(), float64(time.Since(startTime)), metric.WithAttributes(
		attribute.String("type", ev.Type),
	))
}

// UpdateDatabase applies a single NATS event to the SQLite store. It is safe to
// call from any listener (the web app, tests, ...) and is idempotent enough for
// replayed events.
func UpdateDatabase(ctx context.Context, st *store.Store, ev *Event) {
	if st == nil || ev == nil {
		return
	}

	switch ev.Type {
	case "process_started":
		startedAt := time.Now()
		if ev.StartedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, ev.StartedAt); err == nil {
				startedAt = t
			}
		}
		if err := st.CreateRun(ctx, ev.GorunUUID, ev.Title, ev.Hostname, ev.PID, startedAt); err != nil {
			slog.Error("gorun nats: failed to create run", "gorun_uuid", ev.GorunUUID, "error", err)
		}
		for _, server := range ev.Servers {
			if err := st.AddServer(ctx, ev.GorunUUID, server, ""); err != nil {
				slog.Error("gorun nats: failed to add server", "server", server, "error", err)
			}
		}
	case "plan":
		if ev.Server != "" {
			if err := st.AddServer(ctx, ev.GorunUUID, ev.Server, ev.Address); err != nil {
				slog.Error("gorun nats: failed to add server", "server", ev.Server, "error", err)
			}
		}
	case "task":
		if ev.Server == "" || ev.Task == "" {
			return
		}
		duration := time.Duration(ev.Duration)
		if err := st.UpsertTask(ctx, ev.GorunUUID, ev.Server, ev.Task, ev.State, ev.Command, ev.Stdout, ev.Stderr, ev.ExitCode, duration); err != nil {
			slog.Error("gorun nats: failed to upsert task", "error", err)
		}
	case "process_finished":
		duration := time.Duration(ev.Duration)
		finishedAt := time.Now()
		if ev.FinishedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, ev.FinishedAt); err == nil {
				finishedAt = t
			}
		}
		if err := st.FinishRun(ctx, ev.GorunUUID, ev.Success, ev.TotalTasks, ev.Failed, ev.Skipped, duration, finishedAt); err != nil {
			slog.Error("gorun nats: failed to finish run", "gorun_uuid", ev.GorunUUID, "error", err)
		}
	}
}

// handleEvent processes an event with tracing.

// handleEvent processes an event with tracing.
func (h *eventHandler) handleEvent(ev Event) {
	_, span := tracer.Start(context.Background(), "gorun/nats/eventhandler.handle")
	defer span.End()

	h.Apply(ev)
	h.updateDatabase(&ev)
	h.renderAndBroadcast()
}

// Per-event helper methods (retained for compatibility with earlier callers).
func (h *eventHandler) updateProcessStarted(ctx context.Context, ev *Event) {
	if h.store == nil || ev == nil {
		return
	}
	startedAt := time.Now()
	if ev.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, ev.StartedAt); err == nil {
			startedAt = t
		}
	}
	if err := h.store.CreateRun(ctx, ev.GorunUUID, ev.Title, ev.Hostname, ev.PID, startedAt); err != nil {
		slog.Error("gorun nats: failed to create run", "gorun_uuid", ev.GorunUUID, "error", err)
	}
	for _, server := range ev.Servers {
		if err := h.store.AddServer(ctx, ev.GorunUUID, server, ""); err != nil {
			slog.Error("gorun nats: failed to add server", "server", server, "error", err)
		}
	}
}

func (h *eventHandler) updatePlan(ctx context.Context, ev *Event) {
	if h.store == nil || ev == nil {
		return
	}
	if ev.Server != "" {
		if err := h.store.AddServer(ctx, ev.GorunUUID, ev.Server, ev.Address); err != nil {
			slog.Error("gorun nats: failed to add server", "server", ev.Server, "error", err)
		}
	}
}

func (h *eventHandler) updateTask(ctx context.Context, ev *Event) {
	if h.store == nil || ev == nil {
		return
	}
	if ev.Server == "" || ev.Task == "" {
		return
	}
	duration := time.Duration(ev.Duration)
	if err := h.store.UpsertTask(ctx, ev.GorunUUID, ev.Server, ev.Task, ev.State, ev.Command, ev.Stdout, ev.Stderr, ev.ExitCode, duration); err != nil {
		slog.Error("gorun nats: failed to upsert task", "error", err)
	}
}

func (h *eventHandler) updateProcessFinished(ctx context.Context, ev *Event) {
	if h.store == nil || ev == nil {
		return
	}
	duration := time.Duration(ev.Duration)
	finishedAt := time.Now()
	if ev.FinishedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, ev.FinishedAt); err == nil {
			finishedAt = t
		}
	}
	if err := h.store.FinishRun(ctx, ev.GorunUUID, ev.Success, ev.TotalTasks, ev.Failed, ev.Skipped, duration, finishedAt); err != nil {
		slog.Error("gorun nats: failed to finish run", "gorun_uuid", ev.GorunUUID, "error", err)
	}
}
