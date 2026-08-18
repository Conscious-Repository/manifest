package server

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/calendar"
	"manifest/capture"
	"manifest/chatthreads"
	"manifest/contacts"
	"manifest/daily"
	"manifest/errands"
	"manifest/fundraising"
	"manifest/geocode"
	"manifest/gmailauth"
	"manifest/goals"
	"manifest/ledger"
	"manifest/portals"
	"manifest/reading"
	"manifest/realestate"
	"manifest/signals"
	"manifest/spirits"
	"manifest/tasks"
	"manifest/teamportal"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

//go:embed web
var webFiles embed.FS

type Server struct {
	svc        *daily.Service
	goals      *goals.Store
	tasksStore *tasks.Store // the third surface — vault-root `tasks.md` (nilable)
	// ownerInitials identify "me" in the unified todo projection (stage 4):
	// empty/"me"/containing-these-initials owners are mine.
	ownerInitials string
	cal           *calendar.Client
	// Gmail read-only OAuth for the engine's ea-coordinator digest — manifest
	// mints/validates the token the headless engine reads. Nilable.
	gmail *gmailauth.Client
	// Excalibur harness (SPIRITS tab) + the surfaces it drives. All nilable.
	approvals *approvals.Store // the PRIMARY inbox (harness federation: first entry)
	vault     *vaultwriter.Writer
	spirits   *spirits.Store // the PRIMARY harness (write surfaces live here)
	// harnessList is the federation (big-change Phase 4), primary first —
	// runs/queued/feed/approvals merge across it, tagged by name.
	harnessList []Harness
	// hermes routes @hermes off the excalibur harness onto the owner's real
	// do-bot (the local Hermes Agent CLI). Nil → the legacy harness path. Its
	// type + logic live in hermes_delegate.go.
	hermes *hermesCfg
	// deepseekStatePath: the last explicit portal test's result (dataDir,
	// per-machine) — the DegradedPortal feed signal reads it (Phase 7).
	deepseekStatePath string
	// stickyPath: the ⌘I floating post-it file (<dataDir>/sticky.md). Nilable.
	stickyPath string
	// capture: the tray (cmd-ctr Stage — dataDir cards + media). Nilable.
	capture *capture.Store
	// sttURL/sttModel: the lab transcription endpoint the mic buttons proxy to.
	sttURL   string
	sttModel string
	// files: the FILES fleet browser (local roots + per-device agents). Nilable.
	files *filesCfg
	// terminal: the in-app PTY terminal (metis-local, tmux-persistent). Nilable.
	terminal *termCfg
	// devices: the terminal cockpit's ssh fleet (launcher targets). Nilable.
	devices *devCfg
	// activity: the cockpit's fleet vitals collector (STATS stage). Nilable.
	activity *actCfg
	// Read-only headless-Dataview index over the whole vault (M0). Nilable.
	index *vaultindex.Index
	// Contacts (people layer) over the index. Nilable.
	contacts *contacts.Service
	// Reading (book shelf) over the extrinsic zone. Nilable.
	reading           *reading.Service
	extrinsicRootName string // where "+ book" creates records (default "extrinsic")
	// Signals (app-derived FEED cards: cold contacts, stalled Rocks). Nilable.
	signals *signals.Service
	// Portals (external realms — ClickUp, Benchling — polled into the FEED). Nilable.
	portals *portals.Service
	// Real estate (PROPERTIES tab over system/realestate/ records). Nilable.
	realestate     *realestate.Service
	realestateRoot string // vault-relative records root (default "system/realestate")
	bgParcelsPath  string // <dataDir>/realestate/bgParcels.json (map background layer)
	rePortalPath   string // ooda site checkout for the deals.json publish ("" = disabled)
	reImport       *realestate.ImportMemory
	geocoder       *geocode.Service
	statements     *realestate.StatementStore
	// Errands (the action layer — aside effector; records = FEED receipts).
	// Nilable; dataDir state only, never the vault.
	errands        *errands.Store
	errandExec     *errands.Executor
	errandAccounts []string // §6 allowlist ("" = any signed-in account)
	// AION (program cockpit over system/aion/ + live team projection). Nilable.
	aion *aion.Store
	// Private Aion fundraising CRM. This is intentionally outside AionLive and
	// the portal export contract.
	fundraising     *fundraising.Store
	fundraisingSync *fundraising.SheetSync
	// aionLive is the shared vault-base + team-overlay projection served by
	// both listeners. AION has no git/deploy effector.
	aionLive *AionLive
	// Real-estate decision log (system/realestate/backlog.md — an aion.Store
	// pointed at the RE root; backlog methods ONLY). Nilable.
	re          *aion.Store
	aionDataDir string      // live cache/journal, plus legacy/RE operational records
	rePublishes *publishLog // RE publish receipts
	// aionSink receives vault-relative paths to consider for extraction —
	// the post-confirm nudge (aion.ExtractSink satisfies it). Nilable.
	aionSink interface{ Notify([]string) }
	// teamBridge surfaces AION team-portal writes as FEED notices (portal
	// move Phase 4 — same notice kind as clickup/benchling). Nilable.
	teamBridge *teamportal.Bridge
	// todoPlans: the todo-panel plan-record layer (system/todo-plans). Nilable.
	todoPlans *todoPlansCfg
	// threads: the todo-panel comment stores (private/RE-shared/aion). Nilable.
	threads *threadsCfg
	// ledgerStore: the daily shared thread — a tier-3 JSONL projection of
	// thread/chat/run/plan events (persona plan Phase 0). Nilable.
	ledgerStore *ledger.Store
	// personasCfg: intent-tagged agent response personas (persona plan Phase 1;
	// system/agents/personas/<intent>.md records). Nilable.
	personasCfg *personasCfg
	// chat: the native portal chat-with-kairos store (chat-kairos handoff;
	// shared threads on /shared/apps/aion-portal/chat). Nilable.
	chat        *chatthreads.Store
	chatSweepMu sync.Mutex // one chat sweep at a time (ticker vs read-driven)
}

