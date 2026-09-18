package recruiting

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/recruiting/sources"
)

// TIE STRENGTH (social graph plan D-I). A tie is worth what its works are
// worth, and a second shared work makes it stronger instead of being refused.

func pin(t *testing.T, year int) {
	t.Helper()
	was := strengthYear
	strengthYear = func() int { return year }
	t.Cleanup(func() { strengthYear = was })
}

func TestWorksRoundTripOnTheRow(t *testing.T) {
	doc := ParseEdges("# edges\n")
	e := Edge{From: "a", To: "b", Kind: "coauthor", Basis: "paper X", Source: "openalex", Confidence: "0.55",
		Works: []sources.WorkRef{{Ref: "10.1/x", Year: 2024, Authors: 2}, {Ref: "W99", Year: 2019, Authors: 12}}}
	if _, err := doc.Add(e); err != nil {
		t.Fatal(err)
	}
	raw := SerializeEdges(doc)
	if !strings.Contains(raw, "[work:: 10.1/x@2024/2]") || !strings.Contains(raw, "[work:: W99@2019/12]") {
		t.Fatalf("works not on the row:\n%s", raw)
	}
	back := ParseEdges(raw).Edges()
	if len(back) != 1 || len(back[0].Works) != 2 || back[0].Works[1].Authors != 12 || back[0].Works[0].Year != 2024 {
		t.Fatalf("works did not round-trip: %+v", back)
	}
	// the fixpoint: parse → serialize is byte-identical
	if again := SerializeEdges(ParseEdges(raw)); again != raw {
		t.Fatalf("not a fixpoint:\n%s\n---\n%s", raw, again)
	}
}

func TestASecondSharedWorkAccumulatesInsteadOfBeingRefused(t *testing.T) {
	doc := ParseEdges("# edges\n")
	first := Edge{From: "a", To: "b", Kind: "coauthor", Basis: "paper X", Source: "openalex", Confidence: "0.55",
		Works: []sources.WorkRef{{Ref: "10.1/x", Year: 2024, Authors: 2}}}
	if _, merged, err := doc.Merge(first); err != nil || merged {
		t.Fatalf("first claim: merged=%v err=%v", merged, err)
	}
	second := Edge{From: "b", To: "a", Kind: "coauthor", Basis: "paper Y", Source: "openalex", Confidence: "0.60",
		Works: []sources.WorkRef{{Ref: "10.1/y", Year: 2022, Authors: 3}, {Ref: "10.1/x", Year: 2024, Authors: 2}}}
	got, merged, err := doc.Merge(second)
	if err != nil || !merged {
		t.Fatalf("second claim about the same pair must merge: merged=%v err=%v", merged, err)
	}
	if len(doc.Edges()) != 1 {
		t.Fatalf("two rows for one tie: %+v", doc.Edges())
	}
	if len(got.Works) != 2 || got.Confidence != "0.60" || got.Basis != "paper X" {
		t.Fatalf("merge: works=%d conf=%s basis=%q", len(got.Works), got.Confidence, got.Basis)
	}
}

// FRACTIONAL COUNTING: a two-author paper is a tie; a slot on a 40-author
// consortium paper is nearly nothing, and the walk treats it that way.
func TestATwoAuthorPaperOutweighsAConsortiumSlot(t *testing.T) {
	pin(t, 2026)
	pair := Edge{Kind: "coauthor", Confidence: "0.55", Works: []sources.WorkRef{{Ref: "a", Year: 2026, Authors: 2}}}
	crowd := Edge{Kind: "coauthor", Confidence: "0.55", Works: []sources.WorkRef{{Ref: "b", Year: 2026, Authors: 40}}}
	if pair.Strength() != 1 || crowd.Strength() >= 0.03 {
		t.Fatalf("strength: pair=%.3f crowd=%.3f", pair.Strength(), crowd.Strength())
	}
	if pair.PathWeight() != 0.55 || crowd.PathWeight() > 0.02 {
		t.Fatalf("path weight: pair=%.3f crowd=%.3f", pair.PathWeight(), crowd.PathWeight())
	}
	// the floor: the consortium slot is not a route; the pair is
	kept := PathEdges([]Edge{withEnds(pair, "x", "y"), withEnds(crowd, "x", "z")})
	if len(kept) != 1 || kept[0].To != "y" {
		t.Fatalf("PathEdges kept %+v", kept)
	}
	// decay: the same pair paper a half-life ago is worth half
	old := Edge{Kind: "coauthor", Confidence: "0.55", Works: []sources.WorkRef{{Ref: "a", Year: 2018, Authors: 2}}}
	if s := old.Strength(); s < 0.49 || s > 0.51 {
		t.Fatalf("decay after one half-life: %.3f", s)
	}
	// and a claim with no works on file is taken at face value
	if plain := (Edge{Kind: "same_meeting", Confidence: "0.70"}); plain.PathWeight() != 0.70 {
		t.Fatalf("no works → stated confidence: %.2f", plain.PathWeight())
	}
}

