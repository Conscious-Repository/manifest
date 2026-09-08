package server

import (
	"net/http"
	"strings"

	"manifest/graph"
	"manifest/recruiting"
)

// DOMAIN LEVERAGE — GET /api/aion/recruiting/leverage?topic=&hops=&limit=
//
// "Who are the highest-leverage people in X": the people whose expertise
// edges name the topic, ranked by that confidence scaled by how connected
// they are to people we know (recruiting/leverage.go has the formula and
// the reasons). Two reads feed it and nothing is written:
//
//   - the general graph (system/graph): the `expertise` edges the knowledge
//     overlay derived, and the person ↔ person ties ties.go mirrored there;
//   - the recruiting network: network/edges.md plus the calendar / notes
//     derivations the store merges on read (NetworkEdges) — the same edge set
//     the ego graph draws.
//
// The two are unioned by undirected key (the store's rule: a claim on file
// wins), so a coauthorship stated in both places counts once. Names come
// from the same index the ego graph uses, with the graph's own entity titles
// as the fallback, and "known" means exactly what the index knows.
func (s *Server) handleRecruitingLeverage(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	q := r.URL.Query()
	topic := strings.TrimSpace(q.Get("topic"))
	if topic == "" {
		httpError(w, errBadRequest("topic is required, e.g. ?topic=low-field%20mri"))
		return
	}

	var knowledge, general []graph.Edge
	titles := map[string]string{}
	if s.graphStore != nil {
		general = s.graphStore.LoadEdges().Edges()
		for _, e := range general {
			if e.Kind == graph.EdgeExpertise {
				knowledge = append(knowledge, e)
			}
		}
		for _, ent := range s.graphStore.LoadEntities().Entities() {
			if ent.Kind == graph.KindPerson && ent.Title != "" {
				titles[ent.ID] = ent.Title
			}
		}
	}

	ties := s.recruiting.NetworkEdges()
	have := map[string]bool{}
	for _, e := range ties {
		have[recruiting.TieKey(e)] = true
	}
	for _, e := range recruiting.PersonTies(general) {
		if k := recruiting.TieKey(e); !have[k] {
			have[k] = true
			ties = append(ties, e)
		}
	}

	idx := s.personIndex()
	display := func(id string) (string, bool) {
		if n, ok := idx.name[id]; ok && n != "" {
			return n, true
		}
		if t, ok := titles[id]; ok {
			return t, true
		}
		return id, false
	}

	res := recruiting.RankLeverage(topic, knowledge, ties, display, recruiting.LeverageOptions{
		Hops: graphIntParam(r, "hops"), Limit: graphIntParam(r, "limit"),
	})
	writeJSON(w, struct {
		recruiting.LeverageResult
		Configured bool `json:"configured"` // a graph store is wired; without one there is no knowledge to rank
	}{res, s.graphStore != nil})
}
