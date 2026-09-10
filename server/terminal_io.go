package server

import (
	"context"
	"errors"
	"fmt"
	"manifest/agentchat"
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
	se, tr, ob, live := s.projectTerminalTranscript(r.Context(), se, after)
	planningTimeline, _ := s.terminalPlanningTimeline(r.Context(), se)
	writeJSON(w, map[string]any{
		"turns": tr.Turns, "title": tr.Title, "cost": tr.Cost,
		"conversation": s.terminalConversation(se),
		"origin":       se.Origin, "draft": se.isDraft(),
		"related":            s.terminalRelatedChats(se),
		"planningTimeline":   planningTimeline,
		"planningRecipients": s.terminalPlanningChildren(se),
		"codingRecipients":   s.terminalCodingContinuations(r.Context(), se),
		"planningOperations": s.terminalPlanningOperations(se),
		"live":               live, "offset": tr.Offset, "kind": se.Kind, "agentState": ob.AgentState, "connectivity": ob.Connectivity, "process": ob.Process,
	})
}

func (s *Server) projectTerminalTranscript(ctx context.Context, se termSession, after int64) (termSession, termTranscript, terminalObservation, bool) {
	live := false
	ob := terminalUnknown(se.Runtime)
	if se.backend() == "herdr" {
		ob, _ = s.observeTerm(ctx, se)
		live = ob.Process == "running"
		se = s.captureObservedTermIdentity(se, ob)
		if se.Kind == "codex" && se.ResumeID == "" && live && s.terminal.herdr != nil {
			if id, err := s.terminal.herdr.codexProcessRollout(ctx, se); err == nil && id != "" {
				se = s.captureTermResumeID(se, id)
			}
		}
	} else {
		live = s.terminal.liveSet()[tmuxName(se.ID)]
	}
	path := s.terminal.transcriptPath(se)
	tr := termTranscript{Turns: []termTurn{}}
	if path != "" {
		if got, ok := readTranscript(se.Kind, path, after); ok {
			tr = got
		}
	}
	return se, tr, ob, live
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
	if se.isDraft() {
		writeJSON(w, map[string]any{"live": false, "lines": []string{}, "agentState": "not-started", "connectivity": "not-started", "process": "not-started"})
		return
	}
	if se.backend() == "herdr" {
		rt, err := s.runtimeFor(se)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		ob, _ := rt.Inspect(r.Context(), se.Runtime)
		lines := []string{}
		if ob.Process == "running" {
			if screen, e := rt.Screen(r.Context(), se.Runtime); e == nil {
				lines = screen
			} else {
				ob = terminalUnknown(se.Runtime)
			}
		}
		writeJSON(w, map[string]any{"live": ob.Process == "running", "lines": lines, "agentState": ob.AgentState, "connectivity": ob.Connectivity, "process": ob.Process})
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
	var b terminalInput
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if b.Text == "" && b.Key == "" {
		http.Error(w, "nothing to send", http.StatusBadRequest)
		return
	}
	if b.RequestID != "" && (!agentchat.ValidRequestID(b.RequestID) || b.Key != "" || se.backend() != "herdr") {
		httpError(w, errBadRequest("request IDs require a local herdr text submission"))
		return
	}
	if b.Supervise && se.backend() != "herdr" {
		http.Error(w, "supervision unavailable for this backend; nothing sent", http.StatusBadRequest)
		return
	}
	if len(b.Artifacts) > 0 && se.backend() != "herdr" {
		httpError(w, errBadRequest("artifact context is unavailable for this terminal backend; nothing sent"))
		return
	}
	if se.backend() == "herdr" {
		mu := s.termInputMutex(se.ID)
		mu.Lock()
		defer mu.Unlock()
		current, exists, readErr := s.terminal.findChecked(se.ID)
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "no such session", http.StatusNotFound)
			return
		}
		se = current
		if (b.ConversationAgent != "" || b.ConversationID != "") && (se.Origin == nil || se.Origin.Mode != "continue" || se.Origin.Agent != b.ConversationAgent || se.Origin.ID != b.ConversationID) {
			httpError(w, errBadRequest("coding session does not continue this conversation"))
			return
		}
		fingerprint := b.fingerprint()
		ownerText := b.Text
		var continuationContext *terminalInputReceipt
		if b.RequestID != "" {
			receipt, err := s.terminal.readInputReceipt(se.ID, b.RequestID)
			if err == nil {
				if receipt.Fingerprint != fingerprint {
					http.Error(w, "request ID was already used for different content", http.StatusConflict)
					return
				}
				writeTerminalInputReceipt(w, se.ID, receipt)
				return
			}
			if !errors.Is(err, os.ErrNotExist) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if se.Origin != nil && se.Origin.Mode == "continue" && se.Origin.Backend == "terminal" && b.Key == "" {
			root, found := s.terminal.find(se.Origin.ID)
			if !found || root.Kind != se.Origin.Agent || root.Device != "" || (root.Origin != nil && root.Origin.Mode == "continue") {
				httpError(w, errBadRequest("source conversation unavailable"))
				return
			}
			if b.RequestID == "" {
				httpError(w, errBadRequest("continuation messages require a request ID"))
				return
			}
			timeline, _ := s.terminalPlanningTimeline(r.Context(), root)
			key := s.terminalConversation(root).Key
			context, omitted := timelineContinuationContext(key, timeline)
			continuationContext = &terminalInputReceipt{Text: ownerText, ContextSource: key, ContextHash: hashTerminalText(context), HistoryOmitted: omitted}
			b.Text = context + "\n\nCurrent owner instruction (submission " + b.RequestID + "):\n" + ownerText
		}
		if se.Origin != nil && se.Origin.Mode == "continue" && se.Origin.Backend == "" && b.Key == "" {
			if b.RequestID == "" {
				httpError(w, errBadRequest("continuation messages require a request ID"))
				return
			}
			if s.agentChat == nil {
				httpError(w, errBadRequest("source conversation unavailable"))
				return
			}
			source, body, _, ok := s.agentChat.store.Get(se.Origin.Agent, se.Origin.ID)
			if !ok {
				httpError(w, errBadRequest("source conversation unavailable"))
				return
			}
			context, omitted := logicalContinuationContext(source, body, s.codingContinuations(r.Context(), source))
			continuationContext = &terminalInputReceipt{Text: ownerText, ContextSource: sessionConversation(source).Key, ContextHash: hashTerminalText(context), HistoryOmitted: omitted}
			b.Text = context + "\n\nCurrent owner instruction (submission " + b.RequestID + "):\n" + ownerText
		}
		if continuationContext == nil && b.Key == "" {
			if timeline, found := s.terminalPlanningTimeline(r.Context(), se); found {
				if b.RequestID == "" {
					httpError(w, errBadRequest("continuation messages require a request ID"))
					return
				}
				key := s.terminalConversation(se).Key
				context, omitted := timelineContinuationContext(key, timeline)
				continuationContext = &terminalInputReceipt{Text: ownerText, ContextSource: key, ContextHash: hashTerminalText(context), HistoryOmitted: omitted}
				b.Text = context + "\n\nCurrent owner instruction (submission " + b.RequestID + "):\n" + ownerText
			}
		}
		if len(b.Artifacts) > 0 {
			linked := false
			for _, link := range s.terminalConversation(se).Links {
				if link.Kind == "task" && link.ID == b.Task && b.Task != "" {
					linked = true
				}
			}
			if !linked || b.Key != "" {
				httpError(w, errBadRequest("artifact context requires a message and this coding chat's linked task"))
				return
			}
			context, err := s.taskArtifactContext(b.Task, b.Artifacts)
			if err != nil {
				httpError(w, err)
				return
			}
			b.Text += context
		}
		if se.isDraft() && (b.Key != "" || strings.TrimSpace(b.Text) == "") {
			http.Error(w, "send a message to start this draft; keys cannot start it", http.StatusConflict)
			return
		}
		var relaunched bool
		var receipt *terminalInputReceipt
		var err error
		se, relaunched, err = s.ensureHerdrInputLocked(r.Context(), se)
		if err != nil {
			http.Error(w, err.Error(), terminalLaunchStatus(err))
			return
		}
		if b.Key != "" {
			err = s.terminal.herdr.SendKey(r.Context(), se.Runtime, b.Key)
		} else {
			err = s.herdrPromptReady(r.Context(), se)
			if err == nil {
				if b.RequestID != "" {
					receipt = &terminalInputReceipt{ID: b.RequestID, Fingerprint: fingerprint, State: "unconfirmed", Updated: time.Now().UTC().Format(time.RFC3339Nano), Runtime: se.Runtime, Task: b.Task, Artifacts: b.Artifacts}
					if continuationContext != nil {
						receipt.Text = continuationContext.Text
						receipt.ContextSource = continuationContext.ContextSource
						receipt.ContextHash = continuationContext.ContextHash
						receipt.HistoryOmitted = continuationContext.HistoryOmitted
						receipt.SubmittedHash = hashTerminalText(b.Text)
					}
					if err = s.terminal.writeInputReceipt(se.ID, *receipt); err != nil {
						http.Error(w, "input receipt could not be persisted; nothing sent: "+err.Error(), http.StatusInternalServerError)
						return
					}
				}
				if b.Supervise {
					wait := time.Duration(b.TimeoutMS) * time.Millisecond
					if wait <= 0 {
						wait = 30 * time.Second
					}
					_, err = s.terminal.herdr.Prompt(r.Context(), se.Runtime, b.Text, terminalWait{State: "settled", Timeout: wait})
					if err == nil {
						s.codingResultSweep()
					}
				} else {
					err = s.terminal.herdr.SendText(r.Context(), se.Runtime, b.Text)
				}
			}
		}
		if err != nil {
			if receipt != nil {
				writeTerminalInputReceipt(w, se.ID, *receipt)
				return
			}
			http.Error(w, "send outcome: "+err.Error()+"; no automatic retry", terminalLaunchStatus(err))
			return
		}
		if receipt != nil {
			receipt.State = "sent"
			receipt.Updated = time.Now().UTC().Format(time.RFC3339Nano)
			if err = s.terminal.writeInputReceipt(se.ID, *receipt); err != nil {
				http.Error(w, "input submitted but receipt finalization failed; check this request before sending again", http.StatusInternalServerError)
				return
			}
		}
		se.LastUsed = time.Now().Format(time.RFC3339)
		if _, err = s.terminal.updateTermMetadata(se.ID, func(row *termSession) { row.LastUsed = se.LastUsed }); err != nil {
			http.Error(w, "input sent but metadata update failed; do not resend: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if receipt != nil {
			writeTerminalInputReceipt(w, se.ID, *receipt)
		} else {
			writeJSON(w, map[string]any{"ok": true, "relaunched": relaunched})
		}
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
		// A dialog is never waited out: it does not become a prompt, and
		// sending into it is destructive (see termBlockingDialog).
		if why := termBlockingDialog(lines); why != "" {
			return true, fmt.Errorf("relaunch: %s", why)
		}
		if termPromptShowing(lines) {
			break
		}
		time.Sleep(termPromptPoll)
	}
	return true, nil
}

// termPromptShowing spots the CLI's INPUT LINE among the screen tail.
//
// ⚠ A MARKER IS NOT A PROMPT. Claude Code draws `❯ ` for its input box AND
// for the highlighted row of every menu — including the trust dialog a new
// folder opens with, whose default row is `❯ No, exit`. The old prefix match
// read that as "ready", so a relaunch in a folder Claude had not seen fired
// the owner's message into a menu: the text went nowhere and the Enter after
// it chose "No, exit", so the CLI quit, the tmux died, and the message was
// lost. That is what "the session won't relaunch" looked like from the chat.
//
// So an input line is a marker with NOTHING after it. A shell prompt keeps
// the prefix rule (`user@host:~$ `) — a shell has no menus to confuse it.
func termPromptShowing(lines []string) bool {
	last := ""
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(strings.TrimLeft(lines[i], " │┃|"))
		if ln == "" {
			continue
		}
		if last == "" {
			last = ln
		}
		// the CLI's input box: a marker with nothing typed after it. It is not
		// the bottom line — the mode footer sits below it — so this scans.
		for _, p := range []string{">", "❯", "›"} {
			if ln == p {
				return true
			}
		}
	}
	// a shell: the BOTTOM line ends in its prompt character
	return strings.HasSuffix(last, "$") || strings.HasSuffix(last, "#")
}

// termBlockingDialog names the modal the pane is sitting on, or "" when it is
// not sitting on one. These are questions only a person can answer, and the
// answer is one keystroke in the Terminal tab — so the send refuses in words
// rather than pressing Enter on the owner's behalf.
func termBlockingDialog(lines []string) string {
	joined := strings.ToLower(strings.Join(lines, "\n"))
	switch {
	case strings.Contains(joined, "do you trust the files in this folder") ||
		strings.Contains(joined, "yes, i trust this folder") ||
		strings.Contains(joined, "do you trust the contents of this directory"):
		return "Claude Code is asking whether this folder is trusted — open the session in TERMINAL once and answer it; nothing was sent"
	case strings.Contains(joined, "enter to confirm") && strings.Contains(joined, "esc to cancel"):
		return "the session is waiting on a dialog — open it in TERMINAL and answer it; nothing was sent"
	}
	return ""
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
	if se.backend() != "tmux" && se.backend() != "herdr" {
		http.Error(w, "unsupported terminal backend", http.StatusServiceUnavailable)
		return termSession{}, false
	}
	return se, true
}
