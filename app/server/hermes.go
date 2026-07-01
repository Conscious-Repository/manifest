package server

import (
	"net/http"

	"manifest/hermes"
)

// The Hermes surface is deliberately separate from the M4 /api/agents/* routes.
// The browser hits these same-origin handlers; the Go backend is the only holder
// of the API key and proxies to Hermes over Tailscale. The key never leaves here.

// handleHermesStatus reports whether Hermes is configured and (cheaply) reachable,
// so the console can show a connected / needs-setup banner. No secret is returned.
func (s *Server) handleHermesStatus(w http.ResponseWriter, r *http.Request) {
	if s.hermes == nil || !s.hermes.Configured() {
		writeJSON(w, map[string]any{
			"configured": false,
			"hint":       "Add your Hermes API key at ~/.config/manifest/agents/hermes_key (chmod 600), then restart.",
		})
		return
	}
	reachable := s.hermes.Health(r.Context()) == nil
	writeJSON(w, map[string]any{
		"configured": true,
		"reachable":  reachable,
		"model":      s.hermes.Model(),
		"baseURL":    s.hermes.BaseURL(),
	})
}

// handleHermesSkills proxies /v1/skills (validates the key + the {object,data} wrapper).
func (s *Server) handleHermesSkills(w http.ResponseWriter, r *http.Request) {
	if s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "hermes not configured", http.StatusServiceUnavailable)
		return
	}
	skills, err := s.hermes.ListSkills(r.Context())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"data": skills})
}

// handleHermesChat proxies a streamed chat completion: it decodes the browser's
// {messages:[…]} and re-streams Hermes's SSE straight back to the browser, flushing
// each chunk. The API key is injected server-side; the request body carries only messages.
func (s *Server) handleHermesChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "hermes not configured", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Messages []hermes.Message `json:"messages"`
		Profile  string           `json:"profile"`
	}
	if err := decode(r, &body); err != nil {
		httpError(w, err)
		return
	}
	req := hermes.ChatRequest{Messages: body.Messages}
	// Applying a profile parameterizes the call server-side: its brief becomes the
	// system message. (Model tiering is a no-op today — the box exposes one model.)
	if body.Profile != "" && s.profiles != nil {
		if p, ok := s.profiles.Get(body.Profile); ok {
			req.System = p.Brief
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// SSE headers: event stream, no caching, and disable proxy buffering so chunks
	// arrive incrementally.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flush := func() { flusher.Flush() }
	if err := s.hermes.StreamChat(r.Context(), req, w, flush); err != nil {
		// Headers/body may already be partly written; surface the error as an SSE
		// comment so the client can show it without corrupting the stream framing.
		_, _ = w.Write([]byte("\ndata: {\"error\":\"" + sseSafe(err.Error()) + "\"}\n\n"))
		flush()
	}
}

// sseSafe strips characters that would break an SSE data line / JSON string.
func sseSafe(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case '"', '\\', '\n', '\r':
			out = append(out, ' ')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
