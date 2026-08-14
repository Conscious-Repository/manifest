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
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
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
	mux.HandleFunc("POST /fs/mkdir", a.auth(a.handleMkdir))
	mux.HandleFunc("POST /fs/rename", a.auth(a.handleRename))
	mux.HandleFunc("POST /fs/delete", a.auth(a.handleDelete))
	mux.HandleFunc("GET /stats", a.auth(a.handleStats))
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
// Dotfiles are allowed (the browser's hidden toggle governs visibility);
// the roots + no-traversal rules remain the boundary.
func (a *agent) resolve(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) || strings.Contains(p, "..") {
		return "", fmt.Errorf("path must be absolute, no traversal")
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
	Name   string `json:"name"`
	Dir    bool   `json:"dir"`
	Size   int64  `json:"size"`
	MTime  int64  `json:"mtime"`
	Hidden bool   `json:"hidden,omitempty"`
	Link   bool   `json:"link,omitempty"`
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
	out := []entry{}
	for _, d := range des {
		e := entry{
			Name: d.Name(), Dir: d.IsDir(),
			Hidden: strings.HasPrefix(d.Name(), "."),
			Link:   d.Type()&os.ModeSymlink != 0,
		}
		if e.Link {
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
	if _, err := os.Stat(full); err == nil && r.URL.Query().Get("overwrite") != "1" {
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

func (a *agent) handleMkdir(w http.ResponseWriter, r *http.Request) {
	full, err := a.resolve(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if err := os.Mkdir(full, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (a *agent) handleRename(w http.ResponseWriter, r *http.Request) {
	from, err := a.resolve(r.URL.Query().Get("from"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	to, err := a.resolve(r.URL.Query().Get("to"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if _, err := os.Stat(to); err == nil {
		http.Error(w, "target already exists", http.StatusConflict)
		return
	}
	if err := os.Rename(from, to); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (a *agent) handleDelete(w http.ResponseWriter, r *http.Request) {
	full, err := a.resolve(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	for _, root := range a.cfg.Roots {
		rootAbs, _ := filepath.Abs(root)
		if full == rootAbs {
			http.Error(w, "refusing to delete a root", http.StatusForbidden)
			return
		}
	}
	if r.URL.Query().Get("recursive") == "1" {
		err = os.RemoveAll(full)
	} else {
		err = os.Remove(full)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// handleStats reports this box's vitals for the activity monitor (darwin).
// cpu/net are CUMULATIVE COUNTERS (cpu: summed %cpu snapshot; net: interface
// byte totals) — the metis collector keeps the previous sample and deltas.
func (a *agent) handleStats(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"os": runtime.GOOS, "cores": runtime.NumCPU()}
	sysctl := func(k string) string {
		b, _ := exec.Command("sysctl", "-n", k).Output()
		return strings.TrimSpace(string(b))
	}
	// uptime from kern.boottime: "{ sec = 1699999999, usec = 0 } ..."
	if bt := sysctl("kern.boottime"); bt != "" {
		if i := strings.Index(bt, "sec = "); i >= 0 {
			s := bt[i+6:]
			if j := strings.IndexAny(s, ",} "); j > 0 {
				if sec, err := strconv.ParseInt(s[:j], 10, 64); err == nil {
					out["uptime"] = time.Now().Unix() - sec
				}
			}
		}
	}
	// memory: total from hw.memsize, used from vm_stat pages
	if t, err := strconv.ParseInt(sysctl("hw.memsize"), 10, 64); err == nil {
		used := int64(0)
		if vb, err := exec.Command("vm_stat").Output(); err == nil {
			page := int64(16384)
			for _, ln := range strings.Split(string(vb), "\n") {
				if strings.Contains(ln, "page size of") {
					f := strings.Fields(ln)
					if n, err := strconv.ParseInt(f[len(f)-2], 10, 64); err == nil {
						page = n
					}
				}
				for _, k := range []string{"Pages active", "Pages wired down", "Pages occupied by compressor"} {
					if strings.HasPrefix(ln, k) {
						v := strings.TrimSuffix(strings.TrimSpace(strings.SplitN(ln, ":", 2)[1]), ".")
						if n, err := strconv.ParseInt(v, 10, 64); err == nil {
							used += n * page
						}
					}
				}
			}
		}
		out["mem"] = map[string]int64{"used": used, "total": t}
	}
	// swap: vm.swapusage "total = 2048.00M  used = 1024.00M  free = ..."
	if sw := sysctl("vm.swapusage"); sw != "" {
		parseM := func(tag string) int64 {
			i := strings.Index(sw, tag+" = ")
			if i < 0 {
				return 0
			}
			s := sw[i+len(tag)+3:]
			j := 0
			for j < len(s) && (s[j] == '.' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			v, _ := strconv.ParseFloat(s[:j], 64)
			mult := float64(1 << 20)
			if j < len(s) {
				switch s[j] {
				case 'K':
					mult = 1 << 10
				case 'G':
					mult = 1 << 30
				}
			}
			return int64(v * mult)
		}
		out["swap"] = map[string]int64{"used": parseM("used"), "total": parseM("total")}
	}
	// cpu: summed per-process %cpu (over cores*100); collector normalizes
	if pb, err := exec.Command("ps", "-A", "-o", "%cpu").Output(); err == nil {
		total := 0.0
		for _, ln := range strings.Split(string(pb), "\n")[1:] {
			if v, err := strconv.ParseFloat(strings.TrimSpace(ln), 64); err == nil {
				total += v
			}
		}
		out["cpu"] = total / float64(runtime.NumCPU()) // % of all cores
	}
	// net: cumulative bytes across physical interfaces (netstat -ibn)
	if nb, err := exec.Command("netstat", "-ibn").Output(); err == nil {
		var rx, tx int64
		seen := map[string]bool{}
		for _, ln := range strings.Split(string(nb), "\n")[1:] {
			f := strings.Fields(ln)
			if len(f) < 10 || seen[f[0]] || strings.HasPrefix(f[0], "lo") {
				continue
			}
			// only rows with a Link# address column carry interface totals
			if !strings.HasPrefix(f[2], "<Link#") {
				continue
			}
			seen[f[0]] = true
			if v, err := strconv.ParseInt(f[len(f)-5], 10, 64); err == nil {
				rx += v
			}
			if v, err := strconv.ParseInt(f[len(f)-2], 10, 64); err == nil {
				tx += v
			}
		}
		out["net"] = map[string]int64{"rx": rx, "tx": tx}
	}
	// battery: pmset -g batt → "85%; charging"
	if bb, err := exec.Command("pmset", "-g", "batt").Output(); err == nil {
		s := string(bb)
		if i := strings.Index(s, "%"); i > 0 {
			j := i - 1
			for j >= 0 && s[j] >= '0' && s[j] <= '9' {
				j--
			}
			if pct, err := strconv.Atoi(s[j+1 : i]); err == nil {
				out["battery"] = map[string]any{"pct": pct, "charging": strings.Contains(s, "AC Power")}
			}
		}
	}
	// disks: one row per root's filesystem
	disks := []map[string]any{}
	seen := map[string]bool{}
	for _, root := range a.cfg.Roots {
		var st syscall.Statfs_t
		if syscall.Statfs(root, &st) != nil {
			continue
		}
		total := int64(st.Blocks) * int64(st.Bsize)
		free := int64(st.Bavail) * int64(st.Bsize)
		key := fmt.Sprintf("%d/%d", total, free)
		if seen[key] {
			continue
		}
		seen[key] = true
		disks = append(disks, map[string]any{"mount": root, "used": total - free, "total": total})
	}
	out["disks"] = disks
	// top processes on demand (the one fork-heavy probe)
	if r.URL.Query().Get("top") == "1" {
		if tb, err := exec.Command("ps", "-Aceo", "pid,pcpu,pmem,rss,comm", "-r").Output(); err == nil {
			lines := strings.Split(string(tb), "\n")
			procs := []map[string]any{}
			for _, ln := range lines[1:] {
				f := strings.Fields(ln)
				if len(f) < 5 {
					continue
				}
				cpu, _ := strconv.ParseFloat(f[1], 64)
				memPct, _ := strconv.ParseFloat(f[2], 64)
				rss, _ := strconv.ParseInt(f[3], 10, 64)
				procs = append(procs, map[string]any{
					"pid": f[0], "cpu": cpu, "memPct": memPct, "rss": rss * 1024,
					"cmd": strings.Join(f[4:], " "),
				})
				if len(procs) >= 12 {
					break
				}
			}
			out["procs"] = procs
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}

func defaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "manifest-agent", "config.json")
}
