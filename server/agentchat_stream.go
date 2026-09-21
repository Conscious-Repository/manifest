package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"manifest/agentchat"
)

// This is a projection of the session file, not a second event store. On
// reconnect, restore the current turn-open marker for a fresh chatLive.
// IDs are connection-local; historical replies come from the session GET.
func (s *Server) handleAgentChatStream(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	agent, id := r.PathValue("agent"), r.PathValue("id")
	sess, body, _, ok := s.agentChat.store.Get(agent, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if sess.Sharing != nil {
		http.Error(w, "conversation is shared", http.StatusConflict)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	seq := 0
	send := func(kind string, data map[string]any) {
		seq++
		raw, _ := json.Marshal(map[string]any{"seq": seq, "type": kind, "data": data})
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", seq, kind, raw)
	}
	tail := agentChatTail{lastTurn: sess.Turns}
	tail.advance(sess, body, send)
	fl.Flush()
	tick, keep := time.NewTicker(150*time.Millisecond), time.NewTicker(20*time.Second)
	defer tick.Stop()
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keep.C:
			fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		case <-tick.C:
			sess, body, _, ok = s.agentChat.store.Get(agent, id)
			if !ok || sess.Sharing != nil {
				send("error", map[string]any{"error": "conversation unavailable"})
				fl.Flush()
				return
			}
			tail.advance(sess, body, send)
			fl.Flush()
		}
	}
}

type agentChatTail struct {
	lastTurn int
	open     bool
}

func (t *agentChatTail) advance(sess agentchat.Session, body string, send func(string, map[string]any)) {
	start := func() {
		if !t.open {
			send("turn.started", map[string]any{})
			t.open = true
		}
	}
	for _, turn := range agentchat.ParseTurns(body) {
		if turn.N <= t.lastTurn {
			continue
		}
		t.lastTurn = turn.N
		if turn.Who == "user" {
			send("user.message", map[string]any{})
			continue
		}
		start()
		if turn.Who == "system" {
			send("error", map[string]any{"error": turn.Text})
			send("session.error", map[string]any{"error": turn.Text})
		} else {
			agentChatStepEvents(turn.Text, send)
		}
		send("turn.completed", map[string]any{})
		t.open = false
	}
	if sess.Status == agentchat.StatusThinking {
		start()
	} else if t.open {
		send("turn.completed", map[string]any{})
		t.open = false
	}
}

var agentChatStepRE = regexp.MustCompile(`(?m)^### Step \d+ — ([^\n]+)\n`)
var agentChatTokensRE = regexp.MustCompile(`(?m)^- tokens: (\d+)$`)
var agentChatDetailRE = regexp.MustCompile(`(?m)^- (?:rationale|result): (.*)$`)

func agentChatStepEvents(text string, send func(string, map[string]any)) {
	steps := agentChatStepRE.FindAllStringSubmatchIndex(text, -1)
	if len(steps) == 0 {
		send("assistant.message", map[string]any{"text": text})
		return
	}
	for i, step := range steps {
		end := len(text)
		if i+1 < len(steps) {
			end = steps[i+1][0]
		}
		cast, body := strings.TrimSpace(text[step[2]:step[3]]), strings.TrimSpace(text[step[1]:end])
		switch cast {
		case "say":
			send("assistant.delta", map[string]any{"text": body})
		case "thinking":
			if m := agentChatTokensRE.FindStringSubmatch(body); m != nil {
				n, _ := strconv.Atoi(m[1])
				send("thinking.tokens", map[string]any{"tokens": n})
				body = strings.TrimSpace(agentChatTokensRE.ReplaceAllString(body, ""))
			}
			if body != "" {
				send("thinking.delta", map[string]any{"text": body})
			}
		default:
			detail := ""
			if m := agentChatDetailRE.FindStringSubmatch(body); m != nil {
				detail = m[1]
			}
			send("tool.started", map[string]any{"cast": cast, "rationale": detail})
			send("tool.completed", map[string]any{"cast": cast, "summary": detail})
		}
	}
}
