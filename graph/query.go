package graph

import (
	"sort"
	"strings"
)

// Traversal — recruiting's intro-path derivation (network_paths.go), lifted.
// It invents nothing: every hop on a returned path is an edge that passed
// Validate, and every node is an endpoint of one of those edges. Every query
// is deterministic — same edges → identical output — so two loads never
// disagree. A Graph is built per query from the edges that exist; there is
// no cached adjacency to drift from the files.

// Direction names which way a hop was taken relative to the edge's own
// from → to.
type Direction string

const (
	Out Direction = "out" // the edge leaves the node (node is From)
	In  Direction = "in"  // the edge enters the node (node is To)
)

// Hop is one adjacency entry: the neighbour reached and the edge it rode.
type Hop struct {
	Node      Ref       `json:"node"`
	Edge      Edge      `json:"edge"`
	Direction Direction `json:"direction"`
}

// Graph is the adjacency over validated edges.
type Graph struct {
	out   map[string][]Hop // node → edges leaving it
	in    map[string][]Hop // node → edges entering it
	nodes map[string]Ref
	count int
}

// Build keeps only edges that pass Validate under v (an invalid hand-edited
// row is skipped, never traversed) and drops self-loops.
func Build(edges []Edge, v Vocabulary) *Graph {
	g := &Graph{out: map[string][]Hop{}, in: map[string][]Hop{}, nodes: map[string]Ref{}}
	for _, e := range edges {
		if Validate(e, v) != nil {
			continue
		}
		e.From, e.To = R(e.From.Kind, e.From.ID), R(e.To.Kind, e.To.ID)
		if e.From == e.To {
			continue
		}
		f, t := e.From.String(), e.To.String()
		g.nodes[f], g.nodes[t] = e.From, e.To
		g.out[f] = append(g.out[f], Hop{Node: e.To, Edge: e, Direction: Out})
		g.in[t] = append(g.in[t], Hop{Node: e.From, Edge: e, Direction: In})
		g.count++
	}
	// Sorted adjacency is what makes enumeration order — and therefore the
	// tie-break among equal-scoring paths — a function of the input rather
	// than of map iteration (recruiting's rule).
	for _, adj := range []map[string][]Hop{g.out, g.in} {
		for k := range adj {
			sortHops(adj[k])
		}
	}
	return g
}

func sortHops(hops []Hop) {
	sort.SliceStable(hops, func(i, j int) bool {
		if a, b := hops[i].Node.String(), hops[j].Node.String(); a != b {
			return a < b
		}
		if a, b := hops[i].Edge.Weight(), hops[j].Edge.Weight(); a != b {
			return a > b
		}
		return !hops[i].Edge.Inferred && hops[j].Edge.Inferred
	})
}

// Size is the number of edges kept.
func (g *Graph) Size() int { return g.count }

