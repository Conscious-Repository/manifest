package server

// Construction sources and evidence (plan §7): the problem's private
// evidence snapshot, owner-entered sources/passages through the typed
// command path, and the evidence graph (paths Source → Evidence → Claim →
// part → assembly, validated and traversed with the platform graph package).
// A source URL is metadata: nothing here fetches it.

import (
	"net/http"
	"sort"

	"manifest/construction"
	"manifest/graph"
)

func (s *Server) registerConstructionSourceRoutes(mux *http.ServeMux, p string) {
	mux.HandleFunc("GET "+p+"/problems/{id}/sources", s.handleConstructionEvidenceGet)
	mux.HandleFunc("POST "+p+"/problems/{id}/sources", s.handleConstructionFamilyCommand(map[string]bool{"AddSource": true}))
	mux.HandleFunc("GET "+p+"/problems/{id}/evidence", s.handleConstructionEvidenceGet)
	mux.HandleFunc("POST "+p+"/problems/{id}/evidence", s.handleConstructionFamilyCommand(map[string]bool{"AddEvidence": true, "LinkEvidence": true}))
	mux.HandleFunc("GET "+p+"/problems/{id}/evidence/paths", s.handleConstructionEvidencePaths)
	mux.HandleFunc("GET "+p+"/problems/{id}/graph", s.handleConstructionGraph)
}

func (s *Server) handleConstructionEvidenceGet(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	st, err := s.construction.store.Load(sub, r.PathValue("id"))
	if err != nil {
		constructionError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+st.Revision("evidence")+`"`)
	constructionJSON(w, map[string]any{"evidence": st.Evidence, "revision": st.Revision("evidence"), "notice": construction.NonApprovalNotice})
}

// handleConstructionFamilyCommand accepts a typed command whose operations
// all belong to one route's family — the same parse/authorize/commit path
// as every other construction command.
func (s *Server) handleConstructionFamilyCommand(allowed map[string]bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sub, actor, ok := s.constructionBegin(w, r, true)
		if !ok {
			return
		}
		body, ok := readConstructionBody(w, r, construction.MaxCommandBytes)
		if !ok {
			return
		}
		pc, err := construction.ParseCommand(body)
		if err != nil {
			constructionError(w, err)
			return
		}
		if pc.Command.ProblemID != r.PathValue("id") {
			constructionError(w, construction.Invalid("problemId does not match the URL"))
			return
		}
		for _, op := range pc.Ops {
			if !allowed[op.Name()] {
				constructionError(w, construction.Invalid(op.Name()+" is not accepted on this route"))
				return
			}
		}
		s.executeConstructionCommand(w, sub, pc, actor)
	}
}

func (s *Server) handleConstructionEvidencePaths(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	pid := r.PathValue("id")
	a, tok, err := s.construction.store.AssemblyAt(sub, pid, r.URL.Query().Get("assembly"), r.URL.Query().Get("revision"))
	if err != nil {
		constructionError(w, err)
		return
	}
	st, err := s.construction.store.Load(sub, pid)
	if err != nil {
		constructionError(w, err)
		return
	}
	target := r.URL.Query().Get("target")
	if target == "" {
		target = a.ID
	}
	constructionJSON(w, map[string]any{"assemblyId": a.ID, "assemblyRevision": tok, "target": target,
		"paths": construction.EvidencePaths(st.Evidence, a, st.Decisions, target)})
}

type constructionGraphNode struct {
	Ref   string `json:"ref"`
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// handleConstructionGraph returns the validated evidence graph of one
// assembly revision: nodes and edges for the evidence view.
func (s *Server) handleConstructionGraph(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	pid := r.PathValue("id")
	a, tok, err := s.construction.store.AssemblyAt(sub, pid, r.URL.Query().Get("assembly"), r.URL.Query().Get("revision"))
	if err != nil {
		constructionError(w, err)
		return
	}
	st, err := s.construction.store.Load(sub, pid)
	if err != nil {
		constructionError(w, err)
		return
	}
	edges := construction.EvidenceEdges(st.Evidence, a, st.Decisions)
	g := graph.Build(edges, construction.ConstructionVocabulary())
	nodes := []constructionGraphNode{}
	for _, n := range g.Nodes() {
		nodes = append(nodes, constructionGraphNode{Ref: n.String(), Kind: n.Kind, ID: n.ID})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Ref < nodes[j].Ref })
	valid := []graph.Edge{}
	for _, e := range edges {
		if graph.Validate(e, construction.ConstructionVocabulary()) == nil {
			valid = append(valid, e)
		}
	}
	constructionJSON(w, map[string]any{"assemblyId": a.ID, "assemblyRevision": tok, "nodes": nodes, "edges": valid})
}
