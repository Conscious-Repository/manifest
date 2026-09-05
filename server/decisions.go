package server

// THE DECISION LEDGER (manifest P3 Phase 1) — the server face of `decisions`:
//
//   - read:   GET /api/decisions (ledger notes + the backlog's decision items,
//             projected read-only through aion's adapter) · GET
//             /api/decisions/get?id= (one, with its ledger history and the
//             graph edges touching it — the "decision view" projection)
//   - write:  POST /api/decisions/create · POST /api/decisions/update (a
//             partial patch; a status change, an outcome, an actual outcome
//             each become their own lifecycle event)
//
// Cross-links, none of them written twice:
//   - the graph: a created decision is REGISTERED as a `decision` entity in
//     system/graph/entities.md (idempotent, through the graph store), and the
//     record's evidence / downstream refs are DERIVED edges on every graph
//     read (graph.go: evidence → informs/supports the decision; a downstream
//     task depends_on it; a downstream artifact was produced by it);
//   - the ledger: decision.created / updated / decided / revisited / reopened
//     under object={decision,id}, so GET /api/ledger/history reconstructs what
//     happened to a decision and what changed each time. The backlog's own
//     decide action (aion.go) writes decision.decided under its aion-bl/ id.

import (
	"net/http"
	"strings"
	"time"

	"manifest/decisions"
	"manifest/graph"
	"manifest/ledger"
)

// UseDecisions wires the decision ledger store.
func (s *Server) UseDecisions(st *decisions.Store) { s.decisionStore = st }

