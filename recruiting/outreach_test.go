package recruiting

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- the log grammar ----

const outreachFixture = `---
id: outreach/avery-quill
candidate: cand/avery-quill
pii: true
---

# outreach — Avery Quill

a hand-added note the app knows nothing about

- [seq:: 1] [at:: 2026-09-02] [kind:: direct] [status:: draft] [to:: avery@example.test] [sender:: ben@aion.bio] [subject:: MRI Engineer at AION] [foo:: bar]
  > Hi Avery,
  >
  > line two
  > Best,
  > Ben

- [status:: sent] [seq:: 2] [kind:: warm] [via:: aion-net/rj-tevonian] [to:: rj@aion.bio] [to:: cc@aion.bio] [at:: 2026-09-03] [sender:: ben@aion.bio] [subject:: Intro?] [message:: msg_1] [thread:: thr_1] [sent_at:: 2026-09-03T10:00:00Z]
  > one line body
`

// A hand-edited log round-trips byte-identically: reordered fields, an
// unknown field, a stray prose line, and a body with a blank line.
func TestOutreachFixpoint(t *testing.T) {
	d := ParseOutreach(outreachFixture)
	if got := SerializeOutreach(d); got != outreachFixture {
		t.Fatalf("not a fixpoint:\n--- want\n%s\n--- got\n%s", outreachFixture, got)
	}
	es := d.Entries()
	if len(es) != 2 {
		t.Fatalf("entries: %d", len(es))
	}
	if es[0].Seq != 1 || es[0].Kind != OutreachDirect || es[0].Status != OutreachStatusDraft ||
		es[0].To[0] != "avery@example.test" || es[0].Subject != "MRI Engineer at AION" {
		t.Fatalf("entry 1: %+v", es[0])
	}
	if es[0].Body != "Hi Avery,\n\nline two\nBest,\nBen" {
		t.Fatalf("body: %q", es[0].Body)
	}
	if len(es[0].Unknown) != 1 || es[0].Unknown[0].Key != "foo" {
		t.Fatalf("unknown fields: %+v", es[0].Unknown)
	}
	if es[1].Seq != 2 || es[1].Via != "aion-net/rj-tevonian" || len(es[1].To) != 2 ||
		es[1].MessageID != "msg_1" || es[1].ThreadID != "thr_1" || es[1].SentAt != "2026-09-03T10:00:00Z" {
		t.Fatalf("entry 2: %+v", es[1])
	}
	if _, ok := d.CurrentDraft(); ok {
		t.Fatal("a log whose last row is sent has a current draft")
	}
}

// Append is the only mutator: seq climbs, earlier rows are untouched, a
// second parse of the emitted file sees every row, and the emitted bytes
// are themselves a fixpoint.
func TestOutreachAppendOnly(t *testing.T) {
	d := NewOutreachDoc("avery-quill", "Avery Quill")
	first, err := d.Append(OutreachEntry{At: "2026-09-02", Kind: OutreachDirect, Status: OutreachStatusDraft,
		To: []string{"avery@example.test"}, Sender: "ben@aion.bio", Subject: "s1", Body: "Hi Avery,\n\nline two\n"})
	if err != nil || first.Seq != 1 {
		t.Fatalf("first append: %v %+v", err, first)
	}
	before := SerializeOutreach(d)
	if _, err := d.Append(OutreachEntry{At: "2026-09-03", Kind: OutreachDirect, Status: OutreachStatusSent,
		To: []string{"avery@example.test"}, Sender: "ben@aion.bio", Subject: "s1", Body: "b"}); err == nil {
		t.Fatal("a sent row without a message id appended")
	}
	second, err := d.Append(OutreachEntry{At: "2026-09-03", Kind: OutreachDirect, Status: OutreachStatusSent,
		To: []string{"avery@example.test"}, Sender: "ben@aion.bio", Subject: "s1", Body: "b",
		MessageID: "m", ThreadID: "t", SentAt: "2026-09-03T00:00:00Z"})
	if err != nil || second.Seq != 2 {
		t.Fatalf("second append: %v %+v", err, second)
	}
	after := SerializeOutreach(d)
	if !strings.HasPrefix(after, before) {
		t.Fatalf("an append rewrote earlier bytes:\n--- before\n%s\n--- after\n%s", before, after)
	}
	if got := SerializeOutreach(ParseOutreach(after)); got != after {
		t.Fatalf("emitted log is not a fixpoint:\n%s\n---\n%s", after, got)
	}
	if es := ParseOutreach(after).Entries(); len(es) != 2 || es[0].Body != "Hi Avery,\n\nline two" || es[1].MessageID != "m" {
		t.Fatalf("re-parsed: %+v", es)
	}
	if _, err := d.Append(OutreachEntry{Kind: "shout", Status: OutreachStatusDraft, Sender: "x"}); err == nil {
		t.Fatal("an unknown kind appended")
	}
}

