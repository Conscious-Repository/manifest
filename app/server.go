package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
)

//go:embed web
var webFiles embed.FS

type Server struct{ store *Store }

func NewServer(store *Store) *Server { return &Server{store: store} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/day", s.handleDay)
	mux.HandleFunc("/api/goals", s.handleGoals)
	mux.HandleFunc("/api/milestones", s.handleMilestones)

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
		day, err := s.store.Load(date)
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, day)
	case http.MethodPost:
		var body struct {
			Schedule []ScheduleRow `json:"schedule"`
			Tasks    []Task        `json:"tasks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpError(w, err)
			return
		}
		if err := s.store.SaveDay(date, body.Schedule, body.Tasks); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request) {
	s.handleList(w, r, s.store.SaveGoals)
}

func (s *Server) handleMilestones(w http.ResponseWriter, r *http.Request) {
	s.handleList(w, r, s.store.SaveMilestones)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, save func(string, []string) error) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	date := r.URL.Query().Get("date")
	var body struct {
		Items []string `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, err)
		return
	}
	if err := save(date, body.Items); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}
