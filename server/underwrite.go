package server

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"time"

	"manifest/realestate"
)

// The estimate-vintage LOCK (overhaul §3.6, decision 13) + the solo-deal
// creation flow (decision 11). Everything entered is an ESTIMATE until the
// owner's explicit "lock underwriting" gesture snapshots measurables + rock
// ests + deal-slice inputs + the assumption values in effect into a frozen
// `<slug>.underwrite.json` sidecar. After lock, current values are canon and
// the property page shows initial-vs-real. Re-lock is a deliberate overwrite
// behind an explicit flag (the client confirms).

// lockableStatuses gate the action: the lock exists once a property is ACTIVE.
var lockableStatuses = map[string]bool{
	"under_contract": true, "pre_development": true, "construction": true,
}

// handleUnderwriteLock — POST /api/properties/{slug}/underwrite-lock
// body: {"relock": true} to overwrite an existing snapshot.
func (s *Server) handleUnderwriteLock(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Relock bool `json:"relock"`
	}
	_ = decode(r, &b)
	p, ok := s.realestate.Get(r.PathValue("slug"))
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	if !lockableStatuses[p.Status] {
		httpError(w, errBadRequest("lock underwriting once the property is active (under contract / pre-development / construction)"))
		return
	}
	if p.Underwrite != nil && !b.Relock {
		httpError(w, errBadRequest("already locked "+p.Locked+" — re-locking overwrites the snapshot deliberately"))
		return
	}
	srcRaw, _ := s.realestate.Source(p.Path)
	src := realestate.ParseSourceMoneyBytes(srcRaw)
	assumptions := realestate.EffectiveAssumptions(s.loadAssumptions().Values, srcRaw)
	now := time.Now()
	lock := realestate.BuildUnderwriteSnapshot(p, src, assumptions, now)
	out, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		httpError(w, err)
		return
	}
	if err := s.vault.WriteUnderwriteJSON(realestate.UnderwriteRel(p.Path), append(out, '\n')); err != nil {
		httpError(w, err)
		return
	}
	if err := s.vault.SetFrontmatterField(p.Path, "locked", now.Format("2006-01-02")); err != nil {
		httpError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{p.Path})
	}
	s.respondProperty(w, p.Slug)
}

