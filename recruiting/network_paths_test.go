package recruiting

import (
	"reflect"
	"strings"
	"testing"
)

// A small owner network: two founders (owner consent), one advisor, one
// collaborator, one candidate. Ben → advisor → candidate is a 2-hop route on
// asserted edges; Ben → collaborator → candidate is inferred; RJ knows the
// candidate directly.
func pathFixture() ([]NetworkPerson, []Edge) {
	people := []NetworkPerson{
		{ID: "aion-net/ben-anderson", Name: "Benjamin Anderson", Type: "founder", Consent: "owner"},
		{ID: "aion-net/rj-tevonian", Name: "RJ Tevonian", Type: "founder", Consent: "owner"},
		{ID: "aion-net/dana-advisor", Name: "Dana Advisor", Type: "advisor", Consent: "manual"},
		{ID: "aion-net/kim-collab", Name: "Kim Collab", Type: "collaborator", Consent: "public_record"},
		{ID: "cand/avery-quill", Name: "Avery Quill", Type: "candidate", Consent: "manual"},
	}
	edges := []Edge{
		{From: "aion-net/ben-anderson", To: "aion-net/dana-advisor", Kind: "direct_known", Basis: "ben says", Confidence: "0.95", Source: "owner"},
		{From: "aion-net/dana-advisor", To: "cand/avery-quill", Kind: "advisor", Basis: "advised Avery's thesis", Confidence: "0.90", Source: "public_profile"},
		{From: "aion-net/ben-anderson", To: "aion-net/kim-collab", Kind: "coauthor", Basis: "paper 2021", Confidence: "0.80", Source: "openalex"},
		{From: "cand/avery-quill", To: "aion-net/kim-collab", Kind: "same_lab", Basis: "same department 2019", Confidence: "0.60", Inferred: true, Source: "public_profile"},
	}
	return people, edges
}

