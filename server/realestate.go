package server

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/realestate"
	"manifest/record"
)

// PROPERTIES — the real-estate cockpit over system/realestate/ records
// (real-estate-management plan, Pass 1). Reads come from the realestate service;
// the writes (create a property, quick-add a log line, quick-add a ledger row) go
// through the vaultwriter database-class allow-list and reindex the touched record.

func (s *Server) UseRealestate(svc *realestate.Service, root, dataDir string) {
	s.realestate = svc
	s.realestateRoot = root
	s.bgParcelsPath = filepath.Join(dataDir, "realestate", "bgParcels.json")
	s.reImport = realestate.NewImportMemory(dataDir)
	s.geocoder = realestate.NewGeocoder(dataDir)
	s.statements = realestate.NewStatementStore(dataDir)
}

// UseRePortal points the deals.json publish at the ooda site checkout
// (empty disables the feature; the client hides the button).
func (s *Server) UseRePortal(path string) { s.rePortalPath = path }

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
	writeJSON(w, map[string]any{"properties": props, "deals": deals,
		"templates": s.realestate.Templates(), "rePortal": s.rePortalPath != "",
		// derived, never typed (RE spec §2): entity name → {owned, acquiring}
		"holdings": realestate.Holdings(props)})
}

// ---- global underwriting assumptions (RE spec §3 Settings / §4) ----

// assumptionsRel is the record path under the realestate root.
func (s *Server) assumptionsRel() string {
	return s.realestateRootOr() + "/" + realestate.AssumptionsFile
}

// loadAssumptions reads + parses the record (missing file → defaults).
func (s *Server) loadAssumptions() realestate.Assumptions {
	raw, _ := os.ReadFile(filepath.Join(s.vault.VaultRoot(), filepath.FromSlash(s.assumptionsRel())))
	return realestate.ParseAssumptions(string(raw))
}

// handleAssumptionsGet returns the 15 global values + labels + canonical order
// + the derived override index (who overrides each key — from property/deal
// source.json sidecars, so Settings cannot claim an override nothing backs).
func (s *Server) handleAssumptionsGet(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{
		"values":    s.loadAssumptions().Values,
		"keys":      realestate.AssumptionKeys,
		"labels":    realestate.AssumptionLabels,
		"overrides": s.realestate.AssumptionOverrides(),
	})
}

