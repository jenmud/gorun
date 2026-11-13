package ui

import "testing"

func TestNATSModeLabelSaysServe(t *testing.T) {
	if got, want := natsModeLabel("embedded"), "embedded, started by gorun -serve"; got != want {
		t.Errorf("natsModeLabel(embedded) = %q, want %q", got, want)
	}
}
