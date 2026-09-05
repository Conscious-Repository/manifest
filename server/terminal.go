package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

// Terminal (cmd-ctr import, in-app terminal — metis-local v1): a real PTY on
// the box manifest runs on, wrapped in a persistent tmux so sessions survive
// disconnect and navigation. This is also the "manage Claude Code from inside
// the app" surface — claude/codex launch presets run inside the tmux, and a
// minted session id makes `claude --resume` one click.
//
// Trust: this exposes arbitrary code execution as the manifest user over the
// tailnet — the same boundary as the SSH the owner already has, and manifest
// is single-user tailnet-trusted. The WS is gated to same-origin.

// termSession is one registry row (<dataDir>/terminals.json).
type termSession struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`             // shell | claude | codex
	Device    string `json:"device,omitempty"` // "" = this box; else a fleet name
	Cwd       string `json:"cwd"`
	Name      string `json:"name"`
	ResumeID  string `json:"resumeId,omitempty"` // claude --session-id / --resume handle
	Resume    bool   `json:"resume,omitempty"`   // launched via the interactive resume picker
	Started   bool   `json:"started,omitempty"`  // first attach happened → reopen resumes
	CreatedAt string `json:"createdAt"`
	LastUsed  string `json:"lastUsed"`
	Pinned    bool   `json:"pinned,omitempty"`
	// Keep = caffeinated (cmd-ctr ☕): a REMOTE session also runs inside a
	// tmux on the target box, so it survives ssh drops and metis restarts —
	// the metis-side tmux alone only survives browser disconnects. Local
	// sessions are inherently kept (they ARE the metis tmux).
	Keep bool `json:"keep,omitempty"`
}

type termCfg struct {
	regPath   string // <dataDir>/terminals.json
	tmuxTmp   string // TMUX_TMPDIR (writable under the systemd sandbox)
	defaultWd string
	mu        sync.Mutex

	// remote-keep liveness cache: whether a kept session's tmux still runs on
	// its device (cmd-ctr's kept snapshot). Refreshed async — the sessions
	// list never blocks on ssh; a stale answer beats a 6 s stall per row.
	rlMu  sync.Mutex
	rlive map[string]remoteLiveEnt
	rlFly map[string]bool

	// claudeProjects overrides ~/.claude/projects (tests); "" = the home dir.
	claudeProjects string
	// run executes a tmux control command against the sandbox socket dir and
	// returns its stdout. Tests inject a recorder — no real tmux.
	run func(args ...string) ([]byte, error)
	// spawnMu serialises relaunch-on-input per session id (two sends to a
	// dead session within seconds must not spawn two tmuxes).
	spawnMu sync.Mutex
	spawnIn map[string]*sync.Mutex
}

type remoteLiveEnt struct {
	live bool
	at   time.Time
}

func (c *termCfg) rlGet(id string) (remoteLiveEnt, bool) {
	c.rlMu.Lock()
	defer c.rlMu.Unlock()
	e, ok := c.rlive[id]
	return e, ok
}

func (c *termCfg) rlForget(id string) {
	c.rlMu.Lock()
	delete(c.rlive, id)
	c.rlMu.Unlock()
}

var termIDRe = regexp.MustCompile(`^[0-9a-f]{8,32}$`)

// resumeIDRe bounds what may be interpolated into a claude --resume shell arg.
var resumeIDRe = regexp.MustCompile(`^[0-9a-fA-F][0-9a-fA-F-]{7,63}$`)

// UseTerminal enables the terminal (empty tmuxTmp/regPath disables it).
func (s *Server) UseTerminal(regPath, tmuxTmp, defaultWd string) {
	if regPath == "" || tmuxTmp == "" {
		return
	}
	_ = os.MkdirAll(tmuxTmp, 0o700)
	s.terminal = &termCfg{regPath: regPath, tmuxTmp: tmuxTmp, defaultWd: defaultWd,
		rlive: map[string]remoteLiveEnt{}, rlFly: map[string]bool{}}
}

// remoteKeepLive answers "does this kept session's tmux still run on its
// box?" from the cache, kicking an async ssh refresh when stale (60 s TTL).
func (s *Server) remoteKeepLive(se termSession) bool {
	c := s.terminal
	e, ok := c.rlGet(se.ID)
	if !ok || time.Since(e.at) > 60*time.Second {
		c.rlMu.Lock()
		if !c.rlFly[se.ID] {
			c.rlFly[se.ID] = true
			go func() {
				live := s.probeRemoteKeep(se)
				c.rlMu.Lock()
				c.rlive[se.ID] = remoteLiveEnt{live: live, at: time.Now()}
				delete(c.rlFly, se.ID)
				c.rlMu.Unlock()
			}()
		}
		c.rlMu.Unlock()
	}
	return e.live
}

func (s *Server) probeRemoteKeep(se termSession) bool {
	if s.devices == nil || s.devices.probe(se.Device) != "ok" {
		return false
	}
	dev, ok := s.devices.effective(se.Device)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	args := append(s.devices.sshArgs(dev)[1:], "tmux has-session -t "+tmuxName(se.ID)+" >/dev/null 2>&1 && echo Y || echo N")
	out, err := exec.CommandContext(ctx, "ssh", args...).Output()
	return err == nil && strings.Contains(string(out), "Y")
}

func (c *termCfg) load() []termSession {
	var out []termSession
	if b, err := os.ReadFile(c.regPath); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func (c *termCfg) save(list []termSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saveLocked(list)
}

func (c *termCfg) saveLocked(list []termSession) {
	b, _ := json.MarshalIndent(list, "", "  ")
	tmp := c.regPath + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, c.regPath)
	}
}

func (c *termCfg) find(id string) (termSession, bool) {
	for _, s := range c.load() {
		if s.ID == id {
			return s, true
		}
	}
	return termSession{}, false
}

// upsert is a read-modify-write of the whole registry: it holds the lock
// across the load too, so two handlers landing together (a chat send touching
// lastUsed while the WS attach marks Started, an autoName PUT beside a kill)
// cannot each rewrite the file from a stale copy and drop the other's row.
func (c *termCfg) upsert(s termSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := c.load()
	found := false
	for i := range list {
		if list[i].ID == s.ID {
			list[i] = s
			found = true
			break
		}
	}
	if !found {
		list = append([]termSession{s}, list...)
	}
	c.saveLocked(list)
}

// remove forgets a row (history's ✕), under the same lock as upsert.
func (c *termCfg) remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := c.load()
	out := list[:0]
	for _, se := range list {
		if se.ID != id {
			out = append(out, se)
		}
	}
	c.saveLocked(out)
}

// tmuxName is the session's tmux identity.
func tmuxName(id string) string { return "manifest_" + id }

// shortName mints cmd-ctr-style default names: sh1, cc2, cdx1 … (next free
// number for the kind's prefix across the registry).
func (c *termCfg) shortName(kind string) string {
	prefix := map[string]string{"claude": "cc", "codex": "cdx"}[kind]
	if prefix == "" {
		prefix = "sh"
	}
	max := 0
	for _, se := range c.load() {
		var n int
		if _, err := fmt.Sscanf(se.Name, prefix+"%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("%s%d", prefix, max+1)
}

// execLaunch is the "…; exec <tool>" tail every session runs: a PATH export
// (bash -lc is non-interactive — ~/.bashrc returns early, so ~/.local/bin
// never lands on PATH) and a tool guard that drops to a shell with a visible
// ✗ instead of an instant-exit loop when claude/codex isn't installed there
// (cmd-ctr's tool guard).
func (s termSession) execLaunch() string {
	tool := map[string]string{"claude": "claude", "codex": "codex"}[s.Kind]
	if tool == "" {
		return "exec " + s.launchCmd()
	}
	return `export PATH="$HOME/.local/bin:$HOME/.bun/bin:/opt/homebrew/bin:$PATH"; ` +
		"command -v " + tool + " >/dev/null 2>&1 || { printf '\\xe2\\x9c\\x97 " + tool +
		" is not installed here - dropping to a shell\\n'; exec bash -l; }; exec " + s.launchCmd()
}

// cdGuard prefixes the cd with a visible miss instead of silently landing in
// $HOME (cmd-ctr's no-such-directory guard).
func cdGuard(cwd string) string {
	q := shQuote(cwd)
	return "cd " + q + " 2>/dev/null || printf '\\xe2\\x9c\\x97 no such directory: %s\\n' " + q + "; "
}

// tmuxPrelude/tmuxSessionOpts are the shared tmux option strings — one
// definition serves the metis wrap, the agent spawn, and the remote keep wrap
// so a session behaves identically wherever its tmux lives.
// default-terminal must precede new-session (TERM is fixed at creation);
// history-limit likewise only affects panes created after it is set.
var tmuxPreludeArgs = []string{
	"set", "-g", "default-terminal", "tmux-256color", ";",
	"set", "-g", "history-limit", "10000", ";",
	"set", "-ga", "terminal-features", "xterm-256color:RGB,clipboard", ";",
}

func tmuxSessionOptArgs(name string) []string {
	return []string{
		";", "set-option", "-t", name, "status", "off",
		";", "set-option", "-t", name, "mouse", "on",
		";", "set-option", "-t", name, "set-titles", "on",
		";", "set-option", "-t", name, "set-titles-string", "#T",
		";", "set", "-g", "set-clipboard", "on",
	}
}

// remoteKeepWrap wraps a remote command in a create-or-attach tmux ON THE
// TARGET BOX — the caffeination mechanism. The string is shell text the
// remote login shell runs (tmux's `;` separators escaped as \;), and the
// inner command is shQuoted once more for tmux's bash -lc.
func remoteKeepWrap(id, remoteCmd string) string {
	tn := tmuxName(id)
	return `exec tmux set -g default-terminal tmux-256color \; ` +
		`set -g history-limit 10000 \; ` +
		`set -ga terminal-features ` + shQuote("xterm-256color:RGB,clipboard") + ` \; ` +
		`new-session -A -s ` + tn + ` bash -lc ` + shQuote(remoteCmd) + ` \; ` +
		`set-option -t ` + tn + ` status off \; ` +
		`set-option -t ` + tn + ` mouse on \; ` +
		`set-option -t ` + tn + ` set-titles on \; ` +
		`set-option -t ` + tn + ` set-titles-string ` + shQuote("#T") + ` \; ` +
		`set -g set-clipboard on`
}

// remoteInner builds the full command the remote login shell runs for a
// device session (cd guard + tool guard + launch, tmux-wrapped when kept).
func remoteInner(se termSession) string {
	cmd := se.execLaunch()
	if se.Cwd != "" {
		cmd = cdGuard(se.Cwd) + cmd
	}
	if se.Keep {
		cmd = remoteKeepWrap(se.ID, cmd)
	}
	return cmd
}

// launchCmd resolves the inner command a fresh/resumed session runs.
func (s termSession) launchCmd() string {
	switch s.Kind {
	case "claude":
		// cmd-ctr semantics: the FIRST run CREATES the conversation under the
		// minted id (--session-id); only after it has started does a dead-tmux
		// reopen resume it (--resume against a virgin id = "No conversation
		// found" + an exit/reattach loop).
		if s.ResumeID != "" && resumeIDRe.MatchString(s.ResumeID) {
			if s.Started {
				return "claude --resume " + s.ResumeID
			}
			return "claude --session-id " + s.ResumeID
		}
		if s.Resume {
			return "claude --resume" // interactive conversation picker
		}
		return "claude"
	case "codex":
		if s.ResumeID != "" && resumeIDRe.MatchString(s.ResumeID) {
			return "codex resume --yolo " + s.ResumeID
		}
		if s.Resume {
			return "codex resume --yolo"
		}
		return "codex --yolo"
	default:
		return "bash -l"
	}
}

// --- registry endpoints ---

func (s *Server) handleTermSessions(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil {
		writeJSON(w, map[string]any{"sessions": []any{}, "enabled": false})
		return
	}
	list := s.terminal.load()
	live := s.terminal.liveSet()
	type row struct {
		termSession
		Live bool `json:"live"`
	}
	out := make([]row, 0, len(list))
	for _, se := range list {
		l := live[tmuxName(se.ID)]
		// a kept remote session outlives its metis tmux: the remote box's
		// tmux is the truth (cached; refreshed async, never blocks the list)
		if !l && se.Keep && se.Device != "" {
			l = s.remoteKeepLive(se)
		}
		out = append(out, row{se, l})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].LastUsed > out[j].LastUsed
	})
	writeJSON(w, map[string]any{"sessions": out, "enabled": true})
}

func (s *Server) handleTermCreate(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil {
		http.Error(w, "terminal disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Kind         string `json:"kind"`
		Device       string `json:"device"`
		Cwd          string `json:"cwd"`
		Name         string `json:"name"`
		ResumePicker bool   `json:"resumePicker"`
		Keep         bool   `json:"keep"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	kind := b.Kind
	if kind != "claude" && kind != "codex" {
		kind = "shell"
	}
	device := strings.TrimSpace(b.Device)
	if device != "" {
		if s.devices == nil {
			http.Error(w, "no devices configured", http.StatusBadRequest)
			return
		}
		if device == s.devices.selfName {
			device = ""
		} else if _, ok := s.devices.effective(device); !ok {
			http.Error(w, "unknown device "+device, http.StatusBadRequest)
			return
		}
	}
	idb := make([]byte, 8)
	_, _ = rand.Read(idb)
	now := time.Now().Format(time.RFC3339)
	se := termSession{
		ID: hex.EncodeToString(idb), Kind: kind, Device: device,
		Cwd: strings.TrimSpace(b.Cwd), Name: strings.TrimSpace(b.Name),
		Resume:    b.ResumePicker,
		Keep:      b.Keep && device != "", // local sessions are inherently kept
		CreatedAt: now, LastUsed: now,
	}
	if se.Name == "" {
		se.Name = s.terminal.shortName(kind)
	}
	// mint claude's resume handle up front → `claude --resume` works forever.
	// (Not when resuming via the picker — the id would shadow the choice.)
	if kind == "claude" && !b.ResumePicker {
		u := make([]byte, 16)
		_, _ = rand.Read(u)
		se.ResumeID = fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
	}
	s.terminal.upsert(se)
	writeJSON(w, se)
}

