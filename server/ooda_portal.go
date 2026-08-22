package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"manifest/realestate"
	"manifest/teamportal"
)

// The OODA portal's READ surface (ooda-portal plan, Stage B). Everything here
// is a GET behind the shared sign-in gate; the WRITE routes come from the
// shared layer in portal.go, unforked.
//
// The reads are deliberately FLAT: every authorized member gets the same
// bytes, ledger line items included (owner decision 2026-08-20 — the partners
// are co-investors, so the boundary is the sign-in gate, not a redaction
// inside it). ONE deliberate exception (owner decision 2026-08-21):
// GET /api/ooda/email — a PENDING email candidate is its source member's own
// mail, scoped to that member + the admin until confirmed; confirmed notes
// are shared artifacts and flat again. TestOodaReadsAreFlat pins the rule,
// TestPendingEmailNotesAreScopedToSourceAndAdmin pins the exception; any
// other admin/partner divergence is the bug.

// UseOoda wires the OODA projection (nil-safe: no projection, no portal).
func (s *Server) UseOoda(l *OodaLive) { s.oodaLive = l }

// OodaLive returns the in-process OODA projection (nil when unconfigured).
func (s *Server) OodaLiveProjection() *OodaLive { return s.oodaLive }

// NewOodaLive builds the projection over this server's realestate service.
func (s *Server) NewOodaLive() *OodaLive { return newOodaLive(s) }

// OodaReadRoutes is the ReadRoutes hook the OODA PortalHandler is built with.
func OodaReadRoutes(live *OodaLive) func(*http.ServeMux, PortalOptions) {
	return func(mux *http.ServeMux, opt PortalOptions) {
		api := &oodaAPI{live: live, opt: opt}
		mux.HandleFunc("GET /api/ooda/revision", api.revision)
		mux.HandleFunc("GET /api/ooda/dashboard", api.dashboard)
		mux.HandleFunc("GET /api/ooda/portfolio", api.portfolio)
		mux.HandleFunc("GET /api/ooda/property/{slug}", api.property)
		mux.HandleFunc("GET /api/ooda/work", api.work)
		mux.HandleFunc("GET /api/ooda/people", api.people)
		// the MAP payload is ~1 MB of parcel geometry — its own route, fetched
		// lazily on first MAP entry rather than riding the initial load
		mux.HandleFunc("GET /api/ooda/map", api.mapView)
		// the one OODA-only WRITE (Stage C): a bid lands as a proposal, never
		// as a contract record. Everything else a member can write comes from
		// the shared layer in portal.go, unforked.
		if opt.Store != nil && opt.Auth != nil {
			mux.HandleFunc("POST /api/ooda/bid", api.bid)
		}
		// the EMAIL lane (ooda_email.go): members' own mailboxes → pending
		// candidates → confirm into the shared artifact pool. Registers only
		// when the lane is wired.
		api.registerEmailRoutes(mux)
		// the FEED approval lane (ooda_feed.go) + the ARCHIVE read
		api.registerFeedRoutes(mux)
		mux.HandleFunc("GET /api/ooda/archive", api.archive)
		// /data/meta.json keeps the AION client's revision-poll shape
		mux.HandleFunc("GET /data/meta.json", handleAionLiveFile(live, "/data/meta.json"))
	}
}

type oodaAPI struct {
	live *OodaLive
	opt  PortalOptions
}

// snap resolves the composed read, 503 when nothing has composed yet.
func (a *oodaAPI) snap(w http.ResponseWriter) (*oodaSnapshot, bool) {
	if a.live == nil {
		http.Error(w, "ooda projection not configured", http.StatusServiceUnavailable)
		return nil, false
	}
	snap := a.live.Snapshot()
	if snap == nil {
		http.Error(w, "the real-estate records have not composed yet", http.StatusServiceUnavailable)
		return nil, false
	}
	return snap, true
}

func (a *oodaAPI) revision(w http.ResponseWriter, r *http.Request) {
	st := a.live.Status()
	if match := r.Header.Get("If-None-Match"); match != "" && match == `"`+st.EffectiveRevision+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", `"`+st.EffectiveRevision+`"`)
	writeJSON(w, st)
}

