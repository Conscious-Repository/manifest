package recruiting

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

// ⚠ THE STRUCTURED FIELDS OBEY THE SAME CONTRACT AS THE FLAT ONES (Phase 1
// enrichment). A classified link or a topic list fills only where the draft
// is blank; topics union under the controlled normalizer (same spelling
// folded, a different term kept as its own chip); the namesake's chips and
// links stay out; and the vault is byte-identical before and after — a
// lookup enriches the QUEUE, never a record.
func TestLookupMergesStructuredFieldsWithoutOverwriting(t *testing.T) {
	grant := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{{
		SourceID: "fake", ExternalID: "g1", Name: "Dana Reyes",
		Title:    "Principal Investigator",
		Homepage: "https://danareyes.com", // the finder said this: kept
		Topics:   []string{"Low-field MRI"},
		Links:    []string{"https://reporter.nih.gov/project-details/1", "https://danareyes.com"},
		Evidence: []sources.Evidence{{
			SourceID: "fake", URLOrFile: "https://reporter.nih.gov/project-details/1",
			RetrievedAt: testNow, Snippet: "pi: Dana Reyes", Kind: sources.EvidenceGrant, Trust: sources.TrustHigh,
		}},
	}}}
	rs, _, vault := testRunStore(t, grant)
	rs.Register(&fakeAdapter{id: "openalex", drafts: []sources.CandidateDraft{
		{
			SourceID: "openalex", Name: "dana REYES",
			Homepage: "https://dana-reyes.net", // must NOT displace the finder's
			Orcid:    "https://orcid.org/0000-0002-1825-0097",
			Topics:   []string{"low field mri", "Diffusion MRI Reconstruction", "Compressed Sensing"},
			Links:    []string{"https://openalex.org/A1", "https://orcid.org/0000-0002-1825-0097", "https://dana-reyes.net"},
			Evidence: []sources.Evidence{{SourceID: "openalex", URLOrFile: "https://openalex.org/A1", RetrievedAt: testNow, Snippet: "topics: low field mri; Diffusion MRI Reconstruction; Compressed Sensing", Kind: sources.EvidencePublication, Trust: sources.TrustMedium}},
		},
		{
			SourceID: "openalex", Name: "Dana Reyes-Okafor", // a near miss: dropped, never ranked
			Topics:   []string{"Astrophysics"},
			LinkedIn: "https://www.linkedin.com/in/stranger",
			Links:    []string{"https://openalex.org/A999", "https://www.linkedin.com/in/stranger"},
			Evidence: []sources.Evidence{{SourceID: "openalex", URLOrFile: "https://openalex.org/A999", RetrievedAt: testNow, Kind: sources.EvidencePublication, Trust: sources.TrustMedium}},
		},
	}})
	// a source that emits links WITHOUT naming them: the lookup sorts them
	rs.Register(&fakeAdapter{id: "github", drafts: []sources.CandidateDraft{{
		SourceID: "github", Name: "Dana Reyes",
		Links:    []string{"https://github.com/dreyes", "https://linkedin.com/in/dana-reyes", "https://web.mit.edu/~dreyes/"},
		Evidence: []sources.Evidence{{SourceID: "github", URLOrFile: "https://github.com/dreyes", RetrievedAt: testNow, Kind: sources.EvidencePage, Trust: sources.TrustMedium}},
	}}})

	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "low-field MRI"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	vaultBefore := snapshot(t, vault)
	run, res, err := rs.Lookup(context.Background(), run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	assertIdentical(t, "vault after lookup", vaultBefore, snapshot(t, vault))

	d := run.Drafts[0].Draft
	if d.Homepage != "https://danareyes.com" {
		t.Fatalf("a lookup overwrote the finder's homepage: %q", d.Homepage)
	}
	if d.Orcid != "https://orcid.org/0000-0002-1825-0097" || d.Github != "https://github.com/dreyes" ||
		d.LinkedIn != "https://linkedin.com/in/dana-reyes" || d.Site != "https://web.mit.edu/~dreyes/" {
		t.Fatalf("structured fill: %+v", d)
	}
	if strings.Join(d.Topics, "|") != "Low-field MRI|Diffusion MRI Reconstruction|Compressed Sensing" {
		t.Fatalf("topics (same spelling should fold, a different term should not): %v", d.Topics)
	}
	if strings.Join(res.Filled, ",") != "github,linkedin,orcid,site,topics" {
		t.Fatalf("filled: %v", res.Filled)
	}
	for _, l := range d.Links {
		if strings.Contains(l, "stranger") || strings.Contains(l, "A999") {
			t.Fatalf("a near-miss name's link was merged: %v", d.Links)
		}
	}
	if strings.Join(res.Matched, ",") != "openalex,github" {
		t.Fatalf("matched: %+v", res)
	}

	// idempotent, and the raw union still holds everything for back-compat
	run2, res2, err := rs.Lookup(context.Background(), run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Filled) != 0 || len(run2.Drafts[0].Draft.Topics) != 3 {
		t.Fatalf("a second lookup re-filled: %+v %v", res2, run2.Drafts[0].Draft.Topics)
	}
	if len(run2.Drafts[0].Draft.Links) != 8 {
		t.Fatalf("Links is the raw union: %v", run2.Drafts[0].Draft.Links)
	}
	assertIdentical(t, "vault after second lookup", vaultBefore, snapshot(t, vault))
}