// Nodes lists every endpoint, sorted.
func (g *Graph) Nodes() []Ref {
	out := make([]Ref, 0, len(g.nodes))
	for _, r := range g.nodes {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// Has reports whether the node touches any edge.
func (g *Graph) Has(r Ref) bool { _, ok := g.nodes[r.String()]; return ok }

// Degree is a node's edge counts.
type Degree struct {
	In    int `json:"in"`
	Out   int `json:"out"`
	Total int `json:"total"`
}

// Degree counts the edges entering and leaving a node.
func (g *Graph) Degree(r Ref) Degree {
	k := r.String()
	d := Degree{In: len(g.in[k]), Out: len(g.out[k])}
	d.Total = d.In + d.Out
	return d
}

// NeighborFilter narrows a Neighbors read. Zero value = every hop, both
// directions.
type NeighborFilter struct {
	Direction Direction // Out, In, or "" for both
	Kinds     []string  // edge kinds; empty = any
	NodeKind  string    // neighbour's entity kind; "" = any
	Facts     bool      // asserted edges only (drop inferred)
}

func (f NeighborFilter) keep(h Hop) bool {
	if f.Direction != "" && h.Direction != f.Direction {
		return false
	}
	if len(f.Kinds) > 0 && !inSet(f.Kinds, h.Edge.Kind) {
		return false
	}
	if f.NodeKind != "" && h.Node.Kind != f.NodeKind {
		return false
	}
	if f.Facts && h.Edge.Inferred {
		return false
	}
	return true
}

// Neighbors lists the hops around a node: outgoing first, then incoming,
// each in sorted adjacency order.
func (g *Graph) Neighbors(r Ref, f NeighborFilter) []Hop {
	k := r.String()
	out := []Hop{}
	for _, h := range g.out[k] {
		if f.keep(h) {
			out = append(out, h)
		}
	}
	for _, h := range g.in[k] {
		if f.keep(h) {
			out = append(out, h)
		}
	}
	return out
}

// Outgoing / Incoming are the two halves of Neighbors, as edges.
func (g *Graph) Outgoing(r Ref) []Edge { return hopEdges(g.out[r.String()]) }
func (g *Graph) Incoming(r Ref) []Edge { return hopEdges(g.in[r.String()]) }

func hopEdges(hops []Hop) []Edge {
	out := make([]Edge, 0, len(hops))
	for _, h := range hops {
		out = append(out, h.Edge)
	}
	return out
}

// Upstream is "what does this depend on": the nodes reached by following
// OUTGOING edges of the given kinds (DependencyKinds when empty), breadth-
// first, transitively, up to maxDepth hops (0 = unbounded). The node itself
// is never listed.
func (g *Graph) Upstream(r Ref, kinds []string, maxDepth int) []Reach {
	return g.reach(r, Out, kinds, maxDepth)
}

// Downstream is "what depends on this": the reverse edge — the nodes whose
// outgoing dependency edges lead here, transitively.
func (g *Graph) Downstream(r Ref, kinds []string, maxDepth int) []Reach {
	return g.reach(r, In, kinds, maxDepth)
}

// Reach is one node a transitive walk found, with how far and through which
// edge it was first reached.
type Reach struct {
	Node  Ref  `json:"node"`
	Depth int  `json:"depth"`
	Via   Edge `json:"via"`
}

func (g *Graph) reach(start Ref, dir Direction, kinds []string, maxDepth int) []Reach {
	if len(kinds) == 0 {
		kinds = DependencyKinds
	}
	adj := g.out
	if dir == In {
		adj = g.in
	}
	seen := map[string]bool{start.String(): true}
	out := []Reach{}
	frontier := []Ref{start}
	for depth := 1; len(frontier) > 0 && (maxDepth <= 0 || depth <= maxDepth); depth++ {
		var next []Ref
		for _, cur := range frontier {
			for _, h := range adj[cur.String()] {
				if !inSet(kinds, h.Edge.Kind) || seen[h.Node.String()] {
					continue
				}
				seen[h.Node.String()] = true
				out = append(out, Reach{Node: h.Node, Depth: depth, Via: h.Edge})
				next = append(next, h.Node)
			}
		}
		frontier = next
	}
	return out
}

// ---- paths ----

// PathOptions bounds a Paths search. Zero value: undirected, MaxHops 4, top
// 3 — recruiting's intro-path defaults.
type PathOptions struct {
	MaxHops  int      // 0 → DefaultMaxHops
	TopN     int      // 0 → DefaultTopPaths; <0 → all
	Directed bool     // follow edges from → to only (default: either way)
	Kinds    []string // edge kinds to traverse; empty = any
	// Avoid are nodes a path may END at but never pass THROUGH — recruiting's
	// seed rule: the route from another seed onward is strictly shorter and
	// is enumerated on its own.
	Avoid []Ref
}

const (
	// DefaultMaxHops bounds the search depth: an intro through four
	// strangers is not an intro, and the bound is what keeps enumeration cheap.
	DefaultMaxHops = 4
	// DefaultTopPaths is how many ranked paths a view carries.
	DefaultTopPaths = 3
)

// Path is one simple route. Confidence is the product of the hop weights;
// Inferred is set when ANY hop is an inferred edge.
type Path struct {
	Nodes      []Ref   `json:"nodes"`
	Edges      []Edge  `json:"edges"`
	Confidence float64 `json:"confidence"`
	Inferred   bool    `json:"inferred"`
}

// Hops is the path length in edges.
func (p Path) Hops() int { return len(p.Nodes) - 1 }

// String joins the node refs with " > ".
func (p Path) String() string {
	parts := make([]string, 0, len(p.Nodes))
	for _, n := range p.Nodes {
		parts = append(parts, n.String())
	}
	return strings.Join(parts, " > ")
}

// Paths enumerates simple paths from any of `from` to `to` and ranks them:
// fewer hops first, then higher cumulative confidence, then the path string —
// a total, stable order. Returns an empty (non-nil) slice when none.
func (g *Graph) Paths(from []Ref, to Ref, opt PathOptions) []Path {
	maxHops, topN := opt.MaxHops, opt.TopN
	if maxHops <= 0 {
		maxHops = DefaultMaxHops
	}
	if topN == 0 {
		topN = DefaultTopPaths
	}
	out := []Path{}
	if to.ID == "" || !g.Has(to) {
		return out
	}
	avoid := map[string]bool{}
	for _, a := range opt.Avoid {
		avoid[a.String()] = true
	}
	seen := map[string]bool{}
	var starts []Ref
	for _, s := range from {
		if s.ID == "" || s == to || seen[s.String()] {
			continue
		}
		seen[s.String()] = true
		avoid[s.String()] = true // a start is never passed through either
		starts = append(starts, s)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i].String() < starts[j].String() })

	var found []Path
	for _, s := range starts {
		if !g.Has(s) {
			continue
		}
		g.walk(s, to, opt, maxHops, avoid, Path{Nodes: []Ref{s}, Confidence: 1}, &found)
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Hops() != found[j].Hops() {
			return found[i].Hops() < found[j].Hops()
		}
		if found[i].Confidence != found[j].Confidence {
			return found[i].Confidence > found[j].Confidence
		}
		return found[i].String() < found[j].String()
	})
	for i, p := range found {
		if topN > 0 && i >= topN {
			break
		}
		out = append(out, p)
	}
	return out
}

