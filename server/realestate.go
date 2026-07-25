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
		Address, Entity, Kind, Status, Template string
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

// ---- bank-CSV import (spec §3): map columns once → preview → append ----

// handleImportPreview parses an uploaded bank/card csv and returns the raw rows
// plus a suggested column mapping (remembered by header signature when this
// export shape has been imported before) and the learned vendor→category map.
// The client does the mapping/dup-flag UI; apply re-dedupes authoritatively.
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.reImport == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	if _, ok := s.propertyRel(r.PathValue("slug")); !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		httpError(w, err)
		return
	}
	fhs := r.MultipartForm.File["file"]
	if len(fhs) == 0 {
		httpError(w, errBadRequest("no csv in upload"))
		return
	}
	f, err := fhs[0].Open()
	if err != nil {
		httpError(w, err)
		return
	}
	defer f.Close()
	rd := csv.NewReader(f)
	rd.FieldsPerRecord = -1 // lenient — bank exports are ragged
	records, err := rd.ReadAll()
	if err != nil {
		httpError(w, err)
		return
	}
	if len(records) < 2 {
		httpError(w, errBadRequest("csv has no data rows"))
		return
	}
	headers := records[0]
	rows := records[1:]
	if len(rows) > 1000 {
		rows = rows[:1000] // bounded preview — a bank export, not a data lake
	}
	sig := realestate.Signature(headers)
	remembered, vendorCats := s.reImport.Lookup(sig)
	mapping := remembered
	if mapping == nil {
		mapping = realestate.SuggestMapping(headers)
	}
	writeJSON(w, map[string]any{
		"headers": headers, "rows": rows, "signature": sig,
		"mapping": mapping, "remembered": remembered != nil, "vendorCategories": vendorCats,
	})
}

// handleImportApply appends the client-mapped rows, deduping on
// date+amount+vendor against the existing ledger (the spec's key — a re-import
// of the same file is a no-op), then remembers the mapping + vendor categories.
func (s *Server) handleImportApply(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil || s.reImport == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	var b struct {
		Signature string            `json:"signature"`
		Mapping   map[string]string `json:"mapping"`
		Rows      []struct {
			Date, Vendor, Category, Note string
			Amount                       float64
		} `json:"rows"`
	}
	if err := decode(r, &b); err != nil || len(b.Rows) == 0 {
		httpError(w, errBadRequest("rows are required"))
		return
	}
	p, ok := s.realestate.Get(slug)
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	existing := map[string]bool{}
	for _, lr := range p.Ledger {
		existing[importKey(lr.Date, lr.Amount, lr.Vendor)] = true
	}
	ledgerRel := realestate.LedgerRel(p.Path)
	added, skipped := 0, 0
	vendorCats := map[string]string{}
	for _, row := range b.Rows {
		key := importKey(row.Date, row.Amount, row.Vendor)
		if existing[key] {
			skipped++
			continue
		}
		rec := []string{
			strings.TrimSpace(row.Date), "expense", strings.TrimSpace(row.Category),
			strings.TrimSpace(row.Vendor), "", strconv.FormatFloat(row.Amount, 'f', -1, 64),
			"paid", strings.TrimSpace(row.Note), "",
		}
		if err := s.vault.AppendLedgerRow(ledgerRel, realestate.LedgerHeader, rec); err != nil {
			httpError(w, err)
			return
		}
		existing[key] = true
		added++
		if row.Vendor != "" && row.Category != "" {
			vendorCats[row.Vendor] = row.Category
		}
	}
	s.reImport.Remember(b.Signature, b.Mapping, vendorCats)
	prop, _ := s.realestate.Get(slug)
	writeJSON(w, map[string]any{"added": added, "skipped": skipped, "property": prop})
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
