package recruiting

import (
	"sort"
	"strconv"
	"strings"
)

// Intro-path derivation (Phase 4). The network is a set of relationship
// CLAIMS (network/edges.md); this file turns them into ranked intro paths for
// one candidate. It invents nothing: every hop on a returned path is an edge
// that passed ValidateEdge, and every node is an endpoint of one of those
// edges. The search is deterministic — same people + edges → identical
// output — so two board loads never disagree about the best route in.

const (
	// PathKindDerived marks a PathClaim this file computed, as opposed to a
	// hand-authored `## network` row. The inspector keys off the kind.
	PathKindDerived = "derived"

	// MaxIntroHops bounds the search depth. An intro through four strangers
	// is not an intro, and the bound is what keeps enumeration cheap.
	MaxIntroHops = 4

	// DefaultTopPaths is how many ranked paths a view carries per candidate.
	DefaultTopPaths = 3

	// UnstatedEdgeConfidence is the weight of an edge whose row carries no
	// [confidence::]. It is deliberately below any owner-asserted value so a
	// claim with no stated strength never outranks one that has it.
	UnstatedEdgeConfidence = 0.5

	pathSep = " > "
)

// pathGraph is the undirected adjacency built from validated edges.
type pathGraph struct {
	adj map[string][]pathHop
}

type pathHop struct {
	to         string
	confidence float64
	inferred   bool
}

// buildPathGraph keeps only edges that pass the "no claim without a basis"
// rule, and traverses each in both directions: an intro can flow either way
// along a relationship, whichever endpoint the row happened to name first.
func buildPathGraph(edges []Edge) pathGraph {
	g := pathGraph{adj: map[string][]pathHop{}}
	for _, e := range edges {
		if ValidateEdge(e) != nil {
			continue
		}
		from, to := strings.TrimSpace(e.From), strings.TrimSpace(e.To)
		if from == to {
			continue
		}
		conf := UnstatedEdgeConfidence
		if c := strings.TrimSpace(e.Confidence); c != "" {
			conf, _ = strconv.ParseFloat(c, 64) // ValidateEdge already bounded it
		}
		g.adj[from] = append(g.adj[from], pathHop{to: to, confidence: conf, inferred: e.Inferred})
		g.adj[to] = append(g.adj[to], pathHop{to: from, confidence: conf, inferred: e.Inferred})
	}
	// Sorted adjacency is what makes the enumeration order — and therefore
	// the tie-break among equal-scoring paths — a function of the input
	// rather than of map iteration.
	for k := range g.adj {
		hops := g.adj[k]
		sort.SliceStable(hops, func(i, j int) bool {
			if hops[i].to != hops[j].to {
				return hops[i].to < hops[j].to
			}
			if hops[i].confidence != hops[j].confidence {
				return hops[i].confidence > hops[j].confidence
			}
			return !hops[i].inferred && hops[j].inferred
		})
	}
	return g
}

// OwnerSeeds is the v1 default seed set: everyone whose consent is `owner`
// (the founders). It is explicit and small on purpose — an intro path that
// starts from someone who never agreed to be a node is not a path we offer.
func OwnerSeeds(people []NetworkPerson) []string {
	var out []string
	for _, p := range people {
		if p.Consent == "owner" && strings.TrimSpace(p.ID) != "" {
			out = append(out, strings.TrimSpace(p.ID))
		}
	}
	sort.Strings(out)
	return out
}

// derivedPath is a candidate route under consideration, before it is
// projected onto a PathClaim.
type derivedPath struct {
	nodes      []string
	confidence float64
	inferred   bool
}

func (p derivedPath) hops() int { return len(p.nodes) - 1 }

func (p derivedPath) String() string { return strings.Join(p.nodes, pathSep) }

// DerivePaths returns the top-N ranked intro paths from the seed set to
// `target`, as PathClaims. `seeds` empty means OwnerSeeds(people). Ranking:
// fewer hops first, then higher cumulative confidence (the product of the
// hop confidences), then the path string — so the order is total and stable.
// A path is Inferred when ANY hop on it is an inferred edge. No path from the
// seed set yields an empty (non-nil) slice.
func DerivePaths(people []NetworkPerson, edges []Edge, target string, seeds []string, topN int) []PathClaim {
	target = strings.TrimSpace(target)
	if topN <= 0 {
		topN = DefaultTopPaths
	}
	out := []PathClaim{}
	if target == "" {
		return out
	}
	if len(seeds) == 0 {
		seeds = OwnerSeeds(people)
	}
	g := buildPathGraph(edges)
	if len(g.adj[target]) == 0 {
		return out
	}
	seedSet := map[string]bool{}
	var seedList []string
	for _, s := range seeds {
		s = strings.TrimSpace(s)
		if s == "" || s == target || seedSet[s] {
			continue
		}
		seedSet[s] = true
		seedList = append(seedList, s)
	}
	sort.Strings(seedList)

	var found []derivedPath
	for _, seed := range seedList {
		if len(g.adj[seed]) == 0 {
			continue
		}
		walk(g, seed, target, seedSet, derivedPath{nodes: []string{seed}, confidence: 1}, &found)
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].hops() != found[j].hops() {
			return found[i].hops() < found[j].hops()
		}
		if found[i].confidence != found[j].confidence {
			return found[i].confidence > found[j].confidence
		}
		return found[i].String() < found[j].String()
	})
	for i, p := range found {
		if i >= topN {
			break
		}
		out = append(out, PathClaim{
			Path:       p.String(),
			Kind:       PathKindDerived,
			Confidence: FormatConfidence(p.confidence),
			Inferred:   p.inferred,
		})
	}
	return out
}

// walk enumerates simple paths from the current tail to target, bounded by
// MaxIntroHops. It does not pass THROUGH another seed: the route from that
// seed onward is strictly shorter and is enumerated on its own.
func walk(g pathGraph, cur, target string, seeds map[string]bool, p derivedPath, found *[]derivedPath) {
	if p.hops() >= MaxIntroHops {
		return
	}
	for _, h := range g.adj[cur] {
		if contains(p.nodes, h.to) {
			continue
		}
		next := derivedPath{
			nodes:      append(append([]string(nil), p.nodes...), h.to),
			confidence: p.confidence * h.confidence,
			inferred:   p.inferred || h.inferred,
		}
		if h.to == target {
			*found = append(*found, next)
			continue
		}
		if seeds[h.to] {
			continue
		}
		walk(g, h.to, target, seeds, next, found)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// MergePaths lays derived paths after the hand-authored ones. A hand-written
// row is owner evidence and is never rewritten or relabelled; a derived path
// whose route the owner already wrote down is dropped rather than shown
// twice.
func MergePaths(hand, derived []PathClaim) []PathClaim {
	out := make([]PathClaim, 0, len(hand)+len(derived))
	seen := map[string]bool{}
	for _, p := range hand {
		out = append(out, p)
		seen[normalizePath(p.Path)] = true
	}
	for _, p := range derived {
		if seen[normalizePath(p.Path)] {
			continue
		}
		seen[normalizePath(p.Path)] = true
		out = append(out, p)
	}
	return out
}

// normalizePath reduces a path string to its hop sequence so `a > b` and
// `a>b` compare equal.
func normalizePath(s string) string {
	parts := strings.Split(s, ">")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, pathSep)
}
