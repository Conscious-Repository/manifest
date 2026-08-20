package server

import (
	"net/http"
	"strings"
)

// The OODA portal's READ surface (ooda-portal plan, Stage B). Everything here
// is a GET behind the shared sign-in gate; the WRITE routes come from the
// shared layer in portal.go, unforked.
//
// The reads are deliberately FLAT: every authorized member gets the same
// bytes, ledger line items included (owner decision 2026-08-20 — the partners
// are co-investors, so the boundary is the sign-in gate, not a redaction
// inside it). TestOodaReadsAreFlat pins that; if admin and partner responses
// ever differ, that is the bug.

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
	writeJSON(w, buildOodaDashboard(snap, oodaToday()))
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
		writeJSON(w, map[string]any{
			"property":  p,
			"facts":     oodaPropertyFacts(p, oodaToday()),
			"ledger":    p.Ledger, // FULL line items — flat by decision
			"contracts": contracts,
		})
		return
	}
	http.Error(w, "property not found", http.StatusNotFound)
}

func (a *oodaAPI) work(w http.ResponseWriter, r *http.Request) {
	snap, ok := a.snap(w)
	if !ok {
		return
	}
	groups := buildOodaWork(snap, oodaToday())
	// the overlay's own items + pending proposals ride alongside, so the tab
	// shows portal-created work too
	var teamItems, proposals any
	if a.opt.Store != nil {
		ext := a.opt.Store.Ext()
		teamItems, proposals = ext.Items, ext.Proposals
	}
	writeJSON(w, map[string]any{"groups": groups, "teamItems": teamItems, "proposals": proposals})
}

func (a *oodaAPI) people(w http.ResponseWriter, r *http.Request) {
	if a.live == nil {
		http.Error(w, "ooda projection not configured", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"people": a.live.People()})
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
