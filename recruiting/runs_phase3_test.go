package recruiting

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/recruiting/sources"
)

// Phase 3 on the run cache: a queued draft carries the intro paths the
// network derives for its external keys (and for its record once accepted),
// those paths never reach drafts.json, and a pass can be undone — the draft
// returns to `new` as it was, and the run's expiry clock stops.

// seedOwnerNetwork writes an owner seed and one edge from the owner to an
// ORCID nobody on the board has yet.
func seedOwnerNetwork(t *testing.T, store *Store, extKey string) {
	t.Helper()
	// Ensure seeds the founders; a fresh vault may or may not have them
	owner := false
	for _, p := range store.LoadNetworkPeople().People() {
		if p.ID == "aion-net/ben-anderson" && p.Consent == "owner" {
			owner = true
		}
	}
	if !owner {
		if err := store.AddNetworkPerson(NetworkPerson{ID: "aion-net/ben-anderson", Name: "Benjamin Anderson", Type: "founder", Consent: "owner"}); err != nil {
			t.Fatal(err)
		}
	}
	edges := store.LoadEdges()
	if _, err := edges.Add(Edge{From: "aion-net/ben-anderson", To: extKey, Kind: "coauthor", Basis: "paper 2024",
		Confidence: "0.80", Source: "openalex", Observed: "2026-06-01"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveEdges(edges); err != nil {
		t.Fatal(err)
	}
}

func TestDraftPathsDeriveFromExternalKeysThenTheRecord(t *testing.T) {
	const orcid = "ext/orcid/0000-0001-2345-6789"
	d := citedDraft("Avery Quill", "A1")
	d.Links = append(d.Links, "https://orcid.org/0000-0001-2345-6789")
	stranger := citedDraft("Kim Collab", "A2")
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{d, stranger}}
	rs, store, _ := testRunStore(t, fake)
	seedOwnerNetwork(t, store, orcid)

	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "mri"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	// the queued draft is reachable by its ORCID: one hop from the owner
	got := run.Drafts[0].Paths
	if len(got) != 1 || got[0].Path != "aion-net/ben-anderson > "+orcid || got[0].Kind != PathKindDerived || got[0].Observed != "2026-06-01" {
		t.Fatalf("draft paths: %+v", got)
	}
	if !strings.Contains(got[0].Weakest, "coauthor · 0.80") {
		t.Errorf("weakest hop: %q", got[0].Weakest)
	}
	// a draft nobody is linked to has none — omitted, not an empty list
	if run.Drafts[1].Paths != nil {
		t.Errorf("stranger got paths: %+v", run.Drafts[1].Paths)
	}
	// the listing and a single load agree
	if listed := rs.Runs(testNow); len(listed[0].Drafts[0].Paths) != 1 || listed[0].Drafts[0].Paths[0].Path != got[0].Path {
		t.Errorf("listing paths differ: %+v", listed[0].Drafts[0].Paths)
	}
	// nothing derived is on disk
	raw, err := os.ReadFile(filepath.Join(rs.Root(), run.ID, "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	for i, m := range onDisk {
		if _, has := m["paths"]; has {
			t.Errorf("draft %d persisted its derived paths", i)
		}
	}
	// accept repoints the ORCID edge onto the record; the path now ends there
	after, c, err := rs.Accept(run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if p := after.Drafts[0].Paths; len(p) != 1 || p[0].Path != "aion-net/ben-anderson > "+c.ID {
		t.Fatalf("accepted draft paths: %+v", p)
	}
	if len(c.Paths) != 1 || c.Paths[0].Path != after.Drafts[0].Paths[0].Path {
		t.Errorf("the card and the record disagree: %+v vs %+v", c.Paths, after.Drafts[0].Paths)
	}
}

func TestUnrejectRestoresADraftAndTheClock(t *testing.T) {
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{citedDraft("Avery Quill", "A1")}}
	rs, _, vault := testRunStore(t, fake)
	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "mri"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	// a pass on the only draft triages the run and starts the expiry clock
	passed, err := rs.Reject(run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if passed.TriagedAt.IsZero() || passed.ExpiresAt.IsZero() || passed.Counts.Rejected != 1 {
		t.Fatalf("after pass: %+v", passed.RunState)
	}
	before := snapshot(t, vault)

	// undo: back to new, undecided, clock cleared — and the vault untouched
	back, err := rs.Unreject(run.ID, "d1", testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	d := back.Drafts[0]
	if d.Status != DraftNew || !d.DecidedAt.IsZero() || d.Reason != "" || d.CandidateID != "" {
		t.Fatalf("draft after undo: %+v", d)
	}
	if back.Counts.Rejected != 0 || back.Counts.New != 1 || !back.TriagedAt.IsZero() || !back.ExpiresAt.IsZero() {
		t.Fatalf("run after undo: %+v", back.RunState)
	}
	if after := snapshot(t, vault); len(after) != len(before) {
		t.Fatal("an undo touched the vault")
	}
	// it survives a reload, and the run is no longer sweepable
	if got, _ := rs.Get(run.ID); got.Drafts[0].Status != DraftNew || !got.ExpiresAt.IsZero() {
		t.Fatalf("reloaded: %+v", got)
	}
	if gone := rs.Sweep(testNow.Add(2 * RunTTL)); len(gone) != 0 {
		t.Errorf("an un-passed run was swept: %v", gone)
	}
	// only a pass can be undone
	if _, err := rs.Unreject(run.ID, "d1", testNow); err == nil || !strings.Contains(err.Error(), "not passed") {
		t.Errorf("undoing a new draft: %v", err)
	}
	if _, err := rs.Unreject(run.ID, "d9", testNow); err == nil {
		t.Error("undoing a draft that does not exist")
	}
	// and the draft is decidable again — this time, accept
	if _, c, err := rs.Accept(run.ID, "d1", testNow); err != nil || c.Name != "Avery Quill" {
		t.Fatalf("accept after undo: %v %+v", err, c)
	}
	if _, err := rs.Unreject(run.ID, "d1", testNow); err == nil {
		t.Error("an accepted draft cannot be un-passed")
	}
}

// TestProjectRecoversTopicsFromALegacySnippet: a queue looked up before
// Topics existed carries the chips only inside its OpenAlex author-record
// snippet. The projection reads them back out (deduped by topicKey, in
// order) so the card and an accept's knowledge claims see them; a draft
// that already has Topics is left exactly as stored; nothing is written.
func TestProjectRecoversTopicsFromALegacySnippet(t *testing.T) {
	legacy := citedDraft("Joseph Ackerman", "A1")
	legacy.Evidence = append(legacy.Evidence, sources.Evidence{
		SourceID: "openalex", URLOrFile: "https://openalex.org/A1", RetrievedAt: testNow,
		Snippet: "works_count: 1 · cited_by_count: 1 · h_index: 1 · topics: Gyrotron Research; Superconducting Materials; gyrotron research",
		Kind:    sources.EvidencePublication, Trust: sources.TrustMedium,
	})
	stored := citedDraft("Kept As Is", "A2")
	stored.Topics = []string{"Coil Design"}
	stored.Evidence[0].Snippet = "works_count: 3 · topics: Something Else"
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{legacy, stored}}
	rs, _, _ := testRunStore(t, fake)
	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "mri"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	// Execute stores the drafts' Topics as the adapter gave them (legacy: none)
	raw, err := os.ReadFile(filepath.Join(rs.dir(run.ID), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []Draft
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if len(onDisk[0].Draft.Topics) != 0 {
		t.Fatalf("the legacy draft's Topics reached the cache: %v", onDisk[0].Draft.Topics)
	}
	got := strings.Join(run.Drafts[0].Draft.Topics, "|")
	if got != "Gyrotron Research|Superconducting Materials" {
		t.Errorf("recovered topics = %q, want the snippet's two distinct terms in order", got)
	}
	if strings.Join(run.Drafts[1].Draft.Topics, "|") != "Coil Design" {
		t.Errorf("a stored Topics list was touched: %v", run.Drafts[1].Draft.Topics)
	}
	// the same answer on every read
	again, err := rs.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(again.Drafts[0].Draft.Topics, "|") != got {
		t.Errorf("Get projected %v, Execute projected %q", again.Drafts[0].Draft.Topics, got)
	}
}
