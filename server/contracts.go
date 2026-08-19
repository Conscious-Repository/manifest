package server

import (
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/mdfm"
	"manifest/realestate"
)

func trimFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// CONTRACTS + the CAS file store + the contractor surfaces (overhaul pass 2).
// Contracts are the committed-money source; the ledger stays cash facts.
// All writes ride the declared `re-contracts` / `re-files` capabilities.

// UseREFiles wires the content-addressed file store (overhaul §3.3).
func (s *Server) UseREFiles(fs *realestate.FileStore) { s.reFiles = fs }

// contractDraws walks every property ledger once: contract slug → its
// expense draws (cross-property by construction).
func (s *Server) contractDraws() map[string][]realestate.LedgerRow {
	out := map[string][]realestate.LedgerRow{}
	props, err := s.realestate.Properties()
	if err != nil {
		return out
	}
	for _, p := range props {
		for _, r := range p.Ledger {
			if r.Contract == "" || !strings.EqualFold(r.Type, "expense") {
				continue
			}
			row := r
			row.Vendor = orStr(row.Vendor, p.Short) // provenance for the contract page
			out[strings.ToLower(r.Contract)] = append(out[strings.ToLower(r.Contract)], row)
		}
	}
	return out
}

type contractView struct {
	realestate.Contract
	Remaining float64 `json:"remaining"`
}

func (s *Server) contractViews() []contractView {
	draws := s.contractDraws()
	var out []contractView
	for _, c := range s.realestate.Contracts() {
		for _, d := range draws[strings.ToLower(c.Slug)] {
			c.Drawn += d.Amount
		}
		out = append(out, contractView{Contract: c, Remaining: c.Total - c.Drawn})
	}
	return out
}

// handleContractsList — GET /api/realestate/contracts
func (s *Server) handleContractsList(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	views := s.contractViews()
	if views == nil {
		views = []contractView{}
	}
	writeJSON(w, map[string]any{"contracts": views})
}

// handleContractGet — GET /api/realestate/contracts/{slug}: the record + its
// draw picture (Σ drawn, the draw rows, remaining).
func (s *Server) handleContractGet(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	c, ok := s.realestate.GetContract(slug)
	if !ok {
		http.Error(w, "contract not found", http.StatusNotFound)
		return
	}
	draws := s.contractDraws()[strings.ToLower(c.Slug)]
	if draws == nil {
		draws = []realestate.LedgerRow{}
	}
	for _, d := range draws {
		c.Drawn += d.Amount
	}
	// property short names for the allocation rows
	names := map[string]string{}
	if props, err := s.realestate.Properties(); err == nil {
		for _, p := range props {
			names[p.Slug] = orStr(p.Short, p.Slug)
		}
	}
	writeJSON(w, map[string]any{
		"contract": c, "remaining": c.Total - c.Drawn, "draws": draws, "propertyNames": names,
	})
}

type contractBody struct {
	Name        string   `json:"name"`
	Contractor  string   `json:"contractor"` // contractor slug (must exist)
	Status      string   `json:"status"`
	Total       float64  `json:"total"`
	Date        string   `json:"date"`
	Expires     string   `json:"expires"`
	Doc         string   `json:"doc"`
	Allocations []string `json:"allocations"` // "property | node | amount"
	Terms       []string `json:"terms"`
	Exclusions  []string `json:"exclusions"`
	RiskItems   []string `json:"riskItems"`
}

// validateContract checks the invariants shared by create + update.
func (s *Server) validateContract(b *contractBody) (realestate.Contract, error) {
	c := realestate.Contract{
		Name: strings.TrimSpace(b.Name), Contractor: strings.TrimSpace(b.Contractor),
		Status: strings.ToLower(strings.TrimSpace(b.Status)), Total: b.Total,
		Date: strings.TrimSpace(b.Date), Expires: strings.TrimSpace(b.Expires),
		Doc:   strings.TrimSpace(b.Doc),
		Terms: cleanLines(b.Terms), Exclusions: cleanLines(b.Exclusions), RiskItems: cleanLines(b.RiskItems),
	}
	if c.Status == "" {
		c.Status = "proposed"
	}
	okStatus := false
	for _, st := range realestate.ContractStatuses {
		if c.Status == st {
			okStatus = true
		}
	}
	if !okStatus {
		return c, errBadRequest("status must be one of " + strings.Join(realestate.ContractStatuses, "|"))
	}
	if c.Contractor == "" {
		return c, errBadRequest("contractor is required")
	}
	found := false
	for _, e := range s.realestate.Contractors() {
		if strings.EqualFold(e.Slug, c.Contractor) {
			c.Contractor = e.Slug
			found = true
			break
		}
	}
	if !found {
		return c, errBadRequest("no contractor record " + c.Contractor + " — create it first")
	}
	if c.Total <= 0 {
		return c, errBadRequest("total must be positive")
	}
	for _, raw := range b.Allocations {
		a, ok := realestate.ParseAllocationItem(raw)
		if !ok {
			return c, errBadRequest("bad allocation " + raw + " (want property | node | amount)")
		}
		if _, exists := s.propertyRel(a.Property); !exists {
			return c, errBadRequest("unknown property " + a.Property)
		}
		c.Allocations = append(c.Allocations, a)
	}
	if len(c.Allocations) == 0 {
		return c, errBadRequest("at least one allocation is required")
	}
	if diff := c.AllocTotal() - c.Total; diff > 0.009 || diff < -0.009 {
		return c, errBadRequest("Σ allocations must equal total")
	}
	return c, nil
}