// UseLedger wires the daily ledger.
func (s *Server) UseLedger(l *ledger.Store) { s.ledgerStore = l }

// ledger appends one entry, nil-safe and best-effort — call sites stay one line.
func (s *Server) ledger(e ledger.Entry) {
	if s.ledgerStore == nil {
		return
	}
	if e.TS.IsZero() {
		e.TS = time.Now()
	}
	_ = s.ledgerStore.Append(e)
}

// handleLedger serves one day's entries (default: today in the owner's tz).
func (s *Server) handleLedger(w http.ResponseWriter, r *http.Request) {
	if s.ledgerStore == nil {
		writeJSON(w, map[string]any{"date": "", "entries": []any{}, "days": []any{}})
		return
	}
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	if date == "" {
		date = s.ledgerStore.Today()
	}
	entries, err := s.ledgerStore.Day(date)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"date": date, "entries": entries, "days": s.ledgerStore.Days()})
}

// UseTeamPortal wires the team-portal → FEED notices bridge and live overlay.
func (s *Server) UseTeamPortal(b *teamportal.Bridge, st *teamportal.Store, adminEmail string) {
	s.teamBridge = b
	if s.aionLive != nil {
		s.aionLive.UseTeam(st, teamportal.Identity{Email: adminEmail, Name: "Benjamin"})
		_ = s.aionLive.recoverJournal()
	}
}

// AionLive returns the in-process portal/cockpit projection.
func (s *Server) AionLive() *AionLive { return s.aionLive }

// UseAionSink wires the extraction sink for the transcript-confirm nudge.
func (s *Server) UseAionSink(sink interface{ Notify([]string) }) { s.aionSink = sink }

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
func (s *Server) UseContacts(c *contacts.Service) { s.contacts = c; s.wireFundraisingContacts() }
func (s *Server) UseGeocoder(g *geocode.Service)  { s.geocoder = g }

// UseFundraising wires the private Manifest-only Aion CRM.
func (s *Server) UseFundraising(f *fundraising.Store)            { s.fundraising = f; s.wireFundraisingContacts() }
func (s *Server) UseFundraisingSync(sync *fundraising.SheetSync) { s.fundraisingSync = sync }

// UseReading wires the book shelf (READING tab). extrinsicRoot is where the
// "+ book" action creates new records.
func (s *Server) UseReading(r *reading.Service, extrinsicRoot string) {
	s.reading = r
	s.extrinsicRootName = extrinsicRoot
}

