package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FILES fleet browser (cmd-ctr import P8, FAST-agent pattern minimal port):
// browse allowlisted slices of the fleet's filesystems. The LOCAL host (the
// box manifest runs on) is served directly; remote devices run
// cmd/manifest-agent and are reached over the tailnet with short-lived HMAC
// tickets minted from a per-host DERIVED key — the master secret never
// leaves this box (<dataDir>/agent_master, auto-created 0600).

// FilesHost is one browsable machine.
type FilesHost struct {
	Name  string `json:"name"`
	Local bool   `json:"local"`
	url   string
}

type filesCfg struct {
	localName  string
	localRoots []string
	agents     []FilesHost
	masterPath string
	prefsPath  string // <dataDir>/files_prefs.json — per-box pinned homes
	prefsMu    sync.Mutex
}

// UseFiles wires the FILES surface. localRoots empty disables local browsing;
// agents may still be reachable.
func (s *Server) UseFiles(localName string, localRoots []string, agents map[string]string, masterPath string) {
	cfg := &filesCfg{
		localName: localName, localRoots: localRoots, masterPath: masterPath,
		prefsPath: filepath.Join(filepath.Dir(masterPath), "files_prefs.json"),
	}
	var names []string
	for n := range agents {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		cfg.agents = append(cfg.agents, FilesHost{Name: n, url: agents[n]})
	}
	s.files = cfg
}

