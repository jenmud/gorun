package websrv

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jenmud/gorun"
)

// exportDir is the folder the zip unpacks into.
const exportDir = "gorunfile/"

// exportHandler downloads the saved Gorunfile as a zip; see exportZip.
func (a *app) exportHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	path := a.cfgPath
	a.mu.Unlock()
	var buf bytes.Buffer
	if err := exportZip(&buf, path, a.newConfig().Stored, time.Now()); err != nil {
		redirectWith(w, r, "/config", "Could not export: "+err.Error(), true)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="gorunfile-%s.zip"`, time.Now().Format("2006-01-02")))
	_, _ = w.Write(buf.Bytes())
}

// exportZip writes the Gorunfile at path, and the local files it includes, as a zip that loads
// as it is: a Gorunfile with the settings, servers, groups and schedules, and one file per task
// and bundle in tasks/ and bundles/, which it includes. path may be a stored Gorunfile's, and
// the tasks and bundles of stored:: includes are added: resolve reads their text. Git includes are
// kept, with their lock file. Comments aren't, and files the tasks copy or run aren't added.
func exportZip(w io.Writer, path string, resolve func(string) (string, error), at time.Time) error {
	ex := newExporter(nil, resolve)
	if err := ex.read(path); err != nil {
		return err
	}
	include := ex.git
	if len(ex.tasks) > 0 {
		include = append(include, "tasks/*.toml")
	}
	if len(ex.bundles) > 0 {
		include = append(include, "bundles/*.toml")
	}
	if len(include) > 0 {
		ex.top["include"] = include
	}

	z := zip.NewWriter(w)
	add := func(name string, data []byte) error {
		f, err := z.CreateHeader(&zip.FileHeader{Name: exportDir + name, Method: zip.Deflate, Modified: at})
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}
	head := fmt.Sprintf("# Exported by gorun on %s. Each task and bundle is a file of its own in tasks/ and\n# bundles/, included below. Files the tasks copy or run aren't in the export.\n\n", at.Format("2006-01-02"))
	top, err := encodeTOML(ex.top)
	if err != nil {
		return err
	}
	if err := add("Gorunfile", append([]byte(head), top...)); err != nil {
		return err
	}
	for _, part := range []struct {
		table string
		items map[string]any
	}{{"tasks", ex.tasks}, {"bundles", ex.bundles}} {
		for _, name := range slices.Sorted(maps.Keys(part.items)) {
			data, err := encodeTOML(map[string]any{part.table: map[string]any{name: part.items[name]}})
			if err != nil {
				return err
			}
			if err := add(part.table+"/"+fileName(name)+".toml", data); err != nil {
				return err
			}
		}
	}
	if _, stored := gorun.StoredName(path); len(ex.git) > 0 && !stored {
		if lock, err := os.ReadFile(filepath.Join(filepath.Dir(path), "Gorunfile.lock")); err == nil {
			if err := add("Gorunfile.lock", lock); err != nil {
				return err
			}
		}
	}
	return z.Close()
}

// flatten returns the Gorunfile at root in files, and the files it includes there, as one
// Gorunfile. Git and stored:: includes are kept.
func flatten(files fs.FS, root string) ([]byte, error) {
	ex := newExporter(files, nil)
	if err := ex.read(root); err != nil {
		return nil, err
	}
	if len(ex.git) > 0 {
		ex.top["include"] = ex.git
	}
	if len(ex.tasks) > 0 {
		ex.top["tasks"] = ex.tasks
	}
	if len(ex.bundles) > 0 {
		ex.top["bundles"] = ex.bundles
	}
	return encodeTOML(ex.top)
}

// exporter gathers a Gorunfile and its local includes into one top-level table, its tasks and
// its bundles.
type exporter struct {
	top, tasks, bundles map[string]any
	git                 []string // git and stored:: include patterns, kept as they are
	seen                map[string]bool
	// files holds the Gorunfiles by slash path, as an upload does; nil reads this machine's
	files fs.FS
	// resolve reads a stored Gorunfile's text, whose tasks and bundles are then added; nil keeps
	// its include
	resolve func(name string) (string, error)
}

func newExporter(files fs.FS, resolve func(string) (string, error)) *exporter {
	return &exporter{top: map[string]any{}, tasks: map[string]any{}, bundles: map[string]any{}, seen: map[string]bool{}, files: files, resolve: resolve}
}

// open reads the Gorunfile at p and returns its text and the folder its relative includes start
// from, "" for a stored one.
func (ex *exporter) open(p string) (key, dir string, data []byte, err error) {
	if name, ok := gorun.StoredName(p); ok {
		text, err := ex.resolve(name)
		return p, "", []byte(text), err
	}
	if ex.files != nil {
		data, err := fs.ReadFile(ex.files, p)
		return p, path.Dir(p), data, err
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", "", nil, err
	}
	data, err = os.ReadFile(real)
	return real, filepath.Dir(real), data, err
}

// glob lists the files a local include pattern names, relative to dir.
func (ex *exporter) glob(dir, pattern string) ([]string, error) {
	if ex.files != nil {
		if path.IsAbs(pattern) || filepath.IsAbs(pattern) {
			return nil, fmt.Errorf("include %q: an imported Gorunfile can include only files beside it", pattern)
		}
		return fs.Glob(ex.files, path.Join(dir, filepath.ToSlash(pattern)))
	}
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(dir, pattern)
	}
	return filepath.Glob(pattern)
}

// read merges the file at path, then the local files it includes, as gorun loads them.
func (ex *exporter) read(path string) error { return ex.readFile(path, false) }

// readFile is read; with shared, only the file's tasks and bundles are used, as for a stored::
// include.
func (ex *exporter) readFile(p string, shared bool) error {
	key, dir, data, err := ex.open(p)
	if err != nil {
		return err
	}
	if ex.seen[key] {
		return nil
	}
	ex.seen[key] = true
	doc := map[string]any{}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(key), err)
	}
	maps.Copy(ex.tasks, table(doc["tasks"]))
	maps.Copy(ex.bundles, table(doc["bundles"]))
	includes, _ := doc["include"].([]any)
	delete(doc, "tasks")
	delete(doc, "bundles")
	delete(doc, "include")
	if !shared {
		mergeTables(ex.top, doc)
	}

	for _, inc := range includes {
		pattern, _ := inc.(string)
		if _, ok := gorun.StoredName(pattern); ok && ex.resolve != nil {
			if err := ex.readFile(pattern, true); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(pattern, "git::") || strings.HasPrefix(pattern, "stored::") {
			if !slices.Contains(ex.git, pattern) {
				ex.git = append(ex.git, pattern)
			}
			continue
		}
		files, err := ex.glob(dir, pattern)
		if err != nil {
			return err
		}
		slices.Sort(files)
		for _, f := range files {
			if err := ex.readFile(f, shared); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergeTables adds src to dst: tables merge key by key and arrays of tables, like [[schedule]],
// are joined. Names are unique across a Gorunfile's files, so nothing is overwritten.
func mergeTables(dst, src map[string]any) {
	for k, v := range src {
		switch v := v.(type) {
		case map[string]any:
			d, ok := dst[k].(map[string]any)
			if !ok {
				d = map[string]any{}
				dst[k] = d
			}
			mergeTables(d, v)
		case []map[string]any:
			d, _ := dst[k].([]map[string]any)
			dst[k] = append(d, v...)
		default:
			if _, ok := dst[k]; !ok {
				dst[k] = v
			}
		}
	}
}

func table(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func encodeTOML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return dropEmptyHeaders(buf.Bytes()), nil
}

// dropEmptyHeaders removes the [parent] headers the encoder writes above [parent.child] with
// nothing of their own, such as [tasks] and [servers.hosts].
func dropEmptyHeaders(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	var out []string
	for i, line := range lines {
		if name, ok := strings.CutPrefix(line, "["); ok && !strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") {
			next := ""
			for _, l := range lines[i+1:] {
				if l != "" {
					next = strings.TrimLeft(l, "[")
					break
				}
			}
			if strings.HasPrefix(next, strings.TrimSuffix(name, "]")+".") {
				continue
			}
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// fileName is a task or bundle name made safe for a file name.
func fileName(name string) string {
	return unsafeName.ReplaceAllString(name, "_")
}
