package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"manifest/ledger"
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
		Model  string `json:"model"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	title := strings.TrimSpace(b.Title)
	if title == "" && strings.TrimSpace(b.Text) != "" {
		title = firstLine(b.Text, 60)
	}
	id, err := s.spirits.CreateChatSession(b.Spirit, title, b.Model)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	if strings.TrimSpace(b.Text) != "" {
		if err := s.spirits.SpoolChatMessage(b.Spirit, id, b.Text, "dashboard"); err != nil {
			httpError(w, err)
			return
		}
		s.ledger(ledger.Entry{Source: "chat", Kind: "chat.user", Actor: "owner",
			Object: ledger.Object{Kind: ledger.ObjSession, ID: id}, Session: id, Text: ledger.Snip(b.Text, 280)})
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
	s.ledger(ledger.Entry{Source: "chat", Kind: "chat.user", Actor: "owner",
		Object: ledger.Object{Kind: ledger.ObjSession, ID: sum.ID}, Session: sum.ID, Text: ledger.Snip(b.Text, 280)})
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

// handleChatStream is the live SSE tail of a session's event log (A2): the
// browser subscribes once per open session and receives every event the
// engine appends, seq-tagged for resume. Manifest is the file→SSE bus; the
// engine stays a pure file writer. `after=-1` means "only events from now on"
// (the transcript render already covers history); any other value replays
// from that seq first.
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	last := 0
	if a := r.URL.Query().Get("after"); a == "-1" || a == "" {
		// start from the CURRENT end — history came from the session file
		for _, raw := range s.spirits.ChatEvents(id, 0) {
			var seq struct {
				Seq int `json:"seq"`
			}
			if json.Unmarshal(raw, &seq) == nil && seq.Seq > last {
				last = seq.Seq
			}
		}
	} else if n, err := strconv.Atoi(a); err == nil {
		last = n
	}

	send := func(raw json.RawMessage) {
		var meta struct {
			Seq  int    `json:"seq"`
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &meta)
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", meta.Seq, meta.Type, raw)
		if meta.Seq > last {
			last = meta.Seq
		}
	}
	for _, raw := range s.spirits.ChatEvents(id, last) {
		send(raw)
	}
	fl.Flush()

	tick := time.NewTicker(150 * time.Millisecond)
	keep := time.NewTicker(20 * time.Second)
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
			evs := s.spirits.ChatEvents(id, last)
			if len(evs) == 0 {
				continue
			}
			for _, raw := range evs {
				send(raw)
			}
			fl.Flush()
		}
	}
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
