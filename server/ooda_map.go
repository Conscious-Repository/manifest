package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"manifest/realestate"
)

// The OODA portal's MAP payload — the parcel study the owner already runs at
// ooda.group/parcels and in the cockpit's PROPERTIES map, served to partners
// from the live vault instead of a hand-built static geojson.
//
// Two layers ride in one payload:
//
//   - HOLDINGS: our own properties, colored by acquisition state, filtered by
//     oodaVisibleProps so the owner's research tail never leaks. A parcel we
//     hold is dropped from the study layer beneath it — one lot, one polygon.
//   - STUDY: every researched parcel with its assessor facts, so a partner can
//     click any lot on the block and see who owns it and what it owes.
//
// It is ~1 MB, so it is fetched lazily on first MAP entry and cached by
// revision — the same freshness contract as every other OODA read.

type oodaMapParcel struct {
	Slug      string            `json:"slug"`
	Address   string            `json:"address"`
	Owner     string            `json:"owner,omitempty"`
	OwnerAddr string            `json:"ownerAddr,omitempty"`
	SaleDate  string            `json:"saleDate,omitempty"`
	RecDate   string            `json:"recDate,omitempty"`
	TaxStatus string            `json:"taxStatus"` // current | delinquent | lra
	TaxBalDue float64           `json:"taxBalDue,omitempty"`
	Assessed  float64           `json:"assessed,omitempty"`
	LandUse   string            `json:"landUse,omitempty"`
	ParcelID  string            `json:"parcelId,omitempty"`
	Lat       float64           `json:"lat,omitempty"`
	Lng       float64           `json:"lng,omitempty"`
	Features  []json.RawMessage `json:"features,omitempty"`
}

type oodaMapHolding struct {
	Slug     string            `json:"slug"`
	Short    string            `json:"short"`
	Address  string            `json:"address,omitempty"`
	Entity   string            `json:"entity,omitempty"`
	Status   string            `json:"status,omitempty"`
	Acq      string            `json:"acq"` // owned | under-contract | pipeline
	Deal     string            `json:"deal,omitempty"`
	Plan     float64           `json:"plan,omitempty"`
	Paid     float64           `json:"paid,omitempty"`
	Open     int               `json:"open,omitempty"`
	Lat      float64           `json:"lat,omitempty"`
	Lng      float64           `json:"lng,omitempty"`
	Features []json.RawMessage `json:"features,omitempty"`
}

type oodaMapPayload struct {
	Revision string           `json:"revision"`
	Holdings []oodaMapHolding `json:"holdings"`
	Parcels  []oodaMapParcel  `json:"parcels"`
	Counts   map[string]int   `json:"counts"` // tax status → n, for the legend
	// Study names the assessor snapshot behind the parcel layer. Partners are
	// looking at other people's tax positions; how old the pull is changes what
	// the map means, so the page says it.
	Study    realestate.StudyMeta `json:"study"`
	StudyAge string               `json:"studyAge,omitempty"`
}

// oodaMapCache memoizes the payload against the projection revision.
type oodaMapCache struct {
	mu  sync.Mutex
	rev string
	pay *oodaMapPayload
}

