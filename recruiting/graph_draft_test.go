package recruiting

import (
	"strings"
	"testing"

	"manifest/recruiting/sources"
)

// THE THIRD OUTCOME (owner, 2026-09-11): "add them either to the social graph
// OR to the list of people I'm trying to recruit — either way, they're added
// to the social graph." A draft can go INTO THE GRAPH without becoming a
// candidate: one network row, its relationship claims, and no record.
func TestGraphDraftWritesAPersonAndTheirEdgesAndNoCandidate(t *testing.T) {
	dana := citedDraft("Dana Reyes", "A1")
	dana.Links = append(dana.Links, "https://orcid.org/0000-0002-1825-0097")
	// the adapter's claim: Dana coauthored with someone the vault knows only
	// by ORCID. `To` is empty — the person the claim is ABOUT did not exist
	// when the adapter ran, and the store fills it in.
	dana.Edges = []sources.EdgeClaim{{
		From: "ext/orcid/0000-0001-5109-3700", Type: sources.EdgeCoauthor,
		SourceID: "fake", Basis: "both authors on paper A1", Confidence: 0.55,
	}}
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{dana}}
	rs, store, vault := testRunStore(t, fake)
	before := snapshot(t, vault)

	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	got, p, err := rs.Graph(run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	// exactly two files move, and neither is a candidate record
	assertOnlyChanged(t, "after graph", before, snapshot(t, vault), "network/people.md", "network/edges.md")
	if len(store.CandidateSlugs()) != 0 {
		t.Fatal("into-the-graph produced a candidate record")
	}

	// the row: known, not someone the owner would ask
	if p.Name != "Dana Reyes" || p.Org != "Example Lab" || p.Source != "fake" || p.SourceRef != "fake:A1" {
		t.Fatalf("person row: %+v", p)
	}
	if p.Consent != "" {
		t.Fatalf("a graphed person must carry NO consent — OwnerSeeds would start routes from them: %+v", p)
	}
	if p.Email != "" {
		t.Fatalf("D15: an adapter set an email: %+v", p)
	}
	if seeds := OwnerSeeds(store.Connectors()); containsID(seeds, p.ID) {
		t.Fatalf("a graphed person became a path origin: %v", seeds)
	}

	// the edge landed with the person as its endpoint, and the ORCID the
	// draft carried was repointed onto the row
	var found bool
	for _, e := range store.LoadEdges().Edges() {
		if e.Kind == string(sources.EdgeCoauthor) && e.To == p.ID && e.From == "ext/orcid/0000-0001-5109-3700" {
			found = true
		}
		if e.From == "ext/orcid/0000-0002-1825-0097" || e.To == "ext/orcid/0000-0002-1825-0097" {
			t.Fatalf("an edge still names the graphed person by ORCID: %+v", e)
		}
	}
	if !found {
		t.Fatalf("the coauthor claim did not reach edges.md: %+v", store.LoadEdges().Edges())
	}

	// the run says what happened
	if got.Drafts[0].Status != DraftGraphed || got.Drafts[0].CandidateID != p.ID || got.Counts.Graphed != 1 {
		t.Fatalf("draft after graph: %+v counts=%+v", got.Drafts[0], got.Counts)
	}
	if got.Counts.Accepted != 0 || got.Counts.Rejected != 0 {
		t.Fatalf("graphing counted as something else: %+v", got.Counts)
	}
	// nothing left to decide → triaged, like any other decision
	if got.TriagedAt.IsZero() {
		t.Fatal("graphing the last new draft did not triage the run")
	}
	// and it is a decision: not twice, not then accepted, not then passed
	if _, _, err := rs.Graph(run.ID, "d1", testNow); err == nil {
		t.Error("graphing a graphed draft succeeded")
	}
	if _, _, err := rs.Accept(run.ID, "d1", testNow); err == nil {
		t.Error("accepting a graphed draft succeeded")
	}
	if _, err := rs.Reject(run.ID, "d1", "", testNow); err == nil {
		t.Error("passing a graphed draft succeeded")
	}
}

// Suppression is a SEARCH-TIME filter. A person already in the graph does not
// come back as `new` on the next sweep of the same paper: they arrive as a
// duplicate that says where they are.
func TestASweepDoesNotReAskAboutSomeoneInTheGraph(t *testing.T) {
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{citedDraft("Dana Reyes", "A1")}}
	rs, _, _ := testRunStore(t, fake)
	first := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	if _, _, err := rs.Graph(first.ID, "d1", testNow); err != nil {
		t.Fatal(err)
	}
	again := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	d := again.Drafts[0]
	if d.Status != DraftDuplicate || again.Counts.New != 0 {
		t.Fatalf("a graphed person came back as %q: %+v", d.Status, again.Counts)
	}
	if !strings.HasPrefix(d.Reason, "in your graph") || !strings.HasPrefix(d.CandidateID, "aion-net/") {
		t.Fatalf("the duplicate must say WHERE they are: %+v", d)
	}
	// a hand-typed connector with the same name is caught too, by name
	fake2 := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{citedDraft("Kai Ito", "K9")}}
	rs2, store2, _ := testRunStore(t, fake2)
	if err := store2.AddNetworkPerson(NetworkPerson{Name: "Kai Ito", Source: "owner", Consent: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rs2.Graph(mustRun(t, rs2, RunRequest{Source: "fake", Query: "x"}).ID, "d1", testNow); err == nil {
		t.Fatal("graphing someone already in network/people.md succeeded")
	}
}

// The identity round-trips: a graphed row keeps its source_ref through the
// record kernel, and a later draft naming that ORCID resolves onto the row
// rather than staying a stranger.
func TestGraphedPersonIsAnExtIndexTarget(t *testing.T) {
	dana := citedDraft("Dana Reyes", "A1")
	dana.Links = append(dana.Links, "https://orcid.org/0000-0002-1825-0097")
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{dana}}
	rs, store, _ := testRunStore(t, fake)
	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	_, p, err := rs.Graph(run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	// reload from disk: the field survived serialize → parse
	var back NetworkPerson
	for _, x := range store.LoadNetworkPeople().People() {
		if x.ID == p.ID {
			back = x
		}
	}
	if back.SourceRef != "fake:A1" {
		t.Fatalf("source_ref did not round-trip: %+v", back)
	}
	if got := store.extIndex()["ext/orcid/0000-0002-1825-0097"]; got != p.ID {
		t.Fatalf("the person's ORCID resolves to %q, want %q", got, p.ID)
	}
}

func containsID(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
