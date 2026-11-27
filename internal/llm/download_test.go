package llm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// llamaArchive is a small stand-in for a llama.cpp release.
func llamaArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(h *tar.Header, body string) {
		h.Size = int64(len(body))
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	add(&tar.Header{Name: "llama-b1/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	add(&tar.Header{Name: "llama-b1/libllama.so.1.0.0", Typeflag: tar.TypeReg, Mode: 0o755}, "lib")
	add(&tar.Header{Name: "llama-b1/libllama.so", Typeflag: tar.TypeSymlink, Linkname: "libllama.so.1.0.0"}, "")
	add(&tar.Header{Name: "llama-b1/libevil.so", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"}, "")
	add(&tar.Header{Name: "llama-b1/llama-cli", Typeflag: tar.TypeReg, Mode: 0o755}, "tool")
	add(&tar.Header{Name: "llama-b1/LICENSE", Typeflag: tar.TypeReg, Mode: 0o644}, "MIT")
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func serve(t *testing.T, body []byte) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(srv.Close)
	old := client
	client = srv.Client()
	t.Cleanup(func() { client = old })
	return srv.URL
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestFetchChecksTheDigest(t *testing.T) {
	body := []byte("model bytes")
	url := serve(t, body)
	file := filepath.Join(t.TempDir(), "m.gguf")

	if err := fetch(context.Background(), asset{url, digest([]byte("other"))}, file, nil); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("fetch() = %v, want a digest error", err)
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("a download with the wrong digest was kept")
	}
	if _, err := os.Stat(file + ".part"); err == nil {
		t.Fatal("the .part file was left behind")
	}
	if err := fetch(context.Background(), asset{url, digest(body)}, file, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); !bytes.Equal(got, body) {
		t.Errorf("downloaded %q, want %q", got, body)
	}
}

func TestExtractKeepsOnlyLibraries(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "llama.tar.gz")
	if err := os.WriteFile(archive, llamaArchive(t), 0o644); err != nil {
		t.Fatal(err)
	}
	libs := filepath.Join(dir, "libs")
	_ = os.Mkdir(libs, 0o755)
	if err := extract(archive, libs); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(libs)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"libllama.so", "libllama.so.1.0.0"}; !slices.Equal(names, want) {
		t.Errorf("extracted %v, want %v", names, want)
	}
	if got, err := os.ReadFile(filepath.Join(libs, "libllama.so")); err != nil || string(got) != "lib" {
		t.Errorf("libllama.so reads %q, %v; want the linked library", got, err)
	}
}

func TestModelSpec(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "mine.gguf")
	_ = os.WriteFile(local, []byte("x"), 0o644)

	if got, err := model(context.Background(), dir, local, nil); err != nil || got != local {
		t.Errorf("model(path) = %q, %v; want the path itself", got, err)
	}
	if _, err := model(context.Background(), dir, filepath.Join(dir, "missing.gguf"), nil); err == nil {
		t.Error("model() accepted a path that doesn't exist")
	}
	url := serve(t, []byte("weights")) + "/small.gguf"
	got, err := model(context.Background(), dir, url, nil)
	if err != nil || got != filepath.Join(dir, "models", "small.gguf") {
		t.Errorf("model(url) = %q, %v; want it in the models folder", got, err)
	}
}

func TestEveryReleaseIsPinned(t *testing.T) {
	for platform, a := range release {
		if len(a.SHA256) != 64 || !strings.Contains(a.URL, llamaTag) {
			t.Errorf("%s: %+v is not pinned to llama.cpp %s", platform, a, llamaTag)
		}
	}
}

func TestDirPrefersGORUN_LLM_DIR(t *testing.T) {
	t.Setenv("GORUN_CACHE_DIR", "/cache")
	t.Setenv("GORUN_LLM_DIR", "")
	if got := Dir(); got != filepath.Join("/cache", "llm") {
		t.Errorf("with GORUN_CACHE_DIR: Dir() = %q, want /cache/llm", got)
	}
	t.Setenv("GORUN_LLM_DIR", "/opt/gorun/llm")
	if got := Dir(); got != "/opt/gorun/llm" {
		t.Errorf("with GORUN_LLM_DIR: Dir() = %q, want /opt/gorun/llm", got)
	}
}