// --- agent-launched sessions ---

// createAgentTermSession is the agent-facing create: it registers the session
// (so it appears in the SESSIONS rail like any other row) AND spawns the tmux
// backend detached — an agent has no browser attach to boot the session, and
// needs the live tmux up before it starts driving it. Local box only.
// The spawn mirrors the WS attach path exactly (same socket dir via c.tmux,
// same tmux name, same option order), so a later browser attach's
// `new-session -A` lands on this same session.
func (s *Server) createAgentTermSession(kind, cwd, name string) (termSession, string, error) {
	if s.terminal == nil {
		return termSession{}, "", fmt.Errorf("terminal disabled")
	}
	if kind != "claude" && kind != "codex" {
		kind = "shell"
	}
	idb := make([]byte, 8)
	_, _ = rand.Read(idb)
	now := time.Now().Format(time.RFC3339)
	se := termSession{
		ID: hex.EncodeToString(idb), Kind: kind,
		Cwd: strings.TrimSpace(cwd), Name: strings.TrimSpace(name),
		CreatedAt: now, LastUsed: now,
	}
	if se.Name == "" {
		se.Name = s.terminal.shortName(kind)
	}
	// mint claude's resume handle up front, same as handleTermCreate — the
	// caller drives the conversation via `claude --resume <id>`.
	if kind == "claude" {
		u := make([]byte, 16)
		_, _ = rand.Read(u)
		se.ResumeID = fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
	}
	s.terminal.upsert(se)

	tn := tmuxName(se.ID)
	if err := s.spawnTermTmux(se); err != nil {
		// the row stays: it's now an ordinary not-yet-started session the
		// browser attach path can still boot.
		return se, tn, fmt.Errorf("tmux spawn: %w", err)
	}
	// claude has now booted under --session-id → future reopens must --resume.
	se.Started = true
	se.LastUsed = time.Now().Format(time.RFC3339)
	s.terminal.upsert(se)
	return se, tn, nil
}