func (a *oodaAPI) dashboard(w http.ResponseWriter, r *http.Request) {
	snap, ok := a.snap(w)
	if !ok {
		return
	}
	// the team overlay's field state and items ride in (nil without a store)
	// so the counts honor portal-set done/open marks and portal-created work.
	// Fetched per request: the client's revision poll already hashes the team
	// revision (statusLocked), so a PATCH re-renders without any subscription.
	var overrides map[string]teamportal.Override
	var team []teamportal.TeamItem
	if a.opt.Store != nil {
		ext := a.opt.Store.Ext()
		overrides, team = ext.Overrides, ext.Items
	}
	writeJSON(w, buildOodaDashboard(snap, oodaToday(), overrides, team))
}

// portfolio is the list surface: one derived row per property, plus the
// entity/deal context the filters need.
func (a *oodaAPI) portfolio(w http.ResponseWriter, r *http.Request) {
	snap, ok := a.snap(w)
	if !ok {
		return
	}
	today := oodaToday()
	props := oodaVisibleProps(snap)
	rows := make([]oodaFacts, 0, len(props))
	for _, p := range props {
		rows = append(rows, oodaPropertyFacts(p, today))
	}
	writeJSON(w, map[string]any{
		"properties": rows,
		"entities":   snap.Entities,
		"deals":      snap.Deals,
		"holdings":   snap.Holdings,
	})
}

// property is the detail surface. It carries the FULL ledger — every member
// sees vendor names and amounts (owner decision 2026-08-20). There is no
// admin branch here on purpose; do not add one without a §12 amendment.
func (a *oodaAPI) property(w http.ResponseWriter, r *http.Request) {
	snap, ok := a.snap(w)
	if !ok {
		return
	}
	slug := r.PathValue("slug")
	for i := range snap.Properties {
		p := snap.Properties[i]
		if !strings.EqualFold(p.Slug, slug) {
			continue
		}
		// contracts allocating on this property, with their draw-down state
		var contracts []map[string]any
		for _, c := range snap.Contracts {
			for _, al := range c.Allocations {
				if !strings.EqualFold(al.Property, p.Slug) {
					continue
				}
				contracts = append(contracts, map[string]any{
					"slug": c.Slug, "name": c.Name, "contractor": c.Contractor,
					"status": c.Status, "total": c.Total, "nodeId": al.NodeID,
					"amount": al.Amount, "doc": c.Doc, "date": c.Date,
				})
				break
			}
		}
		out := map[string]any{
			"property":  p,
			"facts":     oodaPropertyFacts(p, oodaToday()),
			"ledger":    p.Ledger, // FULL line items — flat by decision
			"contracts": contracts,
		}
		// The underwriting the cockpit shows needs two things the composed
		// Property does not carry: the LIVE source sidecar (an unlocked
		// property's purchase/closing/hard/carry/contingency and its unit
		// fallbacks) and the assumption set the screening outputs are computed
		// against. Both are flat — identical for every member — and both ship
		// by owner decision 2026-08-22: partners already see every ledger line,
		// so withholding the inputs behind the numbers would be a strange place
		// to start redacting.
		if srv := a.live.server(); srv != nil {
			if raw, ok := srv.realestate.Source(p.Path); ok {
				out["source"] = json.RawMessage(raw)
			}
			out["assumptions"] = map[string]any{
				"values":    srv.loadAssumptions().Values,
				"keys":      realestate.AssumptionKeys,
				"labels":    realestate.AssumptionLabels,
				"overrides": srv.realestate.AssumptionOverrides(),
			}
		}
		writeJSON(w, out)
		return
	}
	http.Error(w, "property not found", http.StatusNotFound)
}

