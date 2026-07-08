package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"

	"manifest/approvals"
	"manifest/calendar"
	"manifest/contacts"
	"manifest/daily"
	"manifest/goals"
	"manifest/reading"
	"manifest/spirits"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

//go:embed web
var webFiles embed.FS

type Server struct {
	svc   *daily.Service
	goals *goals.Store
	cal   *calendar.Client
	// Excalibur harness (SPIRITS tab) + the surfaces it drives. All nilable.
	approvals *approvals.Store // the one inbox: excalibur/artifacts/approvals
	vault     *vaultwriter.Writer
	spirits   *spirits.Store
	// Read-only headless-Dataview index over the whole vault (M0). Nilable.
	index *vaultindex.Index
	// Contacts (people layer) over the index. Nilable.
	contacts *contacts.Service
	// Reading (book shelf) over the extrinsic zone. Nilable.
	reading           *reading.Service
	extrinsicRootName string // where "+ book" creates records (default "extrinsic")
}

func New(svc *daily.Service, gs *goals.Store, cal *calendar.Client) *Server {
	return &Server{svc: svc, goals: gs, cal: cal}
}

// UseApprovals / UseVault / UseSpirits wire the excalibur surfaces. All optional.
func (s *Server) UseApprovals(a *approvals.Store) { s.approvals = a }
func (s *Server) UseVault(v *vaultwriter.Writer)  { s.vault = v }

// UseSpirits wires the excalibur harness tree (SPIRITS tab).
func (s *Server) UseSpirits(sp *spirits.Store) { s.spirits = sp }

// UseIndex wires the read-only vault index (contacts + query surfaces).
func (s *Server) UseIndex(ix *vaultindex.Index) { s.index = ix }

// UseContacts wires the people layer (CONTACTS tab).
func (s *Server) UseContacts(c *contacts.Service) { s.contacts = c }

// UseReading wires the book shelf (READING tab). extrinsicRoot is where the
// "+ book" action creates new records.
func (s *Server) UseReading(r *reading.Service, extrinsicRoot string) {
	s.reading = r
	s.extrinsicRootName = extrinsicRoot
}

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
	mux.HandleFunc("/api/goals/close", s.handleGoalClose)        // close a Rock Win/Learn → archive
	mux.HandleFunc("/api/goals/archives", s.handleGoalsArchives) // History view
	mux.HandleFunc("/api/goals/carry", s.handleGoalCarry)        // quarterly review: carry a Rock
	mux.HandleFunc("/api/goals/retro", s.handleGoalRetro)        // quarterly review: save the retro

	// Google Calendar (M3, read-only).
	mux.HandleFunc("/api/calendar/status", s.handleCalStatus)
	mux.HandleFunc("/api/calendar/events", s.handleCalEvents)
	mux.HandleFunc("/api/calendar/connect", s.handleCalConnect)
	mux.HandleFunc("/api/calendar/disconnect", s.handleCalDisconnect)

	// SPIRITS — the excalibur harness console. Read-only over the sibling tree
	// plus record-only user actions (feed keep/discard/snooze, approvals
	// confirm/reject, save-to-vault) and the run-now spool. The engine owns all
	// execution. (This replaces the retired Hermes cockpit — plan §2.5.)
	mux.HandleFunc("GET /api/spirits/status", s.handleSpiritsStatus)
	mux.HandleFunc("GET /api/spirits/runs", s.handleSpiritsRuns)
	mux.HandleFunc("GET /api/spirits/runs/{id}", s.handleSpiritsRun)
	mux.HandleFunc("GET /api/spirits/runs/{id}/prompt", s.handleSpiritsRunPrompt)
	mux.HandleFunc("GET /api/spirits/approvals", s.handleSpiritsApprovals)
	mux.HandleFunc("POST /api/spirits/approvals/{id}/confirm", s.handleSpiritsApprovalConfirm)
	mux.HandleFunc("POST /api/spirits/approvals/{id}/reject", s.handleSpiritsApprovalReject)
	mux.HandleFunc("POST /api/spirits/run-now", s.handleSpiritsRunNow)
	mux.HandleFunc("GET /api/spirits/castables", s.handleSpiritsCastables) // command-bar catalog
	// RITUALS board + in-app markdown editing (spirits-console-upgrade).
	mux.HandleFunc("GET /api/spirits/rituals", s.handleSpiritsRituals)
	mux.HandleFunc("GET /api/spirits/file", s.handleSpiritsFileGet)
	mux.HandleFunc("PUT /api/spirits/file", s.handleSpiritsFilePut)
	mux.HandleFunc("POST /api/spirits/ritual", s.handleSpiritsNewRitual)
	mux.HandleFunc("POST /api/spirits/spirit", s.handleSpiritsNewSpirit)

	// CONTACTS — the people layer over the vault index (plans/contacts-feature.md).
	// Reads are the graph; the only writes are explicit user actions (create a
	// person note, bind an alias, confirm an email).
	mux.HandleFunc("GET /api/contacts", s.handleContactsList)
	mux.HandleFunc("GET /api/contacts/triage", s.handleContactsTriage)
	mux.HandleFunc("GET /api/contacts/page", s.handleContactPage)
	mux.HandleFunc("GET /api/contacts/card", s.handleContactCard)
	mux.HandleFunc("GET /api/contacts/search", s.handleContactsSearch)
	mux.HandleFunc("POST /api/contacts/confirm", s.handleContactsConfirm)
	mux.HandleFunc("POST /api/contacts/dismiss", s.handleContactsDismiss)
	mux.HandleFunc("POST /api/contacts/dismiss-bulk", s.handleContactsDismissBulk)
	mux.HandleFunc("POST /api/contacts/org", s.handleContactsOrg)
	mux.HandleFunc("POST /api/contacts/bind", s.handleContactsBind)
	mux.HandleFunc("POST /api/contacts/note", s.handleContactsNote)
	mux.HandleFunc("POST /api/contacts/email", s.handleContactsEmail)
	mux.HandleFunc("GET /api/contacts/email-review", s.handleContactsEmailReview)
	mux.HandleFunc("POST /api/contacts/email-dismiss", s.handleContactsEmailDismiss)

	// FEED — manifest's one inbox, a first-class surface (feed-central §1).
	// Spirit items + (later) app signals and virtual proposal cards. The old
	// /api/spirits/feed* routes are gone — single user, no compat shims.
	mux.HandleFunc("GET /api/feed", s.handleFeedList)
	mux.HandleFunc("GET /api/feed/badge", s.handleFeedBadge)
	mux.HandleFunc("POST /api/feed/{id}/status", s.handleFeedStatus)
	mux.HandleFunc("POST /api/feed/{id}/save-to-vault", s.handleFeedSaveToVault)

	// READING — the book shelf over the extrinsic zone (reading-plan §3).
	mux.HandleFunc("GET /api/reading", s.handleReadingList)
	mux.HandleFunc("POST /api/reading/book", s.handleReadingCreate)
	mux.HandleFunc("POST /api/reading/finish", s.handleReadingFinish)
	mux.HandleFunc("POST /api/reading/rating", s.handleReadingRating)

	// Universal note view + edits (contacts power-pass §1).
	mux.HandleFunc("GET /api/note", s.handleNoteGet)
	mux.HandleFunc("PUT /api/note", s.handleNotePut)
	mux.HandleFunc("POST /api/note/task", s.handleNoteTask)
	mux.HandleFunc("GET /api/note/resolve", s.handleNoteResolve)

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
