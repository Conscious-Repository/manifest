package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Activity (terminal cockpit Phase 4): the fleet vitals monitor. A collector
// on this box samples every device every 15s — itself via /proc (linux) or
// the same shell probes the Mac agent uses (darwin dev), agent devices via
// their ticketed GET /stats, plain ssh devices via a one-shot command — and
// keeps a 24h ring per device (snapshotted to <dataDir>/activity.json every
// 5m so restarts don't blank the graphs). cpu/net deltas are computed here
// against the previous raw sample; the UI only ever sees rates.

const (
	actInterval = 15 * time.Second
	actKeep     = 24 * time.Hour
	actSnapshot = 5 * time.Minute
)

// actPoint is one ring entry (compact — the history endpoint serves these).
type actPoint struct {
	T    int64   `json:"t"`
	CPU  float64 `json:"cpu"`  // % of all cores
	Mem  float64 `json:"mem"`  // % used
	Swap float64 `json:"swap"` // % used
	Net  float64 `json:"net"`  // bytes/s (rx+tx)
}

// actLive is the latest full sample per device.
type actLive struct {
	At      int64            `json:"at"`
	OS      string           `json:"os"`
	Cores   int              `json:"cores"`
	Uptime  int64            `json:"uptime"`
	CPU     float64          `json:"cpu"`
	Mem     map[string]int64 `json:"mem"`  // used/total
	Swap    map[string]int64 `json:"swap"` // used/total
	RxRate  float64          `json:"rx"`   // bytes/s
	TxRate  float64          `json:"tx"`
	Temp    float64          `json:"temp,omitempty"`
	Battery map[string]any   `json:"battery,omitempty"`
	Disks   []map[string]any `json:"disks,omitempty"`
}

type actPrev struct {
	at        time.Time
	cpuBusy   float64 // cumulative
	cpuTotal  float64
	netRx     int64 // cumulative bytes
	netTx     int64
	agentCPU  float64 // agents report instantaneous cpu — no delta needed
	haveDelta bool
}

type actCfg struct {
	devices  *devCfg
	files    *filesCfg
	snapPath string
	selfName string

	mu     sync.Mutex
	series map[string][]actPoint
	live   map[string]actLive
	prev   map[string]*actPrev
}

// UseActivity wires + starts the collector (call after UseDevices/UseFiles).
func (s *Server) UseActivity(snapPath string) {
	a := &actCfg{
		devices: s.devices, files: s.files, snapPath: snapPath,
		selfName: "self",
		series:   map[string][]actPoint{}, live: map[string]actLive{}, prev: map[string]*actPrev{},
	}
	if s.devices != nil {
		a.selfName = s.devices.selfName
	}
	a.loadSnapshot()
	s.activity = a
	go a.run()
}

func (a *actCfg) loadSnapshot() {
	if b, err := os.ReadFile(a.snapPath); err == nil {
		_ = json.Unmarshal(b, &a.series)
	}
	if a.series == nil {
		a.series = map[string][]actPoint{}
	}
}

func (a *actCfg) saveSnapshot() {
	a.mu.Lock()
	b, _ := json.Marshal(a.series)
	a.mu.Unlock()
	tmp := a.snapPath + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, a.snapPath)
	}
}

func (a *actCfg) run() {
	tick := time.NewTicker(actInterval)
	snap := time.NewTicker(actSnapshot)
	a.sampleAll()
	for {
		select {
		case <-tick.C:
			a.sampleAll()
		case <-snap.C:
			a.saveSnapshot()
		}
	}
}

func (a *actCfg) sampleAll() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); a.record(a.selfName, a.collectSelf()) }()
	if a.devices != nil {
		for _, d := range a.devices.devices {
			wg.Add(1)
			go func(d TermDevice) {
				defer wg.Done()
				if d.Agent != "" && a.files != nil && a.files.agentURL(d.Agent) != "" {
					a.record(d.Name, a.collectAgent(d))
				} else {
					a.record(d.Name, a.collectSSH(d))
				}
			}(d)
		}
	}
	wg.Wait()
}

