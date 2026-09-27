package recruiting

import (
	"context"
	"strings"
	"testing"

	"manifest/recruiting/sources"
)

// ⚠ THE NETWORK VOCABULARY (2026-09-27): the kinds are hire · advisor · expert
// · connector · team. The install seed wrote `type:: founder`, so the old words
// are READ forever but written never — the projection maps them, and the next
// edit of the row converges it on disk.
func TestNetworkPersonLegacyTypeMapsOnReadAndConvergesOnEdit(t *testing.T) {
	doc := ParseNetworkPeople("---\n---\n- [id:: aion-net/ben] [name:: Ben] [type:: founder]\n" +
		"- [id:: aion-net/x] [name:: X] [type:: investor]\n- [id:: aion-net/y] [name:: Y] [type:: external]\n")
	got := map[string]string{}
	for _, p := range doc.People() {
		got[p.ID] = p.Type
	}
	if got["aion-net/ben"] != "team" || got["aion-net/x"] != "connector" || got["aion-net/y"] != "" {
		t.Fatalf("legacy kinds not mapped on read: %+v", got)
	}
	// untouched rows keep their bytes (the fixpoint) …
	if !strings.Contains(SerializeNetworkPeople(doc), "[type:: founder]") {
		t.Fatal("a read rewrote the file")
	}
	// … and an edit of ANY field converges the legacy spelling
	if _, err := doc.Update("aion-net/ben", map[string]string{"note": "co-founder"}); err != nil {
		t.Fatal(err)
	}
	out := SerializeNetworkPeople(doc)
	if strings.Contains(out, "[type:: founder]") || !strings.Contains(out, "[type:: team]") {
		t.Fatalf("an edit did not converge the old type:\n%s", out)
	}
	// the old words are refused as NEW input only through the mapping, never raw
	if _, err := doc.Update("aion-net/x", map[string]string{"kind": "wizard"}); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
}

// Tags are repeated keys deduped by TopicID; the note is one line; the last
// contact is a real date or nothing.
func TestNetworkPersonTagsNoteAndLastContactRoundTrip(t *testing.T) {
	doc := ParseNetworkPeople("---\n---\n- [id:: aion-net/a] [name:: Ada]\n")
	p, err := doc.Update("aion-net/a", map[string]string{
		"tags": "MRI coils, fda-510k, mri-coils, ", "note": "met at ISMRM\n  — coil person", "last_contact": "2026-09-20",
		"kind": "advisor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.Tags, "|") != "MRI coils|fda-510k" {
		t.Fatalf("tags: %+v", p.Tags)
	}
	if p.Note != "met at ISMRM — coil person" || p.LastContact != "2026-09-20" || p.Type != "advisor" {
		t.Fatalf("fields: %+v", p)
	}
	back := ParseNetworkPeople(SerializeNetworkPeople(doc)).People()[0]
	if strings.Join(back.Tags, "|") != "MRI coils|fda-510k" || back.Note != p.Note {
		t.Fatalf("round trip lost fields: %+v", back)
	}
	if _, err := doc.Update("aion-net/a", map[string]string{"last_contact": "last tuesday"}); err == nil {
		t.Fatal("a non-date last contact was accepted")
	}
}

// ⚠ KEEP writes one network row — never a candidate, never a vault note, never
// an email (D15), never consent (known ≠ someone I'd ask) — repoints the edges
// that named the person externally, marks the draft graphed, and refuses the
// same human twice.
func TestKeepDraftWritesOneRowAndNothingElse(t *testing.T) {
	d := citedDraft("Ada Coil", "A1")
	d.Links = append(d.Links, "https://orcid.org/0000-0001-2345-6789", "https://github.com/adacoil")
	d.Contact = map[string]string{"email": "ada@example.test"}
	d.Topics = []string{"low-field MRI", "RF coils", "rf-coils"}
	d.Edges = []sources.EdgeClaim{{From: "ext/orcid/0000-0009-9999-9999", Type: sources.EdgeCoauthor,
		Basis: "shared paper", Confidence: 0.6, SourceID: "fake", Evidence: "https://example.test/paper/A1"}}
	rs, store, vault := testRunStore(t, &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{d}})
	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "coils", DryRun: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, vault)
	candsBefore := len(store.CandidateSlugs())

	after, p, err := rs.Keep(run.ID, "d1", "expert", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != "expert" || p.SourceRef != "fake:A1" || p.ORCID == "" || p.GitHub == "" {
		t.Fatalf("kept row: %+v", p)
	}
	if strings.Join(p.Topics, "|") != "low-field MRI|RF coils" || len(p.Tags) != 0 {
		t.Fatalf("the source's topics are suggestions, never tags: topics=%v tags=%v", p.Topics, p.Tags)
	}
	if p.Email != "" || p.Consent != "" {
		t.Fatalf("keep wrote an email or a consent: %+v", p)
	}
	if after.Drafts[0].Status != DraftGraphed || after.Drafts[0].CandidateID != p.ID {
		t.Fatalf("draft not marked graphed: %+v", after.Drafts[0])
	}
	if len(store.CandidateSlugs()) != candsBefore {
		t.Fatal("keep wrote a candidate")
	}
	// the edge filed onto the ROW, not the external key it arrived under
	var found bool
	for _, e := range store.LoadEdges().Edges() {
		if e.To == p.ID || e.From == p.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the draft's edge was not filed onto the kept row")
	}
	assertOnlyChanged(t, "keep", before, snapshot(t, vault), "network/people.md", "network/edges.md")
	if strings.Contains(snapshot(t, vault)["system/aion/recruiting/network/people.md"], "ada@example.test") {
		t.Fatal("D15: an adapter email reached the kept row")
	}

	// the same human again — by a second run naming the same source id
	run2, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "coils again", DryRun: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if run2.Drafts[0].Status == DraftNew {
		if _, _, err := rs.Keep(run2.ID, "d1", "", testNow); err == nil {
			t.Fatal("the same person was kept twice")
		}
	}
}
