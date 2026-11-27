package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHub serves a small Hugging Face: one repository with a few files, and a gated one.
func fakeHub(t *testing.T, weights []byte) {
	t.Helper()
	sib := func(name string, size int, sha string) map[string]any {
		s := map[string]any{"rfilename": name, "size": size}
		if sha != "" {
			s["lfs"] = map[string]any{"sha256": sha}
		}
		return s
	}
	repo := map[string]any{
		"id": "acme/tiny-GGUF", "downloads": 1234, "likes": 5, "tags": []string{"gguf", "license:apache-2.0"}, "gated": false,
		"siblings": []any{
			sib("README.md", 100, ""),
			sib("tiny-Q8_0.gguf", 900, digest([]byte("bigger"))),
			sib("tiny-Q4_K_M.gguf", len(weights), digest(weights)),
			sib("mmproj-tiny-f16.gguf", 50, digest([]byte("proj"))),
			sib("big/tiny-F16-00001-of-00002.gguf", 5000, digest([]byte("part"))),
		},
	}
	gated := map[string]any{"id": "acme/secret-GGUF", "tags": []string{"license:other"}, "gated": "manual",
		"siblings": []any{sib("secret.gguf", 10, digest([]byte("secret")))}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models":
			if r.URL.Query().Get("filter") != "gguf" {
				t.Errorf("search without the gguf filter: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode([]any{repo, gated})
		case "/api/models/acme/tiny-GGUF":
			_ = json.NewEncoder(w).Encode(repo)
		case "/api/models/acme/secret-GGUF":
			_ = json.NewEncoder(w).Encode(gated)
		case "/acme/tiny-GGUF/resolve/main/tiny-Q4_K_M.gguf":
			_, _ = w.Write(weights)
		case "/acme/tiny-GGUF/resolve/main/tiny-Q8_0.gguf":
			_, _ = w.Write([]byte("not what the digest says"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldClient, oldHub := client, hubURL
	client, hubURL = srv.Client(), srv.URL
	t.Cleanup(func() { client, hubURL = oldClient, oldHub })
}

func TestSearchHubReadsLicenceAndGate(t *testing.T) {
	fakeHub(t, []byte("weights"))
	got, err := SearchHub(context.Background(), "tiny")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "acme/tiny-GGUF" || got[0].License != "apache-2.0" || got[0].Gated || got[0].Downloads != 1234 {
		t.Errorf("first result = %+v", got[0])
	}
	if !got[1].Gated || got[1].License != "other" {
		t.Errorf("gated result = %+v", got[1])
	}
}

func TestHubFilesKeepsSingleFileModels(t *testing.T) {
	fakeHub(t, []byte("weights"))
	_, files, err := HubFiles(context.Background(), "acme/tiny-GGUF")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	// smallest first; no README, projector or split parts
	if strings.Join(names, ",") != "tiny-Q4_K_M.gguf,tiny-Q8_0.gguf" {
		t.Errorf("files = %v", names)
	}
	for _, bad := range []string{"../etc", "acme", "acme/tiny/x", "acme/ti ny"} {
		if _, _, err := HubFiles(context.Background(), bad); err == nil {
			t.Errorf("HubFiles(%q) accepted a name that isn't a repository", bad)
		}
	}
}

func TestDownloadHubChecksTheDigest(t *testing.T) {
	weights := []byte("weights")
	fakeHub(t, weights)
	dir := t.TempDir()
	ctx := context.Background()

	p, err := DownloadHub(ctx, dir, "acme/tiny-GGUF", "tiny-Q4_K_M.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "weights" || p != filepath.Join(dir, "models", "tiny-Q4_K_M.gguf") {
		t.Errorf("downloaded %q to %s", got, p)
	}
	if _, err := DownloadHub(ctx, dir, "acme/tiny-GGUF", "tiny-Q4_K_M.gguf"); err == nil {
		t.Error("a second download overwrote the model")
	}
	if _, err := DownloadHub(ctx, dir, "acme/tiny-GGUF", "tiny-Q8_0.gguf"); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Errorf("a file that doesn't match its digest: err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "models", "tiny-Q8_0.gguf")); err == nil {
		t.Error("the file that didn't match its digest was kept")
	}
	if _, err := DownloadHub(ctx, dir, "acme/tiny-GGUF", "README.md"); err == nil {
		t.Error("a file that isn't a GGUF model was downloaded")
	}
	if _, err := DownloadHub(ctx, dir, "acme/secret-GGUF", "secret.gguf"); err == nil || !strings.Contains(err.Error(), "login") {
		t.Errorf("a gated model: err = %v", err)
	}

	models, err := Models(dir)
	if err != nil || len(models) != 1 || models[0].Name != "tiny-Q4_K_M.gguf" || models[0].Size != int64(len(weights)) {
		t.Errorf("Models = %+v, %v", models, err)
	}
}

func TestModelPathAndSavedModel(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "models"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "models", "a.gguf"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "secret.gguf"), []byte("x"), 0o644)

	for _, bad := range []string{"", "../secret.gguf", "models/a.gguf", ".hidden.gguf", "notes.txt", "missing.gguf"} {
		if _, err := ModelPath(dir, bad); err == nil {
			t.Errorf("ModelPath(%q) accepted it", bad)
		}
	}
	if SavedModel(dir) != "" {
		t.Error("a model was saved before any was picked")
	}
	if err := SaveModel(dir, "a.gguf"); err != nil {
		t.Fatal(err)
	}
	if got := SavedModel(dir); got != filepath.Join(dir, "models", "a.gguf") {
		t.Errorf("SavedModel = %q", got)
	}
	_ = os.Remove(filepath.Join(dir, "models", "a.gguf"))
	if SavedModel(dir) != "" {
		t.Error("a deleted model was still the saved one")
	}
	if err := SaveModel(dir, ""); err != nil || SavedModel(dir) != "" {
		t.Errorf("going back to the default: %v", err)
	}
}