// handlePropertyMeasurables — POST /api/properties/{slug}/measurables: the
// page-side editor for the frontmatter measurables (overhaul §3.1 — the
// record stays hand-editable in Obsidian; this writes the same lines).
// Body: {"units":[{label,beds,baths,sqft,rent},…]} replaces the unit mix
// ([] removes the key); {"set":{"windows":14}} upserts measurables;
// {"remove":["windows"]} deletes them.
func (s *Server) handlePropertyMeasurables(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	p, ok := s.realestate.Get(r.PathValue("slug"))
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return
	}
	var b struct {
		Units  []realestate.Unit  `json:"units"`
		SetU   bool               `json:"setUnits"` // distinguishes "replace with []" from "untouched"
		Set    map[string]float64 `json:"set"`
		Remove []string           `json:"remove"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if b.SetU {
		if len(b.Units) == 0 {
			if err := s.vault.RemoveFrontmatterField(p.Path, "units"); err != nil {
				httpError(w, err)
				return
			}
		} else if err := s.vault.SetFrontmatterField(p.Path, "units", realestate.EmitUnitsList(b.Units)); err != nil {
			httpError(w, err)
			return
		}
	}
	for key, v := range b.Set {
		key = strings.ToLower(strings.TrimSpace(key))
		if !realestate.MeasurableKeyOK(key) {
			httpError(w, errBadRequest("measurable name must be kebab-case and not a reserved field: "+key))
			return
		}
		if err := s.vault.SetFrontmatterField(p.Path, key, trimFloat(v)); err != nil {
			httpError(w, err)
			return
		}
	}
	for _, key := range b.Remove {
		key = strings.ToLower(strings.TrimSpace(key))
		if !realestate.MeasurableKeyOK(key) {
			continue // never remove reserved fields through this lane
		}
		if err := s.vault.RemoveFrontmatterField(p.Path, key); err != nil {
			httpError(w, err)
			return
		}
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{p.Path})
	}
	s.respondProperty(w, p.Slug)
}

// handleDealCreate — POST /api/deals: {"property": "<slug>"} underwrites a
// solo property as its own deal (decision 11 — a solo property IS a deal);
// {"name": "...", "properties": ["<slug>", …]} creates a bundle.
func (s *Server) handleDealCreate(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil || s.index == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Property   string   `json:"property"`
		Name       string   `json:"name"`
		Properties []string `json:"properties"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if strings.TrimSpace(b.Property) != "" {
		slug, err := s.createSoloDeal(strings.TrimSpace(b.Property))
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]any{"slug": slug})
		return
	}
	name := strings.TrimSpace(b.Name)
	if name == "" || len(b.Properties) == 0 {
		httpError(w, errBadRequest("property (solo) or name + properties (bundle) required"))
		return
	}
	var addrs []string
	for _, ps := range b.Properties {
		p, ok := s.realestate.Get(strings.TrimSpace(ps))
		if !ok {
			httpError(w, errBadRequest("unknown property "+ps))
			return
		}
		addrs = append(addrs, p.Address)
	}
	slug := slugify(name)
	rel, err := s.writeDealRecord(slug, name, "", addrs)
	if err != nil {
		httpError(w, err)
		return
	}
	for _, ps := range b.Properties {
		p, _ := s.realestate.Get(strings.TrimSpace(ps))
		_ = s.vault.SetFrontmatterField(p.Path, "deal", "\"[["+slug+"]]\"")
		_ = s.index.ReindexPaths([]string{p.Path})
	}
	_ = s.index.ReindexPaths([]string{rel})
	writeJSON(w, map[string]any{"slug": slug})
}

// createSoloDeal makes `<prop-slug>-deal` (the -deal suffix keeps the deal
// note's basename distinct from the property note — wikilinks stay
// unambiguous) and tethers the property to it. Idempotent: an existing deal
// tether or record just returns.
func (s *Server) createSoloDeal(propSlug string) (string, error) {
	p, ok := s.realestate.Get(propSlug)
	if !ok {
		return "", errBadRequest("unknown property " + propSlug)
	}
	if p.Deal != "" {
		return p.Deal, nil // already in a deal — that deal is the worksheet
	}
	slug := p.Slug + "-deal"
	if _, ok := s.dealBySlug(slug); !ok {
		status := ""
		if _, isDeal := map[string]bool{"opportunity": true, "negotiating": true, "under_contract": true,
			"pre_development": true, "construction": true, "completed": true}[p.Status]; isDeal {
			status = p.Status
		}
		if _, err := s.writeDealRecord(slug, orStr(p.Short, p.Slug), status, []string{p.Address}); err != nil {
			return "", err
		}
	}
	if err := s.vault.SetFrontmatterField(p.Path, "deal", "\"[["+slug+"]]\""); err != nil {
		return "", err
	}
	_ = s.index.ReindexPaths([]string{p.Path, path.Join(s.realestateRootOr(), "deals", slug+".md")})
	return slug, nil
}

// writeDealRecord creates the deal note (write-once via CreateRecord).
func (s *Server) writeDealRecord(slug, name, status string, addrs []string) (string, error) {
	var fm strings.Builder
	fm.WriteString("---\ncategories: [deal]\n")
	if status != "" {
		fm.WriteString("status: " + status + "\n")
	}
	var quoted []string
	for _, a := range addrs {
		quoted = append(quoted, "\""+a+"\"")
	}
	fm.WriteString("properties: [" + strings.Join(quoted, ", ") + "]\n")
	fm.WriteString("---\n\n# " + name + "\n")
	rel := path.Join(s.realestateRootOr(), "deals", slug+".md")
	if _, err := s.vault.CreateRecord(rel, fm.String()); err != nil {
		return "", err
	}
	_ = s.index.ReindexPaths([]string{rel})
	return rel, nil
}
