package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"manifest/realestate"
	"manifest/vaultwriter"
)

// A single live, deal-scoped projection serves the owner and signed-in OODA
// members. Never expose private portfolio APIs to the portal browser.
type dealUnderwriting struct {
	Deal        realestate.Deal            `json:"deal"`
	Source      json.RawMessage            `json:"source"`
	Members     []realestate.Property      `json:"members"`
	Sources     map[string]json.RawMessage `json:"sources"`
	Docs        map[string][]docView       `json:"docs"`
	Contracts   []realestate.Contract      `json:"contracts"`
	Assumptions any                        `json:"assumptions"`
	Revision    string                     `json:"revision"`
	allowed     map[string]bool
}

func (s *Server) buildDealUnderwriting(slug string) (*dealUnderwriting, error) {
	if s.realestate == nil || s.vault == nil {
		return nil, fmt.Errorf("underwriting unavailable")
	}
	d, ok := s.dealBySlug(slug)
	if !ok {
		return nil, os.ErrNotExist
	}
	props, err := s.realestate.Properties()
	if err != nil {
		return nil, err
	}
	out := &dealUnderwriting{Deal: d, Members: []realestate.Property{}, Sources: map[string]json.RawMessage{}, Docs: map[string][]docView{}, Contracts: []realestate.Contract{}, allowed: map[string]bool{}}
	raw, ok := s.realestate.Source(d.Path)
	if ok {
		out.Source = raw
	} else {
		out.Source = json.RawMessage(`{}`)
	}
	member := map[string]bool{}
	for _, p := range props {
		if strings.EqualFold(p.Deal, d.Slug) {
			member[p.Slug] = true
			out.Members = append(out.Members, p)
			if raw, ok := s.realestate.Source(p.Path); ok {
				out.Sources[p.Slug] = raw
			}
			dir := s.docsDir(p.Slug)
			entries, err := os.ReadDir(vaultJoin(s.vault.VaultRoot(), dir))
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			out.Docs[p.Slug] = []docView{}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				info, err := e.Info()
				if err != nil {
					return nil, err
				}
				ref := path.Join(dir, e.Name())
				out.Docs[p.Slug] = append(out.Docs[p.Slug], docView{Name: e.Name(), Path: ref, Size: info.Size(), MTime: info.ModTime().Unix()})
				out.allowed[ref] = true
			}
			for _, row := range p.Ledger {
				if row.Doc != "" {
					ref := row.Doc
					if !strings.Contains(ref, "/") && !strings.HasPrefix(ref, "sha256:") {
						ref = path.Join(dir, ref)
					}
					out.allowed[ref] = true
				}
			}
		}
	}
	for _, c := range s.realestate.Contracts() {
		scoped := c
		scoped.Allocations = nil
		for _, al := range c.Allocations {
			if member[al.Property] {
				scoped.Allocations = append(scoped.Allocations, al)
			}
		}
		if len(scoped.Allocations) > 0 {
			out.Contracts = append(out.Contracts, scoped)
			if c.Doc != "" {
				out.allowed[c.Doc] = true
			}
		}
	}
	// Comparable appraisals and other references require explicit deal inclusion.
	var source map[string]json.RawMessage
	_ = json.Unmarshal(out.Source, &source)
	basis := source["deal_underwriting"]
	if len(basis) == 0 {
		basis = source["lender_diligence"]
	}
	var refs struct {
		SupportingDocuments []struct {
			Path string `json:"path"`
		} `json:"supportingDocuments"`
	}
	_ = json.Unmarshal(basis, &refs)
	for _, d := range refs.SupportingDocuments {
		clean := path.Clean(d.Path)
		if clean == d.Path && strings.HasPrefix(clean, s.realestateRootOr()+"/docs/") {
			out.allowed[clean] = true
		}
	}
	out.Assumptions = s.loadAssumptions().Values
	bytes, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(bytes)
	out.Revision = hex.EncodeToString(sum[:])
	return out, nil
}

func (s *Server) handleDealUnderwriting(w http.ResponseWriter, r *http.Request) {
	out, err := s.buildDealUnderwriting(r.PathValue("slug"))
	if err != nil {
		code := http.StatusServiceUnavailable
		if os.IsNotExist(err) {
			code = http.StatusNotFound
		}
		http.Error(w, "Could not load deal underwriting", code)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", `"`+out.Revision+`"`)
	if r.Header.Get("If-None-Match") == `"`+out.Revision+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, out)
}
func (s *Server) handleDealUnderwritingDoc(w http.ResponseWriter, r *http.Request) {
	out, err := s.buildDealUnderwriting(r.PathValue("slug"))
	ref := r.URL.Query().Get("ref")
	if err != nil || !out.allowed[ref] {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if strings.HasPrefix(ref, "sha256:") {
		r.SetPathValue("hash", strings.TrimPrefix(ref, "sha256:"))
		s.handleREFileGet(w, r)
		return
	}
	if !strings.HasPrefix(ref, s.realestateRootOr()+"/docs/") {
		http.NotFound(w, r)
		return
	}
	full := vaultJoin(s.vault.VaultRoot(), ref)
	if full == "" {
		http.NotFound(w, r)
		return
	}
	resolved, err := filepath.EvalSymlinks(full)
	root := filepath.Join(s.vault.VaultRoot(), filepath.FromSlash(s.realestateRootOr()), "docs")
	root, rootErr := filepath.EvalSymlinks(root)
	relative, relErr := filepath.Rel(root, resolved)
	if err != nil || rootErr != nil || relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, resolved)
}
func (a *oodaAPI) underwriting(w http.ResponseWriter, r *http.Request) {
	if a.live == nil || a.live.server() == nil {
		http.Error(w, "unavailable", 503)
		return
	}
	a.live.server().handleDealUnderwriting(w, r)
}
func (a *oodaAPI) underwritingDoc(w http.ResponseWriter, r *http.Request) {
	if a.live == nil || a.live.server() == nil {
		http.Error(w, "unavailable", 503)
		return
	}
	a.live.server().handleDealUnderwritingDoc(w, r)
}

