package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"manifest/vaultwriter"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Writing library state that is not prose: folders, the library document and
// per-note authorship sidecars. Only folders touch the vault; the other two
// live under dataDir beside the editor's previous-version history, so they
// never appear in the owner's notes or sync.

// writingFolderRel checks a vault-relative folder spelling before any disk
// access: no absolute, dot, hidden or empty segments, no backslashes, nothing
// inside the writing record root.
func (s *Server) writingFolderRel(rel string) (string, error) {
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" || strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) || strings.ContainsAny(rel, "\\\x00") {
		return "", errors.New("Enter a folder path inside the vault, like drafts/essays.")
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return "", errors.New("Folder names cannot be empty or start with a dot.")
		}
	}
	if s.writing != nil && (rel == s.writing.Root || strings.HasPrefix(rel, s.writing.Root+"/")) {
		return "", errors.New("That folder is reserved for writing conversations.")
	}
	return rel, nil
}

func (s *Server) handleWritingFolder(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault unavailable", 503)
		return
	}
	var b struct {
		Path string `json:"path"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	rel, err := s.writingFolderRel(b.Path)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	switch err = s.vault.CreateFolder(rel); {
	case err == nil:
	case errors.Is(err, vaultwriter.ErrNotWritable):
		http.Error(w, "That folder is not writable.", 403)
		return
	case errors.Is(err, os.ErrExist):
		http.Error(w, "a file already exists at that path", 409)
		return
	default:
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, map[string]any{"path": rel})
}

// writingNoteFolderExists keeps note creation from inventing folders: a nested
// path must name an existing, writable folder (POST /api/writing/folder first).
func (s *Server) writingNoteFolderExists(rel string) error {
	i := strings.LastIndex(rel, "/")
	if i < 0 {
		return nil
	}
	dir, err := s.writingFolderRel(rel[:i])
	if err != nil {
		return err
	}
	full, err := vaultwriter.SafePath(s.vault.VaultRoot(), dir)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(full); err != nil || !fi.IsDir() {
		return errors.New("That folder does not exist. Create it first.")
	}
	return nil
}

// ---- library document ------------------------------------------------------

// The library document is owned by the front end; the server checks only that
// it is a JSON object of at most 256 KB. Expected fields:
//
//	favorites  []string                      vault paths pinned in the library
//	smart      []{id, name, query, sort}     saved searches (query = search syntax)
//	style      {rules []string, exceptions []string}  style-check settings
//	prefs      object                        library display preferences
//
// One file per vault (keyed like the file list's vaultID); revision is the
// SHA-256 of the stored bytes, "" when nothing is stored yet.
const writingLibraryMax = 256 << 10

func (s *Server) writingLibraryPath() string {
	if s.vault == nil || s.vault.DataDir() == "" {
		return ""
	}
	return filepath.Join(s.vault.DataDir(), "writing-library", vaultwriter.Revision([]byte(s.vault.VaultRoot()))+".json")
}

func readWritingLibrary(p string) (json.RawMessage, string, error) {
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return json.RawMessage("{}"), "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return b, vaultwriter.Revision(b), nil
}

func (s *Server) handleWritingLibrary(w http.ResponseWriter, r *http.Request) {
	p := s.writingLibraryPath()
	if p == "" {
		http.Error(w, "writing library unavailable", 503)
		return
	}
	if r.Method == http.MethodGet {
		lib, rev, err := readWritingLibrary(p)
		if err != nil {
			http.Error(w, "could not read the writing library", 500)
			return
		}
		writeJSON(w, map[string]any{"library": lib, "revision": rev})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, writingLibraryMax+16<<10)
	var b struct {
		Library    json.RawMessage `json:"library"`
		IfRevision string          `json:"ifRevision"`
	}
	if err := decode(r, &b); err != nil {
		http.Error(w, "the library must be a JSON object of at most 256 KB", 400)
		return
	}
	var compact bytes.Buffer
	if t := bytes.TrimSpace(b.Library); len(t) == 0 || t[0] != '{' || json.Compact(&compact, t) != nil || compact.Len() > writingLibraryMax {
		http.Error(w, "the library must be a JSON object of at most 256 KB", 400)
		return
	}
	s.writingSideMu.Lock()
	defer s.writingSideMu.Unlock()
	lib, rev, err := readWritingLibrary(p)
	if err != nil {
		http.Error(w, "could not read the writing library", 500)
		return
	}
	if b.IfRevision != rev {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"library": lib, "revision": rev})
		return
	}
	if err = os.MkdirAll(filepath.Dir(p), 0o700); err == nil {
		err = writeFileAtomic(p, compact.Bytes(), 0o600)
	}
	if err != nil {
		http.Error(w, "could not save the writing library", 500)
		return
	}
	writeJSON(w, map[string]any{"library": json.RawMessage(compact.Bytes()), "revision": vaultwriter.Revision(compact.Bytes())})
}

// ---- authorship sidecars ---------------------------------------------------

// A sidecar records who wrote which span of a note. The front end owns it:
//
//	{"revision": note revision it was computed against,
//	 "ranges":  [{from, to, author: "alfred"|"pasted"|<id>, quote, prefix, suffix}],
//	 "authors": [{id, name, kind: "human"|"ai", color}]}
//
// Paths mirror writing-history: <dataDir>/writing-authorship/<vault>/<note>.json
// with both parts hashed, so names never leak into the filesystem.
const (
	writingAuthorshipMax    = 1 << 20
	writingAuthorshipRanges = 5000
)

func (s *Server) writingAuthorshipFile(rel string) string {
	if s.vault == nil || s.vault.DataDir() == "" {
		return ""
	}
	return filepath.Join(s.vault.DataDir(), "writing-authorship", vaultwriter.Revision([]byte(filepath.Clean(s.vault.VaultRoot()))),
		vaultwriter.Revision([]byte(filepath.ToSlash(filepath.Clean(rel))))+".json")
}

func (s *Server) handleWritingAuthorship(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	p := s.writingAuthorshipFile(rel)
	if p == "" {
		http.Error(w, "authorship unavailable", 503)
		return
	}
	full, ok := safeVaultPath(s.vault.VaultRoot(), rel)
	if fi, err := os.Stat(full); !ok || err != nil || !fi.Mode().IsRegular() {
		http.Error(w, "invalid note path", 400)
		return
	}
	if r.Method == http.MethodGet {
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			writeJSON(w, map[string]any{"revision": "", "ranges": []any{}, "authors": []any{}})
			return
		}
		if err != nil {
			http.Error(w, "could not read authorship", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
		return
	}
	if !s.vault.CanUserWrite(filepath.ToSlash(rel)) {
		http.Error(w, "That note is read-only.", 403)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, writingAuthorshipMax))
	if err != nil {
		http.Error(w, "authorship must be at most 1 MB", 413)
		return
	}
	var doc struct {
		Revision string `json:"revision"`
		Ranges   []struct {
			From, To                      int
			Author, Quote, Prefix, Suffix string
		} `json:"ranges"`
		Authors []struct {
			ID, Name, Kind, Color string
		} `json:"authors"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		http.Error(w, "invalid authorship document", 400)
		return
	}
	if len(doc.Ranges) > writingAuthorshipRanges {
		http.Error(w, "too many authorship ranges", 400)
		return
	}
	for _, rg := range doc.Ranges {
		if rg.From < 0 || rg.To <= rg.From {
			http.Error(w, "authorship ranges need 0 <= from < to", 400)
			return
		}
	}
	for _, a := range doc.Authors {
		if a.Kind != "" && a.Kind != "human" && a.Kind != "ai" {
			http.Error(w, `author kind must be "human" or "ai"`, 400)
			return
		}
	}
	// Unknown fields survive; the three known ones are always present on read.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for k, empty := range map[string]string{"revision": `""`, "ranges": "[]", "authors": "[]"} {
		if v, ok := fields[k]; !ok || string(bytes.TrimSpace(v)) == "null" {
			fields[k] = json.RawMessage(empty)
		}
	}
	out, _ := json.Marshal(fields)
	s.writingSideMu.Lock()
	defer s.writingSideMu.Unlock()
	if err = os.MkdirAll(filepath.Dir(p), 0o700); err == nil {
		err = writeFileAtomic(p, out, 0o600)
	}
	if err != nil {
		http.Error(w, "could not save authorship", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// carryWritingAuthorship follows a note move. A sidecar already at the
// destination belongs to a note that no longer exists there (moves refuse
// collisions), so it is replaced, or removed when the moved note has none.
func (s *Server) carryWritingAuthorship(from, to string) {
	a, b := s.writingAuthorshipFile(from), s.writingAuthorshipFile(to)
	if a == "" || a == b {
		return
	}
	s.writingSideMu.Lock()
	defer s.writingSideMu.Unlock()
	if _, err := os.Stat(a); err == nil {
		_ = os.Rename(a, b)
	} else {
		_ = os.Remove(b)
	}
}
