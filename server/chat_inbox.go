package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

// ---- the CHAT inbox in one request (2026-09-21) ----
//
// Opening Chats, or moving between threads, used to fan out to a dozen
// requests before the rail could paint: the roster, one session list per
// agent, the spirit list, the terminal registry, four inbox state slots,
// review counts and the task conversations — each a round trip from a phone
// over the tailnet. This handler composes the same payloads in-process, by
// running the existing handlers against a memory response, so every list
// keeps exactly the shape and rules it has today and the client applies each
// part with the code it already has. One request, one paint.

// memResponse is the in-process ResponseWriter the snapshot composes with.
type memResponse struct {
	hdr  http.Header
	code int
	body bytes.Buffer
}

func (m *memResponse) Header() http.Header { return m.hdr }
func (m *memResponse) Write(b []byte) (int, error) {
	if m.code == 0 {
		m.code = http.StatusOK
	}
	return m.body.Write(b)
}
func (m *memResponse) WriteHeader(code int) { m.code = code }

// inline runs one handler in-process and returns its JSON body, or null when
// it did not answer 200 — the client's per-part fallbacks then apply.
func (s *Server) inline(r *http.Request, h http.HandlerFunc, path string, values map[string]string) json.RawMessage {
	req := r.Clone(r.Context())
	req.Method = http.MethodGet
	req.URL = &(*r.URL)
	req.URL.Path = path
	req.URL.RawQuery = ""
	for k, v := range values {
		req.SetPathValue(k, v)
	}
	m := &memResponse{hdr: http.Header{}}
	h(m, req)
	if m.code != http.StatusOK || !json.Valid(m.body.Bytes()) {
		return json.RawMessage("null")
	}
	return json.RawMessage(m.body.Bytes())
}

// GET /api/chat/inbox — everything the rail needs, composed once.
func (s *Server) handleChatInbox(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	roster := s.inline(r, s.handleAgentChatRoster, "/api/agents/chat/roster", nil)
	var rosterBody struct {
		Agents []struct {
			Name    string `json:"name"`
			Backend string `json:"backend"`
		} `json:"agents"`
	}
	_ = json.Unmarshal(roster, &rosterBody)
	agents := map[string]json.RawMessage{}
	for _, a := range rosterBody.Agents {
		if a.Backend == "terminal" || a.Name == "" {
			continue
		}
		agents[a.Name] = s.inline(r, s.portalChatRoute(s.handlePortalChatSessions, s.handleAgentChatSessions), "/api/agents/chat/"+a.Name+"/sessions", map[string]string{"agent": a.Name})
	}
	state := map[string]json.RawMessage{}
	for _, slot := range []string{"pins", "lifecycle", "workstreams", "seen"} {
		state[slot] = s.inline(r, s.handleChatState, "/api/chat/state/inbox/"+slot, map[string]string{"key": "inbox", "slot": slot})
	}
	writeJSON(w, map[string]any{
		"at":          time.Now().UTC().Format(time.RFC3339),
		"roster":      roster,
		"agents":      agents,
		"spirits":     s.inline(r, s.handleChatSessions, "/api/chat/sessions", nil),
		"terminal":    s.inline(r, s.handleTermSessions, "/api/terminal/sessions", nil),
		"state":       state,
		"review":      s.inline(r, s.handleChatReviewStatus, "/api/chat/review-status", nil),
		"taskThreads": s.inline(r, s.handleTaskThreads, "/api/tasks/threads", nil),
	})
}
