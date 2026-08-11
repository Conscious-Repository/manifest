package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/calendar"
	"manifest/contacts"
	"manifest/daily"
	"manifest/errands"
	"manifest/gmailauth"
	"manifest/goals"
	"manifest/portals"
	"manifest/reading"
	"manifest/realestate"
	"manifest/signals"
	"manifest/spirits"
	"manifest/studio"
	"manifest/todos"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

//go:embed web
var webFiles embed.FS

type Server struct {
	svc        *daily.Service
	goals      *goals.Store
	todosStore *todos.Store // the third surface — vault-root `to do.md` (nilable)
	// ownerInitials identify "me" in the unified todo projection (stage 4):
	// empty/"me"/containing-these-initials owners are mine.
	ownerInitials string
	cal           *calendar.Client
	// Gmail read-only OAuth for the engine's ea-coordinator digest — manifest
	// mints/validates the token the headless engine reads. Nilable.
	gmail *gmailauth.Client
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
	geocoder       *realestate.Geocoder
	statements     *realestate.StatementStore
	// Content Studio (STUDIO tab): draft board + read-only X corpus. Nilable.
	studio     *studio.Store
	corpusPath string // <excalibur>/vessel/corpus/x.db
	xPostsFile string // vault-relative X-posts file (default "x posts.md")
	// Errands (the action layer — aside effector; records = FEED receipts).
	// Nilable; dataDir state only, never the vault.
	errands        *errands.Store
	errandExec     *errands.Executor
	errandAccounts []string // §6 allowlist ("" = any signed-in account)
	// AION (program cockpit over system/aion/ + publish effector). Nilable.
	aion          *aion.Store
	aionPortal    aionPortalCfg   // aionbio checkout coordinates ("" path = publish disabled)
	aionDataDir   string          // publish receipts live under <dataDir>/aion/
	aionPublishes *aionPublishLog // lazy-opened receipts log
	// aionSink receives vault-relative paths to consider for extraction —
	// the post-confirm nudge (aion.ExtractSink satisfies it). Nilable.
	aionSink interface{ Notify([]string) }
}

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
func (s *Server) UseContacts(c *contacts.Service) { s.contacts = c }

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

	// TODOS — the third surface over `to do.md` (todos-surface-scope).
	mux.HandleFunc("GET /api/todos", s.handleTodosGet)
	mux.HandleFunc("POST /api/todos/item", s.handleTodoAdd)
	mux.HandleFunc("POST /api/todos/check", s.handleTodoCheck)
	mux.HandleFunc("POST /api/todos/update", s.handleTodoUpdate)
	mux.HandleFunc("POST /api/todos/rank", s.handleTodosRank)                      // unified drag-to-rank (stage 4)
	mux.HandleFunc("GET /api/properties/migrate-todos", s.handlePropTodosMigrate)  // preview
	mux.HandleFunc("POST /api/properties/migrate-todos", s.handlePropTodosMigrate) // commit
	mux.HandleFunc("GET /api/properties/people", s.handleRePeopleGet)              // RE assignee registry
	mux.HandleFunc("PUT /api/properties/people", s.handleRePeopleSave)
	mux.HandleFunc("POST /api/todos/drop", s.handleTodoDrop)
	mux.HandleFunc("/api/todos/split", s.handleTodosSplit) // GET preview · POST commit (one task substrate)
	mux.HandleFunc("POST /api/todos/bucket", s.handleBucketRename)
	mux.HandleFunc("POST /api/todos/issue", s.handleIssueAdd)
	mux.HandleFunc("POST /api/todos/issue/resolve", s.handleIssueResolve)
	mux.HandleFunc("POST /api/todos/issue/to-todo", s.handleIssueToTodo) // reverse conversion (+ optional tether)
	mux.HandleFunc("POST /api/todos/to-issue", s.handleTodoToIssue)

	// AION — program cockpit over system/aion/ records + publish effector.
	if s.aion != nil {
		mux.HandleFunc("GET /api/aion", s.handleAion)
		mux.HandleFunc("PUT /api/aion/people", s.handleAionPeopleSave)
		mux.HandleFunc("PUT /api/aion/vto", s.handleAionVTOSave)
		mux.HandleFunc("PUT /api/aion/finances", s.handleAionFinancesSave)
		mux.HandleFunc("PUT /api/aion/hiring", s.handleAionHiringSave)
		mux.HandleFunc("PUT /api/aion/references", s.handleAionReferencesSave)
		mux.HandleFunc("POST /api/aion/backlog/item", s.handleAionBacklogAdd)
		mux.HandleFunc("POST /api/aion/backlog/{id}/update", s.handleAionBacklogUpdate)
		mux.HandleFunc("POST /api/aion/backlog/{id}/delete", s.handleAionBacklogDelete)
		mux.HandleFunc("POST /api/aion/backlog/{id}/decide", s.handleAionBacklogDecide)
		mux.HandleFunc("GET /api/aion/reconcile", s.handleAionReconcile)
		mux.HandleFunc("POST /api/aion/backlog/link", s.handleAionBacklogLink)
		mux.HandleFunc("POST /api/aion/heuristics/{id}/edit", s.handleAionHeuristicEdit)
		mux.HandleFunc("POST /api/aion/heuristics/{id}/retire", s.handleAionHeuristicRetire)
		mux.HandleFunc("POST /api/aion/heuristics/merge", s.handleAionHeuristicsMerge)
		mux.HandleFunc("POST /api/aion/heuristics/reorder", s.handleAionHeuristicsReorder)
		mux.HandleFunc("GET /api/aion/publish/preview", s.handleAionPublishPreview)
		mux.HandleFunc("POST /api/aion/publish", s.handleAionPublish)
		mux.HandleFunc("POST /api/aion/publish/baseline", s.handleAionPublishBaseline)
		mux.HandleFunc("POST /api/aion/publishes/{id}/ack", s.handleAionPublishAck)
	}

	// Google Calendar (M3, read-only).
	mux.HandleFunc("/api/calendar/status", s.handleCalStatus)
	mux.HandleFunc("/api/calendar/events", s.handleCalEvents)
	mux.HandleFunc("/api/calendar/connect", s.handleCalConnect)
	mux.HandleFunc("/api/calendar/disconnect", s.handleCalDisconnect)

	// Gmail read-only OAuth — reconnect the engine's EA-digest inbox access.
	mux.HandleFunc("/api/gmail/status", s.handleGmailStatus)
	mux.HandleFunc("/api/gmail/connect", s.handleGmailConnect)
	mux.HandleFunc("/api/gmail/disconnect", s.handleGmailDisconnect)

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
	mux.HandleFunc("POST /api/feed/{id}/to-todo", s.handleFeedToTodo)
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

	// CONTENT STUDIO — the draft board + inspiration watchlist (content-studio §8).
	mux.HandleFunc("GET /api/studio", s.handleStudio)
	mux.HandleFunc("POST /api/studio/draft/{id}/dismiss", s.handleStudioDismiss)
	mux.HandleFunc("POST /api/studio/draft/{id}/feedback", s.handleStudioFeedback)
	mux.HandleFunc("POST /api/studio/draft/{id}/edit", s.handleStudioEdit)
	mux.HandleFunc("POST /api/studio/draft/{id}/mark-posted", s.handleStudioMarkPosted)
	mux.HandleFunc("POST /api/studio/draft/{id}/consume-seed", s.handleStudioConsumeSeed)
	mux.HandleFunc("POST /api/studio/draft/{id}/overrule", s.handleStudioOverrule)
	// Queue tab: live-editable x posts.md (§1/§3)
	mux.HandleFunc("GET /api/studio/queue", s.handleStudioQueue)
	mux.HandleFunc("POST /api/studio/migrate", s.handleStudioMigrate)
	mux.HandleFunc("POST /api/studio/bullet/{op}", s.handleStudioBullet)
	mux.HandleFunc("POST /api/studio/queue/mark-posted", s.handleStudioQueuePosted)
	// Inspiration tab writes (§8)
	mux.HandleFunc("POST /api/studio/account/{handle}/commentary", s.handleStudioCommentary)
	mux.HandleFunc("POST /api/studio/account/{handle}/self", s.handleStudioSelf)
	mux.HandleFunc("POST /api/studio/annotate", s.handleStudioAnnotate)
	mux.HandleFunc("POST /api/studio/account/add", s.handleStudioAddAccount)
	mux.HandleFunc("POST /api/studio/commission", s.handleStudioCommission)

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
		s.syncTodoTasks(body.Tasks) // same contract for todo-linked ticks → to do.md
		s.syncAionTasks(body.Tasks) // aion-backed ticks (TodoID "aion:<id>") → aion backlog
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
	if !day.Unplanned || s.todosStore == nil {
		return
	}
	doc, err := s.todosStore.Load()
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
	add := func(dv string, t todos.TodoView) {
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
		day.Pool = append(day.Pool, daily.PoolItem{TodoID: t.ID, Text: t.Text, Area: dv, Tier: tier})
	}
	for _, dv := range v.Domains {
		for _, t := range dv.Todos {
			add(dv.Name, t)
		}
		for _, bk := range dv.Buckets {
			for _, t := range bk.Todos {
				add(dv.Name, t)
			}
		}
	}
	// AION open tasks are backlog items, not to-do.md todos — offer mine here too
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
			day.Pool = append(day.Pool, daily.PoolItem{TodoID: "aion:" + it.ID, Text: it.Text, Area: "Aion", Tier: tier})
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
		TodoID string `json:"todoId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		httpError(w, err)
		return
	}
	var task daily.Task
	switch {
	case strings.HasPrefix(b.TodoID, "aion:") && s.aion != nil:
		// an aion backlog pick: seat with the `aion:<id>` backlink (syncAionTasks
		// mirrors ticks back to the backlog). No promote — the id is content-stable.
		it := s.aion.LoadBacklog().Find(strings.TrimPrefix(b.TodoID, "aion:"))
		if it == nil {
			http.Error(w, "aion task not found", http.StatusNotFound)
			return
		}
		task = daily.Task{Text: it.Text, TodoID: b.TodoID}
	case b.TodoID != "" && s.todosStore != nil:
		// a todos-board pick: pin the durable [todo:: id] so the backlink
		// survives rewording (goals Promote contract)
		doc, err := s.todosStore.Load()
		if err != nil {
			httpError(w, err)
			return
		}
		if !doc.Promote(b.TodoID) {
			http.Error(w, "todo not found", http.StatusNotFound)
			return
		}
		if err := s.todosStore.Save(doc); err != nil {
			httpError(w, err)
			return
		}
		_, t := doc.Find(b.TodoID)
		task = daily.Task{Text: t.Text, TodoID: t.ID}
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
	// AION captures land in the aion backlog (system/aion/backlog.md), not the
	// personal todos board — so they project onto Todos/Goals AND ride the
	// aionbio portal publish diff like any other backlog item.
	if s.aion != nil && strings.HasPrefix(rockID, "aion/") {
		s.captureAionBacklog(w, date, rockID, text)
		return
	}
	if s.todosStore == nil {
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
	tdoc, err := s.todosStore.Load()
	if err != nil {
		httpError(w, err)
		return
	}
	dom := tdoc.EnsureDomain(areaName)
	var todo *todos.Todo
	dom.AllTodos(func(_ *todos.Bucket, t *todos.Todo) { // idempotent: reuse an open same-text capture
		if todo == nil && !t.Checked && strings.EqualFold(t.Text, text) && t.Rock == rockID {
			todo = t
		}
	})
	if todo == nil {
		todo = &todos.Todo{Text: text, Rock: rockID, Added: time.Now().Format("2006-01-02")}
		dom.Todos = append(dom.Todos, todo)
	}
	if err := s.todosStore.Save(tdoc); err != nil { // assigns the id
		httpError(w, err)
		return
	}
	tdoc.Promote(todo.ID)
	if err := s.todosStore.Save(tdoc); err != nil {
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
		if t.TodoID == todo.ID {
			seated = true
			break
		}
	}
	if !seated {
		day, err = s.svc.AddTask(date, daily.Task{Text: text, TodoID: todo.ID})
		if err != nil {
			httpError(w, err)
			return
		}
	}
	s.fillPool(&day)
	writeJSON(w, day)
}

// captureAionBacklog is the AION arm of day-capture: a free-typed task under an
// aion focus slot becomes an aion backlog task ([kind:: task] [rock::], status
// open), seated on the day with an `aion:<id>` backlink so ticks sync both ways
// (syncAionTasks) and it rides the portal publish diff. Idempotent: the same
// title reuses the existing item (aion ItemID = sha1(kind|title)), relinking the
// rock if the existing copy sits under a different or blank one.
func (s *Server) captureAionBacklog(w http.ResponseWriter, date, rockID, text string) {
	now := time.Now()
	it := &aion.BacklogItem{
		Kind: aion.KindTask, Text: text, Rock: rockID,
		Owner: s.ownerInitials, Status: aion.StatusOpen, Captured: now.Format("2006-01-02"),
	}
	if err := s.aion.AddItem(it); err != nil {
		existing := s.aion.LoadBacklog().Find(aion.ItemID(aion.KindTask, text))
		if existing == nil {
			httpError(w, err)
			return
		}
		it.ID = existing.ID
		if existing.Rock != rockID {
			_ = s.aion.UpdateItem(it.ID, map[string]string{"rock": rockID}, now)
		}
	}
	s.stampRockMoved(rockID) // capture is movement
	ref := "aion:" + it.ID
	day, err := s.svc.Load(date)
	if err != nil {
		httpError(w, err)
		return
	}
	seated := false
	for _, t := range day.Tasks {
		if t.TodoID == ref {
			seated = true
			break
		}
	}
	if !seated {
		day, err = s.svc.AddTask(date, daily.Task{Text: text, TodoID: ref})
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
