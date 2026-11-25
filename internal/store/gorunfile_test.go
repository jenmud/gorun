package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestGorunfilesKeepEachChangedVersionByName(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	if _, ok, err := st.Gorunfile(ctx, "web"); err != nil || ok {
		t.Fatalf("Gorunfile() on a new database = %v, %v, want none", ok, err)
	}
	for i, content := range []string{"title = \"a\"\n", "title = \"a\"\n", "title = \"b\"\n"} {
		saved, err := st.SaveGorunfile(ctx, "web", content, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if want := i != 1; saved != want {
			t.Errorf("save %d: saved = %v, want %v", i, saved, want)
		}
	}
	if _, err := st.SaveGorunfile(ctx, "base", "title = \"base\"\n", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := st.Gorunfile(ctx, "web"); err != nil || !ok || got != "title = \"b\"\n" {
		t.Errorf("Gorunfile(web) = %q, %v, %v", got, ok, err)
	}
	if n, _ := st.GorunfileVersions(ctx, "web"); n != 2 {
		t.Errorf("GorunfileVersions(web) = %d, want 2", n)
	}
	list, err := st.Gorunfiles(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "base" || list[1].Versions != 2 {
		t.Errorf("Gorunfiles() = %+v, %v; want base (newest), then web with 2 versions", list, err)
	}
	if err := st.DeleteGorunfile(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.Gorunfiles(ctx); len(list) != 1 {
		t.Errorf("after deleting web: %+v", list)
	}
}

// A row saved before Gorunfiles had names is the default one.
func TestUnnamedGorunfileIsTheDefault(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "gorun.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	if err := st.exec(ctx, `INSERT INTO gorunfiles (content, saved_at) VALUES ('title = "old"', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := st.Gorunfile(ctx, DefaultGorunfile); !ok || got != `title = "old"` {
		t.Errorf("Gorunfile(default) = %q, %v", got, ok)
	}
}
