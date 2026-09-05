package recruiting

import (
	"sort"
	"strings"

	"manifest/graph"
)

// Intro-path derivation (Phase 4). The network is a set of relationship
// CLAIMS (network/edges.md); this file turns them into ranked intro paths for
// one candidate. It invents nothing: every hop on a returned path is an edge
// that passed ValidateEdge, and every node is an endpoint of one of those
// edges. The search is deterministic — same people + edges → identical
// output — so two board loads never disagree about the best route in.
//
// The walk itself now lives in `graph` (P2 lifted it out of here unchanged:
// undirected simple paths, bounded depth, seeds never passed through, ranked
// fewer-hops → higher confidence → path string). This file is the adapter:
// a recruiting Edge is a graph.Edge whose endpoints are all `person`.

const (
	// PathKindDerived marks a PathClaim this file computed, as opposed to a
	// hand-authored `## network` row. The inspector keys off the kind.
	PathKindDerived = "derived"

	// MaxIntroHops bounds the search depth. An intro through four strangers
	// is not an intro, and the bound is what keeps enumeration cheap.
	MaxIntroHops = graph.DefaultMaxHops

	// DefaultTopPaths is how many ranked paths a view carries per candidate.
	DefaultTopPaths = graph.DefaultTopPaths

	// UnstatedEdgeConfidence is the weight of an edge whose row carries no
	// [confidence::]. It is deliberately below any owner-asserted value so a
	// claim with no stated strength never outranks one that has it.
	UnstatedEdgeConfidence = graph.UnstatedConfidence

	pathSep = " > "
)

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
	g := graph.Build(GraphEdges(edges), EdgeVocabulary())
	var starts []graph.Ref
	for _, s := range seeds {
		if s = strings.TrimSpace(s); s != "" {
			starts = append(starts, PersonRef(s))
		}
	}
	for _, p := range g.Paths(starts, PersonRef(target), graph.PathOptions{MaxHops: MaxIntroHops, TopN: topN}) {
		ids := make([]string, 0, len(p.Nodes))
		for _, n := range p.Nodes {
			ids = append(ids, n.ID)
		}
		out = append(out, PathClaim{
			Path:       strings.Join(ids, pathSep),
			Kind:       PathKindDerived,
			Confidence: FormatConfidence(p.Confidence),
			Inferred:   p.Inferred,
		})
	}
	return out
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