// record folds a sample into live + the ring (nil sample = unreachable tick).
func (a *actCfg) record(name string, s *actLive) {
	if s == nil {
		return
	}
	s.At = time.Now().Unix()
	pt := actPoint{T: s.At, CPU: s.CPU, Net: s.RxRate + s.TxRate}
	if s.Mem != nil && s.Mem["total"] > 0 {
		pt.Mem = float64(s.Mem["used"]) / float64(s.Mem["total"]) * 100
	}
	if s.Swap != nil && s.Swap["total"] > 0 {
		pt.Swap = float64(s.Swap["used"]) / float64(s.Swap["total"]) * 100
	}
	a.mu.Lock()
	a.live[name] = *s
	ring := append(a.series[name], pt)
	cut := time.Now().Add(-actKeep).Unix()
	for len(ring) > 0 && ring[0].T < cut {
		ring = ring[1:]
	}
	a.series[name] = ring
	a.mu.Unlock()
}

// --- self (linux /proc; darwin dev falls back to shell probes) --------------

func (a *actCfg) collectSelf() *actLive {
	if runtime.GOOS == "linux" {
		return a.collectSelfLinux()
	}
	return a.collectSelfDarwin()
}

func readProc(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func (a *actCfg) collectSelfLinux() *actLive {
	out := &actLive{OS: "linux", Cores: runtime.NumCPU(), Mem: map[string]int64{}, Swap: map[string]int64{}}
	// cpu: /proc/stat first line, delta vs prev
	var busy, total float64
	if f := strings.Fields(strings.SplitN(readProc("/proc/stat"), "\n", 2)[0]); len(f) >= 8 && f[0] == "cpu" {
		var vals []float64
		for _, s := range f[1:] {
			v, _ := strconv.ParseFloat(s, 64)
			vals = append(vals, v)
			total += v
		}
		if len(vals) >= 5 {
			busy = total - vals[3] - vals[4] // minus idle+iowait
		}
	}
	// mem/swap: /proc/meminfo
	memTotal, memAvail, swapTotal, swapFree := int64(0), int64(0), int64(0), int64(0)
	for _, ln := range strings.Split(readProc("/proc/meminfo"), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			memTotal = v * 1024
		case "MemAvailable:":
			memAvail = v * 1024
		case "SwapTotal:":
			swapTotal = v * 1024
		case "SwapFree:":
			swapFree = v * 1024
		}
	}
	out.Mem["total"], out.Mem["used"] = memTotal, memTotal-memAvail
	out.Swap["total"], out.Swap["used"] = swapTotal, swapTotal-swapFree
	// net: sum non-lo interfaces
	var rx, tx int64
	for _, ln := range strings.Split(readProc("/proc/net/dev"), "\n")[2:] {
		parts := strings.SplitN(ln, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "lo" {
			continue
		}
		f := strings.Fields(parts[1])
		if len(f) >= 9 {
			r, _ := strconv.ParseInt(f[0], 10, 64)
			t, _ := strconv.ParseInt(f[8], 10, 64)
			rx += r
			tx += t
		}
	}
	// uptime
	if f := strings.Fields(readProc("/proc/uptime")); len(f) > 0 {
		up, _ := strconv.ParseFloat(f[0], 64)
		out.Uptime = int64(up)
	}
	// temp: first thermal zone
	if zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*/temp"); len(zones) > 0 {
		if v, err := strconv.ParseFloat(strings.TrimSpace(readProc(zones[0])), 64); err == nil && v > 1000 {
			out.Temp = v / 1000
		}
	}
	out.Disks = statfsDisks(a.rootsPlusSlash())
	a.applyDeltas(a.selfName, out, busy, total, rx, tx)
	return out
}