func (a *oodaAPI) work(w http.ResponseWriter, r *http.Request) {
	snap, ok := a.snap(w)
	if !ok {
		return
	}
	// the overlay's own items are folded INTO the groups (source:"team"), so
	// the tab shows portal-created work; its Overrides feed the grouping too,
	// so an item a member marked done through the portal actually clears.
	// Proposals ride alongside for the property detail's pending-bid chips.
	var proposals any
	var overrides map[string]teamportal.Override
	var teamItems []teamportal.TeamItem
	if a.opt.Store != nil {
		ext := a.opt.Store.Ext()
		teamItems, proposals, overrides = ext.Items, ext.Proposals, ext.Overrides
	}
	groups := buildOodaWork(snap, oodaToday(), overrides, teamItems)
	writeJSON(w, map[string]any{"groups": groups, "proposals": proposals})
}

func (a *oodaAPI) people(w http.ResponseWriter, r *http.Request) {
	if a.live == nil {
		http.Error(w, "ooda projection not configured", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"people": a.live.People()})
}

// bid files a partner's bid as a PROPOSAL, never a contract record.
//
// A bid in this system is a vault record (system/realestate/contracts/<slug>.md,
// written through vaultwriter under `re-contracts`), and §12's boundary is
// absolute: a portal write lands in the derived store, never the vault. So the
// partner's bid becomes a `kind:"bid"` proposal carrying the form as its
// payload, renders immediately as a pending chip on the property, and cards
// into the owner's FEED — where ONE approve click in the private cockpit is
// the owner action that crosses vaultwriter and mints the real contract.
//
// Cost: one owner click per bid. Benefit: a partner can never silently commit
// money against a property, and the doctrine invariant stays clean.
func (a *oodaAPI) bid(w http.ResponseWriter, r *http.Request) {
	id, ok := PortalIdentify(a.opt, w, r)
	if !ok {
		return
	}
	var b struct {
		Property   string  `json:"property"`
		WorkID     string  `json:"workId"`
		Contractor string  `json:"contractor"`
		Amount     float64 `json:"amount"`
		Date       string  `json:"date"`
		Expires    string  `json:"expires"`
		Scope      string  `json:"scope"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	b.Property = strings.TrimSpace(b.Property)
	b.Contractor = strings.TrimSpace(b.Contractor)
	if b.Property == "" || b.Contractor == "" || b.Amount <= 0 {
		httpError(w, errBadRequest("a bid needs a property, a contractor, and an amount"))
		return
	}
	snap, ok := a.snap(w)
	if !ok {
		return
	}
	label, found := b.Property, false
	for _, p := range snap.Properties {
		if strings.EqualFold(p.Slug, b.Property) {
			b.Property, found = p.Slug, true
			if p.Short != "" {
				label = p.Short
			}
			break
		}
	}
	if !found {
		httpError(w, errBadRequest("unknown property "+b.Property))
		return
	}
	title := b.Contractor + " — " + label + " · $" +
		strconv.FormatFloat(b.Amount, 'f', 2, 64)
	// The bid is aimed at the OWNER: he is the only one who can materialize a
	// contract record, so he is the only legitimate decider.
	prop, err := a.opt.Store.ProposeFull(id, a.opt.AdminEmail, "BA", "bid", title,
		"prop/"+b.Property, b.Expires, map[string]any{
			"kind": "bid", "property": b.Property, "workId": b.WorkID,
			"contractor": b.Contractor, "amount": b.Amount,
			"date": b.Date, "expires": b.Expires, "scope": b.Scope,
		}, time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, prop)
}

// oodaLedgerTotals is a small helper the tests use to prove the portal's money
// equals the cockpit's: Σ project paid over the visible portfolio.
func oodaLedgerTotals(snap *oodaSnapshot) (paid, committed float64) {
	for _, p := range oodaVisibleProps(snap) {
		if p.Project != nil {
			paid += p.Project.Paid
			committed += p.Project.Committed
		}
	}
	return paid, committed
}

// OodaTeamAgents is the OODA surface's agent roster — rosterFor(surface) was
// already generic, so a second agent is config, not code.
func (s *Server) OodaTeamAgents() []map[string]any { return s.rosterFor("ooda") }
