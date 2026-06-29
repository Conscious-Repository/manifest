package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"

	"manifest/daily"
	"manifest/goals"
)

//go:embed web
var webFiles embed.FS

type Server struct {
	svc   *daily.Service
	goals *goals.Store
}

func New(svc *daily.Service, gs *goals.Store) *Server {
	return &Server{svc: svc, goals: gs}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Daily manifest.
	mux.HandleFunc("/api/day", s.handleDay)

	// Goals system (M1). /api/goals is now the read projection; the old
	// period-note POST routes are retired in favor of structured editing.
	mux.HandleFunc("/api/goals", s.handleGoalsGet)
	mux.HandleFunc("/api/myplate", s.handleMyPlate)
	mux.HandleFunc("/api/areas", s.handleAreas)
	mux.HandleFunc("/api/areas/reorder", s.handleAreasReorder)
	mux.HandleFunc("/api/goals/item", s.handleGoalItem)
	mux.HandleFunc("/api/goals/check", s.handleGoalCheck)
	mux.HandleFunc("/api/goals/reorder", s.handleGoalsReorder)

	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}