// termInner is the bash -lc command a session's tmux pane runs: cd guard +
// tool guard + launch for a local row; exec ssh to the box for a device
// row. ok=false when the device is unknown.
func (s *Server) termInner(se termSession) (string, bool) {
	if se.Device != "" {
		// remote session: the tmux inner command is ssh to the box. The remote
		// side gets ONE shQuoted command string its login shell runs; the metis
		// side (bash -lc) sees every ssh arg individually shQuoted.
		var dev TermDevice
		ok := false
		if s.devices != nil {
			dev, ok = s.devices.effective(se.Device)
		}
		if !ok {
			return "", false
		}
		parts := []string{"exec", "ssh"}
		for _, a := range s.devices.sshArgs(dev) {
			parts = append(parts, shQuote(a))
		}
		parts = append(parts, shQuote(remoteInner(se)))
		return strings.Join(parts, " "), true
	}
	cwd := se.Cwd
	if cwd == "" {
		cwd = s.terminal.defaultWd
	}
	return cdGuard(cwd) + se.execLaunch(), true
}

// termLaunchArgs is THE tmux argv for a session (cmd-ctr's exact option
// order: default-terminal BEFORE new-session, status off, mouse on,
// set-titles). attach=true is the WS create-or-attach (`new-session -A`,
// runs inside a PTY); attach=false spawns detached (`-d`, fixed size) for
// the agent-create and relaunch-on-input paths. One definition, three
// callers, so a later browser attach lands on the same session.
func (s *Server) termLaunchArgs(se termSession, attach bool) ([]string, error) {
	inner, ok := s.termInner(se)
	if !ok {
		return nil, fmt.Errorf("unknown device %s", se.Device)
	}
	tn := tmuxName(se.ID)
	var ns []string
	if attach {
		ns = []string{"new-session", "-A", "-s", tn, "bash", "-lc", inner}
	} else {
		ns = []string{"new-session", "-d", "-s", tn, "-x", "120", "-y", "32", "bash", "-lc", inner}
	}
	return append(append(append([]string{}, tmuxPreludeArgs...), ns...), tmuxSessionOptArgs(tn)...), nil
}

