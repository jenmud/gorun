// Package llm runs a small language model inside gorun, through llama.cpp loaded at runtime by
// yzma (no CGO). The llama.cpp libraries and the model are downloaded on first use, checked
// against pinned SHA-256 digests, and kept in gorun's cache folder.
package llm

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jenmud/gorun"
	"github.com/jenmud/gorun/internal/envvars"
)

// llamaTag is the llama.cpp build the yzma version in go.mod is made for: yzma's
// download.DefaultVersion names it. Bump both together.
const llamaTag = "b12359"

// client downloads; tests swap it for one that trusts their server.
var client = http.DefaultClient

// asset is a file to download and the SHA-256 its bytes must have.
type asset struct{ URL, SHA256 string }

// release is the llama.cpp build for each platform. The Linux and Windows builds are the Vulkan
// ones: they hold the CPU backends too, and llama.cpp uses those when no Vulkan driver loads.
// The digests come from the llama-cpp-builder manifest for yzma's pinned release.
var release = map[string]asset{
	"linux/amd64":   {ggml("ubuntu-vulkan-x64.tar.gz"), "19b60f00395a3f4954e46a5037695bc1d843404fc9d3124f82a612ecd8fbe9f0"},
	"linux/arm64":   {ggml("ubuntu-vulkan-arm64.tar.gz"), "3f8ae812a58ac92e5a596ca9fbc988f52854d147c5a7fe1264dddc90c1d085b4"},
	"darwin/arm64":  {ggml("macos-arm64.tar.gz"), "82b00abc740160e1b9980e6cc79c117345fa34af058c80e6c0a69aa8ddccde75"},
	"darwin/amd64":  {ggml("macos-x64.tar.gz"), "25320d529077ef5a3d16e7b5da26438f221d88c8c0930a32b0c79457f5eede56"},
	"windows/amd64": {ggml("win-vulkan-x64.zip"), "e8d18c6e4210cca87e7307c2ea78363ac8d158e4e1397358ec96ab70b53a0d5e"},
	"windows/arm64": {ggml("win-vulkan-arm64.zip"), "62cd50089cffdd825524a91c5a8819041eb75d8e2beacd6eca68194dba50574f"},
}

func ggml(suffix string) string {
	return fmt.Sprintf("https://github.com/ggml-org/llama.cpp/releases/download/%[1]s/llama-%[1]s-bin-%[2]s", llamaTag, suffix)
}

// DefaultModel is the model used when none is named: Qwen3 4B Instruct 2507 fine-tuned on gorun
// (training/), 4-bit, 2.5 GB, Apache 2.0. Each build has its own file name because cached models
// are keyed by name and never checked again.
var DefaultModel = asset{
	"https://huggingface.co/jenmud/gorun-qwen3-4b-GGUF/resolve/main/gorun-qwen3-4b-v3-q4_k_m.gguf",
	"53f237147bff4d35dbfbfb21f65694ef21d9c7376726b37eb0e1f2320d32072c",
}

// Dir is where llama.cpp and the models are kept: GORUN_LLM_DIR, else llm in gorun's cache
// folder (GORUN_CACHE_DIR, or the user's cache folder).
func Dir() string {
	if dir := envvars.Get("GORUN_LLM_DIR"); dir != "" {
		return dir
	}
	if dir := envvars.Get("GORUN_CACHE_DIR"); dir != "" {
		return filepath.Join(dir, "llm")
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "gorun", "llm")
}

// Pull downloads llama.cpp for this platform and the default model into dir, unless they are
// there already, so the first question needn't wait for them, and returns where each is.
// gorun -pull-model and the Docker image with the model built in use it.
func Pull(ctx context.Context, dir string, progress io.Writer) (libs, modelFile string, err error) {
	if libs, err = libraries(ctx, dir, progress); err != nil {
		return "", "", fmt.Errorf("llama.cpp: %w", err)
	}
	if modelFile, err = model(ctx, dir, "", progress); err != nil {
		return "", "", fmt.Errorf("model: %w", err)
	}
	return libs, modelFile, nil
}

// Downloaded reports whether llama.cpp and the default model are already in dir.
func Downloaded(dir string) bool {
	for _, f := range []string{filepath.Join("llama-"+llamaTag, ".complete"), filepath.Join("models", path.Base(DefaultModel.URL))} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return false
		}
	}
	return true
}