// handleAssumptionsPut saves edited values (whole-map PUT; only known keys).
// Creates the record on first save.
func (s *Server) handleAssumptionsPut(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Values map[string]float64 `json:"values"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	rel := s.assumptionsRel()
	raw, err := os.ReadFile(filepath.Join(s.vault.VaultRoot(), filepath.FromSlash(rel)))
	if err != nil {
		if _, cerr := s.vault.CreateRecord(rel, realestate.SeedAssumptions()); cerr != nil {
			httpError(w, cerr)
			return
		}
		raw, _ = os.ReadFile(filepath.Join(s.vault.VaultRoot(), filepath.FromSlash(rel)))
	}
	a := realestate.ParseAssumptions(string(raw))
	for k, v := range b.Values {
		if err := a.SetAssumption(k, v); err != nil {
			httpError(w, errBadRequest(err.Error()))
			return
		}
	}
	if err := s.vault.WriteCap("realestate", rel, []byte(realestate.EmitAssumptions(a))); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"values": a.Values})
}

// handlePublishDeals recomposes the ooda site's deals.json from the vault's
// source sidecars — the exact inverse of realestate-seed -expand: multi-parcel
// deals re-nest their members under properties[] in property_slugs order (the
// key is dropped); single-parcel deals' property sources ARE the full deal
// object and pass through verbatim. Array order follows the existing
// deals.json; template deals with no vault record are kept as-is (reported);
// vault deal records absent from the template append at the end. Writes ONLY
// <rePortalPath>/src/data/deals.json — the owner reviews/commits that repo.
func (s *Server) handlePublishDeals(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	if s.rePortalPath == "" {
		httpError(w, errBadRequest("rePortalPath is not configured"))
		return
	}
	dealsPath := filepath.Join(s.rePortalPath, "src", "data", "deals.json")
	tmplRaw, err := os.ReadFile(dealsPath)
	if err != nil {
		httpError(w, errBadRequest("no deals.json at "+dealsPath+" — is rePortalPath a re-portal checkout?"))
		return
	}
	var template []json.RawMessage
	if err := json.Unmarshal(tmplRaw, &template); err != nil {
		httpError(w, errBadRequest("existing deals.json is not a JSON array: "+err.Error()))
		return
	}

	props, err := s.realestate.Properties()
	if err != nil {
		httpError(w, err)
		return
	}
	propPath := map[string]string{}
	for _, p := range props {
		propPath[p.Slug] = p.Path
	}
	deals, _ := s.realestate.Deals()
	dealPath := map[string]string{}
	for _, d := range deals {
		dealPath[d.Slug] = d.Path
	}

	// recompose one deal from the vault; (nil, false) when no vault record exists
	recompose := func(slug string) (map[string]any, bool) {
		if rel, ok := dealPath[slug]; ok { // multi-parcel deal record
			raw, ok2 := s.realestate.Source(rel)
			if !ok2 {
				return nil, false
			}
			var m map[string]any
			if json.Unmarshal(raw, &m) != nil {
				return nil, false
			}
			var members []any
			if sl, ok3 := m["property_slugs"].([]any); ok3 {
				for _, v := range sl {
					ps, _ := v.(string)
					rel2, ok4 := propPath[ps]
					if !ok4 {
						continue
					}
					if praw, ok5 := s.realestate.Source(rel2); ok5 {
						var pm any
						if json.Unmarshal(praw, &pm) == nil {
							members = append(members, pm)
						}
					}
				}
			}
			if len(members) == 0 {
				return nil, false
			}
			delete(m, "property_slugs")
			m["properties"] = members
			return m, true
		}
		// single-parcel: the property record keyed by the DEAL slug carries the full object
		if rel, ok := propPath[slug]; ok {
			if raw, ok2 := s.realestate.Source(rel); ok2 {
				var m map[string]any
				if json.Unmarshal(raw, &m) == nil {
					if members, ok3 := m["properties"].([]any); ok3 && len(members) > 0 {
						return m, true
					}
				}
			}
		}
		return nil, false
	}

	countProps := func(m map[string]any) int {
		if members, ok := m["properties"].([]any); ok {
			return len(members)
		}
		return 0
	}

	var out []any
	seen := map[string]bool{}
	var kept []string
	nDeals, nProps := 0, 0
	for _, traw := range template {
		var t struct {
			Slug string `json:"slug"`
		}
		_ = json.Unmarshal(traw, &t)
		seen[t.Slug] = true
		if m, ok := recompose(t.Slug); ok {
			out = append(out, m)
			nDeals++
			nProps += countProps(m)
			continue
		}
		var asIs any
		_ = json.Unmarshal(traw, &asIs)
		out = append(out, asIs)
		kept = append(kept, t.Slug)
	}
	for _, d := range deals { // new vault deals → append
		if seen[d.Slug] {
			continue
		}
		if m, ok := recompose(d.Slug); ok {
			out = append(out, m)
			nDeals++
			nProps += countProps(m)
		}
	}

	pretty, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		httpError(w, err)
		return
	}
	if err := os.WriteFile(dealsPath, append(pretty, '\n'), 0o644); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"deals": nDeals, "properties": nProps, "kept": kept, "path": dealsPath})
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
	// ?slug= → one record's features only, no background layer (the property
	// page's SVG parcel thumbnail — cheap, not the 1,551-parcel map payload).
	if slug := r.URL.Query().Get("slug"); slug != "" {
		for _, g := range records {
			if strings.EqualFold(g.Slug, slug) {
				writeJSON(w, map[string]any{"records": []realestate.GeoRecord{g}, "bg": nil})
				return
			}
		}
		writeJSON(w, map[string]any{"records": []any{}, "bg": nil})
		return
	}
	rendered := map[string]bool{}
	for i := range records {
		for _, id := range records[i].ParcelIDs {
			rendered[id] = true
		}
		// Pin fallback for polygon-less records: frontmatter lat/lng wins; else the
		// geocode cache; a miss enqueues a background resolve (pins appear next load).
		g := &records[i]
		if len(g.Features) == 0 && g.Lat == 0 && g.Lng == 0 && s.geocoder != nil && g.Title != "" {
			if lat, lng, ok := s.geocoder.Cached(g.Title); ok {
				g.Lat, g.Lng = lat, lng
			} else if g.Type == "property" {
				s.geocoder.Enqueue(g.Title)
			}
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

// slugify makes a filesystem-safe, link-friendly slug from an address/title
// (kernel rule, uncapped — file basenames carry the full address).
func slugify(s string) string { return record.Slug(s, 0) }

// handlePropertyCreate is the Board's `＋ property` ghost row: address (→ slug),
// optional entity/kind/status → a new system/realestate/<slug>.md record + an
// empty ledger csv. Entity is left blank unless provided (Board groups by it).
func (s *Server) handlePropertyCreate(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil || s.index == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Address, Entity, Kind, Status, Template, Deal string
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
	// Template pick seeds the WORK PLAN (pass-5: the work list is the budget —
	// the legacy budget-table seeding is gone; stage names + default weeks come
	// from the template record's ## stages).
	work := ""
	if tpl := strings.TrimSpace(b.Template); tpl != "" {
		for _, t := range s.realestate.Templates() {
			if strings.EqualFold(t.Slug, tpl) && len(t.Stages) > 0 {
				var stages []realestate.WorkStage
				for _, st := range t.Stages {
					ns := realestate.WorkStage{Text: st.Text}
					if st.Weeks > 0 {
						ns.Fields = append(ns.Fields, realestate.WorkField{Key: "weeks",
							Value: strconv.FormatFloat(st.Weeks, 'f', -1, 64)})
					}
					stages = append(stages, ns)
				}
				work = realestate.EmitWork(stages)
				break
			}
		}
	}

	// entity is a hard link to a record (same rule as the field editor)
	entity := strings.TrimSpace(b.Entity)
	if entity != "" {
		matched := ""
		for _, e := range s.realestate.Entities() {
			if strings.EqualFold(e.Name, entity) || strings.EqualFold(e.Slug, entity) {
				matched = e.Name
				break
			}
		}
		if matched == "" {
			httpError(w, errBadRequest("no entity record named "+entity+" — create it in SETTINGS first"))
			return
		}
		entity = matched
	}

	var fm strings.Builder
	fm.WriteString("---\ncategories: [property]\n")
	if entity != "" {
		fm.WriteString("entity: " + entity + "\n")
	}
	fm.WriteString("address: " + strings.TrimSpace(b.Address) + "\n")
	fm.WriteString("status: " + status + "\n")
	fm.WriteString("kind: " + kind + "\n")
	fm.WriteString("control: owned\n")
	if d := strings.TrimSpace(b.Deal); d != "" {
		fm.WriteString("deal: \"[[" + d + "]]\"\n")
	}
	fm.WriteString("hidden: false\n")
	fm.WriteString("---\n\n# " + strings.TrimSpace(b.Address) + "\n")
	if work != "" {
		fm.WriteString("\n## work\n" + work)
	}
	fm.WriteString("\n## log\n")

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

// propertyStatuses is the spec's lifecycle enum (§1) — the only values the
// field editor accepts for status.
var propertyStatuses = map[string]bool{
	"negotiating": true, "under_contract": true, "pre_development": true,
	"construction": true, "completed": true, "leased": true, "listed": true, "sold": true,
}
var propertyKinds = map[string]bool{"rehab": true, "new-construction": true, "mixed": true, "hold": true}

// handlePropertyField is the quick-edit path (Board status chip, page chips):
// one scalar frontmatter field per call, allow-listed keys, enum-checked values.
func (s *Server) handlePropertyField(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	var b struct{ Key, Value string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	key := strings.ToLower(strings.TrimSpace(b.Key))
	val := strings.TrimSpace(b.Value)
	switch key {
	case "status":
		val = strings.ToLower(val)
		if !propertyStatuses[val] {
			httpError(w, errBadRequest("unknown status"))
			return
		}
	case "kind":
		val = strings.ToLower(val)
		if !propertyKinds[val] {
			httpError(w, errBadRequest("unknown kind"))
			return
		}
	case "entity":
		// HARD LINK to an entity record — never free text ("" unlinks). The
		// canonical spelling comes from the record.
		if val != "" {
			matched := ""
			for _, e := range s.realestate.Entities() {
				if strings.EqualFold(e.Name, val) || strings.EqualFold(e.Slug, val) {
					matched = e.Name
					break
				}
			}
			if matched == "" {
				httpError(w, errBadRequest("no entity record named "+val+" — create it in SETTINGS first"))
				return
			}
			val = matched
		}
	case "work-start": // schedule anchor (§3) — ISO date or "" to clear
		if val != "" {
			if _, err := time.Parse("2006-01-02", val); err != nil {
				httpError(w, errBadRequest("work-start must be YYYY-MM-DD"))
				return
			}
		}
	case "owner", "owner-addr", "owner-since": // owner of record — free text ("" clears)
	case "from": // the seller while acquiring (RE spec §2 OWNER) — free text, "" once closed
	case "until": // the exit condition — free text ("" clears)
	case "drive", "agc": // artifact links — URLs, linked never mirrored ("" clears)
		if val != "" && !strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://") {
			httpError(w, errBadRequest(key+" must be a URL"))
			return
		}
	default:
		httpError(w, errBadRequest("key must be status, kind, entity, work-start, from, until, drive, agc, or owner/owner-addr/owner-since"))
		return
	}
	rel, ok := s.propertyRel(r.PathValue("slug"))
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	if err := s.vault.SetFrontmatterField(rel, key, val); err != nil {
		httpError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{rel})
	}
	s.respondProperty(w, r.PathValue("slug"))
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
		Date, Type, Category, Vendor, Contractor, Status, Note, Doc, WorkID string
		Amount                                                              float64
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
	p, ok := s.realestate.Get(r.PathValue("slug"))
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	rel := p.Path
	date := strings.TrimSpace(b.Date)
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	status := strings.TrimSpace(b.Status)
	if status == "" && b.Type == "expense" {
		status = "paid"
	}
	// tether: freeze the work id in the record first, then store the token in note
	s.tetherWorkID(p, strings.TrimSpace(b.WorkID))
	row := []string{
		date, b.Type, strings.TrimSpace(b.Category), strings.TrimSpace(b.Vendor),
		strings.TrimSpace(b.Contractor), strconv.FormatFloat(b.Amount, 'f', -1, 64),
		status, noteWithWork(b.Note, strings.TrimSpace(b.WorkID)), strings.TrimSpace(b.Doc),
	}
	if err := s.vault.AppendLedgerRow(realestate.LedgerRel(rel), realestate.LedgerHeader, row); err != nil {
		httpError(w, err)
		return
	}
	s.respondProperty(w, r.PathValue("slug"))
}

// ---- work management (pass-4): stages · todos · tethered money ----

// handlePropertyWork is the single op endpoint for the `## work` section —
// instant semantics (a checkbox is not a document edit): parse → mutate in
// memory → EmitWork → surgical ReplaceSection → reindex → re-parsed property.
func (s *Server) handlePropertyWork(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	p, ok := s.realestate.Get(slug)
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	var b struct {
		Op       string `json:"op"` // seed | add-stage | add-todo | check | edit | delete | set-field
		ID       string `json:"id"`
		StageID  string `json:"stageId"`
		Text     string `json:"text"`
		Checked  bool   `json:"checked"`
		Template string `json:"template"` // rehab | new-build | phases | empty
		Field    string `json:"field"`    // set-field: est | weeks
		Value    string `json:"value"`    // set-field: numeric string ("" clears)
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	stages := p.Work
	find := func(id string) (si, ti int) {
		for i := range stages {
			if stages[i].ID == id {
				return i, -1
			}
			for j := range stages[i].Todos {
				if stages[i].Todos[j].ID == id {
					return i, j
				}
			}
		}
		return -1, -1
	}
	switch b.Op {
	case "seed":
		if len(stages) > 0 {
			httpError(w, errBadRequest("work section already exists"))
			return
		}
		var names []string
		var weeks []float64
		// template RECORDS own the stage seeds (§3) — consts are the fallback
		tplStages := func(slug string) bool {
			for _, t := range s.realestate.Templates() {
				if strings.EqualFold(t.Slug, slug) && len(t.Stages) > 0 {
					for _, st := range t.Stages {
						names = append(names, st.Text)
						weeks = append(weeks, st.Weeks)
					}
					return true
				}
			}
			return false
		}
		switch b.Template {
		case "rehab":
			if !tplStages("gut-rehab") {
				names = realestate.RehabStages
			}
		case "new-build":
			if !tplStages("new-build") {
				names = realestate.NewBuildStages
			}
		case "phases":
			names = s.phaseNames(p) // seeded FROM site phases; stores never sync
			if len(names) == 0 {
				httpError(w, errBadRequest("no construction phases in the source data"))
				return
			}
		case "empty":
			names = []string{"Next up"}
		default:
			httpError(w, errBadRequest("unknown template"))
			return
		}
		for i, n := range names {
			st := realestate.WorkStage{Text: n}
			if i < len(weeks) && weeks[i] > 0 {
				st.Fields = append(st.Fields, realestate.WorkField{Key: "weeks",
					Value: strconv.FormatFloat(weeks[i], 'f', -1, 64)})
			}
			stages = append(stages, st)
		}
	case "add-stage":
		if strings.TrimSpace(b.Text) == "" {
			httpError(w, errBadRequest("text is required"))
			return
		}
		stages = append(stages, realestate.WorkStage{Text: strings.TrimSpace(b.Text)})
	case "add-todo":
		si, ti := find(b.StageID)
		if si < 0 || ti >= 0 {
			httpError(w, errBadRequest("stageId must name a stage"))
			return
		}
		if strings.TrimSpace(b.Text) == "" {
			httpError(w, errBadRequest("text is required"))
			return
		}
		stages[si].Todos = append(stages[si].Todos, realestate.WorkTodo{Text: strings.TrimSpace(b.Text)})
	case "check":
		si, ti := find(b.ID)
		if si < 0 {
			http.Error(w, "work item not found", http.StatusNotFound)
			return
		}
		if ti < 0 {
			stages[si].Checked = b.Checked
			// schedule pin (§3): checking a stage stamps its end date
			done := ""
			if b.Checked {
				done = time.Now().Format("2006-01-02")
			}
			realestate.SetWorkField(stages, b.ID, "done", done)
		} else {
			stages[si].Todos[ti].Checked = b.Checked
		}
	case "set-field":
		// the money slot / weeks editor: writes [est:: N] or [weeks:: N] on a
		// stage or todo (empty value clears the field).
		si, _ := find(b.ID)
		if si < 0 {
			http.Error(w, "work item not found", http.StatusNotFound)
			return
		}
		key := strings.ToLower(strings.TrimSpace(b.Field))
		val := strings.TrimSpace(b.Value)
		if key != "est" && key != "weeks" {
			httpError(w, errBadRequest("field must be est or weeks"))
			return
		}
		if val != "" {
			if _, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(val, "$", ""), ",", ""), 64); err != nil {
				httpError(w, errBadRequest(key+" must be a number"))
				return
			}
		}
		realestate.SetWorkField(stages, b.ID, key, val)
	case "edit":
		si, ti := find(b.ID)
		if si < 0 {
			http.Error(w, "work item not found", http.StatusNotFound)
			return
		}
		if strings.TrimSpace(b.Text) == "" {
			httpError(w, errBadRequest("text is required"))
			return
		}
		if ti < 0 {
			stages[si].Text = strings.TrimSpace(b.Text)
		} else {
			stages[si].Todos[ti].Text = strings.TrimSpace(b.Text)
		}
	case "delete":
		si, ti := find(b.ID)
		if si < 0 {
			http.Error(w, "work item not found", http.StatusNotFound)
			return
		}
		if ti < 0 {
			stages = append(stages[:si], stages[si+1:]...)
		} else {
			st := &stages[si]
			st.Todos = append(st.Todos[:ti], st.Todos[ti+1:]...)
		}
		// cascade: BID rows tethered to the deleted node (a stage delete catches
		// its todos' tethers via the id prefix) die with it — they are artifacts
		// of the task. Expense rows are REAL transactions and survive untethered.
		for _, row := range p.Ledger {
			if !strings.EqualFold(row.Type, "bid") || row.WorkID == "" {
				continue
			}
			if row.WorkID == b.ID || strings.HasPrefix(row.WorkID, b.ID+"/") {
				if err := s.vault.DeleteLedgerRow(realestate.LedgerRel(p.Path), ledgerRecord(row)); err != nil {
					httpError(w, err)
					return
				}
			}
		}
	default:
		httpError(w, errBadRequest("unknown op"))
		return
	}
	if err := s.writeWork(p.Path, stages); err != nil {
		httpError(w, err)
		return
	}
	s.syncHardCosts(slug)
	s.respondProperty(w, slug)
}

