package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Devices (terminal cockpit Phase 2): the fleet the new-session launcher can
// target. metis (self) is implicit; every other device is an ssh box from
// config, with runtime overrides (user/port/identity — the "gear" form) kept
// in <dataDir>/device_overrides.json. Remote sessions run `ssh -tt` INSIDE a
// metis tmux, so keep-alive is inherent everywhere.
//
// Probe model (cmd-ctr's): tcp-dial the ssh port (offline?), then a BatchMode
// `ssh … exit` (auth works?) → self | ok | needs-key | offline. Cached 30s,
// singleflight per device, probed in parallel — nothing on the WS-open path
// probes.

// TermDevice is one configured box (mirrors main.TerminalDevice — kept as its
// own type so server/ doesn't import package main).
type TermDevice struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	User     string `json:"user"`
	Port     int    `json:"port,omitempty"`
	Identity string `json:"identity,omitempty"`
	Agent    string `json:"agent,omitempty"`
}

type devOverride struct {
	User     string `json:"user,omitempty"`
	Port     int    `json:"port,omitempty"`
	Identity string `json:"identity,omitempty"`
}

type devProbe struct {
	status string // ok | needs-key | offline
	tmux   bool   // tmux present on the box → sessions there are caffeinatable
	at     time.Time
}

type devCfg struct {
	devices       []TermDevice
	overridesPath string
	knownHosts    string // <dataDir>/ssh_known_hosts (ProtectHome: ~/.ssh is read-only)
	selfName      string

	mu     sync.Mutex
	probes map[string]devProbe
	inFly  map[string]chan struct{}
	peers  []tailnetPeer // discovered fleet (tailscale status), cached
	peerAt time.Time
}

// tailnetPeer is one box tailscale can see (cmd-ctr's whole-tailnet list).
type tailnetPeer struct {
	Name   string
	Host   string // first tailscale IP
	OS     string
	Online bool
}

// tailnetPeers lists the tailnet (cached 30s). No tailscale binary → empty.
func (c *devCfg) tailnetPeers() []tailnetPeer {
	c.mu.Lock()
	if time.Since(c.peerAt) < 30*time.Second {
		out := c.peers
		c.mu.Unlock()
		return out
	}
	c.mu.Unlock()

	var peers []tailnetPeer
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if b, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output(); err == nil {
		var st struct {
			Peer map[string]struct {
				HostName     string   `json:"HostName"`
				OS           string   `json:"OS"`
				Online       bool     `json:"Online"`
				TailscaleIPs []string `json:"TailscaleIPs"`
			} `json:"Peer"`
		}
		if json.Unmarshal(b, &st) == nil {
			for _, p := range st.Peer {
				name := strings.ToLower(strings.TrimSuffix(p.HostName, "."))
				if !devNameRe2.MatchString(name) || len(p.TailscaleIPs) == 0 {
					continue
				}
				peers = append(peers, tailnetPeer{Name: name, Host: p.TailscaleIPs[0], OS: p.OS, Online: p.Online})
			}
		}
	}
	sortPeers(peers)
	c.mu.Lock()
	c.peers, c.peerAt = peers, time.Now()
	c.mu.Unlock()
	return peers
}

func sortPeers(ps []tailnetPeer) {
	for i := 1; i < len(ps); i++ { // online first, then name (tiny insertion sort)
		for j := i; j > 0; j-- {
			a, b := ps[j-1], ps[j]
			if (b.Online && !a.Online) || (a.Online == b.Online && b.Name < a.Name) {
				ps[j-1], ps[j] = b, a
			} else {
				break
			}
		}
	}
}

// discovered synthesizes a TermDevice for a tailnet peer that isn't in the
// config (default user = the manifest user; the gear override refines it).
func (c *devCfg) discovered(name string) (TermDevice, bool) {
	for _, p := range c.tailnetPeers() {
		if p.Name == name {
			d := TermDevice{Name: p.Name, Host: p.Host, User: "benjamin"}
			if o, ok := c.loadOverrides()[name]; ok {
				if o.User != "" {
					d.User = o.User
				}
				if o.Port != 0 {
					d.Port = o.Port
				}
				if o.Identity != "" {
					d.Identity = o.Identity
				}
			}
			return d, true
		}
	}
	return TermDevice{}, false
}

