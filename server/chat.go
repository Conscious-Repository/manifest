package server

import (
	"net/http"
	"strconv"
	"strings"
)

// Chat (cmd-ctr import P2): the dashboard's conversational surface over
// chattable spirits. Every handler is a thin file read / spool write on the
// PRIMARY harness — the engine owns the turns. The events endpoint is the
// transport seam a future phone bridge (or SSE upgrade) drives; its `after`
// cursor makes delivery resumable.

func (s *Server) handleChatSpirits(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"spirits": []any{}})
		return
	}
	writeJSON(w, map[string]any{"spirits": s.spirits.ChatSpirits()})
}

func (s *Server) handleChatSessions(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"sessions": []any{}})
		return
	}
	writeJSON(w, map[string]any{"sessions": s.spirits.ChatSessions()})
}

// handleChatSessionCreate creates a session and (optionally) spools the first
// message in one call.
func (s *Server) handleChatSessionCreate(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Spirit string `json:"spirit"`
		Title  string `json:"title"`
		Text   string `json:"text"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	title := strings.TrimSpace(b.Title)
	if title == "" && strings.TrimSpace(b.Text) != "" {
		title = firstLine(b.Text, 60)
	}
	id, err := s.spirits.CreateChatSession(b.Spirit, title)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	if strings.TrimSpace(b.Text) != "" {
		if err := s.spirits.SpoolChatMessage(b.Spirit, id, b.Text, "dashboard"); err != nil {
			httpError(w, err)
			return
		}
	}
	writeJSON(w, map[string]any{"id": id})
}

func (s *Server) handleChatSession(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	sum, body, ok := s.spirits.ChatSession(r.PathValue("id"))
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{
		"session": sum, "body": body,
		"queued": s.spirits.QueuedChatMessages(sum.ID),
	})
}

func (s *Server) handleChatMessage(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Text   string `json:"text"`
		Source string `json:"source"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	sum, _, ok := s.spirits.ChatSession(r.PathValue("id"))
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	if err := s.spirits.SpoolChatMessage(sum.Spirit, sum.ID, b.Text, b.Source); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleChatRename(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Title string `json:"title"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.spirits.RenameChatSession(r.PathValue("id"), b.Title); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleChatDelete(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	if err := s.spirits.DeleteChatSession(r.PathValue("id")); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleChatEvents is the machine stream (bridge/SSE seam): events after seq.
func (s *Server) handleChatEvents(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"events": []any{}})
		return
	}
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	writeJSON(w, map[string]any{"events": s.spirits.ChatEvents(r.PathValue("id"), after)})
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}