func cleanLines(in []string) []string {
	var out []string
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// handleContractCreate — POST /api/realestate/contracts (the manual path;
// the intake proposal's apply lane reuses the same shape later).
func (s *Server) handleContractCreate(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	var b contractBody
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	c, err := s.validateContract(&b)
	if err != nil {
		httpError(w, err)
		return
	}
	if c.Name == "" {
		c.Name = c.Contractor
	}
	base := slugify(c.Name)
	slug := base
	for n := 2; ; n++ {
		if _, exists := s.realestate.GetContract(slug); !exists {
			break
		}
		slug = base + "-" + itoa(n)
	}
	rel := path.Join(s.realestateRootOr(), "contracts", slug+".md")
	if err := s.vault.WriteCap("re-contracts", rel, []byte(realestate.NewContractRecord(c))); err != nil {
		httpError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{rel})
	}
	writeJSON(w, map[string]any{"slug": slug, "path": rel})
}

// handleContractUpdate — POST /api/realestate/contracts/{slug}/update.
// Frontmatter patches only (status flips, silent commitment edits —
// decision 16); an optional change note appends to ## changes for the
// look-back. Body prose is never touched.
func (s *Server) handleContractUpdate(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	c, ok := s.realestate.GetContract(slug)
	if !ok {
		http.Error(w, "contract not found", http.StatusNotFound)
		return
	}
	var b struct {
		Status      *string  `json:"status"`
		Total       *float64 `json:"total"`
		Date        *string  `json:"date"`
		Expires     *string  `json:"expires"`
		Doc         *string  `json:"doc"`
		Allocations []string `json:"allocations"` // full replacement when present
		ChangeNote  string   `json:"changeNote"`  // optional "- YYYY-MM-DD ±N reason" line
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	vals := map[string]*string{}
	set := func(k, v string) { vv := v; vals[k] = &vv }
	if b.Status != nil {
		st := strings.ToLower(strings.TrimSpace(*b.Status))
		okStatus := false
		for _, v := range realestate.ContractStatuses {
			if st == v {
				okStatus = true
			}
		}
		if !okStatus {
			httpError(w, errBadRequest("bad status"))
			return
		}
		c.Status = st
		set("status", st)
	}
	if b.Total != nil {
		c.Total = *b.Total
		set("total", trimFloat(*b.Total))
	}
	if b.Date != nil {
		set("date", strings.TrimSpace(*b.Date))
	}
	if b.Expires != nil {
		set("expires", strings.TrimSpace(*b.Expires))
	}
	if b.Doc != nil {
		set("doc", "\""+strings.TrimSpace(*b.Doc)+"\"")
	}
	if b.Allocations != nil {
		var allocs []realestate.ContractAllocation
		for _, raw := range b.Allocations {
			a, ok := realestate.ParseAllocationItem(raw)
			if !ok {
				httpError(w, errBadRequest("bad allocation "+raw))
				return
			}
			allocs = append(allocs, a)
		}
		c.Allocations = allocs
		set("allocations", realestate.EmitAllocations(allocs))
	}
	if diff := c.AllocTotal() - c.Total; len(c.Allocations) > 0 && (diff > 0.009 || diff < -0.009) {
		httpError(w, errBadRequest("Σ allocations must equal total"))
		return
	}
	raw, err := os.ReadFile(filepath.Join(s.index.VaultRoot(), filepath.FromSlash(c.Path)))
	if err != nil {
		httpError(w, err)
		return
	}
	next := realestate.PatchContractFrontmatter(raw, vals)
	if b.ChangeNote != "" {
		_, body := mdfm.Split(string(next))
		line := "- " + time.Now().Format("2006-01-02") + " " + strings.TrimSpace(b.ChangeNote)
		if strings.Contains(body, "\n## changes") || strings.HasPrefix(body, "## changes") {
			next = []byte(strings.Replace(string(next), "## changes\n", "## changes\n"+line+"\n", 1))
		} else {
			next = []byte(strings.TrimRight(string(next), "\n") + "\n\n## changes\n" + line + "\n")
		}
	}
	if err := s.vault.WriteCap("re-contracts", c.Path, next); err != nil {
		httpError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{c.Path})
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleContractAccept — POST /api/realestate/contracts/{slug}/accept.
// Picking a bid is one decision with two consequences, so it is one call: this
// record commits (status accepted — its allocations now count as hard cost),
// and every OTHER proposed record targeting any of the same work nodes is
// declined. Without the second half the losing bids sit "proposed" forever and
// the node looks like it still has options.
func (s *Server) handleContractAccept(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	win, ok := s.realestate.GetContract(slug)
	if !ok {
		http.Error(w, "contract not found", http.StatusNotFound)
		return
	}
	nodes := map[string]bool{}
	for _, a := range win.Allocations {
		nodes[strings.ToLower(a.Property+"|"+a.NodeID)] = true
	}
	today := time.Now().Format("2006-01-02")
	setStatus := func(c realestate.Contract, status, note string) error {
		raw, err := os.ReadFile(filepath.Join(s.index.VaultRoot(), filepath.FromSlash(c.Path)))
		if err != nil {
			return err
		}
		st := status
		next := realestate.PatchContractFrontmatter(raw, map[string]*string{"status": &st})
		if note != "" {
			line := "- " + today + " " + note
			if strings.Contains(string(next), "\n## changes") {
				next = []byte(strings.Replace(string(next), "## changes\n", "## changes\n"+line+"\n", 1))
			} else {
				next = []byte(strings.TrimRight(string(next), "\n") + "\n\n## changes\n" + line + "\n")
			}
		}
		if err := s.vault.WriteCap("re-contracts", c.Path, next); err != nil {
			return err
		}
		if s.index != nil {
			_ = s.index.ReindexPaths([]string{c.Path})
		}
		return nil
	}
	if err := setStatus(win, "accepted", "accepted — committed against "+strconv.Itoa(len(win.Allocations))+" node(s)"); err != nil {
		httpError(w, err)
		return
	}
	declined := []string{}
	for _, c := range s.realestate.Contracts() {
		if c.Slug == win.Slug || !strings.EqualFold(c.Status, "proposed") {
			continue
		}
		shares := false
		for _, a := range c.Allocations {
			if nodes[strings.ToLower(a.Property+"|"+a.NodeID)] {
				shares = true
				break
			}
		}
		if !shares {
			continue
		}
		if err := setStatus(c, "declined", "declined — "+win.Slug+" accepted for the same work"); err != nil {
			httpError(w, err)
			return
		}
		declined = append(declined, c.Slug)
	}
	writeJSON(w, map[string]any{"ok": true, "accepted": win.Slug, "declined": declined})
}

// ---- CAS uploads (overhaul §3.3) ----

// handleREFileUpload — POST /api/realestate/files?name=<orig>: raw body →
// content-addressed blob + index row; identical bytes dedupe.
func (s *Server) handleREFileUpload(w http.ResponseWriter, r *http.Request) {
	if s.reFiles == nil {
		http.Error(w, "file store not available", http.StatusServiceUnavailable)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		httpError(w, errBadRequest("name is required"))
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, realestate.MaxCASSize))
	if err != nil {
		httpError(w, errBadRequest("upload too large or unreadable"))
		return
	}
	ref, err := s.reFiles.Save(data, name, r.Header.Get("Content-Type"), time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, ref)
}

// handleREFileGet — GET /api/realestate/files/{hash} (?dl=1 to download).
func (s *Server) handleREFileGet(w http.ResponseWriter, r *http.Request) {
	if s.reFiles == nil {
		http.Error(w, "file store not available", http.StatusServiceUnavailable)
		return
	}
	abs, meta, ok := s.reFiles.Lookup("sha256:" + r.PathValue("hash"))
	if !ok {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if meta.Mime != "" {
		w.Header().Set("Content-Type", meta.Mime)
	}
	disp := "inline"
	if r.URL.Query().Get("dl") == "1" {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", disp+"; filename=\""+strings.ReplaceAll(meta.Name, "\"", "")+"\"")
	http.ServeFile(w, r, abs)
}

// ---- contractor record + history page (overhaul §3.7) ----

// handleContractorUpdate — POST /api/realestate/contractors/{slug}/update:
// the quick-edit inspector's save-as-you-go patch. Writing scopes migrates
// the legacy trade string (scopes win on read).
func (s *Server) handleContractorUpdate(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
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
	var b struct {
		Name    *string  `json:"name"`
		Email   *string  `json:"email"`
		Website *string  `json:"website"`
		Scopes  []string `json:"scopes"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	put := func(key, val string) bool {
		return s.vault.SetFrontmatterField(target.Path, key, val) == nil
	}
	if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
		put("name", strings.TrimSpace(*b.Name))
	}
	if b.Email != nil {
		put("email", "\""+strings.TrimSpace(*b.Email)+"\"")
	}
	if b.Website != nil {
		put("website", "\""+strings.TrimSpace(*b.Website)+"\"")
	}
	if b.Scopes != nil {
		var quoted []string
		for _, sc := range b.Scopes {
			if t := strings.ToLower(strings.TrimSpace(sc)); t != "" {
				quoted = append(quoted, t)
			}
		}
		put("scopes", "["+strings.Join(quoted, ", ")+"]")
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{target.Path})
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleContractorPage — GET /api/realestate/contractors/{slug}/page: the
// history page's DERIVED content (contracts by status, properties worked,
// committed/drawn/remaining, open tree tasks/decisions owned, free prose).
// Nothing stored.
func (s *Server) handleContractorPage(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	slug := r.PathValue("slug")
	var rec *realestate.Entity
	for _, c := range s.realestate.Contractors() {
		if strings.EqualFold(c.Slug, slug) {
			t := c
			rec = &t
			break
		}
	}
	if rec == nil {
		http.Error(w, "contractor not found", http.StatusNotFound)
		return
	}
	// the record's free prose (body after the # heading)
	prose := ""
	if raw, err := os.ReadFile(filepath.Join(s.index.VaultRoot(), filepath.FromSlash(rec.Path))); err == nil {
		_, body := mdfm.Split(string(raw))
		var keep []string
		for _, ln := range strings.Split(body, "\n") {
			if strings.HasPrefix(ln, "# ") {
				continue
			}
			keep = append(keep, ln)
		}
		prose = strings.TrimSpace(strings.Join(keep, "\n"))
	}
	// contracts + money
	var contracts []contractView
	var committed, drawn float64
	propsWorked := map[string]bool{}
	for _, cv := range s.contractViews() {
		if !strings.EqualFold(cv.Contractor, slug) {
			continue
		}
		contracts = append(contracts, cv)
		if cv.Accepted() {
			committed += cv.Total
			drawn += cv.Drawn
		}
		for _, a := range cv.Allocations {
			propsWorked[a.Property] = true
		}
	}
	if contracts == nil {
		contracts = []contractView{}
	}
	// open tree tasks/decisions owned ([owner:: slug] across property trees)
	type ownedRow struct {
		ID       string `json:"id"` // composite prop:<slug>/<taskId>
		Text     string `json:"text"`
		Property string `json:"property"`
		Decision bool   `json:"decision,omitempty"`
		Rock     string `json:"rock"`
	}
	owned := []ownedRow{}
	properties := []map[string]string{}
	if props, err := s.realestate.Properties(); err == nil {
		for _, p := range props {
			if propsWorked[p.Slug] {
				properties = append(properties, map[string]string{"slug": p.Slug, "name": orStr(p.Short, p.Name)})
			}
			realestate.WalkNodes(p.Work, func(st *realestate.WorkStage, n *realestate.WorkNode) {
				if n.Task.Checked || !strings.EqualFold(n.Task.Owner, slug) {
					return
				}
				owned = append(owned, ownedRow{
					ID: "prop:" + p.Slug + "/" + n.TaskID(), Text: n.Task.Text,
					Property: orStr(p.Short, p.Name), Decision: n.Decision, Rock: st.Text,
				})
			})
		}
	}
	sort.SliceStable(contracts, func(i, j int) bool { return contracts[i].Date > contracts[j].Date })
	writeJSON(w, map[string]any{
		"contractor": rec, "prose": prose, "contracts": contracts,
		"committed": committed, "drawn": drawn, "remaining": committed - drawn,
		"properties": properties, "owned": owned,
	})
}
