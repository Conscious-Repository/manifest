// Command manifest-agent is the tiny per-device host agent behind the FILES
// fleet browser (cmd-ctr FAST-agent pattern, minimal port): it serves a
// root-allowlisted slice of this machine's filesystem to the manifest server
// over the tailnet.
//
// Auth: manifest (on metis) holds a MASTER secret that never leaves it; this
// agent holds only its per-host DERIVED key (HMAC(master,
// "manifest-agent-key-v1:<host>")), installed once. Every request carries a
// short-lived ticket HMAC'd with the derived key — a lifted ticket dies in
// 60s, a lifted derived key is useless for any other host, and the master is
// unrecoverable from either.
//
// Config: ~/.config/manifest-agent/config.json
//
//	{"name":"macbook","addr":"100.x.y.z:48800","key":"<hex derived key>",
//	 "roots":["/Users/benjamin"]}
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type agentConfig struct {
	Name  string   `json:"name"`
	Addr  string   `json:"addr"`
	Key   string   `json:"key"` // hex derived key
	Roots []string `json:"roots"`
}

const maxReadBytes = 50 << 20

func main() {
	cfgPath := flag.String("config", defaultConfigPath(), "path to agent config")
	flag.Parse()
	b, err := os.ReadFile(*cfgPath)
	if err != nil {
		log.Fatalf("config %s: %v", *cfgPath, err)
	}
	var cfg agentConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		log.Fatalf("config parse: %v", err)
	}
	if cfg.Key == "" || cfg.Addr == "" || len(cfg.Roots) == 0 {
		log.Fatal("config needs name, addr, key, roots (fail closed)")
	}
	key, err := hex.DecodeString(cfg.Key)
	if err != nil || len(key) < 16 {
		log.Fatal("config key must be a hex string (fail closed)")
	}
	a := &agent{cfg: cfg, key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "name": cfg.Name, "roots": cfg.Roots})
	})
	mux.HandleFunc("GET /fs/list", a.auth(a.handleList))
	mux.HandleFunc("GET /fs/read", a.auth(a.handleRead))
	mux.HandleFunc("POST /fs/write", a.auth(a.handleWrite))
	log.Printf("manifest-agent %s serving %s (roots %v)", cfg.Name, cfg.Addr, cfg.Roots)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}

type agent struct {
	cfg agentConfig
	key []byte
}

// auth verifies the short-lived ticket: "<unix-expiry>.<hex hmac(key, expiry)>".
func (a *agent) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tk := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		i := strings.IndexByte(tk, '.')
		if i <= 0 {
			http.Error(w, "no ticket", http.StatusUnauthorized)
			return
		}
		expStr, sig := tk[:i], tk[i+1:]
		exp, err := strconv.ParseInt(expStr, 10, 64)
		if err != nil || time.Now().Unix() > exp {
			http.Error(w, "ticket expired", http.StatusUnauthorized)
			return
		}
		mac := hmac.New(sha256.New, a.key)
		mac.Write([]byte(expStr))
		want := hex.EncodeToString(mac.Sum(nil))
		if subtle.ConstantTimeCompare([]byte(want), []byte(sig)) != 1 {
			http.Error(w, "bad ticket", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// resolve maps a requested path into the roots allowlist (fail closed).
// Dot-segments are refused outright — dotdirs hold credentials (.ssh,
// .config); hiding them from listings is not enough, reads must refuse too.
func (a *agent) resolve(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) || strings.Contains(p, "..") {
		return "", fmt.Errorf("path must be absolute, no traversal")
	}
	for _, seg := range strings.Split(clean, string(filepath.Separator)) {
		if strings.HasPrefix(seg, ".") && seg != "." {
			return "", fmt.Errorf("dot-paths are off limits")
		}
	}
	for _, root := range a.cfg.Roots {
		rootAbs, _ := filepath.Abs(root)
		if clean == rootAbs || strings.HasPrefix(clean, rootAbs+string(filepath.Separator)) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("path outside the agent's roots")
}

type entry struct {
	Name  string `json:"name"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
}

func (a *agent) handleList(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		// no path → list the roots themselves
		var out []entry
		for _, root := range a.cfg.Roots {
			out = append(out, entry{Name: root, Dir: true})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"path": "", "entries": out})
		return
	}
	full, err := a.resolve(p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	des, err := os.ReadDir(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var out []entry
	for _, d := range des {
		if strings.HasPrefix(d.Name(), ".") {
			continue // dotfiles stay private
		}
		e := entry{Name: d.Name(), Dir: d.IsDir()}
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
	_ = json.NewEncoder(w).Encode(map[string]any{"path": full, "entries": out})
}

func (a *agent) handleRead(w http.ResponseWriter, r *http.Request) {
	full, err := a.resolve(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		http.Error(w, "not a readable file", http.StatusNotFound)
		return
	}
	if fi.Size() > maxReadBytes {
		http.Error(w, "file too large for the browser (50MB cap)", http.StatusRequestEntityTooLarge)
		return
	}
	http.ServeFile(w, r, full)
}

func (a *agent) handleWrite(w http.ResponseWriter, r *http.Request) {
	full, err := a.resolve(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if _, err := os.Stat(full); err == nil {
		http.Error(w, "refusing to overwrite an existing file", http.StatusConflict)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxReadBytes)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := os.WriteFile(full, b, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "bytes": len(b)})
}

func defaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "manifest-agent", "config.json")
}