func withEnds(e Edge, from, to string) Edge {
	e.From, e.To, e.Basis, e.Source = from, to, "b", "s"
	return e
}

// member_of is never a hop: a lab is not a person who can introduce you.
func TestMembershipIsNotAnIntroHop(t *testing.T) {
	kept := PathEdges([]Edge{{From: "p", To: "seed/lab-x", Kind: "member_of", Basis: "listed", Source: "web", Confidence: "0.90"}})
	if len(kept) != 0 {
		t.Fatalf("member_of walked: %+v", kept)
	}
}

// THE RUN CACHE DRAWN (D-D, D-F): a sweep's people are bridge nodes with the
// adapter's claims and a member_of edge to the source that named them; the
// vault is untouched by any of it; delete the run and it is all gone.
func TestASweepIsDrawnFromItsCacheAndWritesNoRecord(t *testing.T) {
	dana := citedDraft("Dana Reyes", "A1")
	dana.Links = append(dana.Links, "https://orcid.org/0000-0002-1825-0097")
	dana.Edges = []sources.EdgeClaim{{From: "ext/orcid/0000-0001-5109-3700", Type: sources.EdgeCoauthor,
		SourceID: "fake", Basis: "both authors on A1", Confidence: 0.55,
		Works: []sources.WorkRef{{Ref: "10.1/a1", Year: 2025, Authors: 2}}}}
	kai := citedDraft("Kai Ito", "K2")
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{dana, kai}}
	rs, store, vault := testRunStore(t, fake)
	before := snapshot(t, vault)

	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	// nothing in the vault moved — a sweep is a cache, never a record
	assertOnlyChanged(t, "after a sweep", before, snapshot(t, vault))
	if len(store.LoadNetworkPeople().People()) != 2 { // the two seeded connectors only
		t.Fatalf("a sweep wrote a network row: %+v", store.LoadNetworkPeople().People())
	}

	proj := rs.Projection()
	if len(proj.People) != 2 {
		t.Fatalf("bridge people: %+v", proj.People)
	}
	var danaB BridgePerson
	for _, p := range proj.People {
		if p.Name == "Dana Reyes" {
			danaB = p
		}
	}
	if danaB.ID != "ext/orcid/0000-0002-1825-0097" || danaB.Seed != "source/"+run.ID || danaB.Subject != "coil" || danaB.Swept == "" {
		t.Fatalf("dana as a bridge person: %+v", danaB)
	}
	if proj.Sources[danaB.Seed] != "coil" {
		t.Fatalf("the source node is labelled by the subject: %+v", proj.Sources)
	}
	var member, coauthor bool
	for _, e := range proj.Edges {
		if e.Kind == "member_of" && e.From == danaB.ID && e.To == danaB.Seed {
			member = true
		}
		if e.Kind == "coauthor" && e.To == danaB.ID && e.From == "ext/orcid/0000-0001-5109-3700" && len(e.Works) == 1 {
			coauthor = true
		}
	}
	if !member || !coauthor {
		t.Fatalf("edges drawn from the cache: member=%v coauthor=%v %+v", member, coauthor, proj.Edges)
	}
	// the record store sees them, marked derived, and never writes them
	seen := 0
	for _, e := range store.NetworkEdges() {
		if e.Kind == "member_of" || (e.Kind == "coauthor" && e.To == danaB.ID) {
			seen++
			if !e.Derived {
				t.Fatalf("a cache edge must say it is derived: %+v", e)
			}
		}
	}
	if seen < 3 {
		t.Fatalf("NetworkEdges did not merge the cache: %d", seen)
	}
	if raw, _ := os.ReadFile(filepath.Join(vault, "system/aion/recruiting/network/edges.md")); strings.Contains(string(raw), "member_of") {
		t.Fatal("a cache edge reached the vault")
	}

	// a pass hides the person; a delete removes the source and everyone it named
	if _, err := rs.Reject(run.ID, "d2", "", testNow); err != nil {
		t.Fatal(err)
	}
	if p := rs.Projection().People; len(p) != 1 || p[0].Name != "Dana Reyes" {
		t.Fatalf("a passed draft is no longer a bridge person: %+v", p)
	}
	if _, err := rs.Delete(run.ID); err != nil {
		t.Fatal(err)
	}
	if p := rs.Projection(); len(p.People) != 0 || len(p.Edges) != 0 {
		t.Fatalf("delete the source and the bridge people go with it: %+v", p)
	}
}

