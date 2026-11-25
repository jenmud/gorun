package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestGorunfileKeepsEachChangedVersion(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	if _, ok, err := st.Gorunfile(ctx); err != nil || ok {
		t.Fatalf("Gorunfile() on a new database = %v, %v, want none", ok, err)
	}
	for i, content := range []string{"title = \"a\"\n", "title = \"a\"\n", "title = \"b\"\n"} {
		saved, err := st.SaveGorunfile(ctx, content, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if want := i != 1; saved != want {
			t.Errorf("save %d: saved = %v, want %v", i, saved, want)
		}
	}
	got, ok, err := st.Gorunfile(ctx)
	if err != nil || !ok || got != "title = \"b\"\n" {
		t.Errorf("Gorunfile() = %q, %v, %v", got, ok, err)
	}
	if n, _ := st.GorunfileVersions(ctx); n != 2 {
		t.Errorf("GorunfileVersions() = %d, want 2", n)
	}
}