// collectSelfDarwin covers the dev box — same probes as the agent's /stats.
func (a *actCfg) collectSelfDarwin() *actLive {
	out := &actLive{OS: "darwin", Cores: runtime.NumCPU(), Mem: map[string]int64{}, Swap: map[string]int64{}}
	if b, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		if t, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
			out.Mem["total"] = t
		}
	}
	if pb, err := exec.Command("ps", "-A", "-o", "%cpu").Output(); err == nil {
		total := 0.0
		for _, ln := range strings.Split(string(pb), "\n")[1:] {
			if v, err := strconv.ParseFloat(strings.TrimSpace(ln), 64); err == nil {
				total += v
			}
		}
		out.CPU = total / float64(runtime.NumCPU())
	}
	out.Disks = statfsDisks(a.rootsPlusSlash())
	return out
}

func (a *actCfg) rootsPlusSlash() []string {
	roots := []string{"/"}
	if a.files != nil {
		roots = append(roots, a.files.localRoots...)
	}
	return roots
}

func statfsDisks(paths []string) []map[string]any {
	disks := []map[string]any{}
	seen := map[string]bool{}
	for _, p := range paths {
		var st syscall.Statfs_t
		if syscall.Statfs(p, &st) != nil {
			continue
		}
		total := int64(st.Blocks) * int64(st.Bsize)
		free := int64(st.Bavail) * int64(st.Bsize)
		key := strconv.FormatInt(total, 10) + "/" + strconv.FormatInt(free, 10)
		if seen[key] || total == 0 {
			continue
		}
		seen[key] = true
		disks = append(disks, map[string]any{"mount": p, "used": total - free, "total": total})
	}
	return disks
}

// applyDeltas turns cumulative cpu/net counters into rates using prev.
func (a *actCfg) applyDeltas(name string, out *actLive, busy, total float64, rx, tx int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.prev[name]
	now := time.Now()
	if ok && total > p.cpuTotal {
		out.CPU = (busy - p.cpuBusy) / (total - p.cpuTotal) * 100
	}
	if ok {
		dt := now.Sub(p.at).Seconds()
		if dt > 0 && rx >= p.netRx {
			out.RxRate = float64(rx-p.netRx) / dt
			out.TxRate = float64(tx-p.netTx) / dt
		}
	}
	a.prev[name] = &actPrev{at: now, cpuBusy: busy, cpuTotal: total, netRx: rx, netTx: tx}
}

// --- agent devices (GET /stats with a minted ticket) ------------------------