// ---- the draft generator ----

func draftInput(kind string) DraftInput {
	return DraftInput{
		Kind: kind, Recipient: "RJ Tevonian", Sender: "ben@aion.bio",
		Candidate: Candidate{Name: "Avery Quill", Profile: map[string]string{"title": "MRI Systems Engineer", "org": "Example Lab"}},
		Role:      Role{Title: "MRI Engineer", Location: "St. Louis"},
		Evidence:  []Evidence{{ID: "ev1", URL: "https://example.test/paper", Snippet: "verbatim quoted evidence"}},
		Path:      "aion-net/ben-anderson > aion-net/rj-tevonian > cand/avery-quill",
	}
}

func TestGenerateDraftTemplates(t *testing.T) {
	for _, kind := range OutreachKinds {
		in := draftInput(kind)
		if kind == OutreachDirect {
			in.Recipient = "Avery Quill"
		}
		a, err := GenerateDraft(in)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := GenerateDraft(in)
		if a != b {
			t.Fatalf("%s: not deterministic", kind)
		}
		if a.Subject == "" || a.Body == "" {
			t.Fatalf("%s: empty draft %+v", kind, a)
		}
		for _, want := range []string{"MRI Engineer", "verbatim quoted evidence", "https://example.test/paper", "ben@aion.bio", "Ben"} {
			if !strings.Contains(a.Body, want) && !strings.Contains(a.Subject, want) {
				t.Errorf("%s: draft lacks %q:\n%s", kind, want, a.Body)
			}
		}
		hasPath := strings.Contains(a.Body, "aion-net/rj-tevonian")
		switch kind {
		case OutreachDirect:
			if !strings.HasPrefix(a.Body, "Hi Avery,") || hasPath {
				t.Errorf("direct: %q", a.Body)
			}
		case OutreachWarm:
			if !strings.HasPrefix(a.Body, "Hi RJ,") || !hasPath || !strings.Contains(a.Subject, "Avery Quill") {
				t.Errorf("warm: %s / %q", a.Subject, a.Body)
			}
		case OutreachReferral:
			if !strings.HasPrefix(a.Body, "Hi RJ,") || !hasPath || !strings.Contains(a.Body, "Avery Quill (MRI Systems Engineer, Example Lab)") {
				t.Errorf("referral: %s / %q", a.Subject, a.Body)
			}
		}
	}
	if _, err := GenerateDraft(DraftInput{Kind: "shout"}); err == nil {
		t.Fatal("unknown kind generated")
	}
	// no evidence, no role: still a draft, still honest (no invented facts)
	bare, err := GenerateDraft(DraftInput{Kind: OutreachDirect, Recipient: "Avery", Sender: "ben@aion.bio"})
	if err != nil || strings.Contains(bare.Body, "\"\"") || strings.Contains(bare.Body, "()") {
		t.Fatalf("bare draft: %v %+v", err, bare)
	}
}

// ---- the store workflow ----

type fakeSender struct {
	capable bool
	sent    []OutreachMessage
	err     error
}

func (f *fakeSender) Sender() string    { return "ben@aion.bio" }
func (f *fakeSender) SendCapable() bool { return f.capable }
func (f *fakeSender) Send(_ context.Context, m OutreachMessage) (OutreachSendRef, error) {
	if f.err != nil {
		return OutreachSendRef{}, f.err
	}
	f.sent = append(f.sent, m)
	return OutreachSendRef{MessageID: "msg_1", ThreadID: "thr_1"}, nil
}

