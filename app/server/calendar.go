package server

import (
	"context"
	"net/http"
	"time"

	"manifest/calendar"
)

func (s *Server) handleCalStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{
		"configured": s.cal.Enabled(),
		"needsAuth":  s.cal.NeedsAuth(),
	})
}

type calEventView struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Start  string `json:"start"`
	End    string `json:"end"`
	AllDay bool   `json:"allDay"`
}

// handleCalEvents returns raw events in [start, end) for the month/week view.
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
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
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

// handleCalConnect runs the installed-app loopback OAuth flow (opens the browser
// and waits for the callback), then refreshes the client.
func (s *Server) handleCalConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	if err := calendar.Authorize(ctx); err != nil {
		httpError(w, err)
		return
	}
	s.cal.Reset()
	writeJSON(w, map[string]bool{"configured": s.cal.Enabled()})
}

func (s *Server) handleCalDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := calendar.Disconnect(); err != nil {
		httpError(w, err)
		return
	}
	s.cal.Reset()
	writeJSON(w, map[string]bool{"configured": false})
}