// Private package editor: writes only the current deal's assumptions, preserving
// its records and historical financing. A changed projection requires reload.
func (s *Server) handleDealPackageAssumptions(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Dates    map[string]string   `json:"dates"`
		Revision string              `json:"revision"`
		Name     string              `json:"name"`
		Values   map[string]*float64 `json:"values"`
	}
	if err := decode(r, &input); err != nil {
		httpError(w, err)
		return
	}
	ranges := map[string][2]float64{
		"lease_up_days":           {0, 3650},
		"reserve_years_one_three": {0, 100000}, "reserve_years_four_six": {0, 100000}, "reserve_years_seven_eight": {0, 100000}, "reserve_years_nine_plus": {0, 100000},
		"vacancy_rate": {0, .99}, "opex_rate": {0, .99}, "exit_cap_rate": {.001, 1},
		"rent_growth": {-.99, 1}, "opex_growth": {-.99, 1}, "hold_years": {1, 50}, "selling_cost_pct": {0, 1},
		"replacement_reserve": {0, 100000}, "closing_costs": {0, 100000000},
		"construction_ltc": {0, 1}, "construction_rate": {0, 1}, "term_months": {1, 600}, "reserve_months": {0, 600},
		"refinance_rate": {0, 1}, "refinance_years": {1, 50}, "refinance_ltv": {0, 1},
	}
	for k, v := range input.Values {
		bounds, ok := ranges[k]
		if !ok || (v != nil && (*v < bounds[0] || *v > bounds[1])) {
			http.Error(w, "Invalid assumption: "+k, 400)
			return
		}
		if v != nil && (k == "lease_up_days" || k == "hold_years" || k == "term_months" || k == "reserve_months" || k == "refinance_years") && *v != float64(int(*v)) {
			http.Error(w, "Whole number required: "+k, 400)
			return
		}
	}
	if len(strings.TrimSpace(input.Name)) == 0 || len(input.Name) > 120 {
		http.Error(w, "Package name required (maximum 120 characters)", 400)
		return
	}
	for key, value := range input.Dates {
		if key != "construction_start" && key != "completion_target" {
			http.Error(w, "Invalid date key", 400)
			return
		}
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				http.Error(w, "Invalid date", 400)
				return
			}
		}
	}
	if input.Dates["construction_start"] != "" && input.Dates["completion_target"] != "" && input.Dates["completion_target"] < input.Dates["construction_start"] {
		http.Error(w, "Completion must follow construction start", 400)
		return
	}
	current, err := s.buildDealUnderwriting(r.PathValue("slug"))
	if err != nil {
		http.Error(w, "Deal unavailable", 404)
		return
	}
	if input.Revision == "" || input.Revision != current.Revision {
		http.Error(w, "Deal changed. Reload before saving assumptions.", 409)
		return
	}
	sourceBytes, sourceExists := s.realestate.Source(current.Deal.Path)
	if !sourceExists {
		sourceBytes = nil
	}
	if sourceExists && string(sourceBytes) != string(current.Source) {
		http.Error(w, "Deal changed. Reload before saving.", 409)
		return
	}
	var source map[string]any
	if json.Unmarshal(current.Source, &source) != nil {
		http.Error(w, "Source unavailable", 500)
		return
	}
	basis, _ := source["deal_underwriting"].(map[string]any)
	if basis == nil {
		basis = map[string]any{}
		source["deal_underwriting"] = basis
	}
	pkg, _ := basis["packageAssumptions"].(map[string]any)
	if pkg == nil {
		pkg = map[string]any{}
	}
	pkg["name"] = strings.TrimSpace(input.Name)
	pkg["values"] = input.Values
	if input.Dates != nil {
		pkg["dates"] = input.Dates
	}
	basis["packageAssumptions"] = pkg
	raw, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		httpError(w, err)
		return
	}
	if err = s.vault.WriteSourceJSONIfRevision(realestate.SourceRel(current.Deal.Path), raw, vaultwriter.Revision(sourceBytes)); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
