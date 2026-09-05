package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"manifest/recruiting"
)

// THE EGO GRAPH (surface plan §5) — one view, centred on you, honest at any
// size.
//
// The owner's complaint was that the Network tab was "really unclear to me how
// to use this". It was three lists of ids. The answer the field converged on
// — van Ham & Perer's "Search, Show Context, Expand on Demand", Kumu's
// degree-bounded focus, Linkurious' supernode guardrails — is NOT a canvas of
// everything: it is one centre, a bounded number of hops, and a picture that
// cannot become a hairball because nothing beyond the chosen degree is ever
// sent.
//
// The whole layer is a READ. It writes nothing, derives nothing new, and the
// edges it draws are exactly the ones `recruiting.Store.NetworkEdges()`
// already merges (file ∪ derived). Drawing is not deciding.
//
// ⚠ This does NOT reuse /api/graph. That API is over the vault's entity
// substrate (`kind:id` refs from notes); the recruiting network is a different
// set of nodes with a different identity scheme, and joining the two is its
// own piece of work, not a side effect of drawing a picture.

const (
	// graphMaxDegree is the hop ceiling. Three is already "friends of friends
	// of friends" — past that the picture stops being about you.
	graphMaxDegree = 3
	// graphRingCap bounds ONE ring. A supernode (the owner, who has met
	// everybody) would otherwise render 900 dots and mean nothing; the ring
	// keeps its best and SAYS how many it left out, which is the honest half
	// of a guardrail.
	graphRingCap = 60
	// graphMaxNodes bounds the whole answer.
	graphMaxNodes = 240
)

