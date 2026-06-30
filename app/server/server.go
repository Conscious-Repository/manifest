package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"

	"manifest/agents"
	"manifest/calendar"
	"manifest/daily"
	"manifest/goals"
)

//go:embed web
var webFiles embed.FS

type Server struct {
	svc    *daily.Service
	goals  *goals.Store
	cal    *calendar.Client
	agents *agents.Queue
}

func New(svc *daily.Service, gs *goals.Store, cal *calendar.Client, q *agents.Queue) *Server {
	return &Server{svc: svc, goals: gs, cal: cal, agents: q}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Daily manifest.
	mux.HandleFunc("/api/day", s.handleDay)
	mux.HandleFunc("/api/day/pull", s.handleDayPull)

	// Goals system (M1). /api/goals is now the read projection; the old
	// period-note POST routes are retired in favor of structured editing.
	mux.HandleFunc("/api/goals", s.handleGoalsGet)
	mux.HandleFunc("/api/myplate", s.handleMyPlate)
	mux.HandleFunc("/api/areas", s.handleAreas)
	mux.HandleFunc("/api/areas/reorder", s.handleAreasReorder)
	mux.HandleFunc("/api/goals/item", s.handleGoalItem)
	mux.HandleFunc("/api/goals/check", s.handleGoalCheck)
	mux.HandleFunc("/api/goals/reorder", s.handleGoalsReorder)

	// Google Calendar (M3, read-only).
	mux.HandleFunc("/api/calendar/status", s.handleCalStatus)
	mux.HandleFunc("/api/calendar/events", s.handleCalEvents)
	mux.HandleFunc("/api/calendar/connect", s.handleCalConnect)
	mux.HandleFunc("/api/calendar/disconnect", s.handleCalDisconnect)

	// Agents (M4).
	mux.HandleFunc("/api/agents/status", s.handleAgentsStatus)
	mux.HandleFunc("/api/agents/post", s.handleAgentsPost)
	mux.HandleFunc("/api/agents/approvals/confirm", s.handleApprovalConfirm)
	mux.HandleFunc("/api/agents/approvals/reject", s.handleApprovalReject)

	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", noCache(http.FileServer(http.FS(sub))))
	return mux
}

// noCache makes the browser revalidate the embedded assets every load. embed.FS
// files have a zero modtime (no Last-Modified/ETag), so without this a rebuilt
// app.js/style.css can stay cached and the UI looks stale after an upgrade.
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	switch r.Method {
	case http.MethodGet:
		day, err := s.svc.Load(date)
		if err != nil {
			httpError(w, err)
			return
		}
		s.fillPool(&day)
		writeJSON(w, day)
	case http.MethodPost:
		var body struct {
			Schedule []daily.ScheduleRow `json:"schedule"`
			Tasks    []daily.Task        `json:"tasks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, err)
			return
		}
		if err := s.svc.SaveDay(date, body.Schedule, body.Tasks); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// fillPool attaches the 30-day me pool to an unplanned day so the UI can offer
// quick-add chips. Planned days carry no pool.
func (s *Server) fillPool(day *daily.Day) {
	if !day.Unplanned {
		return
	}
	for _, it := range s.goals.Pool() {
		day.Pool = append(day.Pool, daily.PoolItem{GoalID: it.GoalID, Text: it.Text, Area: it.Area})
	}
}

// handleDayPull pulls a 30-day goal into the day as a [goal:: id]-linked task.
// The goal is promoted (durable id) but never auto-checked.
func (s *Server) handleDayPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	date := r.URL.Query().Get("date")
	var b struct {
		GoalID string `json:"goalId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpError(w, err)
		return
	}
	text, gid, ok := s.goals.Promote(b.GoalID)
	if !ok {
		http.Error(w, "goal not found", http.StatusNotFound)
		return
	}
	day, err := s.svc.AddTask(date, daily.Task{Text: text, GoalID: gid})
	if err != nil {
		httpError(w, err)
		return
	}
	s.fillPool(&day)
	writeJSON(w, day)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}
