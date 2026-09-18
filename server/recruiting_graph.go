package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/recruiting"
	"manifest/recruiting/sources"
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
	ID     string `json:"id"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`            // you | in_touch | pursuing | bridge | passed | source | stranger
	Hop    int    `json:"hop"`             // rings out from the centre
	Deg    int    `json:"deg"`             // edges within the RENDERED set
	Stage  string `json:"stage,omitempty"` // a candidate's stage, so a node shows state and not just topology
	Role   string `json:"role,omitempty"`
	Source string `json:"source,omitempty"` // a bridge node: the adapter that named them
	Seed   string `json:"seed,omitempty"`   // a bridge node: the source node they hang off
	Swept  string `json:"swept,omitempty"`  // a bridge node: when, for the stale fade
	Run    string `json:"run,omitempty"`    // a bridge node: the run and draft pursue/pass act on
	Draft  string `json:"draft,omitempty"`
	// Hue is the source node's colour slot (0–11), carried by its members so
	// "who came from where" can be the fill when the owner flips colour-by;
	// -1 when the node hangs off no source.
	Hue int `json:"hue"`
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
	Mode    string             `json:"mode"` // ego | whole
	// Calendar is "" or the reason the calendar-derived ties are missing —
	// a dead sign-in zeroes every same_meeting edge silently, and an ego view
	// that then says "nobody within reach" is lying about the cause.
	Calendar string `json:"calendar,omitempty"`
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
	totalEdges := len(edges)

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
	state := s.recruiting.BoardState()
	conns := s.recruiting.Connectors()
	bridge := s.recruiting.BridgePeople()
	sourceNodes := s.recruiting.SourceNodes()
	passed := map[string]bool{}
	for k := range s.recruiting.PassedSet() {
		passed[k] = true
	}
	owner := idx.ownerNode(s.recruiting)
	kindOf := graphKinder(board, state, conns, bridge, sourceNodes, passed, owner)

	// ---- the lens (social graph plan D-G): which STATUSES are drawn, and
	// whether sources are. A hidden status is absent from the walk itself,
	// not painted over, so nothing routes through what you cannot see.
	mode := "ego"
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("mode")), "whole") {
		mode = "whole"
	}
	shown := graphStatusFilter(r.URL.Query().Get("status"))
	showSources := r.URL.Query().Get("sources") != "0"
	visible := func(id string) bool {
		k := kindOf(id)
		if k == "source" {
			return showSources
		}
		if k == "you" {
			return true
		}
		return shown[k]
	}
	{
		kept := edges[:0:0]
		for _, e := range edges {
			if visible(e.From) && visible(e.To) {
				kept = append(kept, e)
			}
		}
		edges = kept
	}

	reply := graphReply{
		Degree:   graphDegree(r.URL.Query().Get("degree")),
		Kinds:    kindRows,
		Mode:     mode,
		Calendar: s.calendarTiesMissing(),
		Totals: map[string]int{
			"edges": totalEdges, "people": len(conns), "board": len(board), "bridge": len(bridge), "sources": len(sourceNodes),
		},
	}

	// the search box: matches by label, never drawn until chosen
	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); q != "" {
		reply.Search = graphSearch(q, idx, edges, board, state, conns, bridge, sourceNodes, passed, owner)
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
	hop := map[string]int{center: 0}
	order := []string{center}
	frontier := []string{center}
	omitted := map[string]int{}
	if mode == "whole" {
		// WHOLE MODE (D-G): everything the lens lets through, bounded by the
		// same ceiling and the same ranked cut as a ring — the people you are
		// deciding about survive, strangers go first — and the cut is said.
		all := map[string]bool{center: true}
		for _, e := range edges {
			all[e.From] = true
			all[e.To] = true
		}
		for _, c := range board {
			if visible(c.ID) {
				all[c.ID] = true
			}
		}
		for _, p := range conns {
			if p.Archived == "" && visible(p.ID) {
				all[p.ID] = true
			}
		}
		for _, p := range bridge {
			if visible(p.ID) {
				all[p.ID] = true
			}
		}
		var ids []string
		for id := range all {
			if id != center && id != "" {
				ids = append(ids, id)
			}
		}
		sort.SliceStable(ids, func(i, j int) bool {
			ri, rj := graphRank(kindOf(ids[i])), graphRank(kindOf(ids[j]))
			if ri != rj {
				return ri < rj
			}
			return idx.display(ids[i]) < idx.display(ids[j])
		})
		if len(ids) > graphMaxNodes-1 {
			omitted["whole"] = len(ids) - (graphMaxNodes - 1)
			ids = ids[:graphMaxNodes-1]
		}
		for _, id := range ids {
			// the ring is the rank, so the layout seeds people you know
			// nearer than strangers before the forces take over
			hop[id] = graphRank(kindOf(id)) + 1
			order = append(order, id)
		}
		frontier = nil
	}
	for d := 1; mode == "ego" && d <= reply.Degree && len(order) < graphMaxNodes; d++ {
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
	bridgeBy := map[string]recruiting.BridgePerson{}
	for _, p := range bridge {
		bridgeBy[p.ID] = p
	}
	// a source node's hue is a stable function of its id; a member takes the
	// hue of the first source it hangs off in the drawn set
	memberHue := map[string]int{}
	for _, e := range kept {
		if e.Kind == string(sources.EdgeMemberOf) {
			if _, ok := memberHue[e.From]; !ok {
				memberHue[e.From] = graphHue(e.To)
			}
		}
	}
	for _, id := range order {
		st := state[id]
		n := graphNode{
			ID: id, Label: idx.display(id), Kind: kindOf(id),
			Hop: hop[id], Deg: deg[id], Stage: st[0], Role: st[1], Hue: -1,
		}
		if n.Kind == "source" {
			n.Hue = graphHue(id)
		} else if h, ok := memberHue[id]; ok {
			n.Hue = h
		}
		if b, ok := bridgeBy[id]; ok {
			n.Source, n.Seed, n.Swept, n.Run, n.Draft = b.Source, b.Seed, b.Swept, b.RunID, b.Draft
		}
		reply.Nodes = append(reply.Nodes, n)
	}
	if len(kept) == 0 {
		reply.Missing = graphMissing(edges, conns)
		if reply.Calendar != "" {
			reply.Missing = append([]string{reply.Calendar}, reply.Missing...)
		}
	}
	writeJSON(w, reply)
}

// calendarTiesMissing names the reason the meeting ties are absent, when they
// are: the calendar sign-in has expired (the OAuth app's 7-day tokens, the
// recurring cause), so PastMeetings returns nothing and every same_meeting
// edge is gone. "" when the calendar is fine or not configured at all.
func (s *Server) calendarTiesMissing() string {
	if s.cal == nil || !s.cal.Enabled() {
		return ""
	}
	for _, st := range s.cal.AccountStatuses(time.Now()) {
		if st.NeedsReauth {
			return "your calendar sign-in expired (" + st.Email + ") — reconnect it in Settings; the ties from meetings are missing until you do"
		}
	}
	return ""
}

// graphStatusFilter reads `status=a,b,c`; empty means every status but
// passed — the default the plan names (D-B): looked at and declined is
// hidden, and remembered.
func graphStatusFilter(raw string) map[string]bool {
	out := map[string]bool{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]bool{"in_touch": true, "pursuing": true, "bridge": true, "stranger": true}
	}
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(strings.ToLower(s)); s != "" {
			out[s] = true
		}
	}
	return out
}

