package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
}

// UseFiles wires the FILES surface. localRoots empty disables local browsing;
// agents may still be reachable.
func (s *Server) UseFiles(localName string, localRoots []string, agents map[string]string, masterPath string) {
	cfg := &filesCfg{localName: localName, localRoots: localRoots, masterPath: masterPath}
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

func (f *filesCfg) resolveLocal(p string) (string, error) {
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) || strings.Contains(p, "..") {
		return "", fmt.Errorf("path must be absolute, no traversal")
	}
	// dotdirs hold credentials — refuse reads, don't just hide listings
	for _, seg := range strings.Split(clean, string(filepath.Separator)) {
		if strings.HasPrefix(seg, ".") && seg != "." {
			return "", fmt.Errorf("dot-paths are off limits")
		}
	}
	for _, root := range f.localRoots {
		rootAbs, _ := filepath.Abs(root)
		if clean == rootAbs || strings.HasPrefix(clean, rootAbs+string(filepath.Separator)) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("path outside the browsable roots")
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
	Name  string `json:"name"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
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
		des, err := os.ReadDir(full)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		var out []filesEntry
		for _, d := range des {
			if strings.HasPrefix(d.Name(), ".") {
				continue
			}
			e := filesEntry{Name: d.Name(), Dir: d.IsDir()}
			if fi, err := d.Info(); err == nil {
				e.Size, e.MTime = fi.Size(), fi.ModTime().Unix()
			}
			out = append(out, e)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Dir != out[j].Dir {
				return out[i].Dir
			}
			return out[i].Name < out[j].Name
		})
		writeJSON(w, map[string]any{"path": full, "entries": out})
		return
	}
	s.filesProxy(w, r, host, "/fs/list?path="+urlQueryEscape(p), nil)
}

func (s *Server) handleFilesRead(w http.ResponseWriter, r *http.Request) {
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
		if fi, err := os.Stat(full); err != nil || fi.IsDir() || fi.Size() > 50<<20 {
			http.Error(w, "not a readable file (or >50MB)", http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, full)
		return
	}
	s.filesProxy(w, r, host, "/fs/read?path="+urlQueryEscape(p), nil)
}

func (s *Server) handleFilesUpload(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.Error(w, "files disabled", http.StatusServiceUnavailable)
		return
	}
	host, p := r.URL.Query().Get("host"), r.URL.Query().Get("path")
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if host == s.files.localName {
		full, err := s.files.resolveLocal(p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if _, err := os.Stat(full); err == nil {
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
	s.filesProxy(w, r, host, "/fs/write?path="+urlQueryEscape(p), r.Body)
}

// filesProxy relays one request to a host agent with a fresh ticket.
func (s *Server) filesProxy(w http.ResponseWriter, r *http.Request, host, pathQ string, body io.Reader) {
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
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(r.Context(), method, strings.TrimRight(base, "/")+pathQ, body)
	if err != nil {
		httpError(w, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+ticket)
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, host+" unreachable — is the agent running?", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Length"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func urlQueryEscape(s string) string { return url.QueryEscape(s) }
