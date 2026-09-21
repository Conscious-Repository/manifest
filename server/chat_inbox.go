package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
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
	u := *r.URL // each part gets its own URL: Clone shares the pointer
	u.Path, u.RawQuery = path, ""
	req.URL = &u
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
	// the parts are independent and each handler already serves concurrent
	// requests, so they compose in parallel — the wall time is the slowest
	// part (the terminal registry's live probe), not the sum
	var mu sync.Mutex
	var wg sync.WaitGroup
	agents := map[string]json.RawMessage{}
	state := map[string]json.RawMessage{}
	parts := map[string]json.RawMessage{}
	part := func(store map[string]json.RawMessage, key string, h http.HandlerFunc, path string, values map[string]string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := s.inline(r, h, path, values)
			mu.Lock()
			store[key] = body
			mu.Unlock()
		}()
	}
	for _, a := range rosterBody.Agents {
		if a.Backend == "terminal" || a.Name == "" {
			continue
		}
		part(agents, a.Name, s.portalChatRoute(s.handlePortalChatSessions, s.handleAgentChatSessions), "/api/agents/chat/"+a.Name+"/sessions", map[string]string{"agent": a.Name})
	}
	for _, slot := range []string{"pins", "lifecycle", "workstreams", "seen"} {
		part(state, slot, s.handleChatState, "/api/chat/state/inbox/"+slot, map[string]string{"key": "inbox", "slot": slot})
	}
	part(parts, "spirits", s.handleChatSessions, "/api/chat/sessions", nil)
	part(parts, "terminal", s.handleTermSessions, "/api/terminal/sessions", nil)
	part(parts, "review", s.handleChatReviewStatus, "/api/chat/review-status", nil)
	part(parts, "taskThreads", s.handleTaskThreads, "/api/tasks/threads", nil)
	wg.Wait()
	writeJSON(w, map[string]any{
		"at":          time.Now().UTC().Format(time.RFC3339),
		"roster":      roster,
		"agents":      agents,
		"spirits":     parts["spirits"],
		"terminal":    parts["terminal"],
		"state":       state,
		"review":      parts["review"],
		"taskThreads": parts["taskThreads"],
	})
}