// gatedCandidate adds a candidate that passes the MRI role's gate and has
// an owner-typed address.
func gatedCandidate(t *testing.T, s *Store, email string) Candidate {
	t.Helper()
	c, err := s.AddCandidate(QuickAdd{Text: "https://example.test/people/avery", Name: "Avery Quill",
		Role: "role/mri-engineer", Title: "MRI Systems Engineer", Org: "Example Lab"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if email != "" {
		if _, err := s.UpdateCandidate(c.ID, map[string]string{"email": email}); err != nil {
			t.Fatal(err)
		}
	}
	c, err = s.AddEvidence(c.ID, Evidence{URL: "https://example.test/paper", Kind: "publication", Snippet: "verbatim quoted evidence"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	ev := c.Evidence[len(c.Evidence)-1].ID
	for _, crit := range []string{"low-field MRI hardware", "pulse sequence or coil design", "on-site Saint Louis"} {
		if c, err = s.ScoreFit(c.ID, crit, "4", []string{ev}, false); err != nil {
			t.Fatal(err)
		}
	}
	if !c.Gate.Passed {
		t.Fatalf("fixture candidate does not pass the gate: %+v", c.Gate)
	}
	return c
}

func TestOutreachWorkflow(t *testing.T) {
	s, vault := testStore(t)
	c := gatedCandidate(t, s, "avery@example.test")
	sender := &fakeSender{capable: true}

	// nothing to send yet
	r, err := s.PrepareOutreach(c.ID, sender)
	if err != nil || r.Ready || !strings.Contains(strings.Join(r.Reasons, ";"), "no draft") {
		t.Fatalf("prepare before draft: %v %+v", err, r)
	}

	// draft: generated, recipient resolved from the record, no network
	entry, rec, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachDirect}, "ben@aion.bio", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Seq != 1 || entry.Status != OutreachStatusDraft || len(entry.To) != 1 || entry.To[0] != "avery@example.test" ||
		!strings.Contains(entry.Body, "verbatim quoted evidence") || !strings.Contains(entry.Subject, "MRI Engineer") {
		t.Fatalf("draft: %+v", entry)
	}
	if len(rec.Outreach) != 1 || rec.Outreach[0].Log != "outreach/avery-quill.md" || rec.Outreach[0].Status != OutreachStatusDraft {
		t.Fatalf("pointer row: %+v", rec.Outreach)
	}
	if _, err := os.Stat(filepath.Join(vault, "system/aion/recruiting/outreach/avery-quill.md")); err != nil {
		t.Fatal("log not written under the recruiting root")
	}
	if len(sender.sent) != 0 {
		t.Fatal("drafting reached the sender")
	}

	// the owner edits the subject: a second draft row, the first untouched
	edited, _, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachDirect, Subject: "Edited subject", Body: entry.Body}, "ben@aion.bio", testNow)
	if err != nil || edited.Seq != 2 || edited.Subject != "Edited subject" {
		t.Fatalf("edit: %v %+v", err, edited)
	}
	if es, _ := s.Outreach(c.ID); len(es) != 2 || es[0].Subject != entry.Subject {
		t.Fatalf("log after edit: %+v", es)
	}

	r, err = s.PrepareOutreach(c.ID, sender)
	if err != nil || !r.Ready || r.Draft == nil || r.Draft.Seq != 2 || !r.SendCapable {
		t.Fatalf("prepare: %v %+v", err, r)
	}

	// no approval → refused, nothing sent
	res, err := s.SendOutreach(context.Background(), c.ID, OutreachSendRequest{}, sender, testNow)
	if !errors.Is(err, ErrOutreachNotApproved) || len(sender.sent) != 0 || !res.Readiness.Ready {
		t.Fatalf("unapproved send: %v %+v", err, res.Readiness)
	}

	// approved → sent once, ids persisted, stage moved
	res, err = s.SendOutreach(context.Background(), c.ID, OutreachSendRequest{Approve: true, Actor: "benjamin"}, sender, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || sender.sent[0].Subject != "Edited subject" || sender.sent[0].To[0] != "avery@example.test" {
		t.Fatalf("sent: %+v", sender.sent)
	}
	if res.MessageID != "msg_1" || res.ThreadID != "thr_1" || res.Entry.Seq != 3 || res.Entry.Status != OutreachStatusSent || res.Entry.SentAt == "" {
		t.Fatalf("result: %+v", res.Entry)
	}
	if res.Record.Stage != StageOutreach {
		t.Fatalf("stage after send: %s", res.Record.Stage)
	}
	ref := res.Record.Outreach[0]
	if ref.Status != OutreachStatusSent || ref.MessageID != "msg_1" || ref.ThreadID != "thr_1" || ref.Last != "2026-09-02" {
		t.Fatalf("pointer after send: %+v", ref)
	}
	raw, _ := os.ReadFile(filepath.Join(vault, "system/aion/recruiting/candidates/avery-quill.md"))
	if !strings.Contains(string(raw), "[log:: outreach/avery-quill.md] [last:: 2026-09-02] [status:: sent] [message:: msg_1] [thread:: thr_1]") {
		t.Fatalf("candidate record:\n%s", raw)
	}
	logRaw, _ := os.ReadFile(filepath.Join(vault, "system/aion/recruiting/outreach/avery-quill.md"))
	if got := SerializeOutreach(ParseOutreach(string(logRaw))); got != string(logRaw) {
		t.Fatalf("written log is not a fixpoint:\n%s", logRaw)
	}
	if !strings.Contains(string(logRaw), "[message:: msg_1] [thread:: thr_1] [sent_at:: 2026-09-02T15:04:05Z] [actor:: benjamin]") {
		t.Fatalf("log:\n%s", logRaw)
	}

	// a second approved click: the last row is sent, so there is no draft
	res, err = s.SendOutreach(context.Background(), c.ID, OutreachSendRequest{Approve: true}, sender, testNow)
	if !errors.Is(err, ErrOutreachNotReady) || len(sender.sent) != 1 {
		t.Fatalf("replay: %v (sent %d)", err, len(sender.sent))
	}
}

// No recipient on the record → the draft is written with an empty To, the
// preflight says so, and an approved send still refuses.
func TestOutreachRefusesWithoutRecipient(t *testing.T) {
	s, _ := testStore(t)
	c := gatedCandidate(t, s, "")
	sender := &fakeSender{capable: true}
	entry, _, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachDirect}, "ben@aion.bio", testNow)
	if err != nil || len(entry.To) != 0 {
		t.Fatalf("draft: %v %+v", err, entry)
	}
	r, _ := s.PrepareOutreach(c.ID, sender)
	if r.Ready || !strings.Contains(strings.Join(r.Reasons, ";"), "no recipient") {
		t.Fatalf("readiness: %+v", r)
	}
	if _, err := s.SendOutreach(context.Background(), c.ID, OutreachSendRequest{Approve: true}, sender, testNow); !errors.Is(err, ErrOutreachNotReady) {
		t.Fatalf("send: %v", err)
	}
	if len(sender.sent) != 0 {
		t.Fatal("sent without a recipient")
	}
	// an explicit, malformed recipient is refused at preflight too
	if _, _, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachDirect, To: []string{"not an address"}}, "ben@aion.bio", testNow); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.PrepareOutreach(c.ID, sender); r.Ready {
		t.Fatalf("malformed recipient passed: %+v", r)
	}
}

