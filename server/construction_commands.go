package server

// Construction history, comparison and decisions (P7). The typed command
// route (construction.go) is the only mutation path for owner and agent
// alike; these routes read exact revisions — an assembly's own history, a
// structured comparison of any two exact revisions, and decisions with
// their staleness against the current heads — and accept decision commands
// (propose / owner-only accept / owner-only reject).

import (
	"net/http"
	"strings"

	"manifest/construction"
)

func (s *Server) registerConstructionCommandRoutes(mux *http.ServeMux, p string) {
	mux.HandleFunc("GET "+p+"/problems/{id}/assemblies/{asm}/history", s.handleConstructionAssemblyHistory)
	mux.HandleFunc("GET "+p+"/problems/{id}/compare", s.handleConstructionCompare)
	mux.HandleFunc("GET "+p+"/problems/{id}/decisions", s.handleConstructionDecisions)
	mux.HandleFunc("POST "+p+"/problems/{id}/decisions", s.handleConstructionFamilyCommand(map[string]bool{"ProposeDecision": true, "ApproveDecision": true, "RejectDecision": true}))
}

func (s *Server) handleConstructionAssemblyHistory(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	h, err := s.construction.store.AssemblyHistory(sub, r.PathValue("id"), r.PathValue("asm"), 500)
	if err != nil {
		constructionError(w, err)
		return
	}
	constructionJSON(w, map[string]any{"history": h})
}

// splitRef reads "asm-…" or "asm-…@<revision>".
func splitRef(s string) (string, string) {
	id, rev, _ := strings.Cut(s, "@")
	return id, rev
}

func (s *Server) handleConstructionCompare(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	pid := r.PathValue("id")
	store := s.construction.store
	load := func(ref string) (*construction.Assembly, string, *construction.ValidationReport, error) {
		id, rev := splitRef(ref)
		a, tok, err := store.AssemblyAt(sub, pid, id, rev)
		if err != nil {
			return nil, "", nil, err
		}
		rep, _ := store.ValidationAt(sub, pid, id, tok)
		return a, tok, rep, nil
	}
	a, ra, repA, err := load(r.URL.Query().Get("a"))
	if err != nil {
		constructionError(w, err)
		return
	}
	b, rb, repB, err := load(r.URL.Query().Get("b"))
	if err != nil {
		constructionError(w, err)
		return
	}
	constructionJSON(w, map[string]any{"comparison": construction.CompareAssemblies(a, b, ra, rb, repA, repB), "notice": construction.NonApprovalNotice})
}

func (s *Server) handleConstructionDecisions(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	st, err := s.construction.store.Load(sub, r.PathValue("id"))
	if err != nil {
		constructionError(w, err)
		return
	}
	constructionJSON(w, map[string]any{"decisions": st.Decisions, "states": construction.DecisionStates(st), "selected": st.Problem.SelectedAssembly,
		"notice": "accepted-for-project is a human project decision, never approval for construction. " + construction.NonApprovalNotice})
}
