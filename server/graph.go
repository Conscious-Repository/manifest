package server

// THE ENTITY/EDGE GRAPH (manifest P2 Phase 1) — the server face of `graph`:
//
//   - read:   GET /api/graph (vocabulary + counts) · /api/graph/edges?ref=
//             · /api/graph/neighbors?ref= · /api/graph/paths?from=&to=
//             · /api/graph/bridges?a=&b= · /api/graph/deps?ref=&dir=up|down
//   - write:  POST /api/graph/edges (one claim, validated: no basis → 400)
//             · POST /api/graph/entities (one registration)
//
// Every query runs over STORED claims (system/graph/edges.md) merged with
// DERIVED edges the other primitives already imply and nobody should have to
// re-state: a task line's [depends::] is a `depends_on` edge, its
// [outputs::]/[inputs::] are `produced`/`consumes` edges to artifacts (the
// P1 binding, as edges), and an artifact's provenance task is a `produced`
// edge. A stored claim with the same key wins over a derived one. Derived
// edges are never written to the file — they are recomputed from the objects
// on every read (file-as-truth stays with the objects that own the fact).
//
// Every write appends a ledger event tagged object={from-endpoint} with the
// task endpoint (if any) as a related ref, so an edge's history rides on the
// entity it was claimed about (GET /api/ledger/history).

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"manifest/artifacts"
	"manifest/graph"
	"manifest/ledger"
	"manifest/tasks"
)

// UseGraph wires the entity/edge record store.
func (s *Server) UseGraph(g *graph.Store) { s.graphStore = g }

// graphVocabulary is the closed kind set queries validate against — the
// store's when wired, the platform default otherwise (derived-only reads).
func (s *Server) graphVocabulary() graph.Vocabulary {
	if s.graphStore != nil {
		return s.graphStore.Vocabulary()
	}
	return graph.Default()
}

// derivedGraphEdges projects the edges the task lines and the artifact
// registry already imply. Basis names the field the fact was read from;
// source names the store; nothing here is inferred — a [depends::] token is
// a stated fact, just stated on the task line rather than in edges.md.
func (s *Server) derivedGraphEdges() []graph.Edge {
	var out []graph.Edge
	add := func(from, to graph.Ref, kind, basis, source string) {
		if from.ID == "" || to.ID == "" || from == to {
			return
		}
		out = append(out, graph.Edge{From: from, To: to, Kind: kind, Basis: basis, Source: source})
	}
	task := func(id string, t *tasks.Task) {
		if t == nil {
			return
		}
		me := graph.R(graph.KindTask, id)
		for _, dep := range t.Depends {
			add(me, graph.R(graph.KindTask, dep), graph.EdgeDependsOn, "depends field on the task line", "tasks")
		}
		for _, a := range t.Outputs {
			add(me, graph.R(graph.KindArtifact, a), graph.EdgeProduced, "outputs field on the task line", "tasks")
		}
		for _, a := range t.Inputs {
			add(me, graph.R(graph.KindArtifact, a), graph.EdgeConsumes, "inputs field on the task line", "tasks")
		}
	}
	if s.tasksStore != nil {
		if doc, err := s.tasksStore.Load(); err == nil {
			for _, dom := range doc.Domains {
				dom.AllTasks(func(_ *tasks.Bucket, t *tasks.Task) { task(t.ID, t) })
			}
		}
	}
	if s.realestate != nil {
		if props, err := s.realestate.Properties(); err == nil {
			for _, p := range props {
				for _, t := range p.Tasks {
					task("prop:"+p.Slug+"/"+t.ID, t)
				}
			}
		}
	}
	if s.artifactReg != nil {
		for _, a := range s.artifactReg.List(artifacts.Filter{}) {
			if a.Provenance.Task != "" {
				add(graph.R(graph.KindTask, a.Provenance.Task), graph.R(graph.KindArtifact, a.ID), graph.EdgeProduced, "artifact provenance", "artifact")
			}
			for _, in := range a.Provenance.Inputs {
				add(graph.R(graph.KindArtifact, a.ID), graph.R(graph.KindArtifact, in), graph.EdgeConsumes, "artifact provenance inputs", "artifact")
			}
		}
	}
	return out
}