// UseSignals wires the app-signal emitters (FEED cards).
func (s *Server) UseSignals(sig *signals.Service) { s.signals = sig }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Daily manifest.
	mux.HandleFunc("/api/day", s.handleDay)
	mux.HandleFunc("/api/day/pull", s.handleDayPull)
	mux.HandleFunc("/api/day/capture", s.handleDayCapture)
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
	mux.HandleFunc("POST /api/goals/move", s.handleGoalMove)     // re-parent: ladder-connection edits
	mux.HandleFunc("/api/goals/close", s.handleGoalClose)        // close a Rock Win/Learn → archive
	mux.HandleFunc("/api/goals/archives", s.handleGoalsArchives) // History view
	mux.HandleFunc("/api/goals/carry", s.handleGoalCarry)        // quarterly review: carry a Rock
	mux.HandleFunc("/api/goals/retro", s.handleGoalRetro)        // quarterly review: save the retro

	// TODOS — the third surface over `tasks.md` (todos-surface-scope).
	mux.HandleFunc("GET /api/tasks", s.handleTasksGet)
	mux.HandleFunc("POST /api/tasks/item", s.handleTaskAdd)
	mux.HandleFunc("POST /api/tasks/check", s.handleTaskCheck)
	mux.HandleFunc("POST /api/tasks/update", s.handleTaskUpdate)
	mux.HandleFunc("POST /api/tasks/rank", s.handleTasksRank)                      // unified drag-to-rank (stage 4)
	mux.HandleFunc("GET /api/properties/people", s.handleRePeopleGet)              // RE assignee registry
	mux.HandleFunc("PUT /api/properties/people", s.handleRePeopleSave)
	mux.HandleFunc("POST /api/tasks/drop", s.handleTaskDrop)
	mux.HandleFunc("/api/tasks/split", s.handleTasksSplit) // GET preview · POST commit (one task substrate)
	mux.HandleFunc("POST /api/tasks/bucket", s.handleBucketRename)
	mux.HandleFunc("POST /api/tasks/issue", s.handleIssueAdd)
	mux.HandleFunc("POST /api/tasks/issue/resolve", s.handleIssueResolve)
	mux.HandleFunc("POST /api/tasks/issue/to-task", s.handleIssueToTask) // reverse conversion (+ optional tether)
	mux.HandleFunc("POST /api/tasks/to-issue", s.handleTaskToIssue)
	mux.HandleFunc("GET /api/tasks/delegate/targets", s.handleDelegateTargets) // Phase 6
	mux.HandleFunc("POST /api/tasks/delegate", s.handleDelegate)
	// TODO PANEL (todo-panel plan): record + thread + attachments.
	mux.HandleFunc("GET /api/tasks/panel", s.handleTaskPanel)
	mux.HandleFunc("POST /api/tasks/plan", s.handleTaskPlan)
	mux.HandleFunc("POST /api/tasks/assign", s.handleTaskAssign)
	mux.HandleFunc("POST /api/tasks/fire", s.handleTaskFire)
	mux.HandleFunc("GET /api/tasks/thread", s.handleTaskThreadGet)
	mux.HandleFunc("POST /api/tasks/thread", s.handleTaskThreadPost)
	mux.HandleFunc("POST /api/tasks/thread/file", s.handleTaskThreadFile)
	mux.HandleFunc("GET /api/tasks/thread/file/{hash}", s.handleTaskThreadBlob)
	mux.HandleFunc("GET /api/agents/personas", s.handlePersonas)                     // persona records (persona plan Phase 1)
	mux.HandleFunc("GET /api/harnesses", s.handleHarnesses)                          // harness settings
	mux.HandleFunc("POST /api/harnesses/spirit/portal", s.handleHarnessSpiritPortal) // switch a spirit's conduit

	// AION — program cockpit over the shared live AION projection.
	if s.aion != nil {
		mux.HandleFunc("GET /api/aion", s.handleAion)
		mux.HandleFunc("GET /api/aion/revision", func(w http.ResponseWriter, r *http.Request) {
			if s.aionLive == nil {
				writeJSON(w, map[string]any{"effectiveRevision": ""})
				return
			}
			handleAionLiveRevision(s.aionLive)(w, r)
		})
		mux.HandleFunc("PUT /api/aion/people", s.handleAionPeopleSave)
		mux.HandleFunc("PUT /api/aion/vto", s.handleAionVTOSave)
		mux.HandleFunc("PUT /api/aion/finances", s.handleAionFinancesSave)
		mux.HandleFunc("PUT /api/aion/hiring", s.handleAionHiringSave)
		mux.HandleFunc("PUT /api/aion/references", s.handleAionReferencesSave)
		mux.HandleFunc("POST /api/aion/backlog/item", s.handleAionBacklogAdd)
		mux.HandleFunc("POST /api/aion/backlog/update/{id...}", s.handleAionBacklogUpdate)
		mux.HandleFunc("POST /api/aion/backlog/delete/{id...}", s.handleAionBacklogDelete)
		mux.HandleFunc("POST /api/aion/backlog/decide/{id...}", s.handleAionBacklogDecide)
		// One-release compatibility for legacy hash ids. A single catch-all is
		// used because three {id}/action patterns conflict with the new
		// action/{id...} patterns under Go's ServeMux specificity rules.
		mux.HandleFunc("POST /api/aion/backlog/{legacy...}", s.handleAionBacklogLegacy)
		mux.HandleFunc("POST /api/aion/proposals/decide", s.handleAionProposalDecide)
		mux.HandleFunc("GET /api/aion/activity", s.handleAionCollaborationActivity)
		mux.HandleFunc("POST /api/aion/heuristics/{id}/edit", s.handleAionHeuristicEdit)
		mux.HandleFunc("POST /api/aion/heuristics/{id}/retire", s.handleAionHeuristicRetire)
		mux.HandleFunc("POST /api/aion/heuristics/merge", s.handleAionHeuristicsMerge)
		mux.HandleFunc("POST /api/aion/heuristics/reorder", s.handleAionHeuristicsReorder)
		mux.HandleFunc("GET /api/aion/fundraising", s.handleFundraisingList)
		mux.HandleFunc("POST /api/aion/fundraising/item", s.handleFundraisingCreate)
		mux.HandleFunc("POST /api/aion/fundraising/update/{id...}", s.handleFundraisingUpdate)
		mux.HandleFunc("POST /api/aion/fundraising/archive/{id...}", s.handleFundraisingArchive)
		mux.HandleFunc("POST /api/aion/fundraising/delete/{id...}", s.handleFundraisingDelete)
		mux.HandleFunc("POST /api/aion/fundraising/person/{id...}", s.handleFundraisingPersonAdd)
		mux.HandleFunc("POST /api/aion/fundraising/person-remove/{id...}", s.handleFundraisingPersonRemove)
		mux.HandleFunc("GET /api/aion/fundraising/sync", s.handleFundraisingSyncStatus)
		mux.HandleFunc("POST /api/aion/fundraising/sync", s.handleFundraisingSyncNow)
		mux.HandleFunc("POST /api/aion/fundraising/sync/resolve", s.handleFundraisingSyncResolve)
	}

	// Google Calendar (M3, read-only).
	mux.HandleFunc("/api/calendar/status", s.handleCalStatus)
	mux.HandleFunc("/api/calendar/events", s.handleCalEvents)
	mux.HandleFunc("/api/calendar/connect", s.handleCalConnect)
	mux.HandleFunc("/api/calendar/disconnect", s.handleCalDisconnect)
	mux.HandleFunc("POST /api/calendar/connect/start", s.handleCalConnectStart)
	mux.HandleFunc("POST /api/calendar/connect/finish", s.handleCalConnectFinish)

	// Gmail read-only OAuth — reconnect the engine's EA-digest inbox access.
	mux.HandleFunc("/api/gmail/status", s.handleGmailStatus)
	mux.HandleFunc("/api/gmail/connect", s.handleGmailConnect)
	mux.HandleFunc("/api/gmail/disconnect", s.handleGmailDisconnect)
	mux.HandleFunc("GET /api/gmail/accounts", s.handleGmailAccounts)
	mux.HandleFunc("POST /api/gmail/accounts/set", s.handleGmailAccountSet)
	mux.HandleFunc("POST /api/gmail/connect/start", s.handleGmailConnectStart)
	mux.HandleFunc("POST /api/gmail/connect/finish", s.handleGmailConnectFinish)
	mux.HandleFunc("POST /api/gmail/accounts/disconnect", s.handleGmailAccountDisconnect)

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
	mux.HandleFunc("POST /api/spirits/approvals/{id}/aion", s.handleSpiritsApprovalAion)
	mux.HandleFunc("POST /api/spirits/run-now", s.handleSpiritsRunNow)

	// Chat with spirits (cmd-ctr import P2): sessions are harness files the
	// ENGINE writes; these routes read them + spool user messages. The events
	// route is the bridge/SSE seam (resumable via ?after=seq).
	mux.HandleFunc("GET /api/chat/spirits", s.handleChatSpirits)
	mux.HandleFunc("GET /api/chat/sessions", s.handleChatSessions)
	mux.HandleFunc("POST /api/chat/sessions", s.handleChatSessionCreate)
	mux.HandleFunc("GET /api/chat/sessions/{id}", s.handleChatSession)
	mux.HandleFunc("POST /api/chat/sessions/{id}/messages", s.handleChatMessage)
	mux.HandleFunc("POST /api/chat/sessions/{id}/rename", s.handleChatRename)
	mux.HandleFunc("DELETE /api/chat/sessions/{id}", s.handleChatDelete)
	mux.HandleFunc("GET /api/chat/sessions/{id}/events", s.handleChatEvents)
	mux.HandleFunc("GET /api/chat/sessions/{id}/stream", s.handleChatStream)

	// TERMINAL — in-app PTY over tmux (metis-local; claude/codex presets).
	mux.HandleFunc("GET /api/terminal/sessions", s.handleTermSessions)
	mux.HandleFunc("POST /api/terminal/session", s.handleTermCreate)
	mux.HandleFunc("PUT /api/terminal/session/{id}", s.handleTermUpdate)
	mux.HandleFunc("DELETE /api/terminal/session/{id}", s.handleTermDelete)
	mux.HandleFunc("POST /api/terminal/session/{id}/kill", s.handleTermKill)
	mux.HandleFunc("GET /api/terminal/ws", s.handleTermWS)
	mux.HandleFunc("GET /api/terminal/ls", s.handleTermLs)
	mux.HandleFunc("GET /api/terminal/devices", s.handleTermDevices)
	mux.HandleFunc("PUT /api/terminal/device/{name}", s.handleTermDeviceUpdate)
	mux.HandleFunc("POST /api/terminal/device/{name}/probe", s.handleTermDeviceProbe)
	mux.HandleFunc("GET /api/activity", s.handleActivity)
	mux.HandleFunc("GET /api/activity/history", s.handleActivityHistory)
	mux.HandleFunc("GET /api/activity/top", s.handleActivityTop)

	// LEDGER — the daily shared thread (tier-3 projection; persona plan Phase 0).
	mux.HandleFunc("GET /api/ledger", s.handleLedger)

	// Sticky note (⌘I floating post-it — dataDir scratch, never the vault).
	mux.HandleFunc("GET /api/sticky", s.handleStickyGet)
	mux.HandleFunc("PUT /api/sticky", s.handleStickyPut)

	// Capture tray (cmd-ctr import P5) — /share is the PWA share_target action.
	mux.HandleFunc("GET /api/capture", s.handleCaptureList)
	mux.HandleFunc("GET /api/capture/badge", s.handleCaptureBadge)
	mux.HandleFunc("POST /api/capture/item", s.handleCaptureAdd)
	mux.HandleFunc("POST /api/capture/upload", s.handleCaptureUpload)
	mux.HandleFunc("POST /api/capture/share", s.handleCaptureShare)
	mux.HandleFunc("POST /api/capture/{id}/update", s.handleCaptureUpdate)
	mux.HandleFunc("POST /api/capture/{id}/status", s.handleCaptureStatus)
	mux.HandleFunc("POST /api/capture/{id}/dismiss", s.handleCaptureDismiss)
	mux.HandleFunc("GET /api/capture/media/{name}", s.handleCaptureMedia)

	// STT — the mic buttons' dictation proxy (lab granite-speech; P6).
	mux.HandleFunc("POST /api/stt", s.handleSTT)

	// FILES — fleet file browser (local roots + ticket-authed agents; P8).
	mux.HandleFunc("GET /api/files/hosts", s.handleFilesHosts)
	mux.HandleFunc("GET /api/files/list", s.handleFilesList)
	mux.HandleFunc("GET /api/files/read", s.handleFilesRead)
	mux.HandleFunc("POST /api/files/upload", s.handleFilesUpload)
	mux.HandleFunc("POST /api/files/mkdir", s.handleFilesMkdir)
	mux.HandleFunc("POST /api/files/rename", s.handleFilesRename)
	mux.HandleFunc("POST /api/files/delete", s.handleFilesDelete)
	mux.HandleFunc("/api/files/home", s.handleFilesHome)
	mux.HandleFunc("GET /api/spirits/castables", s.handleSpiritsCastables) // command-bar catalog
	mux.HandleFunc("GET /api/spirits/catalog", s.handleSpiritsCatalog)     // spirit-page vocabularies (conduits + spellbooks)
	mux.HandleFunc("GET /api/spirits/memories", s.handleSpiritsMemories)   // per-spirit memory listing (counts only)
	// RITUALS board + in-app markdown editing (spirits-console-upgrade).
	mux.HandleFunc("GET /api/spirits/rituals", s.handleSpiritsRituals)
	mux.HandleFunc("GET /api/spirits/file", s.handleSpiritsFileGet)
	mux.HandleFunc("PUT /api/spirits/file", s.handleSpiritsFilePut)
	mux.HandleFunc("POST /api/spirits/ritual", s.handleSpiritsNewRitual)
	mux.HandleFunc("POST /api/spirits/spirit", s.handleSpiritsNewSpirit)
	mux.HandleFunc("POST /api/spirits/ritual/delete", s.handleSpiritsDeleteRitual)
	mux.HandleFunc("POST /api/spirits/spirit/delete", s.handleSpiritsDeleteSpirit)

	// CONTACTS — the people layer over the vault index (plans/contacts-feature.md).
	// Reads are the graph; the only writes are explicit user actions (create a
	// person note, bind an alias, confirm an email).
	mux.HandleFunc("GET /api/contacts", s.handleContactsList)
	mux.HandleFunc("GET /api/contacts/triage", s.handleContactsTriage)
	mux.HandleFunc("GET /api/contacts/page", s.handleContactPage)
	mux.HandleFunc("GET /api/contacts/card", s.handleContactCard)
	mux.HandleFunc("GET /api/contacts/search", s.handleContactsSearch)
	mux.HandleFunc("GET /api/contacts/places", s.handleContactPlaces)
	mux.HandleFunc("GET /api/contacts/nearby", s.handleContactsNearby)
	mux.HandleFunc("PUT /api/contacts/location", s.handleContactLocationPut)
	mux.HandleFunc("DELETE /api/contacts/location", s.handleContactLocationDelete)
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
	// errands — the action layer (aside effector); records = FEED receipts
	mux.HandleFunc("/api/errands", s.handleErrands) // GET list · POST compose
	mux.HandleFunc("GET /api/errands/accounts", s.handleErrandAccounts)
	mux.HandleFunc("POST /api/errands/{id}/cancel", s.handleErrandCancel)
	mux.HandleFunc("POST /api/errands/{id}/ack", s.handleErrandAck)
	mux.HandleFunc("POST /api/errands/{id}/input", s.handleErrandInput)
	mux.HandleFunc("POST /api/errands/{id}/retry", s.handleErrandRetry)
	mux.HandleFunc("GET /api/errands/{id}/transcript", s.handleErrandTranscript)
	mux.HandleFunc("POST /api/feed/{id}/status", s.handleFeedStatus)
	mux.HandleFunc("POST /api/feed/{id}/save-to-vault", s.handleFeedSaveToVault)
	mux.HandleFunc("POST /api/feed/{id}/to-task", s.handleFeedToTask)
	mux.HandleFunc("POST /api/feed/{id}/dig", s.handleFeedDig) // "dig →"
	mux.HandleFunc("POST /api/feed/signal/dismiss", s.handleSignalDismiss)
	mux.HandleFunc("POST /api/feed/signal/snooze", s.handleSignalSnooze)

	// PORTALS — external realms (ClickUp, Benchling, calendar, the engine's LLM
	// conduits, docusign-v2) as one panel: list, (re)connect via pasted key, test,
	// poll, disconnect; portal feed items dismiss / promote-to-today.
	mux.HandleFunc("GET /api/portals", s.handlePortals)
	mux.HandleFunc("POST /api/portals/{id}/key", s.handlePortalKey)
	mux.HandleFunc("POST /api/portals/{id}/test", s.handlePortalTest)
	mux.HandleFunc("POST /api/portals/{id}/poll", s.handlePortalPoll)
	mux.HandleFunc("POST /api/portals/{id}/disconnect", s.handlePortalDisconnect)
	mux.HandleFunc("POST /api/portals/item/dismiss", s.handlePortalDismiss)

	// PROPERTIES — the real-estate cockpit over system/realestate/ records.
	// Reads are the Board + property pages; the writes (create, quick-add log,
	// quick-add ledger row) go through the vaultwriter database-class allow-list.
	mux.HandleFunc("GET /api/properties", s.handlePropertiesList)
	mux.HandleFunc("GET /api/properties/geo", s.handlePropertiesGeo)
	mux.HandleFunc("POST /api/properties", s.handlePropertyCreate)
	mux.HandleFunc("GET /api/properties/{slug}", s.handlePropertyGet)
	mux.HandleFunc("POST /api/properties/{slug}/field", s.handlePropertyField)
	mux.HandleFunc("POST /api/properties/{slug}/log", s.handlePropertyLog)
	mux.HandleFunc("POST /api/properties/{slug}/ledger", s.handlePropertyLedger)
	mux.HandleFunc("GET /api/parcels", s.handleParcelsList)
	mux.HandleFunc("GET /api/parcels/export", s.handleParcelsExport)
	mux.HandleFunc("POST /api/parcels/{slug}/log", s.handleParcelLog)
	mux.HandleFunc("GET /api/properties/{slug}/docs", s.handlePropertyDocs)
	mux.HandleFunc("POST /api/properties/{slug}/docs", s.handlePropertyDocUpload)
	mux.HandleFunc("GET /api/realestate/doc", s.handleRealestateDoc)
	// Admin-portal surface: source sidecars are the live canonical for the
	// public-site data; deal pages aggregate member actuals; ledger rows mutate
	// inline; the statement workbench replaces per-property csv import.
	mux.HandleFunc("/api/properties/{slug}/source", s.handlePropertySource) // GET+PUT
	mux.HandleFunc("GET /api/deals/{slug}", s.handleDealPage)
	mux.HandleFunc("/api/deals/{slug}/source", s.handleDealSource) // GET+PUT
	mux.HandleFunc("POST /api/deals/{slug}/field", s.handleDealField)
	mux.HandleFunc("POST /api/properties/{slug}/ledger/mutate", s.handleLedgerMutate)
	mux.HandleFunc("POST /api/properties/{slug}/work", s.handlePropertyWork)
	mux.HandleFunc("POST /api/realestate/publish-deals", s.handlePublishDeals)
	mux.HandleFunc("GET /api/realestate/assumptions", s.handleAssumptionsGet)
	// real-estate decision log (the aion-mirror domain half)
	mux.HandleFunc("GET /api/re/backlog", s.handleReBacklog)
	mux.HandleFunc("POST /api/re/backlog/item", s.handleReBacklogAdd)
	// id LAST as a multi-segment wildcard: portal ids carry a slash
	// ("aion-bl/<slug>"), which a mid-path {id} cannot match. Same shape the
	// aion backlog routes use; {legacy...} keeps the old paths answering.
	mux.HandleFunc("POST /api/re/backlog/update/{id...}", s.handleReBacklogUpdate)
	mux.HandleFunc("POST /api/re/backlog/delete/{id...}", s.handleReBacklogDelete)
	mux.HandleFunc("POST /api/re/backlog/decide/{id...}", s.handleReBacklogDecide)
	mux.HandleFunc("POST /api/re/backlog/{legacy...}", s.handleReBacklogLegacy)
	mux.HandleFunc("GET /api/re/publish/preview", s.handleRePublishPreview)
	mux.HandleFunc("POST /api/re/publish", s.handleRePublish)
	mux.HandleFunc("POST /api/re/publish/ack/{id}", s.handleRePublishAck)
	mux.HandleFunc("PUT /api/realestate/assumptions", s.handleAssumptionsPut)
	mux.HandleFunc("POST /api/realestate/contractors/{slug}", s.handleContractorTrade)
	mux.HandleFunc("POST /api/properties/{slug}/receipt", s.handleReceiptUpload)
	mux.HandleFunc("POST /api/deals/{slug}/export-underwrite", s.handleDealExportUnderwrite)
	mux.HandleFunc("POST /api/realestate/export-tax", s.handleTaxExport)
	mux.HandleFunc("POST /api/realestate/statements/upload", s.handleStatementsUpload)
	mux.HandleFunc("POST /api/realestate/statements/ingest", s.handleStatementsIngest)
	mux.HandleFunc("GET /api/realestate/statements", s.handleStatementsList)
	mux.HandleFunc("POST /api/realestate/statements/row", s.handleStatementsRow)
	mux.HandleFunc("POST /api/realestate/statements/apply", s.handleStatementsApply)
	mux.HandleFunc("GET /api/realestate/entities", s.handleEntitiesList)
	mux.HandleFunc("POST /api/realestate/entities", s.handleEntityCreate)
	mux.HandleFunc("POST /api/realestate/entities/{slug}/save", s.handleEntitySave)
	mux.HandleFunc("POST /api/realestate/bindings", s.handleBindingSave)

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
	// PWA: the webmanifest must serve with its registered type (Linux metis
	// has no .webmanifest mapping by default → text/plain → Chrome ignores it).
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	// The team portal ships on its own listener (PortalHandler, cfg.PortalPort);
	// the recursive web embed would otherwise expose the same tree here through
	// the catch-all. Shadow it so the private cockpit serves no team surface.
	mux.HandleFunc("/portal", http.NotFound)
	mux.HandleFunc("/portal/", http.NotFound)
	// Cache-bust: the shell (index.html) is served with a build-hash ?v= injected
	// into every js/css URL, so a deploy always forces a fresh fetch — no stale
	// asset behind a browser cache or service worker. The hash changes exactly
	// when any embedded asset changes (assetBuildVersion over the content ETags).
	etags := etagFor(sub)
	idx := versionedIndex(sub, assetBuildVersion(etags))
	mux.HandleFunc("GET /{$}", idx)        // exact "/"
	mux.HandleFunc("GET /index.html", idx) // and the explicit path
	mux.Handle("/", noCache(etags, http.FileServer(http.FS(sub))))
	return mux
}