// decisionsOK answers 503 when no store is wired.
func (s *Server) decisionsOK(w http.ResponseWriter) bool {
	if s.decisionStore == nil {
		http.Error(w, "the decision ledger is not configured", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// aionDecisions projects the backlog's decision items (read-only coexistence).
func (s *Server) aionDecisions() []decisions.Decision {
	if s.aion == nil {
		return nil
	}
	return s.aion.LoadBacklog().Decisions()
}

// decisionByID resolves a ledger note first, then a backlog decision by its
// aion-bl/ id.
func (s *Server) decisionByID(id string) (decisions.Decision, bool) {
	if s.decisionStore != nil {
		if d, ok := s.decisionStore.Get(id); ok {
			return d, true
		}
	}
	for _, d := range s.aionDecisions() {
		if d.ID == id {
			return d, true
		}
	}
	return decisions.Decision{}, false
}

// decisionView is the minimal projection: the entity, where its note lives
// (ledger notes only), whether this surface may edit it, its ledger history,
// and the graph edges touching it.
type decisionView struct {
	decisions.Decision
	Editable bool            `json:"editable"`
	Open     *artifactOpen   `json:"open,omitempty"`
	History  *ledger.History `json:"history,omitempty"`
	Graph    []graphHopView  `json:"graph,omitempty"`
}

func (s *Server) decisionView(d decisions.Decision, full bool) decisionView {
	v := decisionView{Decision: d, Editable: d.Source != "aion" && s.decisionStore != nil}
	if v.Editable {
		v.Open = &artifactOpen{Note: s.decisionStore.Rel(d.ID)}
	}
	if !full {
		return v
	}
	if s.ledgerStore != nil {
		if h, err := s.ledgerStore.History(ledger.ObjDecision, d.ID); err == nil {
			v.History = &h
		}
	}
	g, stored := s.graphBuild()
	v.Graph = []graphHopView{}
	for _, h := range g.Neighbors(graph.R(graph.KindDecision, d.ID), graph.NeighborFilter{}) {
		v.Graph = append(v.Graph, graphHopView{Node: h.Node, Direction: h.Direction, Edge: graphEdgeView{Edge: h.Edge, Derived: !stored[h.Edge.Key()]}})
	}
	return v
}

// decisionGraphEdges projects the ledger notes' refs as edges (never
// written): what informed the decision, and what hangs off it.
func (s *Server) decisionGraphEdges(add func(from, to graph.Ref, kind, basis, source string)) {
	if s.decisionStore == nil {
		return
	}
	vocab := s.graphVocabulary()
	for _, d := range s.decisionStore.List() {
		me := graph.R(graph.KindDecision, d.ID)
		for _, l := range d.Evidence {
			kind, id, ok := decisions.RefKind(l.Ref)
			if !ok || !vocab.ValidEntityKind(kind) {
				continue
			}
			edge := graph.EdgeInforms
			if kind == graph.KindHeuristic {
				edge = graph.EdgeSupports
			}
			add(graph.R(kind, id), me, edge, "evidence on the decision record", "decisions")
		}
		for _, l := range d.Downstream {
			kind, id, ok := decisions.RefKind(l.Ref)
			if !ok || !vocab.ValidEntityKind(kind) {
				continue
			}
			switch kind {
			case graph.KindTask:
				add(graph.R(kind, id), me, graph.EdgeDependsOn, "downstream on the decision record", "decisions")
			case graph.KindArtifact:
				add(me, graph.R(kind, id), graph.EdgeProduced, "downstream on the decision record", "decisions")
			default:
				add(me, graph.R(kind, id), graph.EdgeRelated, "downstream on the decision record", "decisions")
			}
		}
	}
}

// --- the ledger hooks ---------------------------------------------------------

// decisionEvent appends one lifecycle line under the decision.
func (s *Server) decisionEvent(kind string, d decisions.Decision, actor, text string, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["status"] = d.Status
	if d.Owner != "" {
		meta["owner"] = d.Owner
	}
	if d.Source != "" {
		meta["source"] = d.Source
	}
	entry := ledger.Entry{Source: "decision", Kind: kind, Actor: orStr(actor, "owner"),
		Object: ledger.Object{Kind: ledger.ObjDecision, ID: d.ID},
		Text:   ledger.Snip(orStr(text, d.Title), 280), Meta: meta}
	if s.decisionStore != nil && d.Source != "aion" {
		entry.Ref = s.decisionStore.Rel(d.ID)
	}
	// the first downstream task rides as the related ref so its history carries the decision
	for _, l := range d.Downstream {
		if kind, id, ok := decisions.RefKind(l.Ref); ok && kind == graph.KindTask {
			entry.Task = id
			break
		}
	}
	s.ledger(entry)
}

// decisionRegister makes the decision a graph entity (idempotent; a replay
// registers nothing and ledgers nothing).
func (s *Server) decisionRegister(d decisions.Decision, actor string) {
	if s.graphStore == nil {
		return
	}
	e := graph.Entity{ID: d.ID, Kind: graph.KindDecision, Title: d.Title, Ref: s.decisionStore.Rel(d.ID),
		Source: "decisions", Added: time.Now().Format("2006-01-02")}
	if got, added, err := s.graphStore.AddEntity(e); err == nil && added {
		s.graphEntityEvent(got, actor)
	}
}

// --- handlers ---------------------------------------------------------------

// handleDecisions — GET /api/decisions?status=&owner=&source=ledger|aion.
func (s *Server) handleDecisions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status, owner, source := strings.ToLower(strings.TrimSpace(q.Get("status"))), strings.TrimSpace(q.Get("owner")), strings.TrimSpace(q.Get("source"))
	var all []decisions.Decision
	if s.decisionStore != nil && source != "aion" {
		all = append(all, s.decisionStore.List()...)
	}
	if source == "" || source == "aion" {
		all = append(all, s.aionDecisions()...)
	}
	out := []decisionView{}
	for _, d := range all {
		if status != "" && d.Status != status {
			continue
		}
		if owner != "" && !strings.EqualFold(d.Owner, owner) {
			continue
		}
		out = append(out, s.decisionView(d, false))
	}
	writeJSON(w, map[string]any{"decisions": out, "count": len(out), "statuses": decisions.Statuses, "configured": s.decisionStore != nil})
}

// handleDecisionGet — GET /api/decisions/get?id=: the decision view.
func (s *Server) handleDecisionGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	d, ok := s.decisionByID(id)
	if !ok {
		http.Error(w, "no such decision", http.StatusNotFound)
		return
	}
	writeJSON(w, s.decisionView(d, true))
}

// decisionBody is the create/update request. Pointer fields so an update can
// tell "unset" from "clear".
type decisionBody struct {
	ID              string                   `json:"id"`
	Title           *string                  `json:"title"`
	Owner           *string                  `json:"owner"`
	Status          *string                  `json:"status"`
	Outcome         *string                  `json:"outcome"`
	Why             *string                  `json:"why"`
	Evidence        *[]decisions.Link        `json:"evidence"`
	Alternatives    *[]decisions.Alternative `json:"alternatives"`
	ExpectedOutcome *string                  `json:"expectedOutcome"`
	ActualOutcome   *string                  `json:"actualOutcome"`
	Downstream      *[]decisions.Link        `json:"downstream"`
	NeededBy        *string                  `json:"neededBy"`
	Sources         *[]string                `json:"sources"`
	Source          string                   `json:"source"`
	Actor           string                   `json:"actor"`
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// handleDecisionCreate — POST /api/decisions/create. Validated by the store
// (no title, a status outside the set, an open twin → 400); the note is
// written, the entity registered in the graph, the ledger told.
func (s *Server) handleDecisionCreate(w http.ResponseWriter, r *http.Request) {
	if !s.decisionsOK(w) {
		return
	}
	var b decisionBody
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	actor := orStr(strings.TrimSpace(b.Actor), "owner")
	dec := decisions.Decision{
		ID: strings.TrimSpace(b.ID), Title: deref(b.Title), Owner: deref(b.Owner), Status: deref(b.Status),
		Outcome: deref(b.Outcome), Why: deref(b.Why), ExpectedOutcome: deref(b.ExpectedOutcome),
		ActualOutcome: deref(b.ActualOutcome), NeededBy: deref(b.NeededBy), Source: orStr(strings.TrimSpace(b.Source), actor),
	}
	if b.Evidence != nil {
		dec.Evidence = *b.Evidence
	}
	if b.Alternatives != nil {
		dec.Alternatives = *b.Alternatives
	}
	if b.Downstream != nil {
		dec.Downstream = *b.Downstream
	}
	if b.Sources != nil {
		dec.Sources = *b.Sources
	}
	got, err := s.decisionStore.Create(dec, time.Now())
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	s.decisionEvent("decision.created", got, actor, got.Title, map[string]any{
		"evidence": len(got.Evidence), "alternatives": len(got.Alternatives), "downstream": len(got.Downstream),
	})
	if got.Status == decisions.StatusDecided {
		s.decisionEvent("decision.decided", got, actor, got.Title+" — "+orStr(got.Outcome, "decided"), map[string]any{"outcome": got.Outcome})
	}
	s.decisionRegister(got, actor)
	writeJSON(w, map[string]any{"decision": s.decisionView(got, false), "created": true})
}

// handleDecisionUpdate — POST /api/decisions/update {id, …partial}. Nothing
// changed → changed:false, no write, no event. A lifecycle transition gets
// its own event kind on top of the field diff.
func (s *Server) handleDecisionUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.decisionsOK(w) {
		return
	}
	var b decisionBody
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	id := strings.TrimSpace(b.ID)
	if id == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	if strings.HasPrefix(id, "aion-bl/") {
		httpError(w, errBadRequest("a backlog decision is edited through the aion backlog"))
		return
	}
	actor := orStr(strings.TrimSpace(b.Actor), "owner")
	ch, err := s.decisionStore.Update(id, decisions.Patch{
		Title: b.Title, Owner: b.Owner, Status: b.Status, Outcome: b.Outcome, Why: b.Why,
		ExpectedOutcome: b.ExpectedOutcome, ActualOutcome: b.ActualOutcome, NeededBy: b.NeededBy,
		Evidence: b.Evidence, Downstream: b.Downstream, Alternatives: b.Alternatives, Sources: b.Sources,
	}, time.Now())
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		httpError(w, errBadRequest(err.Error()))
		return
	}
	if ch.Changed() {
		s.decisionEvent("decision.updated", ch.After, actor, ch.After.Title+" — "+strings.Join(ch.Fields, ", "),
			map[string]any{"fields": ch.Fields, "before": ch.Before.Status})
		switch ch.Transition {
		case "decided":
			s.decisionEvent("decision.decided", ch.After, actor, ch.After.Title+" — "+orStr(ch.After.Outcome, "decided"), map[string]any{"outcome": ch.After.Outcome, "expected": ch.After.ExpectedOutcome})
		case "revisited":
			s.decisionEvent("decision.revisited", ch.After, actor, ch.After.Title+" — "+orStr(ch.After.ActualOutcome, "revisited"), map[string]any{"expected": ch.After.ExpectedOutcome, "actual": ch.After.ActualOutcome})
		case "reopened":
			s.decisionEvent("decision.reopened", ch.After, actor, ch.After.Title+" — reopened", map[string]any{"from": ch.Before.Status})
		}
	}
	writeJSON(w, map[string]any{"decision": s.decisionView(ch.After, false), "changed": ch.Changed(), "fields": ch.Fields, "transition": ch.Transition})
}
