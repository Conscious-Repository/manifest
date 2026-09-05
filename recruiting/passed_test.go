package recruiting

import (
	"strings"
	"testing"

	"manifest/recruiting/sources"
)

// THE POINT OF A PASS. Passing used to mark one row in one run's cache, so
// sweeping the same lab next week asked the same question about the same
// people. The decision has to outlive the run that produced it.

func TestAPassedPersonDoesNotComeBackOnTheNextSweep(t *testing.T) {
	drafts := []sources.CandidateDraft{
		citedDraft("Dana Reyes", "A1"),
		citedDraft("Kai Okonkwo", "A2"),
	}
	rs, store, _ := testRunStore(t, &fakeAdapter{id: "fake", drafts: drafts})

	first := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	if first.Counts.New != 2 {
		t.Fatalf("two people to triage: %+v", first.Counts)
	}
	if _, err := rs.Reject(first.ID, "d1", "too senior", testNow); err != nil {
		t.Fatal(err)
	}

	// the same sweep, again
	second := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	if second.Counts.New != 1 || second.Counts.Rejected != 1 {
		t.Fatalf("the passed person came back: %+v", second.Counts)
	}
	var passed, fresh *Draft
	for i := range second.Drafts {
		switch second.Drafts[i].Draft.Name {
		case "Dana Reyes":
			passed = &second.Drafts[i]
		case "Kai Okonkwo":
			fresh = &second.Drafts[i]
		}
	}
	if passed == nil || passed.Status != DraftRejected {
		t.Fatalf("the passed person is not marked passed: %+v", passed)
	}
	if !strings.Contains(passed.Reason, "too senior") {
		t.Fatalf("the reason must survive to the next sweep: %q", passed.Reason)
	}
	if fresh == nil || fresh.Status != DraftNew {
		t.Fatalf("the undecided person must still be new: %+v", fresh)
	}
	// and the suppression is one line in one file, not a record
	if n := len(store.CandidateSlugs()); n != 0 {
		t.Fatalf("a pass produced %d candidate records", n)
	}
}

// Undo lifts the stone: bringing someone back must actually bring them back.
func TestUndoingAPassLiftsTheSuppression(t *testing.T) {
	rs, store, _ := testRunStore(t, &fakeAdapter{id: "fake",
		drafts: []sources.CandidateDraft{citedDraft("Dana Reyes", "A1")}})

	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	if _, err := rs.Reject(run.ID, "d1", "", testNow); err != nil {
		t.Fatal(err)
	}
	if len(store.PassedSet()) != 1 {
		t.Fatal("no tombstone after a pass")
	}
	if _, err := rs.Unreject(run.ID, "d1", testNow); err != nil {
		t.Fatal(err)
	}
	if n := len(store.PassedSet()); n != 0 {
		t.Fatalf("undo left %d tombstones standing", n)
	}
	again := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	if again.Counts.New != 1 {
		t.Fatalf("after an undo the person must be offered again: %+v", again.Counts)
	}
}

// The tombstone carries no more than it must: a key, a name to match on, the
// reason and the date. A person you declined does not earn a profile.
func TestATombstoneIsNotARecord(t *testing.T) {
	s, _ := testStore(t)
	if err := s.AddPassed(Passed{Key: "fake:A1", Name: "Dana Reyes", Reason: "too senior", Source: "fake"}, testNow); err != nil {
		t.Fatal(err)
	}
	raw := s.raw("passed.md")
	for _, forbidden := range []string{"orcid", "evidence", "profile", "email", "@"} {
		if strings.Contains(strings.ToLower(raw), forbidden) {
			t.Fatalf("the tombstone carries %q: %s", forbidden, raw)
		}
	}
	for _, want := range []string{"fake:A1", "Dana Reyes", "too senior", "2026-09-02"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("the tombstone lost %q: %s", want, raw)
		}
	}
	// passing the same person twice is one fact, with the newer reason
	if err := s.AddPassed(Passed{Key: "fake:A1", Name: "Dana Reyes", Reason: "left the field"}, testNow); err != nil {
		t.Fatal(err)
	}
	if n := len(s.LoadPassed().Passed()); n != 1 {
		t.Fatalf("a second pass wrote a second row: %d", n)
	}
	if got := s.PassedSet()["fake:A1"].Reason; got != "left the field" {
		t.Fatalf("the newer reason must win: %q", got)
	}
}

// A hand-edited row round-trips byte-identically, recognized or not — the
// fixpoint rule every record document in this package keeps.
func TestPassedRoundTripsAHandEdit(t *testing.T) {
	raw := "# passed\n\nnotes a person wrote\n\n" +
		"- [key:: fake:A1] [name:: Dana Reyes] [reason:: too senior] [at:: 2026-09-02]\n" +
		"- a bare line nothing recognizes\n"
	if got := SerializePassed(ParsePassed(raw)); got != raw {
		t.Fatalf("round trip changed the file:\n%q\n%q", raw, got)
	}
}