// Unconfigured sender: drafting works, readiness says why, an approved send
// refuses with ErrOutreachUnconfigured, and the record never reads "sent".
func TestOutreachUnconfiguredNeverFalselySent(t *testing.T) {
	s, _ := testStore(t)
	c := gatedCandidate(t, s, "avery@example.test")
	if _, _, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachDirect}, "ben@aion.bio", testNow); err != nil {
		t.Fatal(err)
	}
	for _, sender := range []OutreachSender{nil, &fakeSender{capable: false}} {
		r, err := s.PrepareOutreach(c.ID, sender)
		if err != nil || r.Ready || r.SendCapable || !strings.Contains(strings.Join(r.Reasons, ";"), "not connected") {
			t.Fatalf("readiness: %v %+v", err, r)
		}
		res, err := s.SendOutreach(context.Background(), c.ID, OutreachSendRequest{Approve: true}, sender, testNow)
		if !errors.Is(err, ErrOutreachUnconfigured) || res.MessageID != "" {
			t.Fatalf("send: %v %+v", err, res)
		}
	}
	v := s.View()
	if v.Candidates[0].Outreach[0].Status != OutreachStatusDraft || v.Candidates[0].Stage != StageNew {
		t.Fatalf("record after refused sends: %+v", v.Candidates[0])
	}
	// a failing transport: nothing is appended, the draft stays current
	failing := &fakeSender{capable: true, err: errors.New("gmail send: HTTP 500")}
	if _, err := s.SendOutreach(context.Background(), c.ID, OutreachSendRequest{Approve: true}, failing, testNow); err == nil {
		t.Fatal("a failed send succeeded")
	}
	if es, _ := s.Outreach(c.ID); len(es) != 1 || es[0].Status != OutreachStatusDraft {
		t.Fatalf("log after failed send: %+v", es)
	}
}