// spawnTermTmux boots a session's tmux detached (no browser, no PTY), in a
// cgroup manifest's own restart cannot reach (see tmuxSpawn).
func (s *Server) spawnTermTmux(se termSession) error {
	args, err := s.termLaunchArgs(se, false)
	if err != nil {
		return err
	}
	return s.terminal.tmuxSpawn(args...)
}

// handleTermAgentCreate (POST /api/terminal/agent-session) accepts
// {kind, cwd, name} and returns the registry row plus its tmux name, so a
// local agent can create a session and drive it from the shell.
func (s *Server) handleTermAgentCreate(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil {
		http.Error(w, "terminal disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Kind string `json:"kind"`
		Cwd  string `json:"cwd"`
		Name string `json:"name"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	kind := b.Kind
	if kind == "" {
		kind = "claude"
	}
	se, tn, err := s.createAgentTermSession(kind, b.Cwd, b.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, struct {
		termSession
		Tmux string `json:"tmux"`
	}{se, tn})
}

func (s *Server) handleTermUpdate(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil {
		http.Error(w, "terminal disabled", http.StatusServiceUnavailable)
		return
	}
	se, ok := s.terminal.find(r.PathValue("id"))
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	var b struct {
		Name     *string `json:"name"`
		AutoName *string `json:"autoName"`
		Pinned   *bool   `json:"pinned"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
		se.Name = strings.TrimSpace(*b.Name)
	}
	// autoName (cmd-ctr): the CLI's own OSC title names the row — but ONLY
	// while it still wears a minted placeholder (sh1/cc2/…). A name the owner
	// typed is frozen forever; junk titles (the bare tool name) are refused.
	if b.AutoName != nil && termPlaceholderRe.MatchString(se.Name) {
		if n := cleanAutoName(*b.AutoName); n != "" {
			se.Name = n
		}
	}
	if b.Pinned != nil {
		se.Pinned = *b.Pinned
	}
	s.terminal.upsert(se)
	writeJSON(w, se)
}

var termPlaceholderRe = regexp.MustCompile(`^(sh|cc|cdx)\d+$`)

// cleanAutoName strips spinner glyphs and rejects junk titles.
func cleanAutoName(t string) string {
	t = strings.TrimLeft(t, " ✳✶✻●◐○•·*—–-")
	t = strings.Join(strings.Fields(t), " ")
	if len(t) > 80 {
		t = t[:80]
	}
	switch strings.ToLower(t) {
	case "", "claude", "claude code", "codex", "bash", "local":
		return ""
	}
	return t
}

// handleTermKill ends the live backend but KEEPS the registry row — the
// session moves to HISTORY (resumable). DELETE below is history's "forget".
func (s *Server) handleTermKill(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil {
		http.Error(w, "terminal disabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	se, ok := s.terminal.find(id)
	if !ok || !termIDRe.MatchString(id) {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	s.terminal.tmux("kill-session", "-t", tmuxName(id))
	s.killRemoteKeep(se)
	se.LastUsed = time.Now().Format(time.RFC3339)
	s.terminal.upsert(se)
	writeJSON(w, map[string]bool{"ok": true})
}

// killRemoteKeep ends a kept session's REMOTE tmux too — ending a
// caffeinated session must not leave a headless copy running on the box.
func (s *Server) killRemoteKeep(se termSession) {
	if !se.Keep || se.Device == "" || s.devices == nil {
		return
	}
	dev, ok := s.devices.effective(se.Device)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	args := append(s.devices.sshArgs(dev)[1:], "tmux kill-session -t "+tmuxName(se.ID)+" 2>/dev/null")
	_ = exec.CommandContext(ctx, "ssh", args...).Run()
	s.terminal.rlForget(se.ID)
}

func (s *Server) handleTermDelete(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil {
		http.Error(w, "terminal disabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if !termIDRe.MatchString(id) {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	// kill the tmux session (both ends for a kept remote), then forget the row
	s.terminal.tmux("kill-session", "-t", tmuxName(id))
	if se, ok := s.terminal.find(id); ok {
		s.killRemoteKeep(se)
	}
	s.terminal.remove(id)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleTermLs lists sub-directories for the launcher's cwd browse picker.
// Not restricted to filesRoots: the terminal is already arbitrary-exec as this
// user, and this only reveals directory NAMES. Metis-local v1 (device= later).
func (s *Server) handleTermLs(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil {
		http.Error(w, "terminal disabled", http.StatusServiceUnavailable)
		return
	}
	if dev := strings.TrimSpace(r.URL.Query().Get("device")); dev != "" && s.devices != nil && dev != s.devices.selfName {
		s.handleTermLsRemote(w, r, dev)
		return
	}
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" || p == "~" {
		p = s.terminal.defaultWd
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(s.terminal.defaultWd, p[2:])
	}
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		httpError(w, err)
		return
	}
	type dirRow struct {
		Name   string `json:"name"`
		Hidden bool   `json:"hidden,omitempty"`
	}
	dirs := []dirRow{}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dirs = append(dirs, dirRow{Name: e.Name(), Hidden: strings.HasPrefix(e.Name(), ".")})
	}
	writeJSON(w, map[string]any{"path": p, "home": s.terminal.defaultWd, "dirs": dirs})
}

// handleTermLsRemote lists a fleet box's sub-dirs via a one-shot ssh (the
// browse picker on a remote device). ~-relative paths resolve remotely.
func (s *Server) handleTermLsRemote(w http.ResponseWriter, r *http.Request, device string) {
	dev, ok := s.devices.effective(device)
	if !ok {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" || p == "~" {
		p = "$HOME"
	} else if !strings.HasPrefix(p, "/") {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}
	q := p
	if q != "$HOME" {
		q = shQuote(filepath.Clean(p))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	args := s.devices.sshArgs(dev)
	args = append(args[1:], "cd "+q+" && pwd && ls -1Ap") // drop -tt for one-shots
	out, err := exec.CommandContext(ctx, "ssh", args...).Output()
	if err != nil {
		http.Error(w, "unreachable", http.StatusBadGateway)
		return
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 0 {
		http.Error(w, "empty reply", http.StatusBadGateway)
		return
	}
	type dirRow struct {
		Name   string `json:"name"`
		Hidden bool   `json:"hidden,omitempty"`
	}
	resolved := strings.TrimSpace(lines[0])
	dirs := []dirRow{}
	for _, ln := range lines[1:] {
		if !strings.HasSuffix(ln, "/") {
			continue
		}
		n := strings.TrimSuffix(ln, "/")
		dirs = append(dirs, dirRow{Name: n, Hidden: strings.HasPrefix(n, ".")})
	}
	writeJSON(w, map[string]any{"path": resolved, "device": device, "dirs": dirs})
}

// tmuxOut runs a tmux control command against the sandbox socket dir and
// returns its stdout (the injected runner in tests).
func (c *termCfg) tmuxOut(args ...string) ([]byte, error) {
	if c.run != nil {
		return c.run(args...)
	}
	cmd := exec.Command("tmux", args...)
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+c.tmuxTmp)
	return cmd.Output()
}

// tmux runs a tmux control command, status only.
func (c *termCfg) tmux(args ...string) error {
	_, err := c.tmuxOut(args...)
	return err
}

// SESSION LIFETIME — why a spawn is not an ordinary exec.
//
// A tmux server started by manifest inherits manifest's cgroup, and this unit
// has no KillMode, so systemd's default (control-group) applies: on `systemctl
// restart manifest` — which the autodeploy timer does on EVERY push — systemd
// SIGTERMs every process in that cgroup. The tmux servers went with it, and
// with them every Claude Code session the cockpit had started, mid-turn.
// Verified on metis: a running session's tmux read
// `0::/system.slice/manifest.service`.
//
// So the server is born in a transient systemd USER scope instead
// (`user@<uid>.service/app.slice/run-*.scope`) — a cgroup manifest's own
// restart does not touch. tmux keeps ONE server per socket, so only the first
// spawn actually creates it; later spawns are short-lived clients whose scope
// is collected immediately. The browser's PTY attach must therefore never be
// the first launch (handleTermWS spawns detached first), or the server would
// be born inside the request's cgroup again.
//
// Where systemd-run is unavailable (a dev Mac, a box with no user manager,
// the tests' injected runner) the spawn falls back to a plain exec and says so
// once: the sessions work, they just do not survive a restart.
var tmuxScopeOnce sync.Once

// tmuxSpawn runs a tmux command that may CREATE the server.
func (c *termCfg) tmuxSpawn(args ...string) error {
	if c.run != nil { // injected runner (tests) — no systemd in the loop
		return c.tmux(args...)
	}
	if !tmuxScopeAvailable() {
		return c.tmux(args...)
	}
	scoped := append([]string{"--user", "--scope", "--collect", "--quiet",
		"--description=manifest terminal session",
		"--setenv=TMUX_TMPDIR=" + c.tmuxTmp, "--", "tmux"}, args...)
	cmd := exec.Command("systemd-run", scoped...)
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+c.tmuxTmp,
		"XDG_RUNTIME_DIR="+xdgRuntimeDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		// a scope that will not start must not cost the session: fall back to
		// the plain spawn, which works and only loses restart survival
		log.Printf("terminal: systemd scope spawn failed (%v: %s) — falling back; "+
			"this session will not survive a manifest restart", err, strings.TrimSpace(string(out)))
		return c.tmux(args...)
	}
	return nil
}

// xdgRuntimeDir is where the user manager's bus lives. The systemd unit does
// not set it (a service is not a login session), so it is derived.
func xdgRuntimeDir() string {
	if d := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); d != "" {
		return d
	}
	return "/run/user/" + strconv.Itoa(os.Getuid())
}

// tmuxScopeAvailable reports whether transient user scopes can be started
// here, probed once: systemd-run on PATH and a user manager listening.
var tmuxScopeOK bool

func tmuxScopeAvailable() bool {
	tmuxScopeOnce.Do(func() {
		if runtime.GOOS != "linux" {
			return
		}
		if _, err := exec.LookPath("systemd-run"); err != nil {
			return
		}
		if _, err := os.Stat(xdgRuntimeDir() + "/systemd/private"); err != nil {
			log.Printf("terminal: no user systemd manager at %s — sessions will not survive a manifest restart", xdgRuntimeDir())
			return
		}
		tmuxScopeOK = true
	})
	return tmuxScopeOK
}

// liveSet returns which tmux sessions currently exist.
func (c *termCfg) liveSet() map[string]bool {
	out := map[string]bool{}
	b, err := c.tmuxOut("list-sessions", "-F", "#{session_name}")
	if err != nil {
		return out
	}
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if ln != "" {
			out[ln] = true
		}
	}
	return out
}

// --- the PTY websocket ---

// handleTermWS upgrades to a WS and attaches a PTY running the session's tmux.
func (s *Server) handleTermWS(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil {
		http.Error(w, "terminal disabled", http.StatusServiceUnavailable)
		return
	}
	// same-origin gate: the trust boundary is the tailnet, but reject
	// cross-site WS attempts outright.
	if o := r.Header.Get("Origin"); o != "" && !sameOrigin(o, r.Host) {
		http.Error(w, "cross-origin refused", http.StatusForbidden)
		return
	}
	id := r.URL.Query().Get("id")
	se, ok := s.terminal.find(id)
	if !ok || !termIDRe.MatchString(id) {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	cols, rows := clampDim(r.URL.Query().Get("c"), 120), clampDim(r.URL.Query().Get("r"), 32)

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()

	// The PTY below is manifest's own child, so a tmux SERVER first started by
	// it would live in manifest's cgroup and die at the next restart. Give the
	// session a scoped home first (idempotent: `new-session -A` then attaches
	// to what this created), and only then attach.
	if !s.terminal.liveSet()[tmuxName(se.ID)] {
		if err := s.spawnTermTmux(se); err != nil {
			c.Write(ctx, websocket.MessageBinary, []byte("\r\n[manifest] "+err.Error()+"\r\n"))
			return
		}
	}
	// the tmux create-or-attach command — the shared definition (termLaunchArgs)
	full, err := s.termLaunchArgs(se, true)
	if err != nil {
		c.Write(ctx, websocket.MessageBinary, []byte("\r\n[manifest] "+err.Error()+"\r\n"))
		return
	}

	cmd := exec.CommandContext(ctx, "tmux", full...)
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+s.terminal.tmuxTmp, "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		c.Write(ctx, websocket.MessageBinary, []byte("\r\n[manifest] failed to start terminal: "+err.Error()+"\r\n"))
		return
	}
	defer func() { _ = ptmx.Close(); _ = cmd.Process.Kill() }()

	// touch lastUsed; the session has now run once → future reopens resume
	se.LastUsed = time.Now().Format(time.RFC3339)
	se.Started = true
	s.terminal.upsert(se)

	// PTY → browser (binary frames)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if werr := c.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				c.Close(websocket.StatusNormalClosure, "pty closed")
				return
			}
		}
	}()

	// browser → PTY (JSON input/resize frames)
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			_, _ = ptmx.Write(data)
			continue
		}
		var msg struct {
			T string `json:"t"`
			D string `json:"d"`
			C int    `json:"c"`
			R int    `json:"r"`
		}
		if json.Unmarshal(data, &msg) != nil {
			_, _ = ptmx.Write(data) // tolerate a bare text frame as literal input
			continue
		}
		switch msg.T {
		case "i":
			_, _ = ptmx.Write([]byte(msg.D))
		case "r":
			if msg.C > 0 && msg.R > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(clampInt(msg.C, 500)), Rows: uint16(clampInt(msg.R, 200))})
			}
		}
	}
}

func clampDim(s string, def int) int {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		return def
	}
	return clampInt(n, 500)
}

func clampInt(n, max int) int {
	if n < 1 {
		return 1
	}
	if n > max {
		return max
	}
	return n
}

func sameOrigin(origin, host string) bool {
	i := strings.Index(origin, "://")
	if i < 0 {
		return false
	}
	oh := origin[i+3:]
	if j := strings.IndexByte(oh, '/'); j >= 0 {
		oh = oh[:j]
	}
	return oh == host
}

// shQuote single-quotes a string for safe use in a bash -lc command.
func shQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var _ = filepath.Join // kept for future cwd resolution