// agentMaster loads (or mints once) the master secret.
func (f *filesCfg) agentMaster() ([]byte, error) {
	if b, err := os.ReadFile(f.masterPath); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return hex.DecodeString(strings.TrimSpace(string(b)))
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(f.masterPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(f.masterPath, []byte(hex.EncodeToString(raw)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return raw, nil
}

// DeriveAgentKey is the per-host key an installed agent holds
// (HMAC(master, "manifest-agent-key-v1:<host>")) — printed by the
// -derive-agent-key flag at install time; the master never ships.
func (f *filesCfg) DeriveAgentKey(host string) (string, error) {
	master, err := f.agentMaster()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte("manifest-agent-key-v1:" + host))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// DeriveAgentKeyFromMaster is the install-time helper behind the
// -derive-agent-key flag (no Server needed).
func DeriveAgentKeyFromMaster(masterPath, host string) (string, error) {
	f := &filesCfg{masterPath: masterPath}
	return f.DeriveAgentKey(host)
}

// mintTicket signs a 60s ticket with the host's derived key.
func (f *filesCfg) mintTicket(host string) (string, error) {
	keyHex, err := f.DeriveAgentKey(host)
	if err != nil {
		return "", err
	}
	key, _ := hex.DecodeString(keyHex)
	exp := strconv.FormatInt(time.Now().Add(60*time.Second).Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(exp))
	return exp + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func (f *filesCfg) agentURL(host string) string {
	for _, a := range f.agents {
		if a.Name == host {
			return a.url
		}
	}
	return ""
}

// resolveLocal contains a path to the roots allowlist. Dotfiles are allowed
// (owner decision — the hidden toggle governs visibility client-side); the
// roots + no-traversal rules remain the boundary.
func (f *filesCfg) resolveLocal(p string) (string, error) {
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) || strings.Contains(p, "..") {
		return "", fmt.Errorf("path must be absolute, no traversal")
	}
	for _, root := range f.localRoots {
		rootAbs, _ := filepath.Abs(root)
		if clean == rootAbs || strings.HasPrefix(clean, rootAbs+string(filepath.Separator)) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("path outside the browsable roots")
}

// --- per-box pinned homes ---------------------------------------------------

type filesPrefs struct {
	Homes map[string]string `json:"homes"`
}

func (f *filesCfg) loadPrefs() filesPrefs {
	out := filesPrefs{Homes: map[string]string{}}
	if b, err := os.ReadFile(f.prefsPath); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	if out.Homes == nil {
		out.Homes = map[string]string{}
	}
	return out
}

func (f *filesCfg) savePrefs(p filesPrefs) error {
	f.prefsMu.Lock()
	defer f.prefsMu.Unlock()
	b, _ := json.MarshalIndent(p, "", "  ")
	tmp := f.prefsPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.prefsPath)
}

// --- handlers ---------------------------------------------------------------

func (s *Server) handleFilesHosts(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		writeJSON(w, map[string]any{"hosts": []any{}})
		return
	}
	hosts := []FilesHost{}
	if len(s.files.localRoots) > 0 {
		hosts = append(hosts, FilesHost{Name: s.files.localName, Local: true})
	}
	hosts = append(hosts, s.files.agents...)
	writeJSON(w, map[string]any{"hosts": hosts})
}

type filesEntry struct {
	Name   string `json:"name"`
	Dir    bool   `json:"dir"`
	Size   int64  `json:"size"`
	MTime  int64  `json:"mtime"`
	Hidden bool   `json:"hidden,omitempty"`
	Link   bool   `json:"link,omitempty"`
}

// listDir is the shared local listing (server + agent keep the same shape).
func listDir(full string) ([]filesEntry, error) {
	des, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	out := []filesEntry{}
	for _, d := range des {
		e := filesEntry{
			Name: d.Name(), Dir: d.IsDir(),
			Hidden: strings.HasPrefix(d.Name(), "."),
			Link:   d.Type()&fs.ModeSymlink != 0,
		}
		if e.Link {
			// a symlink to a dir should still browse like a dir
			if fi, err := os.Stat(filepath.Join(full, d.Name())); err == nil {
				e.Dir = fi.IsDir()
			}
		}
		if fi, err := d.Info(); err == nil {
			e.Size, e.MTime = fi.Size(), fi.ModTime().Unix()
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.Error(w, "files disabled", http.StatusServiceUnavailable)
		return
	}
	host, p := r.URL.Query().Get("host"), r.URL.Query().Get("path")
	if host == s.files.localName {
		if p == "" {
			var out []filesEntry
			for _, root := range s.files.localRoots {
				out = append(out, filesEntry{Name: root, Dir: true})
			}
			writeJSON(w, map[string]any{"path": "", "entries": out})
			return
		}
		full, err := s.files.resolveLocal(p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		out, err := listDir(full)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"path": full, "entries": out})
		return
	}
	s.filesProxy(w, r, host, http.MethodGet, "/fs/list?path="+urlQueryEscape(p), nil)
}

func (s *Server) handleFilesRead(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.Error(w, "files disabled", http.StatusServiceUnavailable)
		return
	}
	host, p := r.URL.Query().Get("host"), r.URL.Query().Get("path")
	dl := r.URL.Query().Get("dl") == "1"
	if host == s.files.localName {
		full, err := s.files.resolveLocal(p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if fi, err := os.Stat(full); err != nil || fi.IsDir() || fi.Size() > 50<<20 {
			http.Error(w, "not a readable file (or >50MB)", http.StatusNotFound)
			return
		}
		if dl {
			w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(full)+"\"")
		}
		http.ServeFile(w, r, full)
		return
	}
	q := "/fs/read?path=" + urlQueryEscape(p)
	if dl {
		q += "&dl=1"
	}
	s.filesProxy(w, r, host, http.MethodGet, q, nil)
}

func (s *Server) handleFilesUpload(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.Error(w, "files disabled", http.StatusServiceUnavailable)
		return
	}
	host, p := r.URL.Query().Get("host"), r.URL.Query().Get("path")
	overwrite := r.URL.Query().Get("overwrite") == "1"
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if host == s.files.localName {
		full, err := s.files.resolveLocal(p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if _, err := os.Stat(full); err == nil && !overwrite {
			http.Error(w, "refusing to overwrite an existing file", http.StatusConflict)
			return
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := os.WriteFile(full, b, 0o644); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "bytes": len(b)})
		return
	}
	q := "/fs/write?path=" + urlQueryEscape(p)
	if overwrite {
		q += "&overwrite=1"
	}
	s.filesProxy(w, r, host, http.MethodPost, q, r.Body)
}

