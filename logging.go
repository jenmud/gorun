package gorun

import (
	"gorun/internal/envvars"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// logOutput is where task and server logs are written, normally the log file.
var logOutput io.Writer = os.Stdout

// SetLogOutput overrides the destination for task and server logs, so they go to
// the log file instead of cluttering the terminal.
func SetLogOutput(w io.Writer) {
	logOutput = w
}

// LogOutput returns the current destination for task and server logs, so it can be
// put back after something interactive silences it.
func LogOutput() io.Writer {
	return logOutput
}

// LogFilePath returns the file gorun writes its logs to: GORUN_LOG_FILE when set
// (a leading ~ is expanded), otherwise ~/.gorun/gorun.log.
func LogFilePath() string {
	p := envvars.Get("GORUN_LOG_FILE")
	home, err := os.UserHomeDir()

	switch {
	case p != "" && strings.HasPrefix(p, "~") && err == nil:
		return filepath.Join(home, p[1:])
	case p != "":
		return p
	case err != nil:
		return "gorun.log"
	}

	return filepath.Join(home, ".gorun", "gorun.log")
}

// OpenLogFile opens path for appending, creating it and its directory if needed.
func OpenLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
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
