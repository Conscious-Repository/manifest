package server

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/realestate"
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
	writeJSON(w, map[string]any{"properties": props, "deals": deals, "templates": s.realestate.Templates()})
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
	// Template pick seeds the budget table (spec §3: address·entity·kind·template
	// is the entire creation form).
	budget := ""
	if tpl := strings.TrimSpace(b.Template); tpl != "" {
		for _, t := range s.realestate.Templates() {
			if strings.EqualFold(t.Slug, tpl) {
				var tb strings.Builder
				tb.WriteString("| category | budget |\n")
				for _, l := range t.Budget {
					tb.WriteString("| " + l.Category + " | " + strconv.FormatFloat(l.Amount, 'f', -1, 64) + " |\n")
				}
				budget = tb.String()
				break
			}
		}
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
	if d := strings.TrimSpace(b.Deal); d != "" {
		fm.WriteString("deal: \"[[" + d + "]]\"\n")
	}
	fm.WriteString("hidden: false\n")
	fm.WriteString("---\n\n# " + strings.TrimSpace(b.Address) + "\n\n## budget\n" + budget + "\n## log\n")

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
	case "entity": // free text ("" clears → back to unassigned)
	default:
		httpError(w, errBadRequest("key must be status, kind, or entity"))
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
		Op       string `json:"op"` // seed | add-stage | add-todo | check | edit | delete
		ID       string `json:"id"`
		StageID  string `json:"stageId"`
		Text     string `json:"text"`
		Checked  bool   `json:"checked"`
		Template string `json:"template"` // rehab | new-build | phases | empty
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
		switch b.Template {
		case "rehab":
			names = realestate.RehabStages
		case "new-build":
			names = realestate.NewBuildStages
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
		for _, n := range names {
			stages = append(stages, realestate.WorkStage{Text: n})
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
		} else {
			stages[si].Todos[ti].Checked = b.Checked
		}
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
	default:
		httpError(w, errBadRequest("unknown op"))
		return
	}
	if err := s.writeWork(p.Path, stages); err != nil {
		httpError(w, err)
		return
	}
	s.respondProperty(w, slug)
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

	// csv: one summary row per property+category, then the ledger detail
	var buf strings.Builder
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"section", "property", "category", "budget", "paid", "committed", "date", "type", "vendor", "amount", "status", "note"})
	for _, p := range members {
		for _, c := range p.Rollup.Categories {
			_ = cw.Write([]string{"rollup", p.Address, c.Category, f2(c.Budget), f2(c.Paid), f2(c.Committed), "", "", "", "", "", ""})
		}
		_ = cw.Write([]string{"rollup", p.Address, "TOTAL", f2(p.Rollup.Budget), f2(p.Rollup.Paid), f2(p.Rollup.Committed), "", "", "", "", "", ""})
		for _, lr := range p.Ledger {
			_ = cw.Write([]string{"ledger", p.Address, lr.Category, "", "", "", lr.Date, lr.Type, lr.Vendor + lr.Contractor, f2(lr.Amount), lr.Status, lr.Note})
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

// handleTaxExport writes one CSV for an entity+year: every ledger line across
// its properties, grouped by category (spec §3's tax handshake).
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
	type line struct {
		p  realestate.Property
		lr realestate.LedgerRow
	}
	var lines []line
	for _, p := range props {
		if !strings.EqualFold(strings.TrimSpace(p.Entity), entity) {
			continue
		}
		for _, lr := range p.Ledger {
			if strings.HasPrefix(lr.Date, b.Year) && lr.Type == "expense" {
				lines = append(lines, line{p, lr})
			}
		}
	}
	sort.Slice(lines, func(i, j int) bool {
		if a, c := strings.ToLower(lines[i].lr.Category), strings.ToLower(lines[j].lr.Category); a != c {
			return a < c
		}
		return lines[i].lr.Date < lines[j].lr.Date
	})
	var buf strings.Builder
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"category", "date", "property", "vendor", "amount", "status", "note"})
	for _, l := range lines {
		_ = cw.Write([]string{l.lr.Category, l.lr.Date, l.p.Address, l.lr.Vendor, f2(l.lr.Amount), l.lr.Status, l.lr.Note})
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
	writeJSON(w, map[string]any{"csv": rel, "lines": len(lines)})
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