// hops lists the adjacency a path search may take from a node.
func (g *Graph) hops(cur Ref, opt PathOptions) []Hop {
	k := cur.String()
	if opt.Directed {
		return g.out[k]
	}
	// undirected: both lists, merged in sorted order so the enumeration stays
	// a function of the input
	merged := append(append([]Hop(nil), g.out[k]...), g.in[k]...)
	sortHops(merged)
	return merged
}

func (g *Graph) walk(cur, target Ref, opt PathOptions, maxHops int, avoid map[string]bool, p Path, found *[]Path) {
	if p.Hops() >= maxHops {
		return
	}
	for _, h := range g.hops(cur, opt) {
		if len(opt.Kinds) > 0 && !inSet(opt.Kinds, h.Edge.Kind) {
			continue
		}
		if containsRef(p.Nodes, h.Node) {
			continue
		}
		next := Path{
			Nodes:      append(append([]Ref(nil), p.Nodes...), h.Node),
			Edges:      append(append([]Edge(nil), p.Edges...), h.Edge),
			Confidence: p.Confidence * h.Edge.Weight(),
			Inferred:   p.Inferred || h.Edge.Inferred,
		}
		if h.Node == target {
			*found = append(*found, next)
			continue
		}
		if avoid[h.Node.String()] {
			continue
		}
		g.walk(h.Node, target, opt, maxHops, avoid, next, found)
	}
}

func containsRef(list []Ref, r Ref) bool {
	for _, v := range list {
		if v == r {
			return true
		}
	}
	return false
}

// ---- bridges ----

// Bridge is one node that lies on a shortest route between two others.
type Bridge struct {
	Node     Ref `json:"node"`
	FromA    int `json:"fromA"`    // hops from a
	FromB    int `json:"fromB"`    // hops from b
	Distance int `json:"distance"` // the a↔b shortest distance it sits on
}

// Bridges answers "who bridges X and Y": every node (neither endpoint) that
// sits on SOME shortest undirected path between a and b, nearest to a first.
// Empty when a and b are unconnected or adjacent. Directed follows edge
// direction a → b.
func (g *Graph) Bridges(a, b Ref, directed bool) []Bridge {
	da := g.distances(a, directed, false)
	db := g.distances(b, directed, true)
	dist, ok := da[b.String()]
	if !ok || dist < 2 {
		return []Bridge{}
	}
	out := []Bridge{}
	for k, n := range g.nodes {
		if n == a || n == b {
			continue
		}
		x, okx := da[k]
		y, oky := db[k]
		if okx && oky && x+y == dist {
			out = append(out, Bridge{Node: n, FromA: x, FromB: y, Distance: dist})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromA != out[j].FromA {
			return out[i].FromA < out[j].FromA
		}
		return out[i].Node.String() < out[j].Node.String()
	})
	return out
}

// distances is BFS hop counts from start. reverse walks edges backwards
// (only meaningful when directed).
func (g *Graph) distances(start Ref, directed, reverse bool) map[string]int {
	dist := map[string]int{start.String(): 0}
	frontier := []Ref{start}
	for len(frontier) > 0 {
		var next []Ref
		for _, cur := range frontier {
			k := cur.String()
			var hops []Hop
			switch {
			case !directed:
				hops = append(append(hops, g.out[k]...), g.in[k]...)
			case reverse:
				hops = g.in[k]
			default:
				hops = g.out[k]
			}
			for _, h := range hops {
				if _, seen := dist[h.Node.String()]; seen {
					continue
				}
				dist[h.Node.String()] = dist[k] + 1
				next = append(next, h.Node)
			}
		}
		frontier = next
	}
	return dist
}