// mapPayload composes (or returns the cached) map read.
func (l *OodaLive) mapPayload() *oodaMapPayload {
	snap := l.Snapshot()
	if snap == nil || l.s == nil || l.s.realestate == nil {
		return &oodaMapPayload{Counts: map[string]int{}}
	}
	// The study geojson lives in the re-portal checkout, OUTSIDE the vault the
	// projection revision fingerprints — so a `git pull` that refreshes the
	// assessor snapshot would otherwise never reach this page. Its own key
	// rides in the cache key.
	layer := l.s.studyLayer()
	rev := snap.Revision + "|" + layer.Key

	l.mapCache.mu.Lock()
	defer l.mapCache.mu.Unlock()
	if l.mapCache.pay != nil && l.mapCache.rev == rev {
		return l.mapCache.pay
	}

	geo, err := l.s.realestate.GeoRecords()
	if err != nil {
		geo = nil
	}
	bySlug := map[string]realestate.GeoRecord{}
	// every assessor parcel id already drawn by a richer layer — first the lots
	// we hold, then each parcel as it is added, so the curated research records
	// win over the wide study snapshot for the same lot
	drawn := map[string]bool{}
	for _, g := range geo {
		if g.Type != "property" {
			continue
		}
		bySlug[strings.ToLower(g.Slug)] = g
	}

	// the COMPOSITE revision, so the browser ETag also invalidates on a
	// refreshed study snapshot
	pay := &oodaMapPayload{Revision: rev, Counts: map[string]int{}}
	today := oodaToday()
	for _, p := range oodaVisibleProps(snap) {
		f := oodaPropertyFacts(p, today)
		h := oodaMapHolding{
			Slug: p.Slug, Short: f.Short, Address: p.Address, Entity: p.Entity,
			Status: p.Status, Acq: f.Acq, Deal: p.Deal,
			Plan: f.Plan, Paid: f.Paid, Open: f.Open,
			Lat: p.Lat, Lng: p.Lng,
		}
		if g, ok := bySlug[strings.ToLower(p.Slug)]; ok {
			h.Features = g.Features
			if h.Lat == 0 && h.Lng == 0 {
				h.Lat, h.Lng = g.Lat, g.Lng
			}
			for _, id := range g.ParcelIDs {
				drawn[strings.TrimSpace(id)] = true
			}
		}
		pay.Holdings = append(pay.Holdings, h)
	}

	// The owner's own RESEARCH records first — same facts as the study layer
	// but curated, and the ones he re-pulls with his notes preserved. Then the
	// wide study snapshot fills in every other lot on the three neighborhoods,
	// so the portal shows the whole block the way ooda.group/parcels does.
	records, _ := l.s.realestate.Parcels()
	add := func(pc realestate.Parcel) {
		if pc.ParcelID != "" && drawn[strings.TrimSpace(pc.ParcelID)] {
			return // a lot we hold is drawn once, as a holding — never twice
		}
		status := pc.TaxStatus
		if status == "" {
			status = "current"
		}
		if pc.ParcelID != "" {
			drawn[strings.TrimSpace(pc.ParcelID)] = true
		}
		pay.Counts[status]++
		pay.Parcels = append(pay.Parcels, oodaMapParcel{
			Slug: pc.Slug, Address: pc.Address, Owner: pc.Owner, OwnerAddr: pc.OwnerAddr,
			SaleDate: pc.SaleDate, RecDate: pc.RecDate, TaxStatus: status,
			TaxBalDue: pc.TaxBalDue, Assessed: pc.Assessed, LandUse: pc.LandUse,
			ParcelID: pc.ParcelID, Lat: pc.Lat, Lng: pc.Lng,
			Features: pc.Features,
		})
	}
	for _, pc := range records {
		add(pc)
	}
	for _, pc := range layer.Parcels {
		add(pc)
	}
	pay.Study = layer.Meta
	pay.StudyAge = layer.Meta.StudyAge(time.Now())
	if pay.Holdings == nil {
		pay.Holdings = []oodaMapHolding{}
	}
	if pay.Parcels == nil {
		pay.Parcels = []oodaMapParcel{}
	}
	l.mapCache.rev, l.mapCache.pay = rev, pay
	return pay
}

// mapView — GET /api/ooda/map. Behind the same sign-in gate as every other
// OODA read, and flat: admin and partner get identical bytes.
func (a *oodaAPI) mapView(w http.ResponseWriter, r *http.Request) {
	if a.live == nil {
		http.Error(w, "ooda projection not configured", http.StatusServiceUnavailable)
		return
	}
	pay := a.live.mapPayload()
	if match := r.Header.Get("If-None-Match"); match != "" && pay.Revision != "" &&
		match == `"map-`+pay.Revision+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if pay.Revision != "" {
		w.Header().Set("ETag", `"map-`+pay.Revision+`"`)
	}
	writeJSONZip(w, r, pay)
}
