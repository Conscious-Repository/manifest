package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"manifest/realestate"
)

// PROPERTIES — the real-estate cockpit over system/realestate/ records
// (real-estate-management plan, Pass 1). Reads come from the realestate service;
// the writes (create a property, quick-add a log line, quick-add a ledger row) go
// through the vaultwriter database-class allow-list and reindex the touched record.

func (s *Server) UseRealestate(svc *realestate.Service, root, bgParcelsPath string) {
	s.realestate = svc
	s.realestateRoot = root
	s.bgParcelsPath = bgParcelsPath
}

func (s *Server) realestateRootOr() string {
	if s.realestateRoot != "" {
		return s.realestateRoot
	}
	return "system/realestate"
}

func (s *Server) handlePropertiesList(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		writeJSON(w, map[string]any{"properties": []any{}, "deals": []any{}})
		return
	}
	props, err := s.realestate.Properties()
	if err != nil {
		httpError(w, err)
		return
	}
	deals, _ := s.realestate.Deals()
	writeJSON(w, map[string]any{"properties": props, "deals": deals})
}

// handlePropertiesGeo feeds the map: every record's parcel Features + the
// background-parcel layer (from dataDir, filtered so a parcel already rendered
// as a record never doubles underneath itself). Nothing here writes anywhere.
func (s *Server) handlePropertiesGeo(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		writeJSON(w, map[string]any{"records": []any{}, "bg": nil})
		return
	}
	records, err := s.realestate.GeoRecords()
	if err != nil {
		httpError(w, err)
		return
	}
	rendered := map[string]bool{}
	for _, g := range records {
		for _, id := range g.ParcelIDs {
			rendered[id] = true
		}
	}
	writeJSON(w, map[string]any{"records": records, "bg": s.backgroundParcels(rendered)})
}

// backgroundParcels loads bgParcels.json and drops features whose properties.id
// matches an already-rendered record parcel. Missing file → nil (map still works).
func (s *Server) backgroundParcels(rendered map[string]bool) any {
	if s.bgParcelsPath == "" {
		return nil
	}
	raw, err := os.ReadFile(s.bgParcelsPath)
	if err != nil {
		return nil
	}
	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Type       string          `json:"type"`
			Geometry   json.RawMessage `json:"geometry"`
			Properties struct {
				ID string `json:"id"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(raw, &fc); err != nil {
		return nil
	}
	feats := make([]any, 0, len(fc.Features))
	for _, f := range fc.Features {
		if rendered[f.Properties.ID] {
			continue
		}
		feats = append(feats, map[string]any{
			"type": "Feature", "geometry": f.Geometry,
			"properties": map[string]string{"id": f.Properties.ID},
		})
	}
	return map[string]any{"type": "FeatureCollection", "features": feats}
}

func (s *Server) handlePropertyGet(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	p, ok := s.realestate.Get(r.PathValue("slug"))
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	writeJSON(w, p)
}

var propIllegal = regexp.MustCompile(`[^a-z0-9]+`)

// slugify makes a filesystem-safe, link-friendly slug from an address/title.
func slugify(s string) string {
	return strings.Trim(propIllegal.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// handlePropertyCreate is the Board's `＋ property` ghost row: address (→ slug),
// optional entity/kind/status → a new system/realestate/<slug>.md record + an
// empty ledger csv. Entity is left blank unless provided (Board groups by it).
func (s *Server) handlePropertyCreate(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil || s.index == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Address, Entity, Kind, Status string
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Address) == "" {
		httpError(w, errBadRequest("address is required"))
		return
	}
	slug := slugify(b.Address)
	if slug == "" {
		httpError(w, errBadRequest("address has no usable characters"))
		return
	}
	status := strings.ToLower(strings.TrimSpace(b.Status))
	if status == "" {
		status = "negotiating"
	}
	kind := strings.ToLower(strings.TrimSpace(b.Kind))
	if kind == "" {
		kind = "rehab"
	}

	var fm strings.Builder
	fm.WriteString("---\ncategories: [property]\n")
	if e := strings.TrimSpace(b.Entity); e != "" {
		fm.WriteString("entity: " + e + "\n")
	}
	fm.WriteString("address: " + strings.TrimSpace(b.Address) + "\n")
	fm.WriteString("status: " + status + "\n")
	fm.WriteString("kind: " + kind + "\n")
	fm.WriteString("control: owned\n")
	fm.WriteString("hidden: false\n")
	fm.WriteString("---\n\n# " + strings.TrimSpace(b.Address) + "\n\n## budget\n\n## log\n")

	rel := path.Join(s.realestateRootOr(), "properties", slug+".md")
	if _, err := s.vault.CreateRecord(rel, fm.String()); err != nil {
		httpError(w, err)
		return
	}
	// Seed an empty ledger (header only) beside the record.
	ledgerRel := realestate.LedgerRel(rel)
	_, _ = s.vault.CreateRecord(ledgerRel, strings.Join(realestate.LedgerHeader, ",")+"\n")
	_ = s.index.ReindexPaths([]string{rel})
	s.respondProperty(w, slug)
}

// handlePropertyLog appends a dated free line to the record's `## log` (newest-first).
func (s *Server) handlePropertyLog(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	var b struct{ Text string }
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Text) == "" {
		httpError(w, errBadRequest("text is required"))
		return
	}
	rel, ok := s.propertyRel(r.PathValue("slug"))
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	line := time.Now().Format("2006-01-02") + " " + strings.TrimSpace(b.Text)
	if err := s.vault.PrependLogLine(rel, line); err != nil {
		httpError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{rel})
	}
	s.respondProperty(w, r.PathValue("slug"))
}

// handlePropertyLedger appends one ledger row (expense or bid) to the csv sidecar.
func (s *Server) handlePropertyLedger(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Date, Type, Category, Vendor, Contractor, Status, Note, Doc string
		Amount                                                      float64
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	b.Type = strings.ToLower(strings.TrimSpace(b.Type))
	if b.Type != "expense" && b.Type != "bid" {
		httpError(w, errBadRequest("type must be expense or bid"))
		return
	}
	rel, ok := s.propertyRel(r.PathValue("slug"))
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	date := strings.TrimSpace(b.Date)
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	status := strings.TrimSpace(b.Status)
	if status == "" && b.Type == "expense" {
		status = "paid"
	}
	row := []string{
		date, b.Type, strings.TrimSpace(b.Category), strings.TrimSpace(b.Vendor),
		strings.TrimSpace(b.Contractor), strconv.FormatFloat(b.Amount, 'f', -1, 64),
		status, strings.TrimSpace(b.Note), strings.TrimSpace(b.Doc),
	}
	if err := s.vault.AppendLedgerRow(realestate.LedgerRel(rel), realestate.LedgerHeader, row); err != nil {
		httpError(w, err)
		return
	}
	s.respondProperty(w, r.PathValue("slug"))
}

// propertyRel resolves a slug to its vault-relative .md path via the store.
func (s *Server) propertyRel(slug string) (string, bool) {
	p, ok := s.realestate.Get(slug)
	if !ok {
		return "", false
	}
	return p.Path, true
}

// respondProperty returns the freshly re-parsed property after a write.
func (s *Server) respondProperty(w http.ResponseWriter, slug string) {
	if p, ok := s.realestate.Get(slug); ok {
		writeJSON(w, p)
		return
	}
	writeJSON(w, realestate.Property{Slug: slug})
}