// A person named by two sweeps of two sources is ONE node hanging off both.
func TestOnePersonTwoSourcesOneNode(t *testing.T) {
	dana := citedDraft("Dana Reyes", "A1")
	dana.Links = append(dana.Links, "https://orcid.org/0000-0002-1825-0097")
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{dana}}
	rs, _, _ := testRunStore(t, fake)
	a := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	b := mustRun(t, rs, RunRequest{Source: "fake", Query: "magnet"})
	proj := rs.Projection()
	if len(proj.People) != 1 {
		t.Fatalf("one person, two sweeps → one node: %+v", proj.People)
	}
	members := 0
	for _, e := range proj.Edges {
		if e.Kind == "member_of" && (e.To == "source/"+a.ID || e.To == "source/"+b.ID) {
			members++
		}
	}
	if members != 2 {
		t.Fatalf("hangs off both sources: %+v", proj.Edges)
	}
}

// The seed the PLACES button names becomes the run's source node; a run that
// names none but sweeps a seed's URL still lands on that seed.
func TestARunKnowsThePlaceItIsFrom(t *testing.T) {
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{citedDraft("Dana Reyes", "A1")}}
	rs, store, _ := testRunStore(t, fake)
	seed, err := store.AddSeed(Seed{Class: SeedLab, Name: "Coil Lab", URL: "https://coil.example.edu/people"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	named := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil", Seed: seed.ID})
	if named.Seed != seed.ID || named.Subject != "Coil Lab" {
		t.Fatalf("named seed: %+v", named.RunState)
	}
	matched := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil", Fields: map[string]string{"seed_url": "https://coil.example.edu/people/"}})
	if matched.Seed != seed.ID {
		t.Fatalf("a run sweeping a seed's URL lands on the seed: %+v", matched.RunState)
	}
	loose := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	if loose.Seed != "source/"+loose.ID || loose.Subject != "coil" {
		t.Fatalf("a run from nowhere is its own source: %+v", loose.RunState)
	}
	_ = time.Now
}

// Accepting a draft whose pair is already on file from an earlier accept
// ACCUMULATES the works onto the one row.
func TestAcceptAccumulatesASecondSharedWork(t *testing.T) {
	first := citedDraft("Dana Reyes", "A1")
	first.Links = append(first.Links, "https://orcid.org/0000-0002-1825-0097")
	first.Edges = []sources.EdgeClaim{{From: "ext/orcid/0000-0001-5109-3700", Type: sources.EdgeCoauthor, SourceID: "fake",
		Basis: "both on paper 1", Confidence: 0.55, Works: []sources.WorkRef{{Ref: "10.1/p1", Year: 2024, Authors: 2}}}}
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{first}}
	rs, store, _ := testRunStore(t, fake)
	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	if _, _, err := rs.Accept(run.ID, "d1", testNow); err != nil {
		t.Fatal(err)
	}
	// the same coauthor, a second paper, a second sweep: the person is now a
	// record, so this arrives as a duplicate — accept the claim through the
	// store directly, as a later phase's "add these works" would
	cand := store.LoadCandidate("dana-reyes")
	second := sources.CandidateDraft{SourceID: "fake", ExternalID: "A1", Name: "Dana Reyes", Links: first.Links,
		Edges: []sources.EdgeClaim{{From: "ext/orcid/0000-0001-5109-3700", Type: sources.EdgeCoauthor, SourceID: "fake",
			Basis: "both on paper 2", Confidence: 0.55, Works: []sources.WorkRef{{Ref: "10.1/p2", Year: 2025, Authors: 3}}}}}
	if err := store.saveDraftEdges(second, cand.Get("id")); err != nil {
		t.Fatal(err)
	}
	edges := store.LoadEdges().Edges()
	if len(edges) != 1 || len(edges[0].Works) != 2 {
		t.Fatalf("the second work must land on the one row: %+v", edges)
	}
}

// A run from before source nodes existed still reads as its own source —
// the same id the graph projection gives it — so nothing swept is invisible
// on PLACES.
func TestOldRunsStandAsTheirOwnSource(t *testing.T) {
	a := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{{Name: "Dana Reyes", Links: []string{"https://orcid.org/0000-0001-0000-0001"},
		Evidence: []sources.Evidence{{SourceID: "fake", URLOrFile: "https://x/1", Snippet: "s", Kind: sources.EvidencePublication, Trust: sources.TrustHigh}}}}}
	rs, _, _ := testRunStore(t, a)
	run, err := rs.Execute(context.Background(), RunRequest{Source: a.ID(), Query: "field cycling"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// strip the seed the way an old file has none
	st, err := rs.load(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	st.Seed, st.Subject = "", ""
	if err := rs.writeRun(st, nil); err != nil {
		t.Fatal(err)
	}
	var got Run
	for _, r := range rs.Runs(time.Now()) {
		if r.ID == run.ID {
			got = r
		}
	}
	if got.Seed != "source/"+run.ID || got.Subject != "field cycling" {
		t.Fatalf("an old run is its own source node: %q %q", got.Seed, got.Subject)
	}
	if _, ok := rs.Projection().Sources[got.Seed]; !ok {
		t.Fatalf("and the graph names the same node: %v", rs.Projection().Sources)
	}
}