// The gate blocks a send: an unevidenced must is a readiness reason.
func TestOutreachRequiresGate(t *testing.T) {
	s, _ := testStore(t)
	c, err := s.AddCandidate(QuickAdd{Text: "https://example.test/x", Name: "Dana Reyes", Role: "role/mri-engineer"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateCandidate(c.ID, map[string]string{"email": "dana@example.test"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachDirect}, "ben@aion.bio", testNow); err != nil {
		t.Fatal(err)
	}
	r, _ := s.PrepareOutreach(c.ID, &fakeSender{capable: true})
	if r.Ready || !strings.Contains(strings.Join(r.Reasons, ";"), "fit gate") {
		t.Fatalf("readiness: %+v", r)
	}
}

// Warm and referral drafts go to the mutual: the recipient is the mutual's
// address, the body names the candidate and the path through the mutual.
func TestOutreachWarmGoesToMutual(t *testing.T) {
	s, _ := testStore(t)
	c := gatedCandidate(t, s, "avery@example.test")
	if _, _, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachWarm}, "ben@aion.bio", testNow); err == nil {
		t.Fatal("a warm draft without a mutual was written")
	}
	if _, _, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachWarm, Via: "aion-net/nobody"}, "ben@aion.bio", testNow); err == nil {
		t.Fatal("an unknown mutual was accepted")
	}
	entry, _, err := s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachWarm, Via: "aion-net/ben-anderson"}, "ben@aion.bio", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Via != "aion-net/ben-anderson" || len(entry.To) != 1 || entry.To[0] != "ben@aion.bio" {
		t.Fatalf("warm draft: %+v", entry)
	}
	if !strings.HasPrefix(entry.Body, "Hi Benjamin,") || !strings.Contains(entry.Body, "Avery Quill") || !strings.Contains(entry.Subject, "Intro to Avery Quill") {
		t.Fatalf("warm body: %s / %q", entry.Subject, entry.Body)
	}
	// a mutual without an address: the recipient must be explicit
	entry, _, err = s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachReferral, Via: "aion-net/rj-tevonian"}, "ben@aion.bio", testNow)
	if err != nil || len(entry.To) != 0 {
		t.Fatalf("referral draft: %v %+v", err, entry)
	}
	entry, _, err = s.DraftOutreach(c.ID, OutreachDraftRequest{Kind: OutreachReferral, Via: "aion-net/rj-tevonian", To: []string{"rj@aion.bio"}}, "ben@aion.bio", testNow)
	if err != nil || entry.To[0] != "rj@aion.bio" || !strings.HasPrefix(entry.Body, "Hi RJ,") {
		t.Fatalf("referral explicit: %v %+v", err, entry)
	}
	people := s.OutreachPeople()
	if len(people) != 2 || people[0].Name != "Benjamin Anderson" {
		t.Fatalf("outreach people: %+v", people)
	}
}
