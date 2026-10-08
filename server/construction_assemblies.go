package server

// Construction assemblies: exact-revision documents, compiled geometry (the
// IR every view draws from), a same-version GLB download, the validation
// report written for a revision, and an uncommitted preview for tentative
// edits. All derive from one canonical assembly revision; nothing here can
// change an assembly except the typed command route.

import (
	"net/http"
	"sync"

	"manifest/construction"
)

func (s *Server) registerConstructionAssemblyRoutes(mux *http.ServeMux, p string) {
	mux.HandleFunc("GET "+p+"/problems/{id}/assemblies/{asm}", s.handleConstructionAssembly)
	mux.HandleFunc("POST "+p+"/problems/{id}/assemblies/{asm}/commands", s.handleConstructionAssemblyCommands)
	mux.HandleFunc("GET "+p+"/problems/{id}/assemblies/{asm}/geometry", s.handleConstructionGeometry)
	mux.HandleFunc("GET "+p+"/problems/{id}/assemblies/{asm}/glb", s.handleConstructionGLB)
	mux.HandleFunc("GET "+p+"/problems/{id}/assemblies/{asm}/validation", s.handleConstructionValidation)
	mux.HandleFunc("POST "+p+"/problems/{id}/assemblies/{asm}/preview", s.handleConstructionPreview)
}

// geometryCache keeps compiled IR by assembly revision token (immutable).
type geometryCache struct {
	mu    sync.Mutex
	items map[string]*construction.GeometryIR
	order []string
}

func (g *geometryCache) get(tok string) *construction.GeometryIR {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.items[tok]
}

func (g *geometryCache) put(tok string, ir *construction.GeometryIR) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.items == nil {
		g.items = map[string]*construction.GeometryIR{}
	}
	if _, ok := g.items[tok]; ok {
		return
	}
	g.items[tok] = ir
	g.order = append(g.order, tok)
	if len(g.order) > 32 {
		delete(g.items, g.order[0])
		g.order = g.order[1:]
	}
}

// constructionIR compiles one exact assembly revision (cached by token).
func (s *Server) constructionIR(sub construction.SubjectRef, problemID, asmID, rev string) (*construction.GeometryIR, *construction.Assembly, string, error) {
	a, tok, err := s.construction.store.AssemblyAt(sub, problemID, asmID, rev)
	if err != nil {
		return nil, nil, "", err
	}
	if ir := s.construction.geometry.get(tok); ir != nil {
		return ir, a, tok, nil
	}
	st, err := s.construction.store.Load(sub, problemID)
	if err != nil {
		return nil, nil, "", err
	}
	ir, err := construction.Compile(a, st.Catalog)
	if err != nil {
		return nil, nil, "", err
	}
	s.construction.geometry.put(tok, ir)
	return ir, a, tok, nil
}

func (s *Server) handleConstructionAssembly(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	a, tok, err := s.construction.store.AssemblyAt(sub, r.PathValue("id"), r.PathValue("asm"), r.URL.Query().Get("revision"))
	if err != nil {
		constructionError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+tok+`"`)
	constructionJSON(w, map[string]any{"assembly": a, "revision": tok})
}

func (s *Server) handleConstructionAssemblyCommands(w http.ResponseWriter, r *http.Request) {
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
	if pc.Command.ProblemID != r.PathValue("id") || pc.Command.AssemblyID != r.PathValue("asm") {
		constructionError(w, construction.Invalid("problemId/assemblyId do not match the URL"))
		return
	}
	s.executeConstructionCommand(w, sub, pc, actor)
}

func (s *Server) handleConstructionGeometry(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	ir, a, tok, err := s.constructionIR(sub, r.PathValue("id"), r.PathValue("asm"), r.URL.Query().Get("revision"))
	if err != nil {
		constructionError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+ir.Hash+`"`)
	constructionJSON(w, map[string]any{"assemblyRevision": tok, "modelHash": a.ModelHash, "geometry": ir,
		"generatorChanged": a.ModelHash != "" && a.ModelHash != ir.Hash})
}

func (s *Server) handleConstructionGLB(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	id := r.PathValue("id")
	ir, a, tok, err := s.constructionIR(sub, id, r.PathValue("asm"), r.URL.Query().Get("revision"))
	if err != nil {
		constructionError(w, err)
		return
	}
	glb, err := construction.GLB(ir, map[string]string{"problemId": id, "assemblyId": a.ID, "assemblyRevision": tok, "assemblyName": a.Name})
	if err != nil {
		constructionError(w, err)
		return
	}
	w.Header().Set("Content-Type", "model/gltf-binary")
	w.Header().Set("Content-Disposition", `attachment; filename="`+a.ID+"-"+tok[:12]+`.glb"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Geometry-Hash", ir.Hash)
	_, _ = w.Write(glb)
}

func (s *Server) handleConstructionValidation(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	id, asmID := r.PathValue("id"), r.PathValue("asm")
	_, tok, err := s.construction.store.AssemblyAt(sub, id, asmID, r.URL.Query().Get("revision"))
	if err != nil {
		constructionError(w, err)
		return
	}
	rep, err := s.construction.store.ValidationAt(sub, id, asmID, tok)
	if err != nil {
		constructionError(w, err)
		return
	}
	constructionJSON(w, map[string]any{"assemblyRevision": tok, "report": rep, "notice": construction.NonApprovalNotice})
}

// handleConstructionPreview evaluates a command WITHOUT writing it: the
// tentative geometry and findings a drag handle shows before commit.
func (s *Server) handleConstructionPreview(w http.ResponseWriter, r *http.Request) {
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
	if pc.Command.ProblemID != r.PathValue("id") || pc.Command.AssemblyID != r.PathValue("asm") {
		constructionError(w, construction.Invalid("problemId/assemblyId do not match the URL"))
		return
	}
	res, ir, err := s.construction.store.Preview(sub, pc, actor, &construction.ApplyContext{ResolveScope: s.constructionScopeResolver(sub)})
	if err != nil {
		constructionError(w, err)
		return
	}
	out := map[string]any{"preview": res}
	if ir != nil && r.URL.Query().Get("geometry") == "1" {
		out["geometry"] = ir
	}
	constructionJSON(w, out)
}
