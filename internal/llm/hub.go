package llm

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// hubURL is Hugging Face; tests point it at their own server.
var hubURL = "https://huggingface.co"

// HubModel is a model repository on Hugging Face that has GGUF files.
type HubModel struct {
	ID        string // owner/name
	License   string // such as apache-2.0, or "" when the repository doesn't say
	Downloads int
	Likes     int
	Gated     bool // its files need a Hugging Face login, which gorun doesn't have
}

// HubFile is a GGUF file in a repository, one quantisation of the model.
type HubFile struct {
	Name   string // its path in the repository
	Size   int64
	SHA256 string
}

// repoID is what a Hugging Face repository name looks like, owner/name.
var repoID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)

type hubRepo struct {
	ID        string   `json:"id"`
	Downloads int      `json:"downloads"`
	Likes     int      `json:"likes"`
	Tags      []string `json:"tags"`
	Gated     any      `json:"gated"` // false, or "auto" or "manual"
	Siblings  []struct {
		Name string `json:"rfilename"`
		Size int64  `json:"size"`
		LFS  *struct {
			SHA256 string `json:"sha256"`
		} `json:"lfs"`
	} `json:"siblings"`
}

func (r hubRepo) model() HubModel {
	m := HubModel{ID: r.ID, Downloads: r.Downloads, Likes: r.Likes}
	for _, t := range r.Tags {
		if l, ok := strings.CutPrefix(t, "license:"); ok {
			m.License = l
		}
	}
	switch g := r.Gated.(type) {
	case bool:
		m.Gated = g
	case string:
		m.Gated = g != ""
	}
	return m
}

// SearchHub finds model repositories with GGUF files whose names match query, the most
// downloaded first.
func SearchHub(ctx context.Context, query string) ([]HubModel, error) {
	q := url.Values{"search": {query}, "filter": {"gguf"}, "sort": {"downloads"}, "direction": {"-1"}, "limit": {"20"}}
	var repos []hubRepo
	if err := hubGet(ctx, "/api/models?"+q.Encode(), &repos); err != nil {
		return nil, err
	}
	out := make([]HubModel, len(repos))
	for i, r := range repos {
		out[i] = r.model()
	}
	return out, nil
}

// HubFiles lists the repository's GGUF model files, smallest first. Multimodal projectors and
// models split over several files are left out: gorun loads one file.
func HubFiles(ctx context.Context, repo string) (HubModel, []HubFile, error) {
	if !repoID.MatchString(repo) {
		return HubModel{}, nil, fmt.Errorf("%q isn't a Hugging Face repository, such as owner/name", repo)
	}
	var r hubRepo
	if err := hubGet(ctx, "/api/models/"+repo+"?blobs=true", &r); err != nil {
		return HubModel{}, nil, err
	}
	var files []HubFile
	for _, s := range r.Siblings {
		base := strings.ToLower(path.Base(s.Name))
		if !strings.HasSuffix(base, ".gguf") || strings.Contains(base, "mmproj") || splitPart.MatchString(base) || s.LFS == nil {
			continue
		}
		files = append(files, HubFile{Name: s.Name, Size: s.Size, SHA256: s.LFS.SHA256})
	}
	slices.SortFunc(files, func(a, b HubFile) int { return cmp.Compare(a.Size, b.Size) })
	return r.model(), files, nil
}

// splitPart matches one part of a model split over several files.
var splitPart = regexp.MustCompile(`-\d{5}-of-\d{5}\.gguf$`)

// DownloadHub downloads file from repo into dir's models folder, checked against the digest
// Hugging Face publishes for it, and returns its path. Progress goes to ctx's chat trace. A
// model with the same file name already there is an error, so nothing is overwritten.
func DownloadHub(ctx context.Context, dir, repo, file string) (string, error) {
	info, files, err := HubFiles(ctx, repo)
	if err != nil {
		return "", err
	}
	if info.Gated {
		return "", fmt.Errorf("%s needs a Hugging Face login to download, which gorun doesn't have", repo)
	}
	i := slices.IndexFunc(files, func(f HubFile) bool { return f.Name == file })
	if i < 0 {
		return "", fmt.Errorf("%s has no GGUF file %q gorun can use", repo, file)
	}
	dest := filepath.Join(dir, "models", path.Base(file))
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("a model called %s is downloaded already", path.Base(file))
	}
	src := hubURL + "/" + repo + "/resolve/main/" + escapePath(file)
	return dest, fetch(ctx, asset{URL: src, SHA256: files[i].SHA256}, dest, nil)
}

// escapePath escapes each part of a repository path for a URL, keeping its slashes.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

func hubGet(ctx context.Context, p string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hubURL+p, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ask Hugging Face: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errors.New("Hugging Face has no such repository")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("ask Hugging Face: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// LocalModel is a GGUF file in the models folder.
type LocalModel struct {
	Name string
	Size int64
}

// Models lists the GGUF files in dir's models folder, by name.
func Models(dir string) ([]LocalModel, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "models"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []LocalModel
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".gguf") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, LocalModel{Name: e.Name(), Size: info.Size()})
	}
	return out, nil
}

// ModelPath is the path of the model called name in dir's models folder, or an error when name
// isn't a plain file name there.
func ModelPath(dir, name string) (string, error) {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") || !strings.HasSuffix(strings.ToLower(name), ".gguf") {
		return "", fmt.Errorf("%q isn't a model in the models folder", name)
	}
	p := filepath.Join(dir, "models", name)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("model %s: %w", name, err)
	}
	return p, nil
}

// savedModelFile keeps the model picked on the Assistant page, so a restart uses it again.
const savedModelFile = "selected-model"

// SavedModel returns the model picked last in dir, as a path, or "" for the default one.
func SavedModel(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, savedModelFile))
	if err != nil {
		return ""
	}
	p, err := ModelPath(dir, strings.TrimSpace(string(data)))
	if err != nil {
		return ""
	}
	return p
}

// SaveModel records name as the model to use in dir; "" goes back to the default one.
func SaveModel(dir, name string) error {
	if name == "" {
		err := os.Remove(filepath.Join(dir, savedModelFile))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, savedModelFile), []byte(name+"\n"), 0o644)
}
