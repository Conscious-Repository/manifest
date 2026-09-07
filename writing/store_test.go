package writing

import (
	"manifest/record"
	"manifest/vaultwriter"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	w := vaultwriter.New(t.TempDir()).Grant(vaultwriter.Capability{Name: "writing", Zone: record.ZoneSystem, Pattern: "system/writing/**", Actor: vaultwriter.ActorUserAction}, vaultwriter.Capability{Name: "writing-agent", Zone: record.ZoneSystem, Pattern: "system/writing/**", Actor: vaultwriter.ActorApprovedProposal})
	return &Store{Writer: w, Root: "system/writing"}
}
func TestConversationPreservesUnknownBytesAndSurvivesMove(t *testing.T) {
	s := fixture(t)
	raw := []byte("# Note\r\n🌿 selected words\r\n")
	rev, _ := s.Writer.CreateNote("note.md", string(raw))
	a := StampAnchor(raw, Anchor{Revision: rev, Start: 12, End: 26, Quote: string(raw[12:26])})
	// Use byte boundaries selected from the literal Unicode text.
	a.Start = strings.Index(string(raw), "selected")
	a.End = a.Start + len("selected words")
	a.Quote = "selected words"
	a = StampAnchor(raw, a)
	if err := ValidateAnchor(raw, a); err != nil {
		t.Fatal(err)
	}
	r := NewReply("comment-1", "owner", "## a heading\n[field:: syntax]\n```\n🌿")
	e := Event{ID: "comment-1", Type: "thread", Anchor: &a, Reply: &r}
	d, err := s.Append("note.md", vaultwriter.Revision(nil), e, false)
	if err != nil {
		t.Fatal(err)
	}
	rec := s.RecordPath("note.md")
	before, _ := s.Writer.ReadVaultFile(rec)
	before = append(before, []byte("\n## hand edited\nunknown: preserve me\n")...)
	if err = s.Writer.WriteCap("writing", rec, before); err != nil {
		t.Fatal(err)
	}
	d, err = s.Read("note.md")
	if err != nil {
		t.Fatal(err)
	}
	reply := NewReply("reply-2", "owner", "follow-up")
	d, err = s.Append("note.md", d.Revision, Event{ID: "reply-2", Thread: e.ID, Type: "reply", Reply: &reply}, false)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := s.Writer.ReadVaultFile(rec)
	if !strings.HasPrefix(string(after), string(before)) {
		t.Fatal("unknown bytes rewritten")
	}
	prior, next, err := s.RelocatedRecord("note.md", "moved.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Writer.MoveNoteWithRecord("note.md", "moved.md", rev, rec, s.RecordPath("moved.md"), prior, next); err != nil {
		t.Fatal(err)
	}
	moved, err := s.Read("moved.md")
	if err != nil || len(moved.Threads) != 1 || len(moved.Threads[0].Replies) != 2 {
		t.Fatalf("lost conversation: %+v %v", moved, err)
	}
	if _, err = os.Stat(filepath.Join(s.Writer.VaultRoot(), "note.md")); !os.IsNotExist(err) {
		t.Fatal("source not moved")
	}
	if _, err = s.Append("moved.md", moved.Revision, Event{ID: "bad-agent", Type: "state", Thread: e.ID, State: "resolved"}, true); err == nil {
		t.Fatal("agent altered owner state")
	}
	if _, err = s.Append("moved.md", d.Revision, Event{ID: "stale-state", Type: "state", Thread: e.ID, State: "resolved"}, false); err == nil {
		t.Fatal("stale record accepted")
	}
}
func TestCorruptRecordAndUnicode(t *testing.T) {
	if _, err := Parse("# hand edited but no identity", "x.md"); err == nil {
		t.Fatal("corruption treated as empty")
	}
	if _, err := Parse("```manifest-writing\nnot JSON\n```", "x.md"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	raw := []byte("🌿 text")
	a := Anchor{Revision: vaultwriter.Revision(raw), Start: 1, End: 4, Quote: string(raw[1:4])}
	if ValidateAnchor(raw, a) == nil {
		t.Fatal("split Unicode accepted")
	}
}