func (a *actCfg) collectAgent(d TermDevice) *actLive {
	body, err := a.agentStats(d.Agent, "")
	if err != nil {
		return nil
	}
	var raw struct {
		OS      string           `json:"os"`
		Cores   int              `json:"cores"`
		Uptime  int64            `json:"uptime"`
		CPU     float64          `json:"cpu"`
		Mem     map[string]int64 `json:"mem"`
		Swap    map[string]int64 `json:"swap"`
		Net     map[string]int64 `json:"net"`
		Battery map[string]any   `json:"battery"`
		Disks   []map[string]any `json:"disks"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return nil
	}
	out := &actLive{
		OS: raw.OS, Cores: raw.Cores, Uptime: raw.Uptime, CPU: raw.CPU,
		Mem: raw.Mem, Swap: raw.Swap, Battery: raw.Battery, Disks: raw.Disks,
	}
	// agent cpu is instantaneous; net is cumulative → delta here
	rx, tx := int64(0), int64(0)
	if raw.Net != nil {
		rx, tx = raw.Net["rx"], raw.Net["tx"]
	}
	a.mu.Lock()
	if p, ok := a.prev[d.Name]; ok {
		dt := time.Since(p.at).Seconds()
		if dt > 0 && rx >= p.netRx {
			out.RxRate = float64(rx-p.netRx) / dt
			out.TxRate = float64(tx-p.netTx) / dt
		}
	}
	a.prev[d.Name] = &actPrev{at: time.Now(), netRx: rx, netTx: tx}
	a.mu.Unlock()
	return out
}

func (a *actCfg) agentStats(agentName, extra string) ([]byte, error) {
	base := a.files.agentURL(agentName)
	ticket, err := a.files.mintTicket(agentName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/stats"+extra, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+ticket)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("agent /stats: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// --- plain ssh devices (one-shot linux probe) -------------------------------

func (a *actCfg) collectSSH(d TermDevice) *actLive {
	if a.devices == nil {
		return nil
	}
	// only probe boxes that already look reachable — never let the collector
	// stack ssh timeouts against a dark host
	if a.devices.probe(d.Name) != "ok" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	script := "cat /proc/stat 2>/dev/null | head -1; echo ==; cat /proc/meminfo 2>/dev/null; echo ==; cat /proc/uptime 2>/dev/null; echo ==; cat /proc/net/dev 2>/dev/null; echo ==; nproc 2>/dev/null; echo ==; df -kP / 2>/dev/null | tail -1"
	args := a.devices.sshArgs(d)
	args = append(args[1:], script) // drop -tt for one-shots
	b, err := exec.CommandContext(ctx, "ssh", args...).Output()
	if err != nil {
		return nil
	}
	parts := strings.Split(string(b), "==\n")
	if len(parts) < 6 {
		return nil
	}
	out := &actLive{OS: "linux", Mem: map[string]int64{}, Swap: map[string]int64{}}
	var busy, total float64
	if f := strings.Fields(strings.TrimSpace(parts[0])); len(f) >= 8 && f[0] == "cpu" {
		var vals []float64
		for _, s := range f[1:] {
			v, _ := strconv.ParseFloat(s, 64)
			vals = append(vals, v)
			total += v
		}
		if len(vals) >= 5 {
			busy = total - vals[3] - vals[4]
		}
	}
	memTotal, memAvail, swapTotal, swapFree := int64(0), int64(0), int64(0), int64(0)
	for _, ln := range strings.Split(parts[1], "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			memTotal = v * 1024
		case "MemAvailable:":
			memAvail = v * 1024
		case "SwapTotal:":
			swapTotal = v * 1024
		case "SwapFree:":
			swapFree = v * 1024
		}
	}
	out.Mem["total"], out.Mem["used"] = memTotal, memTotal-memAvail
	out.Swap["total"], out.Swap["used"] = swapTotal, swapTotal-swapFree
	if f := strings.Fields(strings.TrimSpace(parts[2])); len(f) > 0 {
		up, _ := strconv.ParseFloat(f[0], 64)
		out.Uptime = int64(up)
	}
	var rx, tx int64
	for _, ln := range strings.Split(parts[3], "\n") {
		seg := strings.SplitN(ln, ":", 2)
		if len(seg) != 2 || strings.TrimSpace(seg[0]) == "lo" {
			continue
		}
		f := strings.Fields(seg[1])
		if len(f) >= 9 {
			r, _ := strconv.ParseInt(f[0], 10, 64)
			t, _ := strconv.ParseInt(f[8], 10, 64)
			rx += r
			tx += t
		}
	}
	out.Cores, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
	if f := strings.Fields(strings.TrimSpace(parts[5])); len(f) >= 4 {
		totalK, _ := strconv.ParseInt(f[1], 10, 64)
		usedK, _ := strconv.ParseInt(f[2], 10, 64)
		if totalK > 0 {
			out.Disks = []map[string]any{{"mount": "/", "used": usedK * 1024, "total": totalK * 1024}}
		}
	}
	a.applyDeltas(d.Name, out, busy, total, rx, tx)
	return out
}

// --- endpoints --------------------------------------------------------------

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	if s.activity == nil {
		writeJSON(w, map[string]any{"devices": []any{}})
		return
	}
	a := s.activity
	a.mu.Lock()
	defer a.mu.Unlock()
	type row struct {
		Name   string `json:"name"`
		Status string `json:"status"` // ok | stale | off
		actLive
	}
	names := []string{a.selfName}
	if a.devices != nil {
		for _, d := range a.devices.devices {
			names = append(names, d.Name)
		}
	}
	rows := []row{}
	now := time.Now().Unix()
	for _, n := range names {
		l, ok := a.live[n]
		st := "off"
		if ok {
			if now-l.At < 60 {
				st = "ok"
			} else if now-l.At < 300 {
				st = "stale"
			}
		}
		rows = append(rows, row{Name: n, Status: st, actLive: l})
	}
	writeJSON(w, map[string]any{"devices": rows})
}

func (s *Server) handleActivityHistory(w http.ResponseWriter, r *http.Request) {
	if s.activity == nil {
		http.Error(w, "activity disabled", http.StatusServiceUnavailable)
		return
	}
	dev := r.URL.Query().Get("device")
	rng := r.URL.Query().Get("range")
	dur := time.Hour
	switch rng {
	case "5m":
		dur = 5 * time.Minute
	case "24h":
		dur = 24 * time.Hour
	}
	cut := time.Now().Add(-dur).Unix()
	s.activity.mu.Lock()
	ring := s.activity.series[dev]
	pts := []actPoint{}
	for _, p := range ring {
		if p.T >= cut {
			pts = append(pts, p)
		}
	}
	s.activity.mu.Unlock()
	// downsample to ≤120 buckets: max cpu (spikes matter), avg the rest
	const maxPts = 120
	if len(pts) > maxPts {
		bucket := (len(pts) + maxPts - 1) / maxPts
		ds := []actPoint{}
		for i := 0; i < len(pts); i += bucket {
			end := i + bucket
			if end > len(pts) {
				end = len(pts)
			}
			agg := pts[i]
			n := float64(end - i)
			var mem, swap, net float64
			for _, p := range pts[i:end] {
				if p.CPU > agg.CPU {
					agg.CPU = p.CPU
				}
				mem += p.Mem
				swap += p.Swap
				net += p.Net
			}
			agg.Mem, agg.Swap, agg.Net = mem/n, swap/n, net/n
			agg.T = pts[end-1].T
			ds = append(ds, agg)
		}
		pts = ds
	}
	writeJSON(w, map[string]any{"points": pts, "step": int(actInterval.Seconds())})
}

func (s *Server) handleActivityTop(w http.ResponseWriter, r *http.Request) {
	if s.activity == nil {
		http.Error(w, "activity disabled", http.StatusServiceUnavailable)
		return
	}
	a := s.activity
	dev := r.URL.Query().Get("device")
	// self → local ps
	if dev == a.selfName {
		writeJSON(w, map[string]any{"procs": localTopProcs()})
		return
	}
	var target *TermDevice
	if a.devices != nil {
		if d, ok := a.devices.effective(dev); ok {
			target = &d
		}
	}
	if target == nil {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	if target.Agent != "" && a.files != nil && a.files.agentURL(target.Agent) != "" {
		body, err := a.agentStats(target.Agent, "?top=1")
		if err != nil {
			http.Error(w, "unreachable", http.StatusBadGateway)
			return
		}
		var raw struct {
			Procs []map[string]any `json:"procs"`
		}
		_ = json.Unmarshal(body, &raw)
		writeJSON(w, map[string]any{"procs": raw.Procs})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	args := a.devices.sshArgs(*target)
	args = append(args[1:], "ps axo pid,pcpu,pmem,rss,comm --sort=-pcpu 2>/dev/null | head -13")
	b, err := exec.CommandContext(ctx, "ssh", args...).Output()
	if err != nil {
		http.Error(w, "unreachable", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"procs": parsePS(string(b))})
}

func localTopProcs() []map[string]any {
	var b []byte
	var err error
	if runtime.GOOS == "linux" {
		b, err = exec.Command("ps", "axo", "pid,pcpu,pmem,rss,comm", "--sort=-pcpu").Output()
	} else {
		b, err = exec.Command("ps", "-Aceo", "pid,pcpu,pmem,rss,comm", "-r").Output()
	}
	if err != nil {
		return nil
	}
	return parsePS(string(b))
}

func parsePS(out string) []map[string]any {
	procs := []map[string]any{}
	for _, ln := range strings.Split(out, "\n")[1:] {
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
	return procs
}

