package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Content blocks (iA Writer's): a path alone on its line embeds another vault
// file in the preview and in exports. These two routes list and serve the
// non-Markdown files a block may name, confined to the vault exactly as the
// writing file list is (no dot paths, no symlinks, not the record root).
var writingAssetKinds = map[string]string{
	".png": "image", ".jpg": "image", ".jpeg": "image", ".gif": "image", ".webp": "image", ".svg": "image",
	".csv": "csv", ".tsv": "csv", ".txt": "text",
	".go": "code", ".js": "code", ".ts": "code", ".py": "code", ".css": "code", ".json": "code", ".yaml": "code", ".yml": "code", ".sh": "code",
}

const writingAssetLimit = 5000

func (s *Server) writingAssetHidden(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return true
		}
	}
	return s.writing != nil && (rel == s.writing.Root || strings.HasPrefix(rel, s.writing.Root+"/"))
}

func (s *Server) handleWritingAssets(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault unavailable", 503)
		return
	}
	type asset struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	}
	root := s.vault.VaultRoot()
	out := []asset{}
	_ = filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
		if err != nil || full == root {
			return nil
		}
		rel, _ := filepath.Rel(root, full)
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 || s.writingAssetHidden(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if kind, ok := writingAssetKinds[strings.ToLower(filepath.Ext(rel))]; ok {
			out = append(out, asset{rel, kind})
			if len(out) >= writingAssetLimit {
				return fs.SkipAll
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path) })
	writeJSON(w, map[string]any{"assets": out})
}

func (s *Server) handleWritingAsset(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault unavailable", 503)
		return
	}
	rel := filepath.ToSlash(strings.TrimPrefix(r.URL.Query().Get("path"), "/"))
	if rel == "" || strings.ContainsAny(rel, "\\\x00") || filepath.IsAbs(rel) || s.writingAssetHidden(rel) {
		http.Error(w, "not a vault file", 400)
		return
	}
	if _, ok := writingAssetKinds[strings.ToLower(filepath.Ext(rel))]; !ok {
		http.Error(w, "this kind of file cannot be embedded", 415)
		return
	}
	root := s.vault.VaultRoot()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if !strings.HasPrefix(full, filepath.Clean(root)+string(filepath.Separator)) {
		http.Error(w, "not a vault file", 400)
		return
	}
	fi, err := os.Lstat(full)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 20<<20 {
		http.Error(w, "not found", 404)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	defer f.Close()
	// an embedded file is data: an SVG opened directly runs nothing
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	if strings.EqualFold(filepath.Ext(rel), ".svg") {
		w.Header().Set("Content-Type", "image/svg+xml")
	} else if writingAssetKinds[strings.ToLower(filepath.Ext(rel))] != "image" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	http.ServeContent(w, r, filepath.Base(full), fi.ModTime(), f)
}