// syncHardCosts derives hard_costs in the source sidecar from the work plan
// (Σ stage est totals) — the underwrite number the ooda site's returns run on.
// Only writes once the plan is actually estimated (Σ > 0) and only on change;
// unestimated properties keep their hand-entered underwrite figure. Patches at
// the same nesting sourceMoney reads from (top level, or properties[0] for a
// single-parcel full-deal shape); unknown fields ride along untouched.
func (s *Server) syncHardCosts(slug string) {
	p, ok := s.realestate.Get(slug)
	if !ok {
		return
	}
	hard := 0.0
	for _, st := range p.Work {
		hard += st.EstTotal
	}
	if hard <= 0 {
		return
	}
	raw, ok := s.realestate.Source(p.Path)
	if !ok {
		return
	}
	var src map[string]any
	if json.Unmarshal(raw, &src) != nil {
		return
	}
	target := src
	if props, ok := src["properties"].([]any); ok && len(props) > 0 {
		if first, ok := props[0].(map[string]any); ok {
			target = first
		}
	}
	if cur, ok := target["hard_costs"].(float64); ok && cur == hard {
		return
	}
	target["hard_costs"] = hard
	out, err := json.MarshalIndent(src, "", "  ")
	if err != nil {
		return
	}
	if err := s.vault.WriteSourceJSON(realestate.SourceRel(p.Path), out); err == nil && s.index != nil {
		_ = s.index.ReindexPaths([]string{p.Path})
	}
}

