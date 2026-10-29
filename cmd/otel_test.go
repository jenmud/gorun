package main

import (
	"os"
	"testing"
)

func TestOTELSignalConfigured(t *testing.T) {
	tests := []struct {
		name   string
		signal string
		env    map[string]string
		want   bool
	}{
		{name: "nothing configured", signal: "TRACES", want: false},
		{
			name:   "shared OTLP endpoint configures every signal",
			signal: "METRICS",
			env:    map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318"},
			want:   true,
		},
		{
			name:   "signal endpoint only configures that signal",
			signal: "TRACES",
			env:    map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://collector:4318/v1/traces"},
			want:   true,
		},
		{
			name:   "another signal endpoint does not",
			signal: "METRICS",
			env:    map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://collector:4318/v1/traces"},
			want:   false,
		},
		{
			name:   "explicit signal exporter counts even when disabled",
			signal: "LOGS",
			env:    map[string]string{"OTEL_LOGS_EXPORTER": "none"},
			want:   true,
		},
		{
			name:   "shared protocol only counts",
			signal: "TRACES",
			env:    map[string]string{"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"},
			want:   true,
		},
		{
			name:   "unrelated environment does not count",
			signal: "TRACES",
			env:    map[string]string{"GORUN_NAME": "deploy"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := otelSignalConfigured(func(key string) string {
				return tt.env[key]
			}, tt.signal)

			if got != tt.want {
				t.Errorf("otelSignalConfigured(%s) = %v, want %v", tt.signal, got, tt.want)
			}
		})
	}
}

func TestConfigureOTELDefaults(t *testing.T) {
	keys := []string{
		"OTEL_SDK_DISABLED",
		"OTEL_TRACES_EXPORTER",
		"OTEL_METRICS_EXPORTER",
		"OTEL_LOGS_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
	}
	clear := func(t *testing.T) {
		t.Helper()
		for _, key := range keys {
			t.Setenv(key, "")
		}
	}

	t.Run("disables exporters by default", func(t *testing.T) {
		clear(t)
		configureOTELDefaults()

		for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
			if got := os.Getenv("OTEL_" + signal + "_EXPORTER"); got != "none" {
				t.Errorf("OTEL_%s_EXPORTER = %q, want none", signal, got)
			}
		}
	})

	t.Run("keeps configured signals", func(t *testing.T) {
		clear(t)
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://collector:4318/v1/traces")
		configureOTELDefaults()

		if got := os.Getenv("OTEL_TRACES_EXPORTER"); got != "" {
			t.Errorf("OTEL_TRACES_EXPORTER = %q, want it left unset", got)
		}
		for _, signal := range []string{"METRICS", "LOGS"} {
			if got := os.Getenv("OTEL_" + signal + "_EXPORTER"); got != "none" {
				t.Errorf("OTEL_%s_EXPORTER = %q, want none", signal, got)
			}
		}
	})

	t.Run("does nothing when the SDK is disabled", func(t *testing.T) {
		clear(t)
		t.Setenv("OTEL_SDK_DISABLED", "true")
		configureOTELDefaults()

		for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
			if got := os.Getenv("OTEL_" + signal + "_EXPORTER"); got != "" {
				t.Errorf("OTEL_%s_EXPORTER = %q, want it left unset", signal, got)
			}
		}
	})
}
