package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"sync"

	"manifest/approvals"
	"manifest/calendar"
	"manifest/daily"
	"manifest/feed"
	"manifest/goals"
	"manifest/hermes"
	"manifest/profiles"
	"manifest/spirits"
	"manifest/vaultwriter"
)

//go:embed web
var webFiles embed.FS

type Server struct {
	svc   *daily.Service
	goals *goals.Store
	cal   *calendar.Client
	// Agents cockpit (Hermes). All nilable; unset just disables that surface.
	hermes    *hermes.Client
	profiles  *profiles.Store
	feed      *feed.Store
	approvals *approvals.Store
	vault     *vaultwriter.Writer
	// Excalibur harness (SPIRITS tab) — read side of the sibling tree; nilable.
	spirits *spirits.Store
	// Async on-demand agent runs (see runs.go). Populated lazily by startRun.
	runsMu sync.Mutex
	runs   map[string]*runState
	runSeq int
}

func New(svc *daily.Service, gs *goals.Store, cal *calendar.Client) *Server {
	return &Server{svc: svc, goals: gs, cal: cal}
}

// UseHermes wires the Hermes client (console + jobs/sessions proxy + materialization).
func (s *Server) UseHermes(h *hermes.Client) { s.hermes = h }

// UseProfiles / UseFeed / UseApprovals / UseVault wire the agent stores. All optional.
func (s *Server) UseProfiles(p *profiles.Store)   { s.profiles = p }
func (s *Server) UseFeed(f *feed.Store)           { s.feed = f }
func (s *Server) UseApprovals(a *approvals.Store) { s.approvals = a }
func (s *Server) UseVault(v *vaultwriter.Writer)  { s.vault = v }