// assetBuildVersion is a short hash over every embedded asset's content ETag —
// stable for the binary's life, changing exactly when a rebuild ships new assets.
func assetBuildVersion(etags map[string]string) string {
	keys := make([]string, 0, len(etags))
	for k := range etags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(etags[k]))
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

var assetRefRe = regexp.MustCompile(`(src|href)="((?:js|css)/[^"?]+\.(?:js|css))"`)

// versionedIndex serves index.html with ?v=<ver> appended to every local js/css
// reference, computed once at startup. index.html itself is no-cache so the
// browser always re-reads it (and thus the fresh ?v=) after a deploy.
func versionedIndex(sub fs.FS, ver string) http.HandlerFunc {
	raw, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) { http.Error(w, "index missing", 500) }
	}
	body := []byte(assetRefRe.ReplaceAllString(string(raw), `$1="$2?v=`+ver+`"`))
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}
}

// noCache makes the browser revalidate the embedded assets every load. embed.FS
// files have a zero modtime (no Last-Modified/ETag), so without this a rebuilt
// app.js/style.css can stay cached and the UI looks stale after an upgrade.
// The content-hash ETags (etagFor) make that revalidation CHEAP: an unchanged
// asset answers 304 with no body instead of re-transferring the whole file on
// every reload (~840KB of JS/CSS otherwise re-downloaded per page load).
func noCache(etags map[string]string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || strings.HasSuffix(r.URL.Path, "/") {
			p = path.Join(p, "index.html")
		}
		if tag, ok := etags[p]; ok {
			w.Header().Set("ETag", tag)
			if r.Header.Get("If-None-Match") == tag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

// etagFor hashes every embedded asset once at startup — the binary is the
// deployment unit, so a content hash is stable for its lifetime and changes
// exactly when a rebuild ships new assets.
func etagFor(fsys fs.FS) map[string]string {
	etags := map[string]string{}
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		etags[p] = `"` + hex.EncodeToString(sum[:8]) + `"`
		return nil
	})
	return etags
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
		s.syncTaskTasks(body.Tasks) // personal todo-linked ticks → tasks.md
		s.syncAionTasks(body.Tasks) // aion-backed ticks (TaskID "aion:<id>") → aion backlog
		writeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// fillPool attaches the 30-day me pool to an unplanned day so the UI can offer
// quick-add chips. Planned days carry no pool.
// fillPool offers the TASK SUBSTRATE, tiered (task-substrate §5): (1) todos
// tethered to the day's focused Rocks, (2) other todos from those Rocks'
// domains (buckets included), (3) everything else — the client folds tier 3
// behind an "all domains" reveal.
func (s *Server) fillPool(day *daily.Day) {
	if !day.Unplanned || s.tasksStore == nil {
		return
	}
	doc, err := s.tasksStore.Load()
	if err != nil {
		return
	}
	focusedRocks := map[string]bool{}
	focusedAreas := map[string]bool{}
	gdoc := s.goals.Load()
	for _, p := range day.Focus {
		if p.GoalID == "" {
			continue
		}
		focusedRocks[p.GoalID] = true
		for _, a := range gdoc.Areas {
			for _, rock := range a.Rocks {
				if rock.ID == p.GoalID {
					focusedAreas[strings.ToLower(a.Name)] = true
				}
			}
		}
	}
	v := doc.View(time.Now())
	add := func(dv string, t tasks.TaskView) {
		if t.State != "open" {
			return
		}
		tier := 3
		switch {
		case t.Rock != "" && focusedRocks[t.Rock]:
			tier = 1
		case focusedAreas[strings.ToLower(dv)]:
			tier = 2
		}
		day.Pool = append(day.Pool, daily.PoolItem{TaskID: t.ID, Text: t.Text, Area: dv, Tier: tier})
	}
	for _, dv := range v.Domains {
		for _, t := range dv.Tasks {
			add(dv.Name, t)
		}
		for _, bk := range dv.Buckets {
			for _, t := range bk.Tasks {
				add(dv.Name, t)
			}
		}
	}
	// AION open tasks are backlog items, not tasks.md todos — offer mine here too
	// so a captured aion task is pull-able onto later days like any substrate item.
	if s.aion != nil {
		for _, it := range s.aion.LoadBacklog().Items() {
			if it.Kind != aion.KindTask || it.Checked || !s.isMine(it.Owner) {
				continue
			}
			if it.Status != aion.StatusOpen && it.Status != aion.StatusInProgress && it.Status != "" {
				continue
			}
			tier := 3
			switch {
			case it.Rock != "" && focusedRocks[it.Rock]:
				tier = 1
			case focusedAreas["aion"]:
				tier = 2
			}
			day.Pool = append(day.Pool, daily.PoolItem{TaskID: "aion:" + it.ID, Text: it.Text, Area: "Aion", Tier: tier})
		}
	}
	sort.SliceStable(day.Pool, func(i, j int) bool { return day.Pool[i].Tier < day.Pool[j].Tier })
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
		TaskID string `json:"taskId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpError(w, err)
		return
	}
	var task daily.Task
	switch {
	case strings.HasPrefix(b.TaskID, "aion:") && s.aion != nil:
		// a domain-backlog pick: seat with the `<domain>:<id>` backlink
		// (syncAionTasks mirrors ticks back). No promote — the id is content-stable.
		it := s.aion.LoadBacklog().Find(strings.TrimPrefix(b.TaskID, "aion:"))
		if it == nil {
			http.Error(w, "aion task not found", http.StatusNotFound)
			return
		}
		task = daily.Task{Text: it.Text, TaskID: b.TaskID}
	case strings.HasPrefix(b.TaskID, "re:") && s.re != nil:
		it := s.re.LoadBacklog().Find(strings.TrimPrefix(b.TaskID, "re:"))
		if it == nil {
			http.Error(w, "re task not found", http.StatusNotFound)
			return
		}
		task = daily.Task{Text: it.Text, TaskID: b.TaskID}
	case b.TaskID != "" && s.tasksStore != nil:
		// a todos-board pick: pin the durable [todo:: id] so the backlink
		// survives rewording (goals Promote contract)
		doc, err := s.tasksStore.Load()
		if err != nil {
			httpError(w, err)
			return
		}
		if !doc.Promote(b.TaskID) {
			http.Error(w, "todo not found", http.StatusNotFound)
			return
		}
		if err := s.tasksStore.Save(doc); err != nil {
			httpError(w, err)
			return
		}
		_, t := doc.Find(b.TaskID)
		task = daily.Task{Text: t.Text, TaskID: t.ID}
	default:
		text, gid, ok := s.goals.Promote(b.GoalID)
		if !ok {
			http.Error(w, "goal not found", http.StatusNotFound)
			return
		}
		task = daily.Task{Text: text, GoalID: gid}
	}
	day, err := s.svc.AddTask(date, task)
	if err != nil {
		httpError(w, err)
		return
	}
	s.fillPool(&day)
	writeJSON(w, day)
}

// handleDayCapture is the day-view capture flow (goals-orient plan): a free-typed
// task under a focus slot is appended into goals.md under that slot's stage (the
// milestone), gains a durable [goal:: id], and lands on the day linked — so checks
// sync both ways through the existing syncGoalTasks path. Idempotent on both
// sides: same text twice is one goals line (CaptureTask dedupe) and one day task.
func (s *Server) handleDayCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	date := r.URL.Query().Get("date")
	var b struct {
		RockID  string `json:"rockId"`
		StageID string `json:"stageId"` // legacy client shape — rock derived from the stage id
		Text    string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpError(w, err)
		return
	}
	rockID := strings.TrimSpace(b.RockID)
	if rockID == "" && b.StageID != "" {
		if i := strings.LastIndex(b.StageID, "/"); i > 0 {
			rockID = b.StageID[:i]
		}
	}
	text := strings.Join(strings.Fields(b.Text), " ")
	if rockID == "" || text == "" {
		httpError(w, errBadRequest("rockId and text are required"))
		return
	}
	// Domain captures land in the DOMAIN backlog, not the personal todos board —
	// so they project onto that domain's Rocks view (and, for aion, ride the
	// portal publish diff like any other backlog item).
	if s.aion != nil && strings.HasPrefix(rockID, "aion/") {
		s.captureDomainBacklog(w, s.aion, "aion:", date, rockID, text)
		return
	}
	if s.re != nil && strings.HasPrefix(rockID, "ooda-group/") {
		s.captureDomainBacklog(w, s.re, "re:", date, rockID, text)
		return
	}
	if s.tasksStore == nil {
		httpError(w, errBadRequest("todos unavailable"))
		return
	}
	// task-substrate: a day-typed task under a focus slot becomes a
	// rock-tethered todo in the Rock's domain (goals.md holds no tasks).
	gdoc := s.goals.Load()
	var areaName string
	for _, a := range gdoc.Areas {
		for _, rock := range a.Rocks {
			if rock.ID == rockID {
				areaName = a.Name
			}
		}
	}
	if areaName == "" {
		http.Error(w, "rock not found: "+rockID, http.StatusNotFound)
		return
	}
	tdoc, err := s.tasksStore.Load()
	if err != nil {
		httpError(w, err)
		return
	}
	dom := tdoc.EnsureDomain(areaName)
	var todo *tasks.Task
	dom.AllTasks(func(_ *tasks.Bucket, t *tasks.Task) { // idempotent: reuse an open same-text capture
		if todo == nil && !t.Checked && strings.EqualFold(t.Text, text) && t.Rock == rockID {
			todo = t
		}
	})
	if todo == nil {
		todo = &tasks.Task{Text: text, Rock: rockID, Added: time.Now().Format("2006-01-02")}
		dom.Tasks = append(dom.Tasks, todo)
	}
	if err := s.tasksStore.Save(tdoc); err != nil { // assigns the id
		httpError(w, err)
		return
	}
	tdoc.Promote(todo.ID)
	if err := s.tasksStore.Save(tdoc); err != nil {
		httpError(w, err)
		return
	}
	s.stampRockMoved(rockID) // capture is movement
	day, err := s.svc.Load(date)
	if err != nil {
		httpError(w, err)
		return
	}
	seated := false
	for _, t := range day.Tasks {
		if t.TaskID == todo.ID {
			seated = true
			break
		}
	}
	if !seated {
		day, err = s.svc.AddTask(date, daily.Task{Text: text, TaskID: todo.ID})
		if err != nil {
			httpError(w, err)
			return
		}
	}
	s.fillPool(&day)
	writeJSON(w, day)
}

// captureDomainBacklog is the domain arm of day-capture: a free-typed task
// under a domain focus slot becomes a backlog task ([kind:: task] [rock::],
// status open) in that domain's backlog, seated on the day with a
// `<prefix><id>` backlink so ticks sync both ways (syncAionTasks). Idempotent:
// the same title reuses the existing item (ItemID = sha1(kind|title)),
// relinking the rock if the existing copy sits under a different or blank one.
func (s *Server) captureDomainBacklog(w http.ResponseWriter, st *aion.Store, idPrefix, date, rockID, text string) {
	now := time.Now()
	it := &aion.BacklogItem{
		Kind: aion.KindTask, Text: text, Rock: rockID,
		Owner: s.ownerInitials, Status: aion.StatusOpen, Captured: now.Format("2006-01-02"),
	}
	if err := st.AddItem(it); err != nil {
		existing := st.LoadBacklog().Find(aion.ItemID(aion.KindTask, text))
		if existing == nil {
			httpError(w, err)
			return
		}
		it.ID = existing.ID
		if existing.Rock != rockID {
			_ = st.UpdateItem(it.ID, map[string]string{"rock": rockID}, now)
		}
	}
	s.stampRockMoved(rockID) // capture is movement
	ref := idPrefix + it.ID
	day, err := s.svc.Load(date)
	if err != nil {
		httpError(w, err)
		return
	}
	seated := false
	for _, t := range day.Tasks {
		if t.TaskID == ref {
			seated = true
			break
		}
	}
	if !seated {
		day, err = s.svc.AddTask(date, daily.Task{Text: text, TaskID: ref})
		if err != nil {
			httpError(w, err)
			return
		}
	}
	s.fillPool(&day)
	writeJSON(w, day)
}

// stampRockMoved stamps a Rock's last-movement date (tethered-todo activity
// counts as movement — the rock-stalled signal reads this).
func (s *Server) stampRockMoved(rockID string) {
	if s.goals == nil || rockID == "" {
		return
	}
	doc := s.goals.Load()
	if _, g := doc.FindGoal(rockID); g != nil {
		if rock := doc.RockOf(rockID); rock != nil && rock.ID == rockID {
			rock.Moved = time.Now().Format("2006-01-02")
			_ = s.goals.Save(doc)
		}
	}
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