func TestDerivePathsRanksShortestThenConfidence(t *testing.T) {
	people, edges := pathFixture()
	got := DerivePaths(people, edges, "cand/avery-quill", nil, 5)
	// each derived path names the hop it rests on (the lowest-confidence
	// edge, as the edge's own direction) — Phase 3's "weakest link"
	want := []PathClaim{
		{Path: "aion-net/ben-anderson > aion-net/dana-advisor > cand/avery-quill", Kind: PathKindDerived, Confidence: "0.85", Inferred: false,
			Weakest: "aion-net/dana-advisor > cand/avery-quill · advisor · 0.90"},
		{Path: "aion-net/ben-anderson > aion-net/kim-collab > cand/avery-quill", Kind: PathKindDerived, Confidence: "0.48", Inferred: true,
			Weakest: "cand/avery-quill > aion-net/kim-collab · same_lab · 0.60 · inferred"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths:\n got %+v\nwant %+v", got, want)
	}
}

// A path's freshness is the OLDEST date any hop was observed, and a draft
// known only by external keys gets the same ranked route once the finder is
// asked about every key it answers to.
func TestPathFinderFreshnessAndExternalKeys(t *testing.T) {
	people, edges := pathFixture()
	edges[0].Observed = "2026-08-01"
	edges[1].Observed = "2025-01-15"
	edges = append(edges, Edge{From: "aion-net/dana-advisor", To: "ext/orcid/0000-0001-2345-6789", Kind: "coauthor",
		Basis: "paper 2024", Confidence: "0.70", Source: "openalex", Observed: "2026-06-01"})
	f := NewPathFinder(people, edges)
	got := f.Paths([]string{"cand/avery-quill"}, nil, 5)
	if len(got) != 2 || got[0].Observed != "2025-01-15" {
		t.Fatalf("freshness should be the oldest hop: %+v", got)
	}
	if got[1].Observed != "" {
		t.Errorf("a path with no dated hop has no freshness bound: %+v", got[1])
	}
	ext := f.Paths([]string{"ext/openalex/A1", "ext/orcid/0000-0001-2345-6789"}, nil, 5)
	if len(ext) == 0 || ext[0].Path != "aion-net/ben-anderson > aion-net/dana-advisor > ext/orcid/0000-0001-2345-6789" || ext[0].Observed != "2026-06-01" {
		t.Fatalf("route to an external key: %+v", ext)
	}
	if f.Paths(nil, nil, 5) == nil || len(f.Paths([]string{""}, nil, 5)) != 0 {
		t.Error("no target → empty, non-nil")
	}
	if (*PathFinder)(nil).Paths([]string{"cand/avery-quill"}, nil, 5) == nil {
		t.Error("a nil finder answers empty, not a panic")
	}
}

func TestDerivePathsDirectHopOutranksLonger(t *testing.T) {
	people, edges := pathFixture()
	edges = append(edges, Edge{
		From: "aion-net/rj-tevonian", To: "cand/avery-quill", Kind: "direct_known",
		Basis: "rj knows Avery", Confidence: "0.70", Source: "owner",
	})
	got := DerivePaths(people, edges, "cand/avery-quill", nil, 5)
	if len(got) != 3 {
		t.Fatalf("want 3 paths, got %d: %+v", len(got), got)
	}
	// one hop wins even though its confidence (0.70) is below the 2-hop 0.85
	if got[0].Path != "aion-net/rj-tevonian > cand/avery-quill" || got[0].Confidence != "0.70" {
		t.Fatalf("first path should be the direct hop, got %+v", got[0])
	}
	if got[1].Confidence != "0.85" || got[2].Confidence != "0.48" {
		t.Fatalf("2-hop paths should follow by confidence, got %+v", got[1:])
	}
	// topN caps the list after ranking
	if capped := DerivePaths(people, edges, "cand/avery-quill", nil, 1); len(capped) != 1 || capped[0].Path != got[0].Path {
		t.Fatalf("topN=1 should keep only the best path, got %+v", capped)
	}
}

func TestDerivePathsInferredHopMarksPath(t *testing.T) {
	people, edges := pathFixture()
	got := DerivePaths(people, edges, "cand/avery-quill", nil, 5)
	var sawInferred, sawAsserted bool
	for _, p := range got {
		if strings.Contains(p.Path, "kim-collab") {
			if !p.Inferred {
				t.Errorf("path through the inferred same_lab edge must be Inferred: %+v", p)
			}
			sawInferred = true
		} else if p.Inferred {
			t.Errorf("path on asserted edges only must not be Inferred: %+v", p)
		} else {
			sawAsserted = true
		}
	}
	if !sawInferred || !sawAsserted {
		t.Fatalf("fixture should yield one inferred and one asserted path, got %+v", got)
	}
}

func TestDerivePathsIsolatedCandidateIsEmpty(t *testing.T) {
	people, edges := pathFixture()
	people = append(people, NetworkPerson{ID: "cand/nobody-knows", Name: "Nobody Knows", Type: "candidate"})
	got := DerivePaths(people, edges, "cand/nobody-knows", nil, 5)
	if got == nil || len(got) != 0 {
		t.Fatalf("isolated candidate: want empty non-nil, got %#v", got)
	}
	// a component with no seed in it is unreachable too
	edges = append(edges, Edge{From: "aion-net/stranger", To: "cand/nobody-knows", Kind: "coworker", Basis: "x", Source: "web"})
	if got := DerivePaths(people, edges, "cand/nobody-knows", nil, 5); len(got) != 0 {
		t.Fatalf("no seed reaches the component: want empty, got %+v", got)
	}
	// no edges / no people / blank target: no panic, empty
	for _, tc := range []struct {
		people []NetworkPerson
		edges  []Edge
		target string
	}{
		{nil, nil, "cand/x"}, {people, nil, "cand/x"}, {nil, edges, ""}, {people, edges, "  "},
	} {
		if got := DerivePaths(tc.people, tc.edges, tc.target, nil, 5); len(got) != 0 {
			t.Fatalf("degenerate input should be empty, got %+v", got)
		}
	}
}

func TestDerivePathsSkipsInvalidEdgesAndUsesExplicitSeeds(t *testing.T) {
	people, edges := pathFixture()
	// an edge with no basis is not a claim: it must not create a route
	edges = append(edges, Edge{From: "aion-net/rj-tevonian", To: "cand/avery-quill", Kind: "direct_known", Source: "owner"})
	got := DerivePaths(people, edges, "cand/avery-quill", nil, 5)
	for _, p := range got {
		if strings.HasPrefix(p.Path, "aion-net/rj-tevonian") {
			t.Fatalf("basis-less edge produced a path: %+v", p)
		}
	}
	// explicit seeds replace the owner default entirely
	// (Ben is no longer a seed, so a longer route THROUGH him is fair game
	// and ranks after the direct hop.)
	got = DerivePaths(people, edges, "cand/avery-quill", []string{"aion-net/dana-advisor"}, 5)
	if len(got) != 2 || got[0].Path != "aion-net/dana-advisor > cand/avery-quill" || got[0].Confidence != "0.90" {
		t.Fatalf("explicit seed: want the advisor's direct hop first, got %+v", got)
	}
	if !strings.HasPrefix(got[1].Path, "aion-net/dana-advisor > aion-net/ben-anderson > ") || !got[1].Inferred {
		t.Fatalf("explicit seed: second route should pass through ben and be inferred, got %+v", got[1])
	}
	// an unstated confidence weighs UnstatedEdgeConfidence, never 1.0
	got = DerivePaths(people, []Edge{{From: "aion-net/ben-anderson", To: "cand/avery-quill", Kind: "owner_said", Basis: "b", Source: "owner"}}, "cand/avery-quill", nil, 5)
	if len(got) != 1 || got[0].Confidence != FormatConfidence(UnstatedEdgeConfidence) {
		t.Fatalf("unstated confidence: got %+v", got)
	}
}

func TestDerivePathsDeterministic(t *testing.T) {
	people, edges := pathFixture()
	edges = append(edges, Edge{From: "aion-net/rj-tevonian", To: "cand/avery-quill", Kind: "direct_known", Basis: "rj", Confidence: "0.70", Source: "owner"})
	first := DerivePaths(people, edges, "cand/avery-quill", nil, 5)
	for i := 0; i < 25; i++ {
		// reversed edge order and reversed people order must not change the answer
		rev := make([]Edge, len(edges))
		for j, e := range edges {
			rev[len(edges)-1-j] = e
		}
		if i%2 == 1 {
			rev = edges
		}
		rp := make([]NetworkPerson, len(people))
		for j, p := range people {
			rp[len(people)-1-j] = p
		}
		if got := DerivePaths(rp, rev, "cand/avery-quill", nil, 5); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d diverged:\n got %+v\nwant %+v", i, got, first)
		}
	}
}