// A run's queue is classified as it is written, whichever adapter produced
// it — so a source that only knows "here are some URLs" still yields a
// labelled draft.
func TestExecuteClassifiesLinksForEveryAdapter(t *testing.T) {
	a := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{{
		SourceID: "fake", Name: "Sam Okafor",
		Links:    []string{"https://openalex.org/A2", "https://github.com/sokafor", "https://sokafor.dev/"},
		Evidence: []sources.Evidence{{SourceID: "fake", URLOrFile: "https://openalex.org/A2", RetrievedAt: testNow, Kind: sources.EvidencePublication, Trust: sources.TrustMedium}},
	}}}
	rs, _, _ := testRunStore(t, a)
	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "rf coils"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	d := run.Drafts[0].Draft
	if d.Github != "https://github.com/sokafor" || d.Homepage != "https://sokafor.dev/" || d.Site != "" {
		t.Fatalf("classified: %+v", d)
	}
	if len(d.Topics) != 0 {
		t.Fatalf("topics invented: %v", d.Topics)
	}
}

// ⚠ A QUEUE WRITTEN BEFORE THE STRUCTURED FIELDS EXISTED STILL READS. The
// run cache is a plain JSON round-trip of the draft struct: a legacy
// drafts.json has none of the new keys and must load, look up, and re-save
// without a rewrite of what it did say — and a draft with nothing new to
// say serializes without the new keys, so the on-disk shape only grows when
// there is something to hold.
func TestLookupLegacyQueueRoundTrips(t *testing.T) {
	rs, _, _ := testRunStore(t, nil)
	rs.Register(&fakeAdapter{id: "orcid", drafts: []sources.CandidateDraft{{
		SourceID: "orcid", Name: "Ada Lovelace",
		Links:    []string{"https://orcid.org/0000-0000-0000-0001"},
		Evidence: []sources.Evidence{{SourceID: "orcid", URLOrFile: "https://orcid.org/0000-0000-0000-0001", RetrievedAt: testNow, Kind: sources.EvidencePage, Trust: sources.TrustHigh}},
	}}})

	// hand-write a run exactly as the pre-Phase-1 binary did
	id := "20250901-120000-fake-abc123"
	dir := filepath.Join(rs.Root(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyState := `{"id":"` + id + `","source":"fake","scope":{"role":"","query":"engine","max":25,"dryRun":false},"startedAt":"2025-09-01T12:00:00Z","counts":{"fetched":1,"new":1,"duplicate":0,"accepted":0,"rejected":0},"pinned":false}`
	legacyDrafts := `[{"id":"d1","status":"new","draft":{"sourceId":"fake","name":"Ada Lovelace","title":"Analyst","links":["https://ada.example"],"note":"topics: engines","evidence":[{"sourceId":"fake","urlOrFile":"https://ada.example","retrievedAt":"2025-09-01T12:00:00Z","snippet":"x","kind":"page","trust":"medium"}]}}]`
	for name, body := range map[string]string{"run.json": legacyState, "drafts.json": legacyDrafts} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := rs.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	d := got.Drafts[0].Draft
	if d.Name != "Ada Lovelace" || d.Title != "Analyst" || d.Note != "topics: engines" || len(d.Links) != 1 {
		t.Fatalf("legacy draft lost a field: %+v", d)
	}
	if d.Homepage != "" || d.Topics != nil {
		t.Fatalf("a legacy draft was read with fields it never had: %+v", d)
	}

	// a plain re-save of the untouched legacy draft emits none of the new keys
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"homepage"`, `"linkedin"`, `"github"`, `"orcid"`, `"site"`, `"topics"`} {
		if strings.Contains(string(b), key) {
			t.Fatalf("an empty structured field was serialized: %s", b)
		}
	}

	// and the lookup enriches it in place: the legacy link is sorted (a bare
	// domain → homepage), the registry's page fills orcid, nothing is lost
	run, res, err := rs.Lookup(context.Background(), id, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	d = run.Drafts[0].Draft
	if d.Orcid != "https://orcid.org/0000-0000-0000-0001" || d.Homepage != "https://ada.example" ||
		d.Title != "Analyst" || d.Note != "topics: engines" {
		t.Fatalf("legacy lookup: %+v (filled %v)", d, res.Filled)
	}
	if strings.Join(res.Filled, ",") != "homepage,orcid" {
		t.Fatalf("filled: %v", res.Filled)
	}
	reloaded, err := rs.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Drafts[0].Draft.Orcid != d.Orcid || reloaded.Drafts[0].Draft.Homepage != d.Homepage {
		t.Fatalf("the enrichment did not persist: %+v", reloaded.Drafts[0].Draft)
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