// UseSpirits wires the excalibur harness tree (SPIRITS tab).
func (s *Server) UseSpirits(sp *spirits.Store) { s.spirits = sp }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Daily manifest.
	mux.HandleFunc("/api/day", s.handleDay)
	mux.HandleFunc("/api/day/pull", s.handleDayPull)
	mux.HandleFunc("/api/day/focus", s.handleDayFocus)
	mux.HandleFunc("/api/day/focus/milestone", s.handleDayFocusMilestone)

	// Goals system (M1). /api/goals is now the read projection; the old
	// period-note POST routes are retired in favor of structured editing.
	mux.HandleFunc("/api/goals", s.handleGoalsGet)
	mux.HandleFunc("/api/myplate", s.handleMyPlate)
	mux.HandleFunc("/api/areas", s.handleAreas)
	mux.HandleFunc("/api/areas/reorder", s.handleAreasReorder)
	mux.HandleFunc("/api/goals/item", s.handleGoalItem)
	mux.HandleFunc("/api/goals/check", s.handleGoalCheck)
	mux.HandleFunc("/api/goals/reorder", s.handleGoalsReorder)
	mux.HandleFunc("/api/goals/close", s.handleGoalClose)     // close a Rock Win/Learn → archive
	mux.HandleFunc("/api/goals/archives", s.handleGoalsArchives) // History view
	mux.HandleFunc("/api/goals/carry", s.handleGoalCarry)     // quarterly review: carry a Rock
	mux.HandleFunc("/api/goals/retro", s.handleGoalRetro)     // quarterly review: save the retro

	// Google Calendar (M3, read-only).
	mux.HandleFunc("/api/calendar/status", s.handleCalStatus)
	mux.HandleFunc("/api/calendar/events", s.handleCalEvents)
	mux.HandleFunc("/api/calendar/connect", s.handleCalConnect)
	mux.HandleFunc("/api/calendar/disconnect", s.handleCalDisconnect)

	// Hermes cockpit — everything proxies through here; the API key stays server-side.
	// Console (Step 1)
	mux.HandleFunc("/api/hermes/status", s.handleHermesStatus)
	mux.HandleFunc("/api/hermes/skills", s.handleHermesSkills)
	mux.HandleFunc("/api/hermes/toolsets", s.handleHermesToolsets)
	mux.HandleFunc("/api/hermes/chat", s.handleHermesChat)
	// Profiles (Step 2)
	mux.HandleFunc("GET /api/agents/profiles", s.handleProfilesList)
	mux.HandleFunc("POST /api/agents/profiles", s.handleProfileSave)
	mux.HandleFunc("DELETE /api/agents/profiles/{name}", s.handleProfileDelete)
	// Feed (Step 3)
	mux.HandleFunc("GET /api/feed", s.handleFeedList)
	mux.HandleFunc("POST /api/feed/refresh", s.handleFeedRefresh)
	mux.HandleFunc("POST /api/feed/run", s.handleFeedRun)
	mux.HandleFunc("POST /api/feed/backfill", s.handleFeedBackfill)
	mux.HandleFunc("POST /api/feed/{id}/status", s.handleFeedStatus)
	mux.HandleFunc("POST /api/feed/{id}/save-to-vault", s.handleFeedSaveToVault)
	// Cron + observability (Step 4)
	mux.HandleFunc("GET /api/jobs", s.handleJobsList)
	mux.HandleFunc("POST /api/jobs", s.handleJobCreate)
	mux.HandleFunc("PATCH /api/jobs/{id}", s.handleJobUpdate)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleJobDelete)
	mux.HandleFunc("GET /api/agents/sessions", s.handleSessionsList)
	// Async run status — the frontend polls this after kicking off a scan/draft.
	mux.HandleFunc("GET /api/agents/runs/{id}", s.handleRunStatus)
	// Approvals (Step 5) — record-only gate
	mux.HandleFunc("GET /api/agents/approvals", s.handleApprovalsList)
	mux.HandleFunc("POST /api/agents/approvals/run", s.handleApprovalRun)
	mux.HandleFunc("POST /api/agents/approvals/{id}/confirm", s.handleApprovalConfirm)
	mux.HandleFunc("POST /api/agents/approvals/{id}/reject", s.handleApprovalReject)

	// SPIRITS — excalibur harness console (read-only + user feed actions +
	// run-now spool; the engine owns all execution).
	mux.HandleFunc("GET /api/spirits/status", s.handleSpiritsStatus)
	mux.HandleFunc("GET /api/spirits/feed", s.handleSpiritsFeedList)
	mux.HandleFunc("POST /api/spirits/feed/{id}/status", s.handleSpiritsFeedStatus)
	mux.HandleFunc("GET /api/spirits/runs", s.handleSpiritsRuns)
	mux.HandleFunc("GET /api/spirits/runs/{id}", s.handleSpiritsRun)
	mux.HandleFunc("GET /api/spirits/runs/{id}/prompt", s.handleSpiritsRunPrompt)
	mux.HandleFunc("POST /api/spirits/run-now", s.handleSpiritsRunNow)

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
		s.syncGoalTasks(body.Tasks) // §4: mirror goal-linked task ticks back into goals.md
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

// handleDayFocus sets or clears the day's Focus pick at a slot. Setting persists
// the picked 90-day goal's stable slug to the note's ## Focus block; the Milestone
// and tasks are resolved live from the cascade.
func (s *Server) handleDayFocus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	date := r.URL.Query().Get("date")
	var b struct {
		Slot   int    `json:"slot"`
		GoalID string `json:"goalId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpError(w, err)
		return
	}
	// Promote the picked goal to a durable [goal:: id] so the Focus backlink keeps
	// resolving across later title edits (same pattern as pulling a task).
	gid := b.GoalID
	if gid != "" {
		if _, durable, ok := s.goals.Promote(gid); ok {
			gid = durable
		}
	}
	day, err := s.svc.SetFocus(date, b.Slot, gid)
	if err != nil {
		httpError(w, err)
		return
	}
	s.fillPool(&day)
	writeJSON(w, day)
}

// handleDayFocusMilestone records the chosen 30-day milestone for a Focus slot, so
// the milestone and its cascading tasks resolve from that choice (not the first child).
func (s *Server) handleDayFocusMilestone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	date := r.URL.Query().Get("date")
	var b struct {
		Slot        int    `json:"slot"`
		MilestoneID string `json:"milestoneId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpError(w, err)
		return
	}
	mid := b.MilestoneID
	if mid != "" {
		if _, durable, ok := s.goals.Promote(mid); ok {
			mid = durable
		}
	}
	day, err := s.svc.SetMilestone(date, b.Slot, mid)
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
