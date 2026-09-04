package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Agent-chat Stage S — the three endpoints that let the chat surface READ
// a Claude Code / codex session (transcript = the CLI's own jsonl, screen =
// the tmux pane tail) and WRITE to it (input = tmux send-keys, relaunching
// the tmux first when the session is history). No push channel: the browser
// polls transcript+screen on its own cadence (ARCHITECTURE: two schedulers,
// never three).
//
// Trust: input is arbitrary exec as the manifest user — the same boundary
// the WS already exposes; same same-origin gate, same termIDRe check.

// handleTermTranscript (GET /api/terminal/session/{id}/transcript?after=N)
// → {turns, title, cost, live, offset}. after=<offset from the last reply>
// returns only newer records (the file is the stream).
func (s *Server) handleTermTranscript(w http.ResponseWriter, r *http.Request) {
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	live := s.terminal.liveSet()[tmuxName(se.ID)]
	path := s.terminal.transcriptPath(se)
	tr := termTranscript{Turns: []termTurn{}}
	if path != "" {
		if got, ok := readTranscript(se.Kind, path, after); ok {
			tr = got
		}
	}
	writeJSON(w, map[string]any{
		"turns": tr.Turns, "title": tr.Title, "cost": tr.Cost,
		"live": live, "offset": tr.Offset, "kind": se.Kind,
	})
}

// termScreenLines is how many trailing screen lines the live strip shows.
const termScreenLines = 12

// handleTermScreen (GET /api/terminal/session/{id}/screen) → {live, lines}:
// the last screen lines of the pane — how a permission prompt or a menu
// becomes visible (and answerable via input) without xterm.
func (s *Server) handleTermScreen(w http.ResponseWriter, r *http.Request) {
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	lines, live := s.terminal.screenTail(se.ID)
	writeJSON(w, map[string]any{"live": live, "lines": lines})
}

// screenTail captures the pane (-J joins wrapped lines, -S -12 reaches 12
// lines into scrollback) and keeps the last termScreenLines non-blank-tail
// lines. live=false when the tmux is gone (capture fails).
func (c *termCfg) screenTail(id string) ([]string, bool) {
	out, err := c.tmuxOut("capture-pane", "-p", "-J", "-S", "-12", "-t", tmuxName(id))
	if err != nil {
		return []string{}, false
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > termScreenLines {
		lines = lines[len(lines)-termScreenLines:]
	}
	if lines == nil {
		lines = []string{}
	}
	return lines, true
}

// handleTermInput (POST /api/terminal/session/{id}/input {text?, key?}):
// text = a message (Enter appended; multi-line goes through bracketed
// paste so the CLI's editor takes it as one message); key = raw bytes sent
// as-is (\x03, \x1b, arrows — the quick-key row). A dead session is
// relaunched first (`claude --resume <id>` via the shared spawn), the CLI's
// input line awaited, then the text delivered → {relaunched: true}.
func (s *Server) handleTermInput(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); o != "" && !sameOrigin(o, r.Host) {
		http.Error(w, "cross-origin refused", http.StatusForbidden)
		return
	}
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	if se.Device != "" {
		http.Error(w, "input is metis-local only", http.StatusBadRequest)
		return
	}
	var b struct {
		Text string `json:"text"`
		Key  string `json:"key"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if b.Text == "" && b.Key == "" {
		http.Error(w, "nothing to send", http.StatusBadRequest)
		return
	}
	relaunched, err := s.termEnsureLive(se)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if b.Key != "" {
		err = s.terminal.sendKey(se.ID, b.Key)
	} else {
		err = s.terminal.sendText(se.ID, b.Text)
	}
	if err != nil {
		http.Error(w, "send: "+err.Error(), http.StatusBadGateway)
		return
	}
	se.LastUsed = time.Now().Format(time.RFC3339)
	if relaunched {
		se.Started = true
	}
	s.terminal.upsert(se)
	writeJSON(w, map[string]any{"ok": true, "relaunched": relaunched})
}

// termPromptWait bounds how long a relaunch waits for the CLI's input line.
var termPromptWait = 10 * time.Second
var termPromptPoll = 250 * time.Millisecond

// termEnsureLive relaunches a dead session's tmux and waits for its prompt.
// Serialised per id so two sends within seconds spawn one tmux.
func (s *Server) termEnsureLive(se termSession) (bool, error) {
	c := s.terminal
	c.spawnMu.Lock()
	if c.spawnIn == nil {
		c.spawnIn = map[string]*sync.Mutex{}
	}
	mu := c.spawnIn[se.ID]
	if mu == nil {
		mu = &sync.Mutex{}
		c.spawnIn[se.ID] = mu
	}
	c.spawnMu.Unlock()
	mu.Lock()
	defer mu.Unlock()

	if c.liveSet()[tmuxName(se.ID)] {
		return false, nil
	}
	if err := s.spawnTermTmux(se); err != nil {
		return false, fmt.Errorf("relaunch: %w", err)
	}
	// wait for the CLI's input line (best effort: after the deadline the
	// text is sent anyway — tmux buffers it into the pty)
	deadline := time.Now().Add(termPromptWait)
	for time.Now().Before(deadline) {
		lines, live := c.screenTail(se.ID)
		if !live {
			return true, fmt.Errorf("relaunch: tmux exited before the prompt")
		}
		if termPromptShowing(lines) {
			break
		}
		time.Sleep(termPromptPoll)
	}
	return true, nil
}

// termPromptShowing spots the CLI's input line among the screen tail:
// Claude Code draws `> ` / `❯ `, codex `› `, a dropped shell `$ `.
func termPromptShowing(lines []string) bool {
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimLeft(lines[i], " │┃|")
		for _, p := range []string{">", "❯", "›", "$"} {
			if strings.HasPrefix(ln, p) {
				return true
			}
		}
	}
	return false
}

// sendText delivers a message and presses Enter. One line → send-keys -l;
// several → load-buffer + paste-buffer -p (bracketed paste) so the editor
// takes the block as one message instead of submitting at the first \n.
func (c *termCfg) sendText(id, text string) error {
	tn := tmuxName(id)
	text = strings.TrimRight(text, "\r\n")
	if !strings.Contains(text, "\n") {
		if err := c.tmux("send-keys", "-t", tn, "-l", "--", text); err != nil {
			return err
		}
		return c.tmux("send-keys", "-t", tn, "Enter")
	}
	f, err := os.CreateTemp(c.tmuxTmp, "paste-*.txt")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	f.Close()
	buf := "manifest_in_" + id
	if err := c.tmux("load-buffer", "-b", buf, path); err != nil {
		return err
	}
	if err := c.tmux("paste-buffer", "-p", "-d", "-b", buf, "-t", tn); err != nil {
		return err
	}
	return c.tmux("send-keys", "-t", tn, "Enter")
}

// sendKey writes raw bytes to the pane (no Enter): control chars, escape,
// arrow sequences, a bare y/n.
func (c *termCfg) sendKey(id, key string) error {
	return c.tmux("send-keys", "-t", tmuxName(id), "-l", "--", key)
}

// termRow resolves {id} to a registry row with the shared guards.
func (s *Server) termRow(w http.ResponseWriter, r *http.Request) (termSession, bool) {
	if s.terminal == nil {
		http.Error(w, "terminal disabled", http.StatusServiceUnavailable)
		return termSession{}, false
	}
	id := r.PathValue("id")
	se, ok := s.terminal.find(id)
	if !ok || !termIDRe.MatchString(id) {
		http.Error(w, "no such session", http.StatusNotFound)
		return termSession{}, false
	}
	return se, true
}