// writeWork emits + surgically replaces the `## work` section, then reindexes.
func (s *Server) writeWork(rel string, stages []realestate.WorkStage) error {
	if err := s.vault.ReplaceSection(rel, "work", realestate.EmitWork(stages)); err != nil {
		return err
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{rel})
	}
	return nil
}

// phaseNames pulls construction_phases names from the source sidecar (property
// slice or single-parcel full-deal shape) for the "from site phases" seed.
func (s *Server) phaseNames(p realestate.Property) []string {
	raw, ok := s.realestate.Source(p.Path)
	if !ok {
		return nil
	}
	var v struct {
		ConstructionPhases []struct {
			Phase string `json:"phase"`
		} `json:"construction_phases"`
		Properties []struct {
			ConstructionPhases []struct {
				Phase string `json:"phase"`
			} `json:"construction_phases"`
		} `json:"properties"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	phases := v.ConstructionPhases
	if len(phases) == 0 && len(v.Properties) > 0 {
		phases = v.Properties[0].ConstructionPhases
	}
	var out []string
	for _, ph := range phases {
		if strings.TrimSpace(ph.Phase) != "" {
			out = append(out, strings.TrimSpace(ph.Phase))
		}
	}
	return out
}

// tetherWorkID freezes a derived work id into the record (goals Promote
// pattern) so the tether survives renames; no-op when already explicit.
func (s *Server) tetherWorkID(p realestate.Property, workID string) {
	if workID == "" {
		return
	}
	stages := p.Work
	if realestate.FreezeWorkID(stages, workID) {
		_ = s.writeWork(p.Path, stages)
	}
}

// noteWithWork renders the canonical stored note (display note + tether token).
func noteWithWork(note, workID string) string {
	note = strings.TrimSpace(note)
	if workID == "" {
		return note
	}
	tok := "[work:: " + workID + "]"
	if note == "" {
		return tok
	}
	return note + " " + tok
}

// ---- docs (spec §3): the property's document folder, vault-homed ----

type docView struct {
	Name  string `json:"name"`
	Path  string `json:"path"` // vault-relative (for the raw-doc endpoint)
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
}

func (s *Server) docsDir(slug string) string {
	return path.Join(s.realestateRootOr(), "docs", slug)
}

func (s *Server) handlePropertyDocs(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	if _, ok := s.propertyRel(slug); !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	rel := s.docsDir(slug)
	entries, _ := os.ReadDir(vaultJoin(s.vault.VaultRoot(), rel))
	docs := []docView{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		docs = append(docs, docView{Name: e.Name(), Path: rel + "/" + e.Name(), Size: info.Size(), MTime: info.ModTime().Unix()})
	}
	writeJSON(w, map[string]any{"docs": docs})
}

// handlePropertyDocUpload is the app's FIRST multipart/binary path: drag-drop
// files land in <reRoot>/docs/<slug>/ via the guarded SaveDoc (sanitized name,
// extension allowlist, 25MB cap, write-once).
func (s *Server) handlePropertyDocUpload(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	if _, ok := s.propertyRel(slug); !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	if err := r.ParseMultipartForm(int64(vaultwriterMaxDoc) + 1<<20); err != nil {
		httpError(w, err)
		return
	}
	var saved []string
	for _, fh := range r.MultipartForm.File["file"] {
		f, err := fh.Open()
		if err != nil {
			httpError(w, err)
			return
		}
		data, err := io.ReadAll(io.LimitReader(f, int64(vaultwriterMaxDoc)+1))
		f.Close()
		if err != nil {
			httpError(w, err)
			return
		}
		rel, err := s.vault.SaveDoc(s.docsDir(slug), fh.Filename, data)
		if err != nil {
			httpError(w, err)
			return
		}
		saved = append(saved, rel)
	}
	if len(saved) == 0 {
		httpError(w, errBadRequest("no files in upload"))
		return
	}
	writeJSON(w, map[string]any{"saved": saved})
}

// handleReceiptUpload attaches a receipt to ONE ledger row: multipart `file` +
// `original` field (JSON LedgerRow, exact-match). The file lands in the
// property's docs folder; the row's doc column gets the saved filename — the
// evidence that reconciles a firm price without a bank transaction.
func (s *Server) handleReceiptUpload(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	rel, ok := s.propertyRel(slug)
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	if err := r.ParseMultipartForm(int64(vaultwriterMaxDoc) + 1<<20); err != nil {
		httpError(w, err)
		return
	}
	var orig realestate.LedgerRow
	if err := json.Unmarshal([]byte(r.FormValue("original")), &orig); err != nil {
		httpError(w, errBadRequest("original ledger row required"))
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		httpError(w, errBadRequest("no file in upload"))
		return
	}
	f, err := files[0].Open()
	if err != nil {
		httpError(w, err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(vaultwriterMaxDoc)+1))
	f.Close()
	if err != nil {
		httpError(w, err)
		return
	}
	savedRel, err := s.vault.SaveDoc(s.docsDir(slug), files[0].Filename, data)
	if err != nil {
		httpError(w, err)
		return
	}
	origRec := ledgerRecord(orig)
	repl := orig
	repl.Doc = path.Base(savedRel)
	if err := s.vault.UpdateLedgerRow(realestate.LedgerRel(rel), origRec, ledgerRecord(repl)); err != nil {
		httpError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{rel})
	}
	s.respondProperty(w, slug)
}

// handleRealestateDoc serves a doc/export file raw (view a PDF, download a csv).
// Read-only, pinned under the realestate root, traversal-guarded.
func (s *Server) handleRealestateDoc(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		http.Error(w, "vault unavailable", http.StatusServiceUnavailable)
		return
	}
	rel := path.Clean(strings.TrimPrefix(r.URL.Query().Get("path"), "/"))
	root := s.realestateRootOr()
	if !strings.HasPrefix(rel, root+"/docs/") && !strings.HasPrefix(rel, root+"/exports/") {
		http.Error(w, "not a docs/exports path", http.StatusForbidden)
		return
	}
	full := vaultJoin(s.vault.VaultRoot(), rel)
	if full == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	http.ServeFile(w, r, full)
}

// vaultJoin joins vault-relative rel onto root, refusing traversal escapes.
func vaultJoin(root, rel string) string {
	full := filepath.Join(root, filepath.FromSlash(rel))
	cleanRoot := filepath.Clean(root)
	if rl, err := filepath.Rel(cleanRoot, full); err != nil || rl == ".." || strings.HasPrefix(rl, ".."+string(filepath.Separator)) {
		return ""
	}
	return full
}

// vaultwriterMaxDoc mirrors vaultwriter.MaxDocBytes for the multipart cap.
const vaultwriterMaxDoc = 25 << 20

// ---- source sidecars: the live canonical for the public-site data ----

// handlePropertySource GETs/PUTs a property record's engine-shaped source
// object. PUT takes the FULL object (the client round-trips everything it
// received, so unknown fields survive — the fidelity contract).
func (s *Server) handlePropertySource(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	rel, ok := s.propertyRel(r.PathValue("slug"))
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	s.serveSource(w, r, rel)
}

func (s *Server) serveSource(w http.ResponseWriter, r *http.Request, mdRel string) {
	switch r.Method {
	case http.MethodGet:
		raw, ok := s.realestate.Source(mdRel)
		if !ok {
			writeJSON(w, map[string]any{"source": nil})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"source":`))
		_, _ = w.Write(raw)
		_, _ = w.Write([]byte(`}`))
	case http.MethodPut:
		if s.vault == nil {
			http.Error(w, "vault unavailable", http.StatusServiceUnavailable)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			httpError(w, err)
			return
		}
		pretty, err := realestate.PrettySource(body)
		if err != nil {
			httpError(w, errBadRequest("body must be a JSON object"))
			return
		}
		if err := s.vault.WriteSourceJSON(realestate.SourceRel(mdRel), pretty); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// dealBySlug finds a deal record.
