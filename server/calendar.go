package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

func (s *Server) handleCalStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"configured": s.cal.Enabled(),
		"needsAuth":  s.cal.NeedsAuth(),
		"hasCreds":   s.cal.HasCreds(),
		"accounts":   s.cal.Accounts(),
	})
}

type calEventView struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Start  string `json:"start"`
	End    string `json:"end"`
	AllDay bool   `json:"allDay"`
}

// handleCalEvents returns events in [start, end) merged across all connected
// accounts for the month/week view.
func (s *Server) handleCalEvents(w http.ResponseWriter, r *http.Request) {
	if !s.cal.Enabled() {
		writeJSON(w, map[string]any{"configured": false, "events": []calEventView{}})
		return
	}
	loc := s.cal.Location()
	start, err1 := time.ParseInLocation("2006-01-02", r.URL.Query().Get("start"), loc)
	end, err2 := time.ParseInLocation("2006-01-02", r.URL.Query().Get("end"), loc)
	if err1 != nil || err2 != nil {
		http.Error(w, "start and end must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	events, err := s.cal.Events(ctx, start, end.AddDate(0, 0, 1))
	if err != nil {
		httpError(w, err)
		return
	}
	views := make([]calEventView, 0, len(events))
	for _, e := range events {
		views = append(views, calEventView{
			ID: e.ID, Title: e.Title, AllDay: e.AllDay,
			Start: e.Start.Format(time.RFC3339), End: e.End.Format(time.RFC3339),
		})
	}
	writeJSON(w, map[string]any{"configured": true, "events": views})
}

// handleCalConnect runs the loopback OAuth flow for ONE Google account (the
// browser account chooser lets you pick a different account each time), then adds
// it. Safe to call repeatedly to connect multiple accounts.
func (s *Server) handleCalConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	email, err := s.cal.AddAccount(ctx)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"connected": email, "accounts": s.cal.Accounts()})
}

// handleCalDisconnect removes one account (by email).
func (s *Server) handleCalDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var b struct {
		Account string `json:"account"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpError(w, err)
		return
	}
	if b.Account == "" {
		http.Error(w, "account is required", http.StatusBadRequest)
		return
	}
	if err := s.cal.RemoveAccount(b.Account); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"accounts": s.cal.Accounts()})
}