// libraries returns the folder holding llama.cpp's libraries for this platform, downloading
// them into dir first when they aren't there.
func libraries(ctx context.Context, dir string, progress io.Writer) (string, error) {
	a, ok := release[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("no llama.cpp build for %s/%s: set GORUN_LLM_URL to use a model server", runtime.GOOS, runtime.GOARCH)
	}
	libs := filepath.Join(dir, "llama-"+llamaTag)
	if _, err := os.Stat(filepath.Join(libs, ".complete")); err == nil {
		return libs, nil
	}
	archive := filepath.Join(dir, path.Base(a.URL))
	if err := fetch(ctx, a, archive, progress); err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(archive) }() // the libraries are what's kept

	gorun.ChatTraceFrom(ctx).Statusf("Unpacking llama.cpp")
	_ = os.RemoveAll(libs)
	if err := os.MkdirAll(libs, 0o755); err != nil {
		return "", err
	}
	if err := extract(archive, libs); err != nil {
		return "", fmt.Errorf("extract %s: %w", path.Base(a.URL), err)
	}
	return libs, os.WriteFile(filepath.Join(libs, ".complete"), nil, 0o644)
}

// model returns the path of the GGUF file named by spec: a local path, an https URL (not
// checked), or "" for DefaultModel. A URL is downloaded into dir once.
func model(ctx context.Context, dir, spec string, progress io.Writer) (string, error) {
	a := DefaultModel
	switch {
	case spec == "":
	case strings.HasPrefix(spec, "https://"):
		a = asset{URL: spec}
	default:
		if _, err := os.Stat(spec); err != nil {
			return "", fmt.Errorf("model: %w", err)
		}
		return spec, nil
	}
	file := filepath.Join(dir, "models", path.Base(a.URL))
	if _, err := os.Stat(file); err == nil {
		return file, nil
	}
	return file, fetch(ctx, a, file, progress)
}

// fetch downloads a to file, through a .part file so an interrupted download is never used,
// and fails if the bytes don't match its digest.
func fetch(ctx context.Context, a asset, file string, progress io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", a.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", a.URL, resp.Status)
	}

	part := file + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(part) }()
	hash := sha256.New()
	counter := &meter{w: progress, name: path.Base(a.URL), total: resp.ContentLength, trace: gorun.ChatTraceFrom(ctx)}
	_, err = io.Copy(io.MultiWriter(out, hash, counter), resp.Body)
	counter.done()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download %s: %w", a.URL, err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); a.SHA256 != "" && got != a.SHA256 {
		return fmt.Errorf("download %s: sha256 %s, want %s", a.URL, got, a.SHA256)
	}
	return os.Rename(part, file)
}

// meter writes a download's progress to w, and to the chat's trace, at most once a second.
type meter struct {
	w         io.Writer
	name      string
	total, n  int64
	last      time.Time
	wroteSome bool
	trace     *gorun.ChatTrace
}

func (m *meter) Write(p []byte) (int, error) {
	m.n += int64(len(p))
	if time.Since(m.last) > time.Second {
		m.last = time.Now()
		m.wroteSome = m.w != nil
		if m.total > 0 {
			m.trace.Statusf("Downloading %s, the first time only: %d%% of %d MB", m.name, m.n*100/m.total, m.total>>20)
			if m.w != nil {
				fmt.Fprintf(m.w, "\rdownloading %s  %3d%% of %d MB", m.name, m.n*100/m.total, m.total>>20)
			}
		} else {
			m.trace.Statusf("Downloading %s, the first time only: %d MB", m.name, m.n>>20)
			if m.w != nil {
				fmt.Fprintf(m.w, "\rdownloading %s  %d MB", m.name, m.n>>20)
			}
		}
	}
	return len(p), nil
}

func (m *meter) done() {
	if m.w != nil && m.wroteSome {
		fmt.Fprintf(m.w, "\rdownloaded %s, %d MB%s\n", m.name, m.n>>20, strings.Repeat(" ", 12))
	}
}

// extract writes the shared libraries in archive into dir, flattened. Everything else in a
// llama.cpp release (the command line tools) is left out.
func extract(archive, dir string) error {
	if strings.HasSuffix(archive, ".zip") {
		return extractZip(archive, dir)
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Base(h.Name)
		if !isLibrary(name) {
			continue
		}
		target := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeSymlink:
			// only links to a sibling, such as libllama.so -> libllama.so.1
			if link := h.Linkname; link == path.Base(link) {
				if err := os.Symlink(link, target); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			if err := writeFile(target, tr, 0o755); err != nil {
				return err
			}
		}
	}
}

func extractZip(archive, dir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		name := path.Base(f.Name)
		if f.FileInfo().IsDir() || !isLibrary(name) {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(filepath.Join(dir, name), r, 0o755)
		_ = r.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func isLibrary(name string) bool {
	return strings.HasSuffix(name, ".dll") || strings.HasSuffix(name, ".dylib") ||
		strings.HasSuffix(name, ".so") || strings.Contains(name, ".so.")
}

func writeFile(name string, r io.Reader, perm os.FileMode) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
