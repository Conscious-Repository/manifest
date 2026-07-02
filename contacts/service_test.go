package contacts

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"manifest/vaultindex"
	"manifest/vaultwriter"
)

var now = time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)

// harness builds a temp vault with the Shoumik scenario + an open index, store,
// writer, and service.
func harness(t *testing.T) (*Service, *vaultindex.Index, string) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Shoumik: NO note; only [[links]] from meeting notes + a daily + an undated note.
	write("2026-05-19 shoumik sync.md", "---\ncategories:\n  - aion\n  - sync\n---\n"+
		"[[shoumik dabir]] [[justin mares]]\n\n**shoumik:** hi\n**ben:** yo\n**shoumik:** ok\n")
	write("2026-05-20 aion timelines sync.md", "---\ncategories: [sync]\n---\n[[shoumik dabir]] roadmap\n")
	write("intrinsic/2026-07-01.md", "<!-- manifest:start -->\nmeeting [[shoumik dabir]] today\n")
	write("random idea.md", "some thought referencing [[shoumik dabir]]\n") // undated → mention
	write("Agents/brief-shoumik.md", "---\ncategories: [research]\n---\nAI brief on [[shoumik dabir]].\n")
	// a person WITH a note but no interactions → blank last-met
	write("alice.md", "---\ncategories: [people]\nalias: [Al]\n---\nfriend\n")
	// a note-less name seen ONLY in a daily → triage
	write("intrinsic/2026-06-01.md", "<!-- manifest:start -->\ncoffee with [[bob jones]]\n")

	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := New(ix, store, vaultwriter.New(root), nil, nil)
	return svc, ix, root
}

func TestShoumikPageFromLinksAlone(t *testing.T) {
	svc, _, _ := harness(t)
	p, ok := svc.Page("shoumik dabir", now)
	if !ok {
		t.Fatal("Shoumik must have a page from links alone")
	}
	if p.HasNote || p.NoteBody != "" {
		t.Fatalf("expected no note / blank pane, got hasNote=%v body=%q", p.HasNote, p.NoteBody)
	}
	// last-met = newest DATED source only (the 07-01 daily), never a guess
	if p.LastMet != "2026-07-01" {
		t.Fatalf("last-met = %q, want 2026-07-01 (dated sources only)", p.LastMet)
	}
	// timeline = 3 dated (2 syncs + daily), newest first; AI brief excluded
	if len(p.Timeline) != 3 {
		t.Fatalf("timeline = %d entries, want 3: %+v", len(p.Timeline), p.Timeline)
	}
	if p.Timeline[0].Date != "2026-07-01" || p.Timeline[0].SourceType != "daily" {
		t.Fatalf("newest timeline entry = %+v", p.Timeline[0])
	}
	if p.Timeline[1].SourceType != "sync" || p.Timeline[2].SourceType != "sync" {
		t.Fatalf("sync source types wrong: %+v", p.Timeline)
	}
	for _, e := range p.Timeline {
		if e.Path == "Agents/brief-shoumik.md" {
			t.Fatal("AI-authored note must not appear in the timeline")
		}
	}
	// undated reference is a mention, not a dated event
	if len(p.Mentions) != 1 || p.Mentions[0].Path != "random idea.md" {
		t.Fatalf("mentions = %+v, want [random idea.md]", p.Mentions)
	}
	// the speaker-body sync is a transcript
	if len(p.Transcripts) != 1 || p.Transcripts[0].Path != "2026-05-19 shoumik sync.md" {
		t.Fatalf("transcripts = %+v", p.Transcripts)
	}
}

