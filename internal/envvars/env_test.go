package envvars_test

import (
	"github.com/jenmud/gorun/internal/envvars"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGet(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		set      bool
		expected string
	}{
		{name: "found", key: "GORUN_NAME", value: "testing-gorun", set: true, expected: "testing-gorun"},
		{name: "not_found", key: "GORUN_NOT_FOUND", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(tt.key, tt.value)
			}

			actual := envvars.Get(tt.key)

			if diff := cmp.Diff(tt.expected, actual); diff != "" {
				t.Errorf("Get(%q) mismatch (-want +got):\n%s", tt.key, diff)
			}
		})
	}
}

func TestBool(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{name: "1", value: "1", expected: true},
		{name: "true", value: "true", expected: true},
		{name: "yes", value: "yes", expected: true},
		{name: "TRUE", value: "TRUE", expected: true},
		{name: "Yes", value: "Yes", expected: true},
		{name: "on", value: "on", expected: true},
		{name: "0", value: "0", expected: false},
		{name: "false", value: "false", expected: false},
		{name: "no", value: "no", expected: false},
		{name: "empty", value: "", expected: false},
		{name: "random", value: "maybe", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GORUN_DEBUG", tt.value)

			actual := envvars.Bool("GORUN_DEBUG")

			if diff := cmp.Diff(tt.expected, actual); diff != "" {
				t.Errorf("Bool(%q) mismatch (-want +got):\n%s", tt.value, diff)
			}
		})
	}
}