// devFieldRes mirror the config-load validation for runtime override input.
var (
	devUserRe2 = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]{0,31}$`)
	devNameRe2 = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
)

// UseDevices wires the fleet (empty list still enables the endpoint — the
// selector then only offers self).
func (s *Server) UseDevices(selfName string, devices []TermDevice, overridesPath, knownHosts string) {
	if selfName == "" {
		selfName = "metis"
	}
	s.devices = &devCfg{
		devices: devices, overridesPath: overridesPath, knownHosts: knownHosts,
		selfName: selfName, probes: map[string]devProbe{}, inFly: map[string]chan struct{}{},
	}
}

func (c *devCfg) loadOverrides() map[string]devOverride {
	out := map[string]devOverride{}
	if b, err := os.ReadFile(c.overridesPath); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func (c *devCfg) saveOverrides(m map[string]devOverride) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	tmp := c.overridesPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.overridesPath)
}

// effective returns the device with any runtime override applied — config
// devices first, then tailnet-discovered peers.
func (c *devCfg) effective(name string) (TermDevice, bool) {
	for _, d := range c.devices {
		if d.Name != name {
			continue
		}
		if o, ok := c.loadOverrides()[name]; ok {
			if o.User != "" {
				d.User = o.User
			}
			if o.Port != 0 {
				d.Port = o.Port
			}
			if o.Identity != "" {
				d.Identity = o.Identity
			}
		}
		return d, true
	}
	return c.discovered(name)
}

// sshArgs builds the validated argv prefix for reaching a device. Every field
// has passed the config/override regexes; nothing user-typed lands here raw.
func (c *devCfg) sshArgs(d TermDevice) []string {
	args := []string{
		"-tt", // one-shot callers strip this via args[1:] — keep it FIRST
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=4",
		// a silently-dropped path dies in ~45 s instead of hanging until TCP
		// gives up; with remote keep (☕) the reattach then self-heals
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + c.knownHosts,
	}
	if d.Port != 0 && d.Port != 22 {
		args = append(args, "-p", strconv.Itoa(d.Port))
	}
	if d.Identity != "" {
		args = append(args, "-i", d.Identity)
	}
	return append(args, d.User+"@"+d.Host)
}

// probe returns the cached status or runs a fresh probe (singleflight).
func (c *devCfg) probe(name string) string {
	st, _ := c.probeFull(name)
	return st
}

// probeFull also answers whether the box has tmux (the caffeination gate).
func (c *devCfg) probeFull(name string) (string, bool) {
	c.mu.Lock()
	if p, ok := c.probes[name]; ok && time.Since(p.at) < 30*time.Second {
		c.mu.Unlock()
		return p.status, p.tmux
	}
	if ch, running := c.inFly[name]; running {
		c.mu.Unlock()
		<-ch
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.probes[name].status, c.probes[name].tmux
	}
	ch := make(chan struct{})
	c.inFly[name] = ch
	c.mu.Unlock()

	status, tmux := c.runProbe(name)

	c.mu.Lock()
	c.probes[name] = devProbe{status: status, tmux: tmux, at: time.Now()}
	delete(c.inFly, name)
	close(ch)
	c.mu.Unlock()
	return status, tmux
}

func (c *devCfg) runProbe(name string) (string, bool) {
	d, ok := c.effective(name)
	if !ok {
		return "offline", false
	}
	port := d.Port
	if port == 0 {
		port = 22
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(d.Host, strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return "offline", false
	}
	_ = conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	// one round-trip answers both "can we ssh" and "is tmux there" — the
	// tmux presence gates the ☕ keep-alive toggle (cmd-ctr's caffeinatable)
	args := append(c.sshArgs(d), "command -v tmux >/dev/null 2>&1 && echo has-tmux || echo no-tmux")
	// -tt needs no pty for the probe; strip it to avoid "no tty" noise
	out, err := exec.CommandContext(ctx, "ssh", args[1:]...).Output()
	if err != nil {
		return "needs-key", false
	}
	return "ok", strings.Contains(string(out), "has-tmux")
}

func (c *devCfg) invalidate(name string) {
	c.mu.Lock()
	delete(c.probes, name)
	c.mu.Unlock()
}

// --- endpoints ---

func (s *Server) handleTermDevices(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil {
		writeJSON(w, map[string]any{"devices": []any{}})
		return
	}
	type row struct {
		Name       string `json:"name"`
		Self       bool   `json:"self,omitempty"`
		Host       string `json:"host,omitempty"`
		User       string `json:"user,omitempty"`
		Port       int    `json:"port,omitempty"`
		Identity   string `json:"identity,omitempty"`
		Agent      string `json:"agent,omitempty"`
		OS         string `json:"os,omitempty"`
		Status     string `json:"status"`
		Overridden bool   `json:"overridden,omitempty"`
		Discovered bool   `json:"discovered,omitempty"`
		Caffeinate bool   `json:"caffeinate,omitempty"` // tmux on the box → ☕ keep-alive available
	}
	self := "metis"
	if s.devices != nil {
		self = s.devices.selfName
	}
	// self is always caffeinatable — every session IS a metis tmux
	rows := []row{{Name: self, Self: true, Status: "self", Caffeinate: true}}
	if s.devices != nil {
		ovr := s.devices.loadOverrides()
		// the whole visible fleet: configured devices + tailnet-discovered
		// peers (deduped by name and by tailscale IP)
		type target struct {
			name       string
			online     bool // discovered-and-offline skips the ssh probe
			os         string
			discovered bool
		}
		targets := []target{}
		seenName, seenHost := map[string]bool{self: true}, map[string]bool{}
		for _, d := range s.devices.devices {
			targets = append(targets, target{name: d.Name, online: true})
			seenName[d.Name] = true
			seenHost[d.Host] = true
		}
		for _, p := range s.devices.tailnetPeers() {
			if seenName[p.Name] || seenHost[p.Host] || strings.EqualFold(p.Name, self) {
				continue
			}
			seenName[p.Name] = true
			targets = append(targets, target{name: p.Name, online: p.Online, os: p.OS, discovered: true})
		}
		type res struct {
			i int
			r row
		}
		ch := make(chan res, len(targets))
		for i, t := range targets {
			go func(i int, t target) {
				d, _ := s.devices.effective(t.name)
				_, hasOvr := ovr[t.name]
				status, tmux := "offline", false
				if t.online {
					status, tmux = s.devices.probeFull(t.name)
					// tailnet says it's up — no ssh path just means no key/port
					if status == "offline" && t.discovered {
						status = "needs-key"
					}
				}
				ch <- res{i, row{
					Name: d.Name, Host: d.Host, User: d.User, Port: d.Port,
					Identity: d.Identity, Agent: d.Agent, OS: t.os,
					Status: status, Overridden: hasOvr, Discovered: t.discovered,
					Caffeinate: tmux,
				}}
			}(i, t)
		}
		got := make([]row, len(targets))
		for range targets {
			r := <-ch
			got[r.i] = r.r
		}
		rows = append(rows, got...)
	}
	writeJSON(w, map[string]any{"devices": rows})
}

func (s *Server) handleTermDeviceUpdate(w http.ResponseWriter, r *http.Request) {
	if s.devices == nil {
		http.Error(w, "no devices configured", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if !devNameRe2.MatchString(name) {
		http.Error(w, "bad device name", http.StatusBadRequest)
		return
	}
	if _, ok := s.devices.effective(name); !ok {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	var b struct {
		User     string `json:"user"`
		Port     int    `json:"port"`
		Identity string `json:"identity"`
		Clear    bool   `json:"clear"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	m := s.devices.loadOverrides()
	if b.Clear {
		delete(m, name)
	} else {
		o := devOverride{}
		if u := strings.TrimSpace(b.User); u != "" {
			if !devUserRe2.MatchString(u) {
				http.Error(w, "bad user", http.StatusBadRequest)
				return
			}
			o.User = u
		}
		if b.Port != 0 {
			if b.Port < 1 || b.Port > 65535 {
				http.Error(w, "bad port", http.StatusBadRequest)
				return
			}
			o.Port = b.Port
		}
		if id := strings.TrimSpace(b.Identity); id != "" {
			if !filepath.IsAbs(id) {
				http.Error(w, "identity must be an absolute path on this box", http.StatusBadRequest)
				return
			}
			o.Identity = id
		}
		m[name] = o
	}
	if err := s.devices.saveOverrides(m); err != nil {
		httpError(w, err)
		return
	}
	s.devices.invalidate(name)
	writeJSON(w, map[string]any{"ok": true, "status": s.devices.probe(name)})
}

func (s *Server) handleTermDeviceProbe(w http.ResponseWriter, r *http.Request) {
	if s.devices == nil {
		http.Error(w, "no devices configured", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	s.devices.invalidate(name)
	writeJSON(w, map[string]any{"status": s.devices.probe(name)})
}