func TestShoumikCreateNoteReRendersIdentically(t *testing.T) {
	svc, _, root := harness(t)
	before, _ := svc.Page("shoumik dabir", now)

	key, err := svc.SaveNote("shoumik dabir", "shoumik dabir", "Portfolio founder. Wants to lead our round.")
	if err != nil {
		t.Fatal(err)
	}
	// the note was created at vault root with categories: [people]
	raw, err := os.ReadFile(filepath.Join(root, "shoumik dabir.md"))
	if err != nil {
		t.Fatalf("note not created: %v", err)
	}
	if !containsAll(string(raw), "categories: [people]", "Portfolio founder") {
		t.Fatalf("created note wrong:\n%s", raw)
	}
	// re-render: same timeline/last-met, now WITH the note + body
	after, ok := svc.Page(key, now)
	if !ok || !after.HasNote {
		t.Fatalf("after save: ok=%v hasNote=%v", ok, after.HasNote)
	}
	if after.LastMet != before.LastMet || len(after.Timeline) != len(before.Timeline) {
		t.Fatalf("page should re-render identically (timeline/last-met unchanged): before=%d/%s after=%d/%s",
			len(before.Timeline), before.LastMet, len(after.Timeline), after.LastMet)
	}
	if after.NoteBody != "Portfolio founder. Wants to lead our round." {
		t.Fatalf("note body = %q", after.NoteBody)
	}
}

func TestBlankLastMetWithoutDatedEvidence(t *testing.T) {
	svc, _, _ := harness(t)
	list, err := svc.List(now)
	if err != nil {
		t.Fatal(err)
	}
	var alice *Contact
	for i := range list {
		if list[i].Key == "alice" {
			alice = &list[i]
		}
	}
	if alice == nil {
		t.Fatal("alice (people note) should be a contact")
	}
	if alice.LastMet != "" {
		t.Fatalf("a contact with no dated interaction must show blank last-met, got %q", alice.LastMet)
	}
	// Shoumik IS in the list (auto-contact via meeting context), with a date
	var sh *Contact
	for i := range list {
		if list[i].Key == "shoumik dabir" {
			sh = &list[i]
		}
	}
	if sh == nil || sh.LastMet != "2026-07-01" || sh.HasNote {
		t.Fatalf("shoumik list row = %+v", sh)
	}
}

func TestTriageQueueAndDecisions(t *testing.T) {
	svc, _, _ := harness(t)
	tri, _ := svc.Triage()
	if !hasKey(tri, "bob jones") {
		t.Fatalf("bob jones (daily-only note-less) should be in triage: %+v", tri)
	}
	if hasKeyTri(tri, "shoumik dabir") {
		t.Fatal("shoumik is meeting-context → auto-contact, never in triage")
	}
	// dismiss → gone for good
	if err := svc.Dismiss("bob jones"); err != nil {
		t.Fatal(err)
	}
	tri2, _ := svc.Triage()
	if hasKeyTri(tri2, "bob jones") {
		t.Fatal("dismissed name must not reappear")
	}
}

func TestCreateFlowSearchAndBind(t *testing.T) {
	svc, _, root := harness(t)
	// search surfaces the existing target with a ref count BEFORE creating
	refs, _ := svc.Search("shoumik")
	if !hasRef(refs, "shoumik dabir") {
		t.Fatalf("search should surface existing [[shoumik dabir]] refs: %+v", refs)
	}
	// give shoumik a note, then bind a second spelling → alias recorded on the note
	if _, err := svc.SaveNote("shoumik dabir", "shoumik dabir", "note"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Bind("shoumik", "shoumik dabir", "Shoumik"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "shoumik dabir.md"))
	if !containsAll(string(raw), "alias:", "Shoumik") {
		t.Fatalf("bind should record the variant as an alias:\n%s", raw)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
func hasKey(ts []TriageItem, k string) bool { return hasKeyTri(ts, k) }
func hasKeyTri(ts []TriageItem, k string) bool {
	for _, t := range ts {
		if t.Key == k {
			return true
		}
	}
	return false
}
func hasRef(rs []Ref, k string) bool {
	for _, r := range rs {
		if r.Key == k {
			return true
		}
	}
	return false
}
