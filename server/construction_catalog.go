package server

// Construction catalog (plan §6, §12.6): the problem-local, evidence-pinned
// materials and products, edited only through typed owner commands, and a
// substitution preview that shows the fact diff, dimension changes and the
// re-run validation before anything is pinned. No procurement, stock, price
// or certified-performance claims exist anywhere here.

import (
	"encoding/json"
	"net/http"

	"manifest/construction"
)

func (s *Server) registerConstructionCatalogRoutes(mux *http.ServeMux, p string) {
	mux.HandleFunc("GET "+p+"/problems/{id}/materials", s.handleConstructionCatalogGet("materials"))
	mux.HandleFunc("POST "+p+"/problems/{id}/materials", s.handleConstructionFamilyCommand(map[string]bool{"SetMaterialProperty": true}))
	mux.HandleFunc("GET "+p+"/problems/{id}/products", s.handleConstructionCatalogGet("products"))
	mux.HandleFunc("POST "+p+"/problems/{id}/products", s.handleConstructionFamilyCommand(map[string]bool{"AddProduct": true, "UpdateProduct": true, "SetProductLifecycle": true}))
	mux.HandleFunc("GET "+p+"/problems/{id}/assemblies/{asm}/substitution", s.handleConstructionSubstitution)
}

func (s *Server) handleConstructionCatalogGet(part string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sub, _, ok := s.constructionBegin(w, r, false)
		if !ok {
			return
		}
		st, err := s.construction.store.Load(sub, r.PathValue("id"))
		if err != nil {
			constructionError(w, err)
			return
		}
		w.Header().Set("ETag", `"`+st.Revision("catalog")+`"`)
		out := map[string]any{"revision": st.Revision("catalog"), "notice": construction.NonApprovalNotice}
		if part == "materials" {
			out["materials"], out["families"], out["units"] = st.Catalog.Materials, construction.MaterialFamilies(), construction.PropertyUnits()
		} else {
			current := []construction.Product{}
			seen := map[string]bool{}
			for _, p := range st.Catalog.Products {
				if seen[p.ID] {
					continue
				}
				seen[p.ID] = true
				cur, _ := st.Catalog.CurrentProduct(p.ID)
				current = append(current, cur)
			}
			out["products"], out["revisions"] = current, st.Catalog.Products
		}
		constructionJSON(w, out)
	}
}

// handleConstructionSubstitution previews pinning a product on a component:
// the substitution report plus the validation of the same typed SetProduct
// command on a copy (nothing is written).
func (s *Server) handleConstructionSubstitution(w http.ResponseWriter, r *http.Request) {
	sub, actor, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	pid, asmID := r.PathValue("id"), r.PathValue("asm")
	q := r.URL.Query()
	st, err := s.construction.store.Load(sub, pid)
	if err != nil {
		constructionError(w, err)
		return
	}
	a := st.Assemblies[asmID]
	if a == nil {
		constructionError(w, construction.NotFound("no such assembly in this problem"))
		return
	}
	rep, err := construction.Substitution(a, st.Catalog, q.Get("component"), q.Get("product"))
	if err != nil {
		constructionError(w, err)
		return
	}
	cmd := map[string]any{"schemaVersion": 1, "requestId": "substitution-preview", "problemId": pid, "assemblyId": asmID,
		"expectedAssemblyRevision": st.Revision("assembly:" + asmID),
		"operations":               []map[string]any{{"op": "SetProduct", "componentId": q.Get("component"), "productId": q.Get("product"), "applyDimensions": q.Get("applyDimensions") != "0"}}}
	raw, _ := json.Marshal(cmd)
	pc, err := construction.ParseCommand(raw)
	if err != nil {
		constructionError(w, err)
		return
	}
	prev, _, err := s.construction.store.Preview(sub, pc, actor, &construction.ApplyContext{})
	out := map[string]any{"substitution": rep, "assemblyRevision": st.Revision("assembly:" + asmID)}
	if err != nil {
		out["refused"] = err.Error()
	} else {
		out["preview"] = prev
	}
	if cur := st.Validation[asmID]; cur != nil {
		out["current"] = cur.Counts
	}
	constructionJSON(w, out)
}