// graphHue is the colour slot of a source node: a stable hash of its id
// into twelve, so the same lab is the same hue every time it is drawn.
func graphHue(id string) int {
	h := uint32(2166136261)
	for i := 0; i < len(id); i++ {
		h ^= uint32(id[i])
		h *= 16777619
	}
	return int(h % 12)
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

// graphKinder answers what a node IS — the plan's STATUS (D-B), which is
// what makes a node encode operational state instead of topology. Nothing is
// stored for it: a contact or a consent:owner connector is in_touch, an
// active candidate is pursuing (a projection of the record's stage, D-H), an
// archived one or a tombstoned person is passed, a person a live sweep named
// is bridge, a seed or run is a source, and an endpoint in none of those is a
// stranger — an old edge naming someone nobody knows any more.
func graphKinder(board []recruiting.PersonIdentity, state map[string][2]string, conns []recruiting.NetworkPerson,
	bridge []recruiting.BridgePerson, sourceNodes map[string]string, passed map[string]bool, owner string) func(string) string {
	on := map[string]string{}
	for id := range sourceNodes {
		on[id] = "source"
	}
	for _, p := range bridge {
		on[p.ID] = "bridge"
	}
	for _, c := range board {
		if st := state[c.ID]; st[0] == recruiting.StageArchived {
			on[c.ID] = "passed"
		} else {
			on[c.ID] = "pursuing"
		}
	}
	for _, p := range conns {
		if p.Archived != "" {
			continue
		}
		// a route may START only from consent:owner (OwnerSeeds); a row put
		// there any other way is known, which on this graph is in_touch too
		on[p.ID] = "in_touch"
	}
	return func(id string) string {
		if id != "" && id == owner {
			return "you"
		}
		if k, ok := on[id]; ok {
			return k
		}
		if strings.HasPrefix(id, "contact/") {
			return "in_touch"
		}
		if strings.HasPrefix(id, "seed/") || strings.HasPrefix(id, "source/") {
			return "source"
		}
		if passed[id] {
			return "passed"
		}
		return "stranger"
	}
}

// graphRank orders a ring before it is cut: the people you are deciding
// about, then the ones you know, then the sources and the people a sweep
// named, then strangers, then the passed.
func graphRank(kind string) int {
	switch kind {
	case "you":
		return 0
	case "pursuing":
		return 1
	case "in_touch":
		return 2
	case "source":
		return 3
	case "bridge":
		return 4
	case "stranger":
		return 5
	}
	return 6
}

// graphSearch is the entry point, not the canvas: it answers "who" without
// drawing anybody, and the answer is what you then centre on.
func graphSearch(q string, idx personIndex, edges []recruiting.Edge,
	board []recruiting.PersonIdentity, state map[string][2]string, conns []recruiting.NetworkPerson,
	bridge []recruiting.BridgePerson, sourceNodes map[string]string, passed map[string]bool, owner string) []graphSearchMatch {
	kindOf := graphKinder(board, state, conns, bridge, sourceNodes, passed, owner)
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

// graphProfile is the deterministic section of the profile panel (social
// graph plan §5): never guessed, assembled from what is on file for this id
// wherever it lives — a candidate record, a network row, the run cache.
type graphProfile struct {
	ID      string            `json:"id"`
	Label   string            `json:"label"`
	Kind    string            `json:"kind"`
	Stage   string            `json:"stage,omitempty"`
	Role    string            `json:"role,omitempty"`
	Org     string            `json:"org,omitempty"`
	Title   string            `json:"title,omitempty"`
	Links   []string          `json:"links"`   // identity links on file, in file order
	Sources []graphProfileSrc `json:"sources"` // the source nodes this person hangs off
	Ties    []graphProfileTie `json:"ties"`    // every claim naming them, with works
	Run     string            `json:"run,omitempty"`
	Draft   string            `json:"draft,omitempty"`
	Swept   string            `json:"swept,omitempty"`
}

type graphProfileSrc struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type graphProfileTie struct {
	Other      string            `json:"other"`
	OtherLabel string            `json:"otherLabel"`
	Kind       string            `json:"kind"`
	Basis      string            `json:"basis,omitempty"`
	Confidence string            `json:"confidence,omitempty"`
	Inferred   bool              `json:"inferred"`
	Works      []sources.WorkRef `json:"works,omitempty"`
	Strength   float64           `json:"strength"`
}

// GET /api/aion/recruiting/graph/node?id=
func (s *Server) handleRecruitingGraphNode(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		httpError(w, errBadRequest("which node?"))
		return
	}
	idx := s.personIndex()
	board := s.recruiting.Identities()
	state := s.recruiting.BoardState()
	conns := s.recruiting.Connectors()
	bridge := s.recruiting.BridgePeople()
	sourceNodes := s.recruiting.SourceNodes()
	passed := map[string]bool{}
	for k := range s.recruiting.PassedSet() {
		passed[k] = true
	}
	kindOf := graphKinder(board, state, conns, bridge, sourceNodes, passed, idx.ownerNode(s.recruiting))

	p := graphProfile{ID: id, Label: idx.display(id), Kind: kindOf(id), Links: []string{}, Sources: []graphProfileSrc{}, Ties: []graphProfileTie{}}
	p.Stage, p.Role = state[id][0], state[id][1]
	// identity links, from wherever the person's own row lives
	if strings.HasPrefix(id, "cand/") {
		for _, slug := range s.recruiting.CandidateSlugs() {
			doc := s.recruiting.LoadCandidate(slug)
			if doc.Get("id") != id {
				continue
			}
			prof := doc.Profile()
			p.Org, p.Title = prof["org"], prof["title"]
			for _, k := range []string{"orcid", "website", "github", "linkedin", "x", "scholar"} {
				if v := strings.TrimSpace(prof[k]); v != "" {
					p.Links = append(p.Links, v)
				}
			}
			break
		}
	}
	for _, c := range conns {
		if c.ID == id {
			p.Org, p.Title = c.Org, c.Title
			for _, v := range []string{c.ORCID, c.GitHub, c.LinkedIn} {
				if strings.TrimSpace(v) != "" {
					p.Links = append(p.Links, v)
				}
			}
		}
	}
	for _, b := range bridge {
		if b.ID == id {
			p.Org, p.Title, p.Run, p.Draft, p.Swept = b.Org, b.Title, b.RunID, b.Draft, b.Swept
			p.Links = append(p.Links, b.Links...)
		}
	}
	for _, e := range s.recruiting.NetworkEdges() {
		other := ""
		switch id {
		case e.From:
			other = e.To
		case e.To:
			other = e.From
		default:
			continue
		}
		if e.Kind == string(sources.EdgeMemberOf) && e.From == id {
			p.Sources = append(p.Sources, graphProfileSrc{ID: other, Label: idx.display(other)})
			continue
		}
		p.Ties = append(p.Ties, graphProfileTie{
			Other: other, OtherLabel: idx.display(other), Kind: e.Kind, Basis: e.Basis,
			Confidence: e.Confidence, Inferred: e.Inferred, Works: e.Works, Strength: e.Strength(),
		})
	}
	sort.SliceStable(p.Ties, func(i, j int) bool {
		if p.Ties[i].Strength != p.Ties[j].Strength {
			return p.Ties[i].Strength > p.Ties[j].Strength
		}
		return p.Ties[i].OtherLabel < p.Ties[j].OtherLabel
	})
	writeJSON(w, p)
}
