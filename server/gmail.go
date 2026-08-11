package server

import (
	"context"
	"net/http"
	"time"

	"manifest/gmailauth"
)

func (s *Server) UseGmail(g *gmailauth.Client) { s.gmail = g }

// gmailPortalRow surfaces the Gmail read-only connection in the Portals panel.
// Like HeyPocket it is engine-managed (the excalibur ea-coordinator digest reads
// the token), but the reconnect is an OAuth flow manifest owns — so the row is
// kind:"oauth" and the frontend wires its connect/disconnect to /api/gmail/*.
func (s *Server) gmailPortalRow() panelRow {
	row := panelRow{ID: "gmail", Name: "Gmail (EA digest)", Kind: "oauth", Engine: true, Masked: "oauth"}
	if s.gmail == nil {
		row.State, row.Note = "sealed", "not enabled"
		return row
	}
	st := s.gmail.Status(time.Now())
	switch {
	case !st.HasCreds:
		row.State, row.Note = "sealed", "add ~/.config/manifest/google_credentials.json, then reconnect"
	case st.NeedsReauth:
		row.State = "degraded"
		row.Err = "sign-in expired — reconnect to restore the waiting-on digest"
		if st.Email != "" {
			row.Accounts = []string{st.Email}
		}
	case st.Connected:
		row.State = "open"
		row.Note = "read-only · feeds the engine's ea-coordinator digest"
		if st.Email != "" {
			row.Accounts = []string{st.Email}
		}
	case st.HasToken:
		// token present, first check still pending — show connected-ish, not sealed
		row.State = "open"
		row.Note = "read-only · verifying…"
		if st.Email != "" {
			row.Accounts = []string{st.Email}
		}
	default:
		row.State, row.Note = "sealed", "connect a Google account (read-only) for the waiting-on digest"
	}
	return row
}

// handleGmailStatus reports the current auth state (throttled, non-blocking).
func (s *Server) handleGmailStatus(w http.ResponseWriter, r *http.Request) {
	if s.gmail == nil {
		writeJSON(w, map[string]any{"configured": false})
		return
	}
	st := s.gmail.Status(time.Now())
	writeJSON(w, map[string]any{
		"hasCreds": st.HasCreds, "connected": st.Connected,
		"needsReauth": st.NeedsReauth, "email": st.Email, "detail": st.Detail,
	})
}

// handleGmailConnect runs the loopback OAuth flow (opens the browser) and writes
// the engine's token. Blocks up to 3 minutes on the user's consent, like the
// calendar connect.
func (s *Server) handleGmailConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.gmail == nil {
		httpError(w, errBadRequest("gmail not enabled"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	email, err := s.gmail.Connect(ctx)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"connected": email})
}

// handleGmailDisconnect removes the engine's gmail token.
func (s *Server) handleGmailDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.gmail == nil {
		httpError(w, errBadRequest("gmail not enabled"))
		return
	}
	if err := s.gmail.Disconnect(); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