// graphNode is one person in the picture.
type graphNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`            // you | connector | considering | stranger
	Hop   int    `json:"hop"`             // rings out from the centre
	Deg   int    `json:"deg"`             // edges within the RENDERED set
	Stage string `json:"stage,omitempty"` // a candidate's stage, so a node shows state and not just topology
	Role  string `json:"role,omitempty"`
}

// graphReply is the whole answer: what to draw, and what was left out.
type graphReply struct {
	Center  string             `json:"center"`
	Degree  int                `json:"degree"`
	Nodes   []graphNode        `json:"nodes"`
	Edges   []recruiting.Edge  `json:"edges"`
	Kinds   []graphKindCount   `json:"kinds"`
	Omitted map[string]int     `json:"omitted,omitempty"` // hop → how many that ring could not show
	Totals  map[string]int     `json:"totals"`            // the whole graph, for the honest empty state
	Missing []string           `json:"missing,omitempty"` // what a person would have to do to fill it
	Focus   map[string]string  `json:"focus,omitempty"`   // the centre's own row, for the panel header
	Search  []graphSearchMatch `json:"search,omitempty"`
}

type graphKindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type graphSearchMatch struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// GET /api/aion/recruiting/graph?center=&degree=&kind=&q=
//
// An empty center means YOU — the connector row the calendar derivation
// already treats as the owner. `q` answers the search box without drawing
// anything, which is van Ham & Perer's step one: you search, then you expand.
func (s *Server) handleRecruitingGraph(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	idx := s.personIndex()
	edges := s.recruiting.NetworkEdges()

	kinds := map[string]bool{}
	for _, k := range r.URL.Query()["kind"] {
		for _, one := range strings.Split(k, ",") {
			if one = strings.TrimSpace(one); one != "" {
				kinds[one] = true
			}
		}
	}
	// the kind counts describe the WHOLE graph, not the filtered one — a chip
	// that shows how many it would add is a chip you can decide about
	counts := map[string]int{}
	for _, e := range edges {
		counts[e.Kind]++
	}
	var kindRows []graphKindCount
	for k, n := range counts {
		kindRows = append(kindRows, graphKindCount{Kind: k, Count: n})
	}
	sort.Slice(kindRows, func(i, j int) bool {
		if kindRows[i].Count != kindRows[j].Count {
			return kindRows[i].Count > kindRows[j].Count
		}
		return kindRows[i].Kind < kindRows[j].Kind
	})

	if len(kinds) > 0 {
		kept := edges[:0:0]
		for _, e := range edges {
			if kinds[e.Kind] {
				kept = append(kept, e)
			}
		}
		edges = kept
	}

	board := s.recruiting.Identities()
	conns := s.recruiting.Connectors()
	owner := idx.ownerNode(s.recruiting)

	reply := graphReply{
		Degree: graphDegree(r.URL.Query().Get("degree")),
		Kinds:  kindRows,
		Totals: map[string]int{
			"edges": len(s.recruiting.NetworkEdges()), "people": len(conns), "board": len(board),
		},
	}

	// the search box: matches by label, never drawn until chosen
	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); q != "" {
		reply.Search = graphSearch(q, idx, edges, board, conns, owner)
	}

	center := strings.TrimSpace(r.URL.Query().Get("center"))
	if center == "" {
		center = owner
	}
	if center == "" {
		// nobody is marked as you, so there is no centre to stand at — say
		// what would make one rather than drawing an arbitrary node
		reply.Missing = graphMissing(edges, conns)
		writeJSON(w, reply)
		return
	}
	reply.Center = center
	reply.Focus = map[string]string{"id": center, "label": idx.display(center)}

	adj := map[string][]recruiting.Edge{}
	for _, e := range edges {
		adj[e.From] = append(adj[e.From], e)
		adj[e.To] = append(adj[e.To], e)
	}

	// ---- the bounded walk. Nothing past `degree` is ever added, so the
	// hairball is unreachable by construction rather than by a slider.
	kindOf := graphKinder(board, conns, owner)
	hop := map[string]int{center: 0}
	order := []string{center}
	frontier := []string{center}
	omitted := map[string]int{}
	for d := 1; d <= reply.Degree && len(order) < graphMaxNodes; d++ {
		var next []string
		seen := map[string]bool{}
		for _, from := range frontier {
			for _, e := range adj[from] {
				other := e.To
				if other == from {
					other = e.From
				}
				if other == "" || other == from {
					continue
				}
				if _, ok := hop[other]; ok || seen[other] {
					continue
				}
				seen[other] = true
				next = append(next, other)
			}
		}
		// a ring is ranked before it is cut: the people you are DECIDING
		// about first, then the ones you would ask, then everyone else — so
		// a cut ring loses strangers, not candidates
		sort.SliceStable(next, func(i, j int) bool {
			ri, rj := graphRank(kindOf(next[i])), graphRank(kindOf(next[j]))
			if ri != rj {
				return ri < rj
			}
			return idx.display(next[i]) < idx.display(next[j])
		})
		if len(next) > graphRingCap {
			omitted[strconv.Itoa(d)] = len(next) - graphRingCap
			next = next[:graphRingCap]
		}
		if len(order)+len(next) > graphMaxNodes {
			room := graphMaxNodes - len(order)
			omitted[strconv.Itoa(d)] += len(next) - room
			next = next[:room]
		}
		for _, id := range next {
			hop[id] = d
			order = append(order, id)
		}
		frontier = next
		if len(next) == 0 {
			break
		}
	}
	if len(omitted) > 0 {
		reply.Omitted = omitted
	}

	// ---- the edges WITHIN the drawn set, and the degree each node has there
	deg := map[string]int{}
	var kept []recruiting.Edge
	for _, e := range edges {
		_, a := hop[e.From]
		_, b := hop[e.To]
		if !a || !b {
			continue
		}
		kept = append(kept, e)
		deg[e.From]++
		deg[e.To]++
	}
	reply.Edges = kept

	// ⚠ NOT View(): View derives every candidate's intro paths from these very
	// edges, so drawing the picture through it pays the whole derivation twice
	// — and the graph only wants two fields.
	state := s.recruiting.BoardState()
	for _, id := range order {
		st := state[id]
		reply.Nodes = append(reply.Nodes, graphNode{
			ID: id, Label: idx.display(id), Kind: kindOf(id),
			Hop: hop[id], Deg: deg[id], Stage: st[0], Role: st[1],
		})
	}
	if len(kept) == 0 {
		reply.Missing = graphMissing(edges, conns)
	}
	writeJSON(w, reply)
}

func graphDegree(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 2
	}
	if n > graphMaxDegree {
		return graphMaxDegree
	}
	return n
}

// graphKinder answers what a node IS — which is what makes a node encode
// operational state instead of topology (the standing critique of the global
// graph view: pretty, and it tells you nothing you can act on).
func graphKinder(board []recruiting.PersonIdentity, conns []recruiting.NetworkPerson, owner string) func(string) string {
	on := map[string]string{}
	for _, c := range board {
		on[c.ID] = "considering"
	}
	for _, p := range conns {
		if p.Archived == "" {
			on[p.ID] = "connector"
		}
	}
	return func(id string) string {
		if id != "" && id == owner {
			return "you"
		}
		if k, ok := on[id]; ok {
			return k
		}
		return "stranger"
	}
}

func graphRank(kind string) int {
	switch kind {
	case "you":
		return 0
	case "considering":
		return 1
	case "connector":
		return 2
	}
	return 3
}

// graphSearch is the entry point, not the canvas: it answers "who" without
// drawing anybody, and the answer is what you then centre on.
func graphSearch(q string, idx personIndex, edges []recruiting.Edge,
	board []recruiting.PersonIdentity, conns []recruiting.NetworkPerson, owner string) []graphSearchMatch {
	kindOf := graphKinder(board, conns, owner)
	seen := map[string]bool{}
	add := func(out []graphSearchMatch, id string) []graphSearchMatch {
		if id == "" || seen[id] || len(out) >= 25 {
			return out
		}
		label := idx.display(id)
		if !strings.Contains(strings.ToLower(label), q) {
			return out
		}
		seen[id] = true
		return append(out, graphSearchMatch{ID: id, Label: label, Kind: kindOf(id)})
	}
	var out []graphSearchMatch
	for _, c := range board {
		out = add(out, c.ID)
	}
	for _, p := range conns {
		if p.Archived == "" {
			out = add(out, p.ID)
		}
	}
	for _, e := range edges {
		out = add(out, e.From)
		out = add(out, e.To)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := graphRank(out[i].Kind), graphRank(out[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// graphMissing names the gestures that would fill an empty picture. An empty
// state that only says "empty" is a dead end; this one is the design.
func graphMissing(edges []recruiting.Edge, conns []recruiting.NetworkPerson) []string {
	var out []string
	live := 0
	for _, p := range conns {
		if p.Archived == "" {
			live++
		}
	}
	if live == 0 {
		out = append(out, "mark someone you would ask — open PEOPLE, then `who I'd ask`")
	}
	if len(edges) == 0 {
		out = append(out, "sweep a paper or a repo — its authors and contributors arrive already connected")
	}
	if live > 0 && len(edges) > 0 {
		out = append(out, "nobody here is within reach of the centre — try a wider degree")
	}
	return out
}