func (s *Server) dealBySlug(slug string) (realestate.Deal, bool) {
	deals, _ := s.realestate.Deals()
	for _, d := range deals {
		if strings.EqualFold(d.Slug, slug) {
			return d, true
		}
	}
	return realestate.Deal{}, false
}

// handleDealPage is the deal page's one read: record + source + member
// property projections + the aggregate actuals strip (manifest's own number).
func (s *Server) handleDealPage(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	d, ok := s.dealBySlug(r.PathValue("slug"))
	if !ok {
		http.Error(w, "deal not found", http.StatusNotFound)
		return
	}
	members := s.dealMemberProps(d.Slug)
	var agg realestate.Rollup
	withLedgers := 0
	for _, m := range members {
		agg.Budget += m.Rollup.Budget
		agg.Paid += m.Rollup.Paid
		agg.Committed += m.Rollup.Committed
		if len(m.Ledger) > 0 {
			withLedgers++
		}
	}
	if agg.Budget > 0 {
		agg.PaidPct = agg.Paid / agg.Budget
		agg.CommittedPct = agg.Committed / agg.Budget
	}
	src, _ := s.realestate.Source(d.Path)
	writeJSON(w, map[string]any{
		"deal": d, "source": src, "members": members,
		"actuals": agg, "membersWithLedgers": withLedgers,
	})
}

