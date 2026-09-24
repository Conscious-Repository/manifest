package recruiting

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRecordOutreachOutcomeRecoversPartialWriteAndPreservesNewerDraft(t *testing.T) {
	s, _ := testStore(t)
	c := gatedCandidate(t, s, "avery@example.test")
	first, _, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Subject: "First", Body: "Reviewed first"}, "ben@aion.bio", testNow)
	if err != nil {
		t.Fatal(err)
	}
	newer, _, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Subject: "Newer", Body: "Keep this unsent draft"}, "ben@aion.bio", testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	path := s.Path(OutreachLogName(CandidateSlug(c.ID)))
	before, _ := os.ReadFile(path)
	outcome := OutreachOutcome{OperationID: "sha256:" + strings.Repeat("a", 64), Revision: OutreachRevision(first), Sender: first.Sender, To: first.To, Subject: first.Subject, Body: first.Body, MessageID: "message-first", ThreadID: "thread-first", ConfirmedAt: testNow.Add(2 * time.Hour)}
	write := s.write
	s.write = func(path string, b []byte) error {
		if strings.Contains(path, "/candidates/") {
			return errors.New("candidate disk unavailable")
		}
		return write(path, b)
	}
	if _, err = s.RecordOutreachOutcome(c.ID, outcome); err == nil {
		t.Fatal("missing partial-write error")
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), string(before)) {
		t.Fatal("history rewritten")
	}
	entries, err := s.Outreach(c.ID)
	if err != nil || len(entries) != 3 || entries[2].DraftSeq != first.Seq {
		t.Fatal(entries, err)
	}
	if draft, ok := CurrentOutreachDraft(entries); !ok || draft.Seq != newer.Seq {
		t.Fatal("late receipt swallowed newer draft", draft)
	}
	s.write = write
	entry, err := s.RecordOutreachOutcome(c.ID, outcome)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Seq != 3 {
		t.Fatal("retry duplicated receipt", entry)
	}
	doc := s.LoadCandidate(CandidateSlug(c.ID))
	pointer := doc.Outreach()[0]
	if doc.Get("stage") != StageOutreach || pointer.Status != OutreachStatusDraft || pointer.MessageID != outcome.MessageID || len(pointer.Operations) != 1 {
		t.Fatal(doc.Get("stage"), pointer)
	}
	// A later owner stage edit survives retry once the application marker exists.
	if _, err = s.SetStage(c.ID, StageReviewing); err != nil {
		t.Fatal(err)
	}
	candidateBefore, _ := os.ReadFile(s.Path("candidates/" + CandidateSlug(c.ID) + ".md"))
	if _, err = s.RecordOutreachOutcome(c.ID, outcome); err != nil {
		t.Fatal(err)
	}
	candidateAfter, _ := os.ReadFile(s.Path("candidates/" + CandidateSlug(c.ID) + ".md"))
	if string(candidateBefore) != string(candidateAfter) {
		t.Fatal("retry overwrote owner edits")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("retry rewrote log")
	}
	if SerializeOutreach(ParseOutreach(string(after))) != string(after) {
		t.Fatal("new receipt fields break fixpoint")
	}
	wrong := outcome
	wrong.Body = "unreviewed"
	if _, err = s.RecordOutreachOutcome(c.ID, wrong); err == nil {
		t.Fatal("mismatched envelope recorded")
	}
}

func TestRecordOutreachOutcomeConsumesOnlyMatchingDraft(t *testing.T) {
	s, _ := testStore(t)
	c := gatedCandidate(t, s, "avery@example.test")
	first, _, _ := s.DraftOutreach(c.ID, OutreachDraftRequest{Subject: "First", Body: "First"}, "ben@aion.bio", testNow)
	second, _, _ := s.DraftOutreach(c.ID, OutreachDraftRequest{Subject: "Second", Body: "Second"}, "ben@aion.bio", testNow.Add(time.Hour))
	makeOutcome := func(d OutreachEntry, key string, at time.Time) OutreachOutcome {
		return OutreachOutcome{OperationID: "sha256:" + strings.Repeat(key, 64), Revision: OutreachRevision(d), Sender: d.Sender, To: d.To, Subject: d.Subject, Body: d.Body, MessageID: "message-" + key, ThreadID: "thread-" + key, ConfirmedAt: at}
	}
	newest := makeOutcome(second, "b", testNow.Add(3*time.Hour))
	older := makeOutcome(first, "a", testNow.Add(2*time.Hour))
	if _, err := s.RecordOutreachOutcome(c.ID, newest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordOutreachOutcome(c.ID, older); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Outreach(c.ID)
	if _, ok := CurrentOutreachDraft(entries); ok {
		t.Fatal("sent latest draft remains sendable")
	}
	if pointer := s.LoadCandidate(CandidateSlug(c.ID)).Outreach()[0]; pointer.MessageID != newest.MessageID || len(pointer.Operations) != 2 {
		t.Fatal("old receipt replaced newer pointer", pointer)
	}
}
