package recruiting

import (
	"strings"
	"testing"
	"time"

	"manifest/recruiting/sources"
)

// coauthorDraft is one author of one paper, as the OpenAlex works path emits
// them: an external key for the coauthor, an empty near endpoint.
func coauthorDraft(name, orcid string, coauthors ...string) sources.CandidateDraft {
	d := sources.CandidateDraft{
		SourceID: "openalex", Name: name,
		Links: []string{"https://orcid.org/" + orcid},
		Evidence: []sources.Evidence{{
			SourceID: "openalex", URLOrFile: "https://doi.org/10.1234/x",
			RetrievedAt: testNow, Kind: sources.EvidencePublication,
			Snippet: "A paper, Journal, 2026", Trust: sources.TrustMedium,
		}},
	}
	for _, c := range coauthors {
		d.Edges = append(d.Edges, sources.EdgeClaim{
			From: sources.ExtNodePrefix + "orcid/" + c, Type: sources.EdgeCoauthor,
			SourceID: "openalex", Basis: c + " and " + name + " are both authors on A paper",
			Confidence: 0.55,
		})
	}
	return d
}

// The graph must not carry one person under two names. An edge written when
// someone was a stranger is repointed at their record the moment they land.
func TestEdgesRepointWhenTheStrangerBecomesARecord(t *testing.T) {
	s, _ := testStore(t)
	alice := "0000-0001-0000-0001"
	bob := "0000-0002-0000-0002"

	// Alice lands first; her edge names Bob by his ORCID, because Bob is
	// nobody here yet.
	if _, err := s.AcceptDraft(coauthorDraft("Alice Ng", alice, bob), testNow); err != nil {
		t.Fatal(err)
	}
	edges := s.LoadEdges().Edges()
	if len(edges) != 1 {
		t.Fatalf("one claim: %+v", edges)
	}
	if edges[0].From != "ext/orcid/"+bob {
		t.Fatalf("a stranger is named by a durable key: %q", edges[0].From)
	}
	if !strings.HasPrefix(edges[0].To, "cand/") {
		t.Fatalf("the near endpoint became the record: %q", edges[0].To)
	}

	// Bob lands. His draft's edge names Alice — who IS a record now — and
	// the edge that named HIM must stop saying ext/orcid.
	if _, err := s.AcceptDraft(coauthorDraft("Bob Ito", bob, alice), testNow); err != nil {
		t.Fatal(err)
	}
	edges = s.LoadEdges().Edges()
	if len(edges) != 1 {
		t.Fatalf("the same claim from the other side is the SAME claim: %+v", edges)
	}
	e := edges[0]
	if strings.Contains(e.From, "ext/") || strings.Contains(e.To, "ext/") {
		t.Fatalf("both endpoints are records now: %+v", e)
	}
	if !strings.HasPrefix(e.From, "cand/") || !strings.HasPrefix(e.To, "cand/") {
		t.Fatalf("endpoints: %+v", e)
	}
}

// An edge already on file is not written twice — re-sweeping the same paper
// is a normal thing to do.
func TestEdgesDedupeTheSameClaim(t *testing.T) {
	s, _ := testStore(t)
	a, b, c := "0000-0001-0000-0001", "0000-0002-0000-0002", "0000-0003-0000-0003"
	if _, err := s.AcceptDraft(coauthorDraft("Alice Ng", a, b, c), testNow); err != nil {
		t.Fatal(err)
	}
	first := len(s.LoadEdges().Edges())
	if first != 2 {
		t.Fatalf("two coauthors, two claims: %d", first)
	}
	// the same person cannot be accepted twice, so re-run the claim through a
	// second record that shares one of the coauthors
	if _, err := s.AcceptDraft(coauthorDraft("Bob Ito", b, c), testNow); err != nil {
		t.Fatal(err)
	}
	for _, e := range s.LoadEdges().Edges() {
		if e.From == e.To {
			t.Fatalf("an edge from someone to themselves: %+v", e)
		}
	}
	seen := map[string]bool{}
	for _, e := range s.LoadEdges().Edges() {
		k := edgeKey(e.From, e.To, e.Kind)
		if seen[k] {
			t.Fatalf("the same claim twice: %+v", e)
		}
		seen[k] = true
	}
}

// A candidate whose profile carries an ORCID link is the same person as the
// external key that names it — resolution reads links, not just source_ref.
func TestExtKeyFromURL(t *testing.T) {
	cases := map[string]string{
		"https://orcid.org/0000-0002-5263-5070": "ext/orcid/0000-0002-5263-5070",
		"https://openalex.org/A5023888391":      "ext/openalex/A5023888391",
		"https://github.com/torvalds":           "ext/github/torvalds",
		"https://github.com/numpy/numpy":        "ext/github/numpy",
		"https://openalex.org/W3035965352":      "", // a work is not a person
		"https://example.org/people/jane":       "",
		"":                                      "",
	}
	for in, want := range cases {
		if got := ExtKeyFromURL(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// The record lands before its edges: an edge pointing at a candidate file
// that was never written is an orphan the graph can never explain.
func TestAcceptWritesTheRecordBeforeTheEdges(t *testing.T) {
	s, _ := testStore(t)
	order := []string{}
	s.write = func(abs string, b []byte) error {
		switch {
		case strings.Contains(abs, "candidates/"):
			order = append(order, "record")
		case strings.Contains(abs, "edges.md"):
			order = append(order, "edges")
		}
		return nil
	}
	if _, err := s.AcceptDraft(coauthorDraft("Alice Ng", "0000-0001-0000-0001", "0000-0002-0000-0002"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(order) < 2 || order[0] != "record" {
		t.Fatalf("write order: %v", order)
	}
}
