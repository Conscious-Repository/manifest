package server

import (
	"encoding/csv"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"manifest/realestate"
)

// PARCELS — the research-parcel layer over system/realestate/parcels/ (St. Louis
// Assessor data pulled by cmd/parcel-pull). Read-only intel + an owner notes log,
// SEPARATE from owned properties. One list endpoint feeds both the map overlay
// and the spreadsheet; the log endpoint reuses the property log write path.

// handleParcelsList returns every research parcel (facts + polygon features +
// notes) — the payload for the map overlay AND the spreadsheet table.
func (s *Server) handleParcelsList(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		writeJSON(w, map[string]any{"parcels": []any{}})
		return
	}
	parcels, err := s.realestate.Parcels()
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"parcels": parcels})
}

// studyParcelsPath resolves the parcel-study geojson. The re-portal CHECKOUT
// wins: it is the file the public ooda.group/parcels page serves, so reading it
// there keeps the two maps on one snapshot and makes refreshing it a `git pull`
// rather than a hand copy onto every host. The dataDir path is the fallback for
// a machine with no checkout.
func (s *Server) studyParcelsPath() string {
	if s.rePortalPath != "" {
		p := filepath.Join(s.rePortalPath, "public", "study-parcels.geojson")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if s.studyFallback != "" {
		if _, err := os.Stat(s.studyFallback); err == nil {
			return s.studyFallback
		}
	}
	return ""
}

// studyLayer loads the study snapshot (nil-safe, cached in the realestate pkg).
func (s *Server) studyLayer() *realestate.StudyLayer {
	l, err := realestate.LoadStudyParcels(s.studyParcelsPath())
	if err != nil || l == nil {
		return &realestate.StudyLayer{}
	}
	return l
}

// drawnParcelIDs is every assessor parcel already rendered by a RICHER layer:
// the owner's own research records (they carry his notes) and the lots we hold
// (they render as properties). One lot, one polygon.
func (s *Server) drawnParcelIDs(records []realestate.Parcel) map[string]bool {
	drawn := map[string]bool{}
	for _, p := range records {
		if id := strings.TrimSpace(p.ParcelID); id != "" {
			drawn[id] = true
		}
	}
	if s.realestate != nil {
		if geo, err := s.realestate.GeoRecords(); err == nil {
			for _, g := range geo {
				for _, id := range g.ParcelIDs {
					if id = strings.TrimSpace(id); id != "" {
						drawn[id] = true
					}
				}
			}
		}
	}
	return drawn
}

// handleParcelsStudy returns the WIDE assessor layer — every lot in the three
// study neighborhoods (~2,450), minus the ones a richer layer already draws.
// Its own route rather than part of /api/parcels because it is ~1.5 MB and only
// the map wants it; the spreadsheet and the CSV export stay on the records.
func (s *Server) handleParcelsStudy(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		writeJSON(w, map[string]any{"parcels": []any{}, "meta": nil})
		return
	}
	records, _ := s.realestate.Parcels()
	layer := s.studyLayer()
	out := realestate.StudyParcelsExcept(layer, s.drawnParcelIDs(records))
	if out == nil {
		out = []realestate.Parcel{}
	}
	writeJSONZip(w, r, map[string]any{
		"parcels": out, "meta": layer.Meta,
		"age": layer.Meta.StudyAge(time.Now()),
	})
}

// handleParcelLog appends one owner note to a parcel's `## log` (newest-first),
// dated — the same guarded write path the property log uses.
func (s *Server) handleParcelLog(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "parcels not available", http.StatusServiceUnavailable)
		return
	}
	var b struct{ Text string }
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Text) == "" {
		httpError(w, errBadRequest("text is required"))
		return
	}
	p, ok := s.realestate.GetParcel(r.PathValue("slug"))
	if !ok {
		http.Error(w, "parcel not found", http.StatusNotFound)
		return
	}
	line := time.Now().Format("2006-01-02") + " " + strings.TrimSpace(b.Text)
	if err := s.vault.PrependLogLine(p.Path, line); err != nil {
		httpError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{p.Path})
	}
	if fresh, ok := s.realestate.GetParcel(r.PathValue("slug")); ok {
		writeJSON(w, fresh)
		return
	}
	writeJSON(w, p)
}

// handleParcelsExport streams the study parcels as a CSV spreadsheet download.
func (s *Server) handleParcelsExport(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		http.Error(w, "parcels not available", http.StatusServiceUnavailable)
		return
	}
	parcels, err := s.realestate.Parcels()
	if err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=\"study-parcels.csv\"")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"address", "owner", "owner_address", "acquired", "tax_status",
		"tax_balance_due", "assessed", "land_use", "parcel_id", "city_block", "ward", "notes"})
	for _, p := range parcels {
		acquired := p.SaleDate
		if acquired == "" {
			acquired = p.RecDate
		}
		_ = cw.Write([]string{
			p.Address, p.Owner, p.OwnerAddr, acquired, p.TaxStatus,
			strconv.FormatFloat(p.TaxBalDue, 'f', 2, 64),
			strconv.FormatFloat(p.Assessed, 'f', 0, 64),
			p.LandUse, p.ParcelID, p.CityBlock, p.Ward,
			strings.Join(p.Log, " | "),
		})
	}
	cw.Flush()
}
