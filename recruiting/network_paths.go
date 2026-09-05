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
	return NewPathFinder(people, edges).Paths([]string{target}, seeds, topN)
}

// PathFinder is the network graph built once, so a listing that asks about
// many people (every draft in every run) does not rebuild the adjacency per
// question. Same edges → identical answers as DerivePaths.
type PathFinder struct {
	people []NetworkPerson
	g      *graph.Graph
}

// NewPathFinder builds the traversal graph over the network's claims.
func NewPathFinder(people []NetworkPerson, edges []Edge) *PathFinder {
	return &PathFinder{people: people, g: graph.Build(GraphEdges(edges), EdgeVocabulary())}
}

// Paths is DerivePaths for one person who may be known under several ids —
// a queued draft is reachable only by its external keys (ext/orcid/…,
// ext/openalex/…) until accept repoints those onto its record. Routes to
// every target are ranked together; the top-N wins.
func (f *PathFinder) Paths(targets []string, seeds []string, topN int) []PathClaim {
	if topN <= 0 {
		topN = DefaultTopPaths
	}
	out := []PathClaim{}
	if f == nil || f.g == nil {
		return out
	}
	if len(seeds) == 0 {
		seeds = OwnerSeeds(f.people)
	}
	var starts []graph.Ref
	for _, s := range seeds {
		if s = strings.TrimSpace(s); s != "" {
			starts = append(starts, PersonRef(s))
		}
	}
	var found []graph.Path
	seenTarget := map[string]bool{}
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t == "" || seenTarget[t] {
			continue
		}
		seenTarget[t] = true
		// every target's own top-N, then re-ranked together: a route to an
		// external key is as real as one to the record id
		found = append(found, f.g.Paths(starts, PersonRef(t), graph.PathOptions{MaxHops: MaxIntroHops, TopN: topN})...)
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
		if i >= topN {
			break
		}
		out = append(out, pathClaim(p))
	}
	return out
}

// pathClaim renders one graph path as the record-shaped claim, naming the
// hop it rests on and the oldest date any hop was seen.
func pathClaim(p graph.Path) PathClaim {
	ids := make([]string, 0, len(p.Nodes))
	for _, n := range p.Nodes {
		ids = append(ids, n.ID)
	}
	c := PathClaim{
		Path:       strings.Join(ids, pathSep),
		Kind:       PathKindDerived,
		Confidence: FormatConfidence(p.Confidence),
		Inferred:   p.Inferred,
	}
	weakest, oldest := -1, ""
	for i, e := range p.Edges {
		if weakest < 0 || e.Weight() < p.Edges[weakest].Weight() {
			weakest = i
		}
		if o := strings.TrimSpace(e.Observed); o != "" && (oldest == "" || o < oldest) {
			oldest = o
		}
	}
	if weakest >= 0 {
		e := p.Edges[weakest]
		parts := []string{e.From.ID + pathSep + e.To.ID, e.Kind, FormatConfidence(e.Weight())}
		if e.Inferred {
			parts = append(parts, "inferred")
		}
		c.Weakest = strings.Join(parts, " · ")
	}
	c.Observed = oldest
	return c
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