// graphEdges is stored ∪ derived (stored key wins), plus the stored key set
// so a view can say which claims are on file.
func (s *Server) graphEdges() ([]graph.Edge, map[string]bool) {
	var stored []graph.Edge
	if s.graphStore != nil {
		stored = s.graphStore.LoadEdges().Edges()
	}
	keys := map[string]bool{}
	for _, e := range stored {
		keys[e.Key()] = true
	}
	return graph.Merge(stored, s.derivedGraphEdges()), keys
}

// graphBuild is the traversal graph over graphEdges.
func (s *Server) graphBuild() (*graph.Graph, map[string]bool) {
	edges, stored := s.graphEdges()
	return graph.Build(edges, s.graphVocabulary()), stored
}

// graphEdgeView is one edge on the wire, flagged by where it came from.
type graphEdgeView struct {
	graph.Edge
	Derived bool `json:"derived"`
}

func graphEdgeViews(edges []graph.Edge, stored map[string]bool) []graphEdgeView {
	out := make([]graphEdgeView, 0, len(edges))
	for _, e := range edges {
		out = append(out, graphEdgeView{Edge: e, Derived: !stored[e.Key()]})
	}
	return out
}

// graphHopView is a neighbour hop with the edge flagged.
type graphHopView struct {
	Node      graph.Ref       `json:"node"`
	Direction graph.Direction `json:"direction"`
	Edge      graphEdgeView   `json:"edge"`
}

// --- the ledger hooks ---------------------------------------------------------

// graphEdgeEvent mirrors an added claim into the ledger under its from-
// endpoint; a task on either end rides as the related ref so the task's
// history carries the edge too.
func (s *Server) graphEdgeEvent(e graph.Edge, actor string) {
	meta := map[string]any{
		"from": e.From.String(), "to": e.To.String(), "edgeKind": e.Kind, "basis": e.Basis,
		"inferred": e.Inferred, "source": e.Source,
	}
	if e.Confidence != "" {
		meta["confidence"] = e.Confidence
	}
	if e.Evidence != "" {
		meta["evidence"] = e.Evidence
	}
	entry := ledger.Entry{Source: "graph", Kind: "graph.edge.added", Actor: orStr(actor, "owner"),
		Object: ledger.Object{Kind: e.From.Kind, ID: e.From.ID},
		Text:   ledger.Snip(e.From.String()+" "+e.Kind+" "+e.To.String()+" — "+e.Basis, 280), Meta: meta}
	for _, r := range []graph.Ref{e.From, e.To} {
		if r.Kind == graph.KindTask && entry.Task == "" {
			entry.Task = r.ID
		}
	}
	s.ledger(entry)
}

// graphEntityEvent records a registration under the entity itself.
func (s *Server) graphEntityEvent(e graph.Entity, actor string) {
	entry := ledger.Entry{Source: "graph", Kind: "graph.entity.added", Actor: orStr(actor, "owner"),
		Object: ledger.Object{Kind: e.Kind, ID: e.ID}, Ref: e.Ref,
		Text: ledger.Snip(orStr(e.Title, e.ID), 280),
		Meta: map[string]any{"entityKind": e.Kind, "source": e.Source}}
	if e.Kind == graph.KindTask {
		entry.Task = e.ID
	}
	s.ledger(entry)
}

// --- handlers ---------------------------------------------------------------