// dealMemberProps: members are the property records whose deal:: link names
// this slug (the authoritative join since -expand).
func (s *Server) dealMemberProps(slug string) []realestate.Property {
	props, err := s.realestate.Properties()
	if err != nil {
		return nil
	}
	var out []realestate.Property
	for _, p := range props {
		if strings.EqualFold(p.Deal, slug) {
			out = append(out, p)
		}
	}
	return out
}

// handleDealSource GET/PUTs the deal-level source object.
func (s *Server) handleDealSource(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	d, ok := s.dealBySlug(r.PathValue("slug"))
	if !ok {
		http.Error(w, "deal not found", http.StatusNotFound)
		return
	}
	s.serveSource(w, r, d.Path)
}

// dealStatuses: the deal enum includes "opportunity" (deals.json vocabulary).
var dealStatuses = map[string]bool{
	"negotiating": true, "under_contract": true, "pre_development": true,
	"construction": true, "completed": true, "leased": true, "listed": true,
	"sold": true, "opportunity": true,
}

// handleDealField edits a deal md frontmatter scalar (status today).
func (s *Server) handleDealField(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	d, ok := s.dealBySlug(r.PathValue("slug"))
	if !ok {
		http.Error(w, "deal not found", http.StatusNotFound)
		return
	}
	var b struct{ Key, Value string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	key := strings.ToLower(strings.TrimSpace(b.Key))
	val := strings.ToLower(strings.TrimSpace(b.Value))
	if key != "status" || !dealStatuses[val] {
		httpError(w, errBadRequest("only status (deal enum) is editable here"))
		return
	}
	if err := s.vault.SetFrontmatterField(d.Path, "status", val); err != nil {
		httpError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{d.Path})
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- ledger row mutation (property page inline edit/delete) ----

func ledgerRecord(r realestate.LedgerRow) []string {
	// on-disk note carries the tether token; RawNote (from a parsed row) wins,
	// else it's reconstructed canonically from Note + WorkID.
	note := strings.TrimSpace(r.RawNote)
	if note == "" {
		note = noteWithWork(r.Note, strings.TrimSpace(r.WorkID))
	}
	return []string{
		strings.TrimSpace(r.Date), strings.TrimSpace(r.Type), strings.TrimSpace(r.Category),
		strings.TrimSpace(r.Vendor), strings.TrimSpace(r.Contractor),
		strconv.FormatFloat(r.Amount, 'f', -1, 64),
		strings.TrimSpace(r.Status), note, strings.TrimSpace(r.Doc),
	}
}

func (s *Server) handleLedgerMutate(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	rel, ok := s.propertyRel(slug)
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	var b struct {
		Original    realestate.LedgerRow  `json:"original"`
		Replacement *realestate.LedgerRow `json:"replacement"` // nil → delete
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	orig := ledgerRecord(b.Original)
	// The parsed ledger normalizes amounts; the on-disk row may carry "1240.55"
	// vs "1240.550000" — ledgerRecord round-trips through the same float format
	// used on write, so exact-match holds for app-written rows; hand-edited rows
	// match via rowsEqual's trimmed comparison.
	var err error
	if b.Replacement == nil {
		err = s.vault.DeleteLedgerRow(realestate.LedgerRel(rel), orig)
	} else {
		if t := strings.ToLower(strings.TrimSpace(b.Replacement.Type)); t != "expense" && t != "bid" {
			httpError(w, errBadRequest("type must be expense or bid"))
			return
		}
		// replacement note is rebuilt canonically (never a stale echoed rawNote)
		b.Replacement.RawNote = ""
		if p, ok := s.realestate.Get(slug); ok {
			s.tetherWorkID(p, strings.TrimSpace(b.Replacement.WorkID))
		}
		err = s.vault.UpdateLedgerRow(realestate.LedgerRel(rel), orig, ledgerRecord(*b.Replacement))
	}
	if err != nil {
		httpError(w, err)
		return
	}
	s.respondProperty(w, slug)
}

// ---- exports (spec §3): the handshake files to the spreadsheets ----

// handleDealExportUnderwrite writes exports/<deal>-underwrite.csv + .json:
// per-property and per-category budget/paid/committed plus full ledger detail
// for every member that has a property record. Overwrite-allowed (regenerable).
func (s *Server) handleDealExportUnderwrite(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	deals, _ := s.realestate.Deals()
	var deal *realestate.Deal
	for i := range deals {
		if strings.EqualFold(deals[i].Slug, slug) {
			deal = &deals[i]
			break
		}
	}
	if deal == nil {
		http.Error(w, "deal not found", http.StatusNotFound)
		return
	}
	members := s.dealMembers(*deal)

	// csv: one summary row per property+STAGE (pass-5: work list = budget;
	// columns renamed est/committed/paid), then the ledger detail
	var buf strings.Builder
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"section", "property", "stage", "est", "committed", "paid", "date", "type", "vendor", "amount", "status", "note"})
	for _, p := range members {
		// full-project budget summary (category · budget · committed · paid)
		// above the stage detail
		if p.Project != nil {
			for _, c := range p.Project.Categories {
				_ = cw.Write([]string{"budget-summary", p.Address, c.Key, f2(c.Budget), f2(c.Committed), f2(c.Paid), "", "", "", "", "", ""})
			}
			_ = cw.Write([]string{"budget-summary", p.Address, "TOTAL", f2(p.Project.PlanTotal), f2(p.Project.Committed), f2(p.Project.Paid), "", "", "", "", "", ""})
		}
		for _, st := range p.Work {
			_ = cw.Write([]string{"rollup", p.Address, st.Text, f2(st.EstTotal), f2(st.Committed), f2(st.Paid), "", "", "", "", "", ""})
		}
		_ = cw.Write([]string{"rollup", p.Address, "TOTAL", f2(p.Rollup.Budget), f2(p.Rollup.Committed), f2(p.Rollup.Paid), "", "", "", "", "", ""})
		for _, lr := range p.Ledger {
			_ = cw.Write([]string{"ledger", p.Address, lr.WorkID, "", "", "", lr.Date, lr.Type, lr.Vendor + lr.Contractor, f2(lr.Amount), lr.Status, lr.Note})
		}
	}
	cw.Flush()

	jsonBody, _ := json.MarshalIndent(map[string]any{
		"deal": deal.Slug, "name": deal.Name, "members": members,
	}, "", "  ")

	base := s.realestateRootOr() + "/exports/" + deal.Slug + "-underwrite"
	if err := s.vault.WriteExport(base+".csv", []byte(buf.String())); err != nil {
		httpError(w, err)
		return
	}
	if err := s.vault.WriteExport(base+".json", jsonBody); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"csv": base + ".csv", "json": base + ".json", "members": len(members)})
}

