package recruiting

import (
	"manifest/graph"
	"manifest/recruiting/sources"
)

// THE GRAPH ADAPTER (manifest P2 Phase 1). recruiting's Edge was the proven
// relationship-claim primitive; `graph` is that primitive generalized. This
// file is the whole seam between them: the recruiting vocabulary (one entity
// kind — every endpoint here is a person, whether a network row, a
// candidate, or an `ext/…` external key — over the adapter layer's closed
// EdgeType set), and the projection of a row into a typed edge. Validation
// and path search run in `graph`; this package keeps its own record files,
// its own identity resolution (edges_identity.go), and its own consent rules.
//
// The import edge runs recruiting → graph only. `graph` knows nothing of
// candidates or PII, and `aion` still cannot reach this package.

// EdgeVocabulary is recruiting's closed kind set as a graph vocabulary: the
// edge kinds are declared once, in `sources`, where the adapters that emit
// them live — this is a view of that set, not a second copy.
func EdgeVocabulary() graph.Vocabulary {
	kinds := make([]string, 0, len(sources.EdgeTypes))
	for _, t := range sources.EdgeTypes {
		kinds = append(kinds, string(t))
	}
	return graph.Vocabulary{EntityKinds: []string{graph.KindPerson}, EdgeKinds: kinds}
}

// PersonRef is the graph endpoint for a recruiting node id.
func PersonRef(id string) graph.Ref { return graph.R(graph.KindPerson, id) }

// Graph projects one row onto the general edge — same fields, typed
// endpoints.
func (e Edge) Graph() graph.Edge {
	return graph.Edge{
		From: PersonRef(e.From), To: PersonRef(e.To), Kind: e.Kind, Basis: e.Basis,
		Confidence: e.Confidence, Inferred: e.Inferred, Source: e.Source,
		Evidence: e.Evidence, Observed: e.Observed, Unknown: e.Unknown,
	}
}

// GraphEdges projects a document's rows.
func GraphEdges(edges []Edge) []graph.Edge {
	out := make([]graph.Edge, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.Graph())
	}
	return out
}
