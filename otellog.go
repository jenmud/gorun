package gorun

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	otellog "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
)

// teeHandler sends every record to both handlers.
type teeHandler struct{ a, b slog.Handler }

func (t teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return t.a.Enabled(ctx, l) || t.b.Enabled(ctx, l)
}

func (t teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var err error
	if t.a.Enabled(ctx, r.Level) {
		err = t.a.Handle(ctx, r.Clone())
	}
	if t.b.Enabled(ctx, r.Level) {
		_ = t.b.Handle(ctx, r)
	}
	return err
}

func (t teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return teeHandler{t.a.WithAttrs(attrs), t.b.WithAttrs(attrs)}
}

func (t teeHandler) WithGroup(name string) slog.Handler {
	return teeHandler{t.a.WithGroup(name), t.b.WithGroup(name)}
}

// otelHandler is a slog handler that emits records to the OpenTelemetry Logs API, so the
// instrumented build sends gorun's logs to the collector. With no SDK set it does nothing.
type otelHandler struct {
	logger otellog.Logger
	level  slog.Level
	attrs  []otellog.KeyValue
	group  string // prefix for the attributes that follow, e.g. "server."
}

func newOTelHandler(p otellog.LoggerProvider, level slog.Level) *otelHandler {
	return &otelHandler{logger: p.Logger("gorun"), level: level}
}

func newGlobalOTelHandler(level slog.Level) *otelHandler {
	return newOTelHandler(logglobal.GetLoggerProvider(), level)
}

func (h *otelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level && h.logger.Enabled(ctx, otellog.EnabledParameters{Severity: severity(l)})
}

func (h *otelHandler) Handle(ctx context.Context, r slog.Record) error {
	var rec otellog.Record
	rec.SetTimestamp(r.Time)
	rec.SetSeverity(severity(r.Level))
	rec.SetSeverityText(r.Level.String())
	rec.SetBody(otellog.StringValue(r.Message))
	rec.AddAttributes(h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		rec.AddAttributes(keyValues(h.group, a)...)
		return true
	})
	h.logger.Emit(ctx, rec)
	return nil
}

func (h *otelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append([]otellog.KeyValue(nil), h.attrs...)
	for _, a := range attrs {
		c.attrs = append(c.attrs, keyValues(h.group, a)...)
	}
	return &c
}

func (h *otelHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.group = h.group + name + "."
	return &c
}

func severity(l slog.Level) otellog.Severity {
	switch {
	case l >= slog.LevelError:
		return otellog.SeverityError
	case l >= slog.LevelWarn:
		return otellog.SeverityWarn
	case l >= slog.LevelInfo:
		return otellog.SeverityInfo
	}
	return otellog.SeverityDebug
}

// keyValues converts an attribute to OpenTelemetry ones; a group becomes dotted keys.
func keyValues(prefix string, a slog.Attr) []otellog.KeyValue {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		p := prefix
		if a.Key != "" {
			p += a.Key + "."
		}
		var out []otellog.KeyValue
		for _, ga := range v.Group() {
			out = append(out, keyValues(p, ga)...)
		}
		return out
	}
	key := prefix + a.Key
	switch v.Kind() {
	case slog.KindString:
		return []otellog.KeyValue{otellog.String(key, v.String())}
	case slog.KindInt64:
		return []otellog.KeyValue{otellog.Int64(key, v.Int64())}
	case slog.KindUint64:
		return []otellog.KeyValue{otellog.String(key, fmt.Sprint(v.Uint64()))}
	case slog.KindFloat64:
		return []otellog.KeyValue{otellog.Float64(key, v.Float64())}
	case slog.KindBool:
		return []otellog.KeyValue{otellog.Bool(key, v.Bool())}
	}
	return []otellog.KeyValue{otellog.String(key, strings.TrimSpace(fmt.Sprint(v.Any())))}
}