// dealMembers matches a deal's member address strings to property records
// (slug equality after slugification, or address prefix).
func (s *Server) dealMembers(d realestate.Deal) []realestate.Property {
	props, err := s.realestate.Properties()
	if err != nil {
		return nil
	}
	var out []realestate.Property
	for _, member := range d.Properties {
		ms := slugify(member)
		for _, p := range props {
			if p.Slug == ms || strings.HasPrefix(slugify(p.Address), ms) || strings.HasPrefix(ms, p.Slug) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// handleTaxExport (pass-5): the CPA package — one CSV per entity+year, THREE
// sections: property-tethered lines (owned by the entity), admin lines (the
// entity's own ledger), and intercompany lines grouped by paying→owning pair.
func (s *Server) handleTaxExport(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Entity string `json:"entity"`
		Year   string `json:"year"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Year) == "" {
		httpError(w, errBadRequest("year is required"))
		return
	}
	entity := strings.TrimSpace(b.Entity) // "" → the unassigned group
	props, err := s.realestate.Properties()
	if err != nil {
		httpError(w, err)
		return
	}
	var buf strings.Builder
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"section", "pair", "date", "property", "vendor", "type", "category",
		"amount", "receipt", "statement", "reconciled", "note"})
	lines := 0
	write := func(section, pair, prop string, lr realestate.LedgerRow, workText string) {
		cat := lr.Cat
		if cat == "" {
			cat = "hard"
		}
		if strings.EqualFold(lr.Type, "income") {
			cat = lr.Category // rent | capital
		}
		if workText != "" {
			cat += " · " + workText
		}
		reconciled := "n"
		if lr.Doc != "" || lr.Stmt != "" {
			reconciled = "y"
		}
		_ = cw.Write([]string{section, pair, lr.Date, prop, lr.Vendor + lr.Contractor,
			strings.ToLower(lr.Type), cat, f2(lr.Amount), lr.Doc, lr.Stmt, reconciled, lr.Note})
		lines++
	}
	inYear := func(lr realestate.LedgerRow) bool {
		return strings.HasPrefix(lr.Date, b.Year) &&
			(strings.EqualFold(lr.Type, "expense") || strings.EqualFold(lr.Type, "income"))
	}
	// 1. property section — rows in properties this entity OWNS (paid by it or unmarked)
	// 3. intercompany — collected while walking (either direction touching the entity)
	type icLine struct {
		pair, prop, workText string
		lr                   realestate.LedgerRow
	}
	var ic []icLine
	for _, p := range props {
		// tether id → "stage · task" for the category column
		workText := map[string]string{}
		for _, st := range p.Work {
			workText[st.ID] = st.Text
			for _, td := range st.Todos {
				workText[td.ID] = st.Text + " · " + td.Text
			}
		}
		owns := strings.EqualFold(strings.TrimSpace(p.Entity), entity)
		for _, lr := range p.Ledger {
			if !inYear(lr) {
				continue
			}
			payer := strings.TrimSpace(lr.PaidBy)
			income := strings.EqualFold(lr.Type, "income")
			switch {
			case owns && (income || payer == "" || strings.EqualFold(payer, entity)):
				write("property", "", p.Address, lr, workText[lr.WorkID])
			case owns && payer != "": // someone else paid for this entity's property
				ic = append(ic, icLine{payer + " → " + entity, p.Address, workText[lr.WorkID], lr})
			case !owns && strings.EqualFold(payer, entity) && !income: // this entity paid another's property
				owner := strings.TrimSpace(p.Entity)
				if owner == "" {
					owner = "unassigned"
				}
				ic = append(ic, icLine{entity + " → " + owner, p.Address, workText[lr.WorkID], lr})
			}
		}
	}
	// 2. admin section — the entity's own admin ledger
	if entity != "" {
		adminRel := s.realestateRootOr() + "/entities/" + slugify(entity) + ".ledger.csv"
		if raw, err := os.ReadFile(vaultJoin(s.vault.VaultRoot(), adminRel)); err == nil {
			for _, lr := range realestate.ParseLedgerBytes(raw) {
				if inYear(lr) {
					adminLr := lr
					adminLr.Cat = lr.Category // admin rows categorize by their own column
					write("admin", "", "", adminLr, "")
				}
			}
		}
	}
	sort.Slice(ic, func(i, j int) bool {
		if ic[i].pair != ic[j].pair {
			return ic[i].pair < ic[j].pair
		}
		return ic[i].lr.Date < ic[j].lr.Date
	})
	for _, l := range ic {
		write("intercompany", l.pair, l.prop, l.lr, l.workText)
	}
	cw.Flush()
	eslug := slugify(entity)
	if eslug == "" {
		eslug = "unassigned"
	}
	rel := s.realestateRootOr() + "/exports/tax-" + eslug + "-" + b.Year + ".csv"
	if err := s.vault.WriteExport(rel, []byte(buf.String())); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"csv": rel, "lines": lines})
}

// ---- entities & contractors (pass-5 §5/§6) ----

func (s *Server) handleEntitiesList(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		writeJSON(w, map[string]any{"entities": []any{}, "contractors": []any{}})
		return
	}
	writeJSON(w, map[string]any{
		"entities":    s.realestate.Entities(),
		"contractors": s.realestate.Contractors(),
		"bindings":    s.reImport.LabelBindings(),
	})
}

// handleEntityCreate makes an entity or contractor record (the autocomplete's
// quiet `create "<name>" →` completion — an explicit user action).
func (s *Server) handleEntityCreate(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil || s.index == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	var b struct{ Name, Kind string }
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Name) == "" {
		httpError(w, errBadRequest("name is required"))
		return
	}
	kind := "entity"
	dir := "entities"
	if b.Kind == "contractor" {
		kind, dir = "contractor", "contractors"
	}
	slug := slugify(b.Name)
	rel := path.Join(s.realestateRootOr(), dir, slug+".md")
	content := "---\ncategories: [" + kind + "]\nname: " + strings.TrimSpace(b.Name) + "\n---\n\n# " + strings.TrimSpace(b.Name) + "\n"
	if _, err := s.vault.CreateRecord(rel, content); err != nil {
		httpError(w, err)
		return
	}
	_ = s.index.ReindexPaths([]string{rel})
	writeJSON(w, map[string]any{"slug": slug, "name": strings.TrimSpace(b.Name), "path": rel})
}

// handleContractorTrade sets a contractor record's trade frontmatter field
// (the CONTRACTORS table's inline picker).
func (s *Server) handleContractorTrade(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	var b struct{ Trade string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	slug := r.PathValue("slug")
	var target *realestate.Entity
	for _, c := range s.realestate.Contractors() {
		if strings.EqualFold(c.Slug, slug) {
			t := c
			target = &t
			break
		}
	}
	if target == nil {
		http.Error(w, "contractor not found", http.StatusNotFound)
		return
	}
	if err := s.vault.SetFrontmatterField(target.Path, "trade", strings.ToLower(strings.TrimSpace(b.Trade))); err != nil {
		httpError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{target.Path})
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleEntitySave updates an entity's owners / admin-categories frontmatter
// (SETTINGS). Ownership cycles are rejected with a clear error.
func (s *Server) handleEntitySave(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	ents := s.realestate.Entities()
	var target *realestate.Entity
	for i := range ents {
		if strings.EqualFold(ents[i].Slug, slug) {
			target = &ents[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "entity not found", http.StatusNotFound)
		return
	}
	var b struct {
		Owners          *[]realestate.Owner         `json:"owners"`
		AdminCategories *[]string                   `json:"adminCategories"`
		Partnered       *bool                       `json:"partnered"`
		Accounts        *[]realestate.EntityAccount `json:"accounts"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if b.Partnered != nil {
		if err := s.vault.SetFrontmatterField(target.Path, "partnered", strconv.FormatBool(*b.Partnered)); err != nil {
			httpError(w, err)
			return
		}
	}
	if b.Accounts != nil {
		var items []string
		for _, a := range *b.Accounts {
			label := strings.TrimSpace(a.Label)
			if label == "" {
				continue
			}
			kind := strings.TrimSpace(a.Kind)
			state := strings.TrimSpace(a.State)
			if state == "" {
				state = "not-connected"
			}
			items = append(items, `"`+label+` | `+kind+` | `+state+`"`)
		}
		if err := s.vault.SetFrontmatterField(target.Path, "accounts", "["+strings.Join(items, ", ")+"]"); err != nil {
			httpError(w, err)
			return
		}
	}
	if b.Owners != nil {
		for _, o := range *b.Owners {
			if realestate.OwnershipCycle(ents, target.Slug, o.Ref) {
				httpError(w, errBadRequest("ownership cycle: "+o.Ref+" is already (transitively) owned by "+target.Slug))
				return
			}
		}
		var items []string
		for _, o := range *b.Owners {
			items = append(items, `"[[`+o.Ref+`]] `+strconv.FormatFloat(o.Percent, 'f', -1, 64)+`%"`)
		}
		if err := s.vault.SetFrontmatterField(target.Path, "owners", "["+strings.Join(items, ", ")+"]"); err != nil {
			httpError(w, err)
			return
		}
	}
	if b.AdminCategories != nil {
		var items []string
		for _, c := range *b.AdminCategories {
			if strings.TrimSpace(c) != "" {
				items = append(items, strings.TrimSpace(c))
			}
		}
		if err := s.vault.SetFrontmatterField(target.Path, "admin-categories", "["+strings.Join(items, ", ")+"]"); err != nil {
			httpError(w, err)
			return
		}
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{target.Path})
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleBindingSave edits a statement source-label → entity binding (SETTINGS).
func (s *Server) handleBindingSave(w http.ResponseWriter, r *http.Request) {
	if s.reImport == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	var b struct{ Label, Entity string }
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Label) == "" {
		httpError(w, errBadRequest("label is required"))
		return
	}
	s.reImport.BindLabel(b.Label, b.Entity)
	writeJSON(w, map[string]any{"bindings": s.reImport.LabelBindings()})
}

func f2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// importKey is the dedupe key: date + amount + lowercased vendor.
func importKey(date string, amount float64, vendor string) string {
	return strings.TrimSpace(date) + "|" + strconv.FormatFloat(amount, 'f', 2, 64) + "|" + strings.ToLower(strings.TrimSpace(vendor))
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