func TestMergePathsPreservesHandAuthored(t *testing.T) {
	hand := []PathClaim{
		{Path: "aion-net/ben-anderson > aion-net/rj-tevonian > cand/avery-quill", Kind: "referral_path", Confidence: "0.70", Inferred: false},
		{Path: "aion-net/ben-anderson > aion-net/dana-advisor > cand/avery-quill", Kind: "referral_path", Confidence: "0.99"},
	}
	people, edges := pathFixture()
	derived := DerivePaths(people, edges, "cand/avery-quill", nil, 5)
	got := MergePaths(hand, derived)
	if len(got) != 3 {
		t.Fatalf("want 2 hand + 1 new derived, got %d: %+v", len(got), got)
	}
	if !reflect.DeepEqual(got[:2], hand) {
		t.Fatalf("hand-authored rows must pass through first and untouched: %+v", got[:2])
	}
	// the derived duplicate of a hand-written route is dropped, the owner's
	// confidence and kind win
	if got[1].Confidence != "0.99" || got[1].Kind != "referral_path" {
		t.Fatalf("owner row overwritten: %+v", got[1])
	}
	if got[2].Kind != PathKindDerived || !strings.Contains(got[2].Path, "kim-collab") {
		t.Fatalf("derived row should follow: %+v", got[2])
	}
}

// End to end through the store: the fixture candidate's hand-authored row
// stays, and the fixture edges yield a derived direct hop from RJ.
func TestStoreViewCarriesDerivedPaths(t *testing.T) {
	s, _ := testStore(t)
	for _, f := range [][2]string{
		{"network/people.md", "network-people.md"},
		{"network/edges.md", "network-edges.md"},
		{"candidates/avery-quill.md", "candidate-hand-edited.md"},
	} {
		if err := s.save(f[0], read(t, f[1])); err != nil {
			t.Fatal(err)
		}
	}
	check := func(label string, c Candidate) {
		t.Helper()
		if len(c.Paths) < 2 {
			t.Fatalf("%s: want hand + derived paths, got %+v", label, c.Paths)
		}
		if c.Paths[0].Kind != "referral_path" || c.Paths[0].Confidence != "0.70" {
			t.Fatalf("%s: hand-authored row not first/untouched: %+v", label, c.Paths[0])
		}
		var direct bool
		for _, p := range c.Paths[1:] {
			if p.Kind != PathKindDerived {
				t.Fatalf("%s: non-derived path after the hand rows: %+v", label, p)
			}
			if p.Path == "aion-net/rj-tevonian > cand/avery-quill" && p.Confidence == "0.95" && !p.Inferred {
				direct = true
			}
		}
		if !direct {
			t.Fatalf("%s: RJ's direct_known edge should derive a 1-hop path, got %+v", label, c.Paths)
		}
	}
	v := s.View()
	if len(v.Candidates) != 1 {
		t.Fatalf("want 1 candidate, got %d", len(v.Candidates))
	}
	check("View", v.Candidates[0])
	c, err := s.SetStage("cand/avery-quill", StageShortlist)
	if err != nil {
		t.Fatal(err)
	}
	check("candidateView", c)
	// derived paths are computed, never written back onto the record
	if raw := s.raw("candidates/avery-quill.md"); strings.Contains(raw, PathKindDerived) {
		t.Fatalf("derived path leaked into the record:\n%s", raw)
	}
}
