package gorun

import (
	"gorun/internal/envvars"
	"io"
	"log/slog"
	"os"
	"strings"
)

// logOutput is where task and server logs are written. The TUI discards them.
var logOutput io.Writer = os.Stdout

// SetLogOutput overrides the destination for task and server logs. The TUI uses
// this to discard them so they don't corrupt the screen.
func SetLogOutput(w io.Writer) {
	logOutput = w
}

// NewLogger returns a new logger.
func NewLogger(level string, w io.Writer, json bool) *slog.Logger {
	var l slog.Level

	switch strings.ToUpper(level) {
	case "INFO":
		l = slog.LevelInfo
	case "WARN", "WARNING":
		l = slog.LevelWarn
	case "ERROR":
		l = slog.LevelError
	case "DEBUG":
		l = slog.LevelDebug
	default:
		l = slog.LevelInfo
	}

	if json {
		handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l})
		return slog.New(handler)
	}

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: l})
	logger := slog.New(handler)
	return logger.With(
		"gorun",
		slog.String("name", envvars.Get("GORUN_NAME")),
		slog.Int("PID", os.Getpid()),
	)
}
