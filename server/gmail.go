package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"manifest/gmailauth"
)

func (s *Server) UseGmail(g *gmailauth.Client) { s.gmail = g }

// gmailPortalRow surfaces the Gmail read-only connection in the Portals panel.
// Like HeyPocket it is engine-managed (the excalibur ea-coordinator digest and
// email-sync read the tokens), but the connect is an OAuth flow manifest owns —
// so the row is kind:"oauth" and the frontend wires its account panel to
// /api/gmail/*. Multi-account: every connected mailbox is listed; per-account
// sync/extract/workspace routing lives in the accounts panel.
func (s *Server) gmailPortalRow() panelRow {
	row := panelRow{ID: "gmail", Name: "Gmail (email-sync + EA digest)", Kind: "oauth", Engine: true, Masked: "oauth"}
	if s.gmail == nil {
		row.State, row.Note = "sealed", "not enabled"
		return row
	}
	st := s.gmail.Status(time.Now())
	accounts := s.gmail.Accounts(time.Now())
	for _, a := range accounts {
		row.Accounts = append(row.Accounts, a.Email)
	}
	switch {
	case !st.HasCreds:
		row.State, row.Note = "sealed", "add ~/.config/manifest/google_credentials.json, then reconnect"
	case st.NeedsReauth:
		row.State = "degraded"
		row.Err = "sign-in expired — reconnect to restore the waiting-on digest"
	case st.Connected, st.HasToken:
		row.State = "open"
		note := "read-only · email-sync + waiting-on digest"
		if n := len(accounts); n > 1 {
			note += " · " + fmt.Sprintf("%d accounts", n)
		}
		row.Note = note
	default:
		row.State, row.Note = "sealed", "connect a Google account (read-only) for email-sync + the waiting-on digest"
	}
	return row
}

// handleGmailAccounts lists every connected account with its effective
// sync/extract/workspace settings (the Portals accounts panel).
func (s *Server) handleGmailAccounts(w http.ResponseWriter, r *http.Request) {
	if s.gmail == nil {
		writeJSON(w, map[string]any{"accounts": []any{}})
		return
	}
	writeJSON(w, map[string]any{"accounts": s.gmail.Accounts(time.Now())})
}

// handleGmailAccountSet persists one account's routing settings.
func (s *Server) handleGmailAccountSet(w http.ResponseWriter, r *http.Request) {
	if s.gmail == nil {
		httpError(w, errBadRequest("gmail not enabled"))
		return
	}
	var b struct {
		Email     string `json:"email"`
		Sync      bool   `json:"sync"`
		Extract   bool   `json:"extract"`
		Workspace string `json:"workspace"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.gmail.SetAccount(b.Email, b.Sync, b.Extract, b.Workspace); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]any{"accounts": s.gmail.Accounts(time.Now())})
}

// handleGmailConnectStart begins the paste-back OAuth flow (manifest runs
// headless — the loopback listener can't reach the owner's browser).
func (s *Server) handleGmailConnectStart(w http.ResponseWriter, r *http.Request) {
	if s.gmail == nil {
		httpError(w, errBadRequest("gmail not enabled"))
		return
	}
	authURL, err := s.gmail.StartConnect()
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"authUrl": authURL})
}

// handleGmailConnectFinish completes the paste-back flow from the redirect URL
// the owner pasted.
func (s *Server) handleGmailConnectFinish(w http.ResponseWriter, r *http.Request) {
	if s.gmail == nil {
		httpError(w, errBadRequest("gmail not enabled"))
		return
	}
	var b struct {
		Redirect string `json:"redirect"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	email, primary, err := s.gmail.FinishConnect(ctx, b.Redirect)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]any{"connected": email, "primary": primary,
		"accounts": s.gmail.Accounts(time.Now())})
}

// handleGmailAccountDisconnect removes one account (primary or extra).
func (s *Server) handleGmailAccountDisconnect(w http.ResponseWriter, r *http.Request) {
	if s.gmail == nil {
		httpError(w, errBadRequest("gmail not enabled"))
		return
	}
	var b struct {
		Email string `json:"email"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.gmail.DisconnectAccount(b.Email); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"accounts": s.gmail.Accounts(time.Now())})
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
