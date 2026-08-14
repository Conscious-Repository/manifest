package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
	Kind      string `json:"kind"` // shell | claude | codex
	Cwd       string `json:"cwd"`
	Name      string `json:"name"`
	ResumeID  string `json:"resumeId,omitempty"` // claude --session-id / --resume handle
	Resume    bool   `json:"resume,omitempty"`   // launched via the interactive resume picker
	CreatedAt string `json:"createdAt"`
	LastUsed  string `json:"lastUsed"`
	Pinned    bool   `json:"pinned,omitempty"`
}

type termCfg struct {
	regPath   string // <dataDir>/terminals.json
	tmuxTmp   string // TMUX_TMPDIR (writable under the systemd sandbox)
	defaultWd string
	mu        sync.Mutex
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
	s.terminal = &termCfg{regPath: regPath, tmuxTmp: tmuxTmp, defaultWd: defaultWd}
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

func (c *termCfg) upsert(s termSession) {
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
	c.save(list)
}

// tmuxName is the session's tmux identity.
func tmuxName(id string) string { return "manifest_" + id }

// launchCmd resolves the inner command a fresh/resumed session runs.
func (s termSession) launchCmd() string {
	switch s.Kind {
	case "claude":
		if s.ResumeID != "" && resumeIDRe.MatchString(s.ResumeID) {
			return "claude --resume " + s.ResumeID
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
		out = append(out, row{se, live[tmuxName(se.ID)]})
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
		Cwd          string `json:"cwd"`
		Name         string `json:"name"`
		ResumePicker bool   `json:"resumePicker"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	kind := b.Kind
	if kind != "claude" && kind != "codex" {
		kind = "shell"
	}
	idb := make([]byte, 8)
	_, _ = rand.Read(idb)
	now := time.Now().Format(time.RFC3339)
	se := termSession{
		ID: hex.EncodeToString(idb), Kind: kind,
		Cwd: strings.TrimSpace(b.Cwd), Name: strings.TrimSpace(b.Name),
		Resume:    b.ResumePicker,
		CreatedAt: now, LastUsed: now,
	}
	if se.Name == "" {
		se.Name = kind + " · " + time.Now().Format("Jan 2 15:04")
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
		Name   *string `json:"name"`
		Pinned *bool   `json:"pinned"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
		se.Name = strings.TrimSpace(*b.Name)
	}
	if b.Pinned != nil {
		se.Pinned = *b.Pinned
	}
	s.terminal.upsert(se)
	writeJSON(w, se)
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
	// kill the tmux session, then forget the row
	s.terminal.tmux("kill-session", "-t", tmuxName(id))
	list := s.terminal.load()
	out := list[:0]
	for _, se := range list {
		if se.ID != id {
			out = append(out, se)
		}
	}
	s.terminal.save(out)
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

// tmux runs a tmux control command against the sandbox socket dir.
func (c *termCfg) tmux(args ...string) error {
	cmd := exec.Command("tmux", args...)
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+c.tmuxTmp)
	return cmd.Run()
}

// liveSet returns which tmux sessions currently exist.
func (c *termCfg) liveSet() map[string]bool {
	out := map[string]bool{}
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+c.tmuxTmp)
	b, err := cmd.Output()
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

	// build the tmux create-or-attach command (cmd-ctr's exact option order:
	// default-terminal BEFORE new-session, status off, mouse on, set-titles).
	name := tmuxName(id)
	cwd := se.Cwd
	if cwd == "" {
		cwd = s.terminal.defaultWd
	}
	inner := fmt.Sprintf("cd %s 2>/dev/null; exec %s", shQuote(cwd), se.launchCmd())
	tmuxArgs := []string{
		"new-session", "-A", "-s", name, "bash", "-lc", inner, ";",
		"set-option", "-t", name, "status", "off", ";",
		"set-option", "-t", name, "mouse", "on", ";",
		"set-option", "-t", name, "set-titles", "on", ";",
		"set-option", "-t", name, "set-titles-string", "#T",
	}
	// prepend the terminal-features config so full-screen TUIs render color
	full := append([]string{
		"set", "-g", "default-terminal", "tmux-256color", ";",
		"set", "-ga", "terminal-features", "xterm-256color:RGB,clipboard", ";",
	}, tmuxArgs...)

	cmd := exec.CommandContext(ctx, "tmux", full...)
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+s.terminal.tmuxTmp, "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		c.Write(ctx, websocket.MessageBinary, []byte("\r\n[manifest] failed to start terminal: "+err.Error()+"\r\n"))
		return
	}
	defer func() { _ = ptmx.Close(); _ = cmd.Process.Kill() }()

	// touch lastUsed
	se.LastUsed = time.Now().Format(time.RFC3339)
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