// handleFilesMkdir creates one directory level inside the roots.
func (s *Server) handleFilesMkdir(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.Error(w, "files disabled", http.StatusServiceUnavailable)
		return
	}
	host, p := r.URL.Query().Get("host"), r.URL.Query().Get("path")
	if host == s.files.localName {
		full, err := s.files.resolveLocal(p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if err := os.Mkdir(full, 0o755); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	s.filesProxy(w, r, host, http.MethodPost, "/fs/mkdir?path="+urlQueryEscape(p), nil)
}

// handleFilesRename renames/moves within the roots (from → to, both resolved).
func (s *Server) handleFilesRename(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.Error(w, "files disabled", http.StatusServiceUnavailable)
		return
	}
	host, from, to := r.URL.Query().Get("host"), r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if host == s.files.localName {
		fromFull, err := s.files.resolveLocal(from)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		toFull, err := s.files.resolveLocal(to)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if _, err := os.Stat(toFull); err == nil {
			http.Error(w, "target already exists", http.StatusConflict)
			return
		}
		if err := os.Rename(fromFull, toFull); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	s.filesProxy(w, r, host, http.MethodPost, "/fs/rename?from="+urlQueryEscape(from)+"&to="+urlQueryEscape(to), nil)
}

// handleFilesDelete removes a file or (recursive=1) a directory tree.
func (s *Server) handleFilesDelete(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.Error(w, "files disabled", http.StatusServiceUnavailable)
		return
	}
	host, p := r.URL.Query().Get("host"), r.URL.Query().Get("path")
	recursive := r.URL.Query().Get("recursive") == "1"
	if host == s.files.localName {
		full, err := s.files.resolveLocal(p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		// never delete a root itself
		for _, root := range s.files.localRoots {
			rootAbs, _ := filepath.Abs(root)
			if full == rootAbs {
				http.Error(w, "refusing to delete a browse root", http.StatusForbidden)
				return
			}
		}
		if recursive {
			err = os.RemoveAll(full)
		} else {
			err = os.Remove(full)
		}
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	q := "/fs/delete?path=" + urlQueryEscape(p)
	if recursive {
		q += "&recursive=1"
	}
	s.filesProxy(w, r, host, http.MethodPost, q, nil)
}

// handleFilesHome gets/sets the per-box pinned start folder.
func (s *Server) handleFilesHome(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.Error(w, "files disabled", http.StatusServiceUnavailable)
		return
	}
	host := r.URL.Query().Get("host")
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]any{"path": s.files.loadPrefs().Homes[host]})
		return
	}
	var b struct {
		Path string `json:"path"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	prefs := s.files.loadPrefs()
	if strings.TrimSpace(b.Path) == "" {
		delete(prefs.Homes, host)
	} else {
		prefs.Homes[host] = strings.TrimSpace(b.Path)
	}
	if err := s.files.savePrefs(prefs); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// filesProxy relays one request to a host agent with a fresh ticket. Range
// headers pass through both ways so remote video/pdf previews can seek.
func (s *Server) filesProxy(w http.ResponseWriter, r *http.Request, host, method, pathQ string, body io.Reader) {
	base := s.files.agentURL(host)
	if base == "" {
		http.Error(w, "unknown host "+host, http.StatusNotFound)
		return
	}
	ticket, err := s.files.mintTicket(host)
	if err != nil {
		httpError(w, err)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), method, strings.TrimRight(base, "/")+pathQ, body)
	if err != nil {
		httpError(w, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+ticket)
	if v := r.Header.Get("Range"); v != "" {
		req.Header.Set("Range", v)
	}
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, host+" unreachable — is the agent running?", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Disposition", "Accept-Ranges", "Content-Range"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func urlQueryEscape(s string) string { return url.QueryEscape(s) }
