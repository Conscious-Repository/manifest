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

	"manifest/realestate"
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
