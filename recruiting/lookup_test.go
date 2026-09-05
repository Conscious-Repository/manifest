package recruiting

import (
	"context"
	"errors"
	"strings"
	"testing"

	"manifest/recruiting/sources"
)

// ⚠ A LOOKUP MERGES ONLY EXACT NAMES. The whole point of the pass is to make a
// one-fact draft worth deciding on; the whole danger is writing a stranger's
// citations onto it. Matching is normalized-name equality — a near miss is
// dropped, never ranked — and a source that is down costs its own answer and
// nothing else.
func TestLookupMergesOnlyTheSameNameAndNeverOverwrites(t *testing.T) {
	grant := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{{
		SourceID: "fake", ExternalID: "g1", Name: "Juan Manuel Rivas-Davila",
		Title: "Principal Investigator", // org deliberately empty: the pass fills it
		Links: []string{"https://reporter.nih.gov/project-details/11237107"},
		Evidence: []sources.Evidence{{
			SourceID: "fake", URLOrFile: "https://reporter.nih.gov/project-details/11237107",
			RetrievedAt: testNow, Snippet: "pi: Juan Manuel Rivas-Davila",
			Kind: sources.EvidenceGrant, Trust: sources.TrustHigh,
		}},
	}}}
	rs, _, _ := testRunStore(t, grant)

	// the index that knows this person, plus a namesake it must NOT merge
	rs.Register(&fakeAdapter{id: "openalex", drafts: []sources.CandidateDraft{
		{
			SourceID: "openalex", Name: "juan manuel  RIVAS-DAVILA!", // same person, spelled loudly
			Org: "Stanford University", Location: "Stanford, CA",
			Links: []string{
				"https://openalex.org/A123",
				"https://reporter.nih.gov/project-details/11237107", // already held — must not double
			},
			Evidence: []sources.Evidence{{
				SourceID: "openalex", URLOrFile: "https://openalex.org/A123",
				RetrievedAt: testNow, Snippet: "power amplifiers for MRI", Kind: sources.EvidencePublication, Trust: sources.TrustHigh,
			}},
		},
		{
			SourceID: "openalex", Name: "Juan Rivas", // a DIFFERENT person
			Org:      "Somewhere Else",
			Links:    []string{"https://openalex.org/A999"},
			Evidence: []sources.Evidence{{SourceID: "openalex", URLOrFile: "https://openalex.org/A999", RetrievedAt: testNow, Kind: sources.EvidencePublication, Trust: sources.TrustHigh}},
		},
	}})
	// an index that is down: reported, never fatal
	rs.Register(&fakeAdapter{id: "orcid", err: errors.New("503")})

	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "low-field MRI", DryRun: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	run, res, err := rs.Lookup(context.Background(), run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(res.Matched, ",") != "openalex" {
		t.Fatalf("matched: %+v", res)
	}
	if strings.Join(res.Failed, ",") != "orcid" {
		t.Fatalf("a source that errored was not reported: %+v", res)
	}
	d := run.Drafts[0].Draft
	if len(d.Links) != 2 {
		t.Fatalf("links merged wrong (a held link doubled, or the namesake got in): %+v", d.Links)
	}
	for _, l := range d.Links {
		if strings.Contains(l, "A999") {
			t.Fatalf("a different person's link was merged: %+v", d.Links)
		}
	}
	if len(d.Evidence) != 2 {
		t.Fatalf("citations: %+v", d.Evidence)
	}
	// filled only where the draft was blank; the source that found them keeps
	// the last word on what it said
	if d.Org != "Stanford University" || strings.Join(res.Filled, ",") != "location,org" {
		t.Fatalf("fill: org=%q filled=%+v", d.Org, res.Filled)
	}
	if d.Title != "Principal Investigator" {
		t.Fatalf("a lookup overwrote a field the draft already had: %q", d.Title)
	}
	if run.Drafts[0].LookedUpAt.IsZero() {
		t.Fatal("the pass left no stamp")
	}

	// idempotent: asking twice adds nothing twice
	run2, res2, err := rs.Lookup(context.Background(), run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Links != 0 || res2.Cites != 0 {
		t.Fatalf("a second lookup duplicated: %+v", res2)
	}
	if len(run2.Drafts[0].Draft.Links) != 2 || len(run2.Drafts[0].Draft.Evidence) != 2 {
		t.Fatalf("a second lookup grew the draft: %+v", run2.Drafts[0].Draft)
	}

	// and it survives a reload — the enrichment was persisted, not in-memory
	reloaded, err := rs.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Drafts[0].Draft.Links) != 2 || reloaded.Drafts[0].LookedUpAt.IsZero() {
		t.Fatalf("the lookup did not persist: %+v", reloaded.Drafts[0])
	}
}

// The source that produced the draft is not asked about it again.
func TestLookupSkipsTheSourceThatFoundThem(t *testing.T) {
	own := &fakeAdapter{id: "openalex", drafts: []sources.CandidateDraft{{
		SourceID: "openalex", Name: "Ada Lovelace",
		Links:    []string{"https://openalex.org/A1"},
		Evidence: []sources.Evidence{{SourceID: "openalex", URLOrFile: "https://openalex.org/A1", RetrievedAt: testNow, Kind: sources.EvidencePublication, Trust: sources.TrustHigh}},
	}}}
	rs, _, _ := testRunStore(t, own)
	run, err := rs.Execute(context.Background(), RunRequest{Source: "openalex", Query: "analytical engine", DryRun: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	before := len(own.seen)
	_, res, err := rs.Lookup(context.Background(), run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(own.seen) != before {
		t.Fatal("the lookup asked the source that already answered")
	}
	for _, id := range res.Asked {
		if id == "openalex" {
			t.Fatalf("asked: %+v", res.Asked)
		}
	}
}