func (s *Server) graphOK(w http.ResponseWriter) bool {
	if s.graphStore == nil {
		http.Error(w, "the graph store is not configured", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// graphRefParam reads one `kind:id` query param; a kindless ref is a 400.
func graphRefParam(w http.ResponseWriter, r *http.Request, name string) (graph.Ref, bool) {
	ref := graph.ParseRef(r.URL.Query().Get(name))
	if ref.Kind == "" || ref.ID == "" {
		httpError(w, errBadRequest(name+" must be kind:id (e.g. task:inbox/gutters)"))
		return ref, false
	}
	return ref, true
}

func graphKindsParam(r *http.Request) []string {
	var out []string
	for _, k := range strings.Split(r.URL.Query().Get("kind"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func graphIntParam(r *http.Request, name string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(name)))
	return n
}

// handleGraph — GET /api/graph: the vocabulary and what the graph holds.
func (s *Server) handleGraph(w http.ResponseWriter, _ *http.Request) {
	edges, stored := s.graphEdges()
	g := graph.Build(edges, s.graphVocabulary())
	entities := []graph.Entity{}
	if s.graphStore != nil {
		entities = s.graphStore.LoadEntities().Entities()
	}
	writeJSON(w, map[string]any{
		"vocabulary": s.graphVocabulary(),
		"configured": s.graphStore != nil,
		"entities":   entities,
		"stored":     len(stored),
		"derived":    len(edges) - len(stored),
		"edges":      g.Size(),
		"nodes":      len(g.Nodes()),
	})
}

// handleGraphEdges — GET /api/graph/edges?ref=kind:id[&kind=a,b]: every edge
// touching the entity (all edges when ref is absent).
func (s *Server) handleGraphEdges(w http.ResponseWriter, r *http.Request) {
	edges, stored := s.graphEdges()
	kinds := graphKindsParam(r)
	want := graph.ParseRef(r.URL.Query().Get("ref"))
	out := []graph.Edge{}
	for _, e := range edges {
		if want.ID != "" && e.From != want && e.To != want {
			continue
		}
		if len(kinds) > 0 && !containsStr(kinds, e.Kind) {
			continue
		}
		out = append(out, e)
	}
	writeJSON(w, map[string]any{"ref": want, "edges": graphEdgeViews(out, stored), "count": len(out)})
}

// handleGraphNeighbors — GET /api/graph/neighbors?ref=&dir=out|in&kind=&nodeKind=&facts=1.
func (s *Server) handleGraphNeighbors(w http.ResponseWriter, r *http.Request) {
	ref, ok := graphRefParam(w, r, "ref")
	if !ok {
		return
	}
	g, stored := s.graphBuild()
	q := r.URL.Query()
	f := graph.NeighborFilter{
		Direction: graph.Direction(strings.TrimSpace(q.Get("dir"))), Kinds: graphKindsParam(r),
		NodeKind: strings.TrimSpace(q.Get("nodeKind")), Facts: q.Get("facts") == "1",
	}
	hops := []graphHopView{}
	for _, h := range g.Neighbors(ref, f) {
		hops = append(hops, graphHopView{Node: h.Node, Direction: h.Direction, Edge: graphEdgeView{Edge: h.Edge, Derived: !stored[h.Edge.Key()]}})
	}
	writeJSON(w, map[string]any{"ref": ref, "degree": g.Degree(ref), "neighbors": hops, "count": len(hops)})
}

// handleGraphPaths — GET /api/graph/paths?from=a[,b]&to=&max=&top=&directed=1&kind=.
func (s *Server) handleGraphPaths(w http.ResponseWriter, r *http.Request) {
	to, ok := graphRefParam(w, r, "to")
	if !ok {
		return
	}
	var from []graph.Ref
	for _, part := range strings.Split(r.URL.Query().Get("from"), ",") {
		if ref := graph.ParseRef(part); ref.Kind != "" && ref.ID != "" {
			from = append(from, ref)
		}
	}
	if len(from) == 0 {
		httpError(w, errBadRequest("from must name at least one kind:id"))
		return
	}
	g, _ := s.graphBuild()
	paths := g.Paths(from, to, graph.PathOptions{
		MaxHops: graphIntParam(r, "max"), TopN: graphIntParam(r, "top"),
		Directed: r.URL.Query().Get("directed") == "1", Kinds: graphKindsParam(r),
	})
	views := []map[string]any{}
	for _, p := range paths {
		views = append(views, map[string]any{
			"path": p.String(), "nodes": p.Nodes, "edges": p.Edges, "hops": p.Hops(),
			"confidence": graph.FormatConfidence(p.Confidence), "inferred": p.Inferred,
		})
	}
	writeJSON(w, map[string]any{"from": from, "to": to, "paths": views, "count": len(views)})
}

// handleGraphBridges — GET /api/graph/bridges?a=&b=&directed=1: who bridges
// a and b.
func (s *Server) handleGraphBridges(w http.ResponseWriter, r *http.Request) {
	a, ok := graphRefParam(w, r, "a")
	if !ok {
		return
	}
	b, ok := graphRefParam(w, r, "b")
	if !ok {
		return
	}
	g, _ := s.graphBuild()
	bridges := g.Bridges(a, b, r.URL.Query().Get("directed") == "1")
	writeJSON(w, map[string]any{"a": a, "b": b, "bridges": bridges, "count": len(bridges)})
}

// handleGraphDeps — GET /api/graph/deps?ref=&dir=up|down&kind=&depth=: what
// this depends on (up: outgoing dependency edges) or what depends on it
// (down: the reverse edge), transitively.
func (s *Server) handleGraphDeps(w http.ResponseWriter, r *http.Request) {
	ref, ok := graphRefParam(w, r, "ref")
	if !ok {
		return
	}
	g, _ := s.graphBuild()
	kinds, depth := graphKindsParam(r), graphIntParam(r, "depth")
	dir := strings.TrimSpace(r.URL.Query().Get("dir"))
	var reach []graph.Reach
	switch dir {
	case "", "up":
		dir = "up"
		reach = g.Upstream(ref, kinds, depth)
	case "down":
		reach = g.Downstream(ref, kinds, depth)
	default:
		httpError(w, errBadRequest("dir must be up or down"))
		return
	}
	writeJSON(w, map[string]any{"ref": ref, "dir": dir, "reach": reach, "count": len(reach)})
}

// graphEdgeBody is the POST /api/graph/edges request.
type graphEdgeBody struct {
	From       string `json:"from"` // kind:id
	To         string `json:"to"`   // kind:id
	Kind       string `json:"kind"`
	Basis      string `json:"basis"`
	Confidence string `json:"confidence"`
	Inferred   bool   `json:"inferred"`
	Source     string `json:"source"`
	Evidence   string `json:"evidence"`
	Observed   string `json:"observed"`
	Actor      string `json:"actor"`
}

// handleGraphEdgeAdd — POST /api/graph/edges. Validated by the store (no
// basis, no source, a kind outside the set → 400); a replay of a claim
// already on file answers added=false and writes nothing.
func (s *Server) handleGraphEdgeAdd(w http.ResponseWriter, r *http.Request) {
	if !s.graphOK(w) {
		return
	}
	var b graphEdgeBody
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	e := graph.Edge{
		From: graph.ParseRef(b.From), To: graph.ParseRef(b.To), Kind: strings.TrimSpace(b.Kind),
		Basis: strings.TrimSpace(b.Basis), Confidence: strings.TrimSpace(b.Confidence), Inferred: b.Inferred,
		Source: orStr(strings.TrimSpace(b.Source), "owner"), Evidence: strings.TrimSpace(b.Evidence),
		Observed: orStr(strings.TrimSpace(b.Observed), time.Now().Format("2006-01-02")),
	}
	got, added, err := s.graphStore.AddEdge(e)
	if err != nil {
		httpError(w, err)
		return
	}
	actor := orStr(strings.TrimSpace(b.Actor), "owner")
	if added {
		s.graphEdgeEvent(got, actor)
	}
	writeJSON(w, map[string]any{"edge": got, "added": added})
}

// handleGraphEntityAdd — POST /api/graph/entities {id, kind, title, ref, source}.
func (s *Server) handleGraphEntityAdd(w http.ResponseWriter, r *http.Request) {
	if !s.graphOK(w) {
		return
	}
	var b struct {
		ID, Kind, Title, Ref, Source, Actor string
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	e := graph.Entity{
		ID: strings.TrimSpace(b.ID), Kind: strings.TrimSpace(b.Kind), Title: strings.TrimSpace(b.Title),
		Ref: strings.TrimSpace(b.Ref), Source: orStr(strings.TrimSpace(b.Source), "owner"),
		Added: time.Now().Format("2006-01-02"),
	}
	got, added, err := s.graphStore.AddEntity(e)
	if err != nil {
		httpError(w, err)
		return
	}
	actor := orStr(strings.TrimSpace(b.Actor), "owner")
	if added {
		s.graphEntityEvent(got, actor)
	}
	writeJSON(w, map[string]any{"entity": got, "added": added})
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
