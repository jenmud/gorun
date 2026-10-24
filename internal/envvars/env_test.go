package envvars_test

import (
	"gorun/internal/envvars"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGet__found(t *testing.T) {
	t.Setenv("GORUN_NAME", "testing-gorun")
	t.Setenv("GORUN_DEBUG", "1")

	actual := envvars.Get("GORUN_NAME")
	expected := "testing-gorun"

	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s", t.Name(), diff)
	}
}

func TestGet__not_found(t *testing.T) {
	t.Setenv("GORUN_NAME", "testing-gorun")
	t.Setenv("GORUN_DEBUG", "1")

	actual := envvars.Get("GORUN_NOT_FOUND")
	expected := ""

	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("%s: mismatch (-want +got):\n%s", t.Name(), diff)
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
