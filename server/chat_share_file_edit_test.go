package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"manifest/agentchat"
	"manifest/artifacts"
)

func TestTeamFileEditEligibilityPredicate(t *testing.T) {
	text := []byte("# notes\nplain text\n")
	bin := []byte{0x00, 0x01, 0x02, 'x'}
	cases := []struct {
		name     string
		file     chatShareFile
		data     []byte
		eligible bool
		reason   string
	}{
		{"text artifact", chatShareFile{ArtifactID: "a1", Hash: artifacts.Hash(text), References: []string{"delivery:d"}}, text, true, "registered text artifact"},
		{"record snapshot", chatShareFile{ArtifactID: "a2", Hash: artifacts.Hash(text), Record: "task inbox/first"}, text, false, "private record snapshot"},
		{"uploaded blob", chatShareFile{Hash: artifacts.Hash(text), Name: "up.txt"}, text, false, "uploaded file"},
		{"plan version", chatShareFile{ArtifactID: "a3", Hash: artifacts.Hash(text), References: []string{"delivery:d", "terminal-plan:t:k"}}, text, false, "task plan"},
		{"binary", chatShareFile{ArtifactID: "a4", Hash: artifacts.Hash(bin)}, bin, false, "not editable text"},
		{"bytes changed", chatShareFile{ArtifactID: "a5", Hash: artifacts.Hash(text)}, []byte("other"), false, "unavailable"},
		{"bytes missing", chatShareFile{ArtifactID: "a6", Hash: artifacts.Hash(text)}, nil, false, "unavailable"},
	}
	for _, c := range cases {
		got := teamFileEditEligibility(c.file, c.data)
		if got.Eligible != c.eligible || !strings.Contains(got.Reason, c.reason) {
			t.Fatalf("%s: %+v", c.name, got)
		}
		if got.Eligible != (got.Base == c.file.Hash) {
			t.Fatalf("%s: only an eligible file names its edit base: %+v", c.name, got)
		}
	}
	// the privacy exclusion wins over capability: a record snapshot of text is
	// never eligible, whatever else is true of it
	if teamFileEditEligibility(chatShareFile{ArtifactID: "a", Hash: artifacts.Hash(text), Record: "note x.md"}, text).Eligible {
		t.Fatal("record snapshot eligible")
	}
}

// Default OFF: the reviewed envelope is byte-identical to before (no edit
// field, same Revision). ON: every file carries its eligibility inside the
// reviewed bytes, so the revision changes and a publish confirmed against the
// OFF review is refused as changed — the staged-bytes rule, not a new one.
func TestShareReviewFileEditEligibilitySwitch(t *testing.T) {
	s, st, _, _ := relationshipsFixture(t)
	team, _ := chatFixture(t)
	s.chat = team.chat
	snapshot := contextPreview(t, s, "task", "inbox/first")
	code, ref := contextRetain(t, s, "task", "inbox/first", snapshot["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	plain, err := s.artifactReg.Put(artifacts.Put{Ref: "docs/brief.md", Content: []byte("team brief\n")})
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Create("kairos-private", "kairos-private", "Share me", "")
	if err != nil {
		t.Fatal(err)
	}
	sess, body, _, _ := st.Get("kairos-private", id)
	sess.Deliveries = []agentchat.Delivery{{ID: "done", State: agentchat.DeliveryCompleted, Context: &agentchat.MessageContext{Artifacts: []agentchat.ArtifactReference{{ID: ref["id"].(string), Revision: ref["revision"].(string)}, {ID: plain.Artifact.ID, Revision: plain.Revision.Hash}}}}}

	if s.shareTeamFileEdit {
		t.Fatal("the switch must default off")
	}
	off := s.chatShareReview(context.Background(), sess, body, nil)
	raw, _ := json.Marshal(off)
	if strings.Contains(string(raw), `"edit"`) {
		t.Fatal("switch off must leave the reviewed bytes unchanged", string(raw))
	}
	if again := s.chatShareReview(context.Background(), sess, body, nil); again.Revision != off.Revision {
		t.Fatal("review is not a fixpoint")
	}

	s.UseShareTeamFileEditEligibility(true)
	on := s.chatShareReview(context.Background(), sess, body, nil)
	if on.Revision == off.Revision {
		t.Fatal("eligibility must be inside the fingerprinted envelope")
	}
	if again := s.chatShareReview(context.Background(), sess, body, nil); again.Revision != on.Revision {
		t.Fatal("eligibility must be deterministic (fixpoint)")
	}
	byName := map[string]chatShareFile{}
	for _, f := range on.Files {
		if f.Edit == nil {
			t.Fatalf("every file carries an eligibility when on: %+v", f)
		}
		byName[f.Name] = f
	}
	if e := byName["docs/brief.md"].Edit; !e.Eligible || e.Base != plain.Revision.Hash {
		t.Fatalf("registered text artifact: %+v", e)
	}
	if e := byName["inbox/first#context-"+snapshot["revision"].(string)].Edit; e.Eligible || !strings.Contains(e.Reason, "private record snapshot") {
		t.Fatalf("record snapshot must never be eligible: %+v", e)
	}
	// the stored-envelope validator accepts the on-shape bytes and they round-trip
	payload, _ := json.Marshal(on)
	if !validStoredShareReview(payload) {
		t.Fatal("stored review with eligibility rejected")
	}
	var decoded chatShareReview
	if err := json.Unmarshal(payload, &decoded); err != nil || decoded.Files[0].Edit == nil {
		t.Fatal("eligibility lost on decode", err)
	}
	reencoded, _ := json.Marshal(decoded)
	if string(reencoded) != string(payload) {
		t.Fatal("stored envelope is not byte-identical after a round trip")
	}
}
