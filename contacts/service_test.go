package contacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/vaultindex"
	"manifest/vaultwriter"
)

var now = time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)

// harness builds a temp vault with the Shoumik scenario + an open index, store,
// writer, and service (no calendar).
func harness(t *testing.T) (*Service, *vaultindex.Index, string) {
	return harnessCal(t, nil)
}

// harnessCal is harness with an injected CalendarReader (for calendar-verified
// "last met" and the email-review queue).
func harnessCal(t *testing.T, cal CalendarReader) (*Service, *vaultindex.Index, string) {
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
		"[[shoumik dabir]] [[justin mares]]\n\n**shoumik:** hi\n**ben:** yo\n**shoumik:** ok\n\n## Next steps\n- [ ] send the deck\n")
	write("2026-05-20 aion timelines sync.md", "---\ncategories: [sync]\n---\n[[shoumik dabir]] roadmap\n")
	write("intrinsic/2026-07-01.md", "<!-- manifest:start -->\nmeeting [[shoumik dabir]] today\n")
	write("random idea.md", "some thought referencing [[shoumik dabir]]\n") // undated → mention
	write("Agents/brief-shoumik.md", "---\ncategories: [research]\n---\nAI brief on [[shoumik dabir]].\n")
	// a person WITH a note but no interactions → blank last-met
	write("alice.md", "---\ncategories: [people]\nalias: [Al]\n---\nfriend\n")
	// a person WITH a note (no email yet) whose email local-part will match in review
	write("michael trinh.md", "---\ncategories: [people]\n---\nAion.\n")
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
	svc := New(ix, store, vaultwriter.New(root), cal, nil)
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
	// last-mentioned = newest DATED source only (the 07-01 daily), never a guess.
	// (LastMet is calendar-only now; no calendar in this harness → blank.)
	if p.LastMentioned != "2026-07-01" {
		t.Fatalf("last-mentioned = %q, want 2026-07-01 (dated sources only)", p.LastMentioned)
	}
	if p.LastMet != "" {
		t.Fatalf("last-met (calendar) must be blank without a calendar, got %q", p.LastMet)
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
	if after.LastMentioned != before.LastMentioned || len(after.Timeline) != len(before.Timeline) {
		t.Fatalf("page should re-render identically (timeline/last-mentioned unchanged): before=%d/%s after=%d/%s",
			len(before.Timeline), before.LastMentioned, len(after.Timeline), after.LastMentioned)
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
	if alice.LastMentioned != "" {
		t.Fatalf("a contact with no dated interaction must show blank last-mentioned, got %q", alice.LastMentioned)
	}
	// Shoumik IS in the list (auto-contact via meeting context), with a mention date
	var sh *Contact
	for i := range list {
		if list[i].Key == "shoumik dabir" {
			sh = &list[i]
		}
	}
	if sh == nil || sh.LastMentioned != "2026-07-01" || sh.HasNote {
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

func TestOpenLoopsAndToggle(t *testing.T) {
	svc, ix, root := harness(t)
	// the sync note's unchecked task surfaces as an open loop for BOTH attendees
	loops := svc.OpenLoops("shoumik dabir")
	var g *OpenLoopGroup
	var task *OpenLoopItem
	for i := range loops {
		for j := range loops[i].Loops {
			if loops[i].Loops[j].Kind == "checkbox" {
				g, task = &loops[i], &loops[i].Loops[j]
			}
		}
	}
	if task == nil {
		t.Fatalf("expected an unchecked checkbox loop, got %+v", loops)
	}
	if task.Text != "send the deck" {
		t.Fatalf("loop text = %q", task.Text)
	}
	// a loop in a multi-person note appears for each attendee (no dedupe)
	if len(svc.OpenLoops("justin mares")) == 0 {
		t.Fatal("the same loop should surface for the other attendee")
	}
	// toggling it (the dashboard write) checks it off in the source file
	vw := vaultwriter.New(root)
	if err := vw.ToggleTask(g.Path, task.Line, true); err != nil {
		t.Fatal(err)
	}
	if err := ix.ReindexPaths([]string{g.Path}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(g.Path)))
	if !strings.Contains(string(raw), "- [x] send the deck") {
		t.Fatalf("task not checked off in file:\n%s", raw)
	}
	if len(svc.OpenLoops("shoumik dabir")) != 0 {
		t.Fatal("a checked task should no longer be an open loop")
	}
}

func TestConfirmPersonCreatesLowercaseNote(t *testing.T) {
	svc, _, root := harness(t)
	// hit "Person" on a triage name with a Capitalized display
	if err := svc.Confirm("bob jones", "Bob Jones"); err != nil {
		t.Fatal(err)
	}
	// the file is created LOWERCASE with categories: [people] (the vault convention)
	raw, err := os.ReadFile(filepath.Join(root, "bob jones.md"))
	if err != nil {
		t.Fatalf("expected lowercase bob jones.md to be created: %v", err)
	}
	if !containsAll(string(raw), "categories: [people]") {
		t.Fatalf("created note must carry categories: [people]:\n%s", raw)
	}
	// the actual on-disk entry name must be lowercase (macOS FS is case-insensitive,
	// so check the real directory entry, not os.Stat which case-folds)
	entries, _ := os.ReadDir(root)
	foundName := ""
	for _, e := range entries {
		if strings.EqualFold(e.Name(), "bob jones.md") {
			foundName = e.Name()
		}
	}
	if foundName != "bob jones.md" {
		t.Fatalf("filename must be lowercase, got %q", foundName)
	}
	// it leaves triage and becomes a note-backed contact
	if tri, _ := svc.Triage(); hasKeyTri(tri, "bob jones") {
		t.Fatal("a confirmed person should leave the triage queue")
	}
	list, _ := svc.List(now)
	var bob *Contact
	for i := range list {
		if list[i].Key == "bob jones" {
			bob = &list[i]
		}
	}
	if bob == nil || !bob.HasNote {
		t.Fatalf("confirmed person should be a note-backed contact: %+v", bob)
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

// fakeCal is an in-memory CalendarReader for calendar-verified last-met tests.
type fakeCal struct {
	past     []Event
	upcoming []Event
}

func (f fakeCal) PastMeetings(now time.Time, days int) []Event { return f.past }
func (f fakeCal) Upcoming(now time.Time, days int) []Event     { return f.upcoming }

func TestCalendarLastMetAndEmailReview(t *testing.T) {
	at := func(y, m, d, hh int) time.Time { return time.Date(y, time.Month(m), d, hh, 0, 0, 0, time.UTC) }
	// Live-verified scenario: Shoumik has two emails (name-match + local-part match);
	// Michael's group meeting omits his name in title AND attendee → local-part only.
	cal := fakeCal{past: []Event{
		{Start: at(2026, 7, 2, 8), Title: "Benjamin <> Shoumik",
			Attendees: []Attendee{{Name: "Shoumik Dabir", Email: "dabir@anfavc.com"}}},
		{Start: at(2026, 6, 15, 10), Title: "Aion sync",
			Attendees: []Attendee{{Email: "shoumik.dabir@gmail.com"}}},
		{Start: at(2026, 3, 26, 9), Title: "Team Catch-up: Aging Roadmap",
			Attendees: []Attendee{{Email: "michaeltrinh19@gmail.com"}, {Name: "Random Person", Email: "stranger@random.com"}}},
	}}
	svc, _, _ := harnessCal(t, cal)

	// ---- email-review proposes the right attendee→contact pairs ----
	rev, err := svc.EmailReview(now)
	if err != nil {
		t.Fatal(err)
	}
	if c := candFor(rev, "dabir@anfavc.com"); c == nil || c.ContactKey != "shoumik dabir" || c.Via != "name" {
		t.Fatalf("dabir@anfavc.com should match shoumik dabir by name: %+v", c)
	}
	if c := candFor(rev, "shoumik.dabir@gmail.com"); c == nil || c.ContactKey != "shoumik dabir" || c.Via != "email" {
		t.Fatalf("shoumik.dabir@gmail.com should match shoumik dabir by email local-part: %+v", c)
	}
	if c := candFor(rev, "michaeltrinh19@gmail.com"); c == nil || c.ContactKey != "michael trinh" || c.Via != "email" {
		t.Fatalf("michaeltrinh19 should match michael trinh by local-part (title/name omit him): %+v", c)
	}
	if candFor(rev, "stranger@random.com") != nil {
		t.Fatal("an attendee matching no existing contact must not be proposed")
	}

	// ---- dismiss one pair → it never returns ----
	if err := svc.DismissEmailCandidate("shoumik.dabir@gmail.com", "shoumik dabir"); err != nil {
		t.Fatal(err)
	}
	if candFor(mustReview(t, svc), "shoumik.dabir@gmail.com") != nil {
		t.Fatal("a dismissed email pair must not reappear")
	}

	// ---- before linking: calendar last-met blank; neglect basis = mentions ----
	before, _ := svc.Page("shoumik dabir", now)
	if before.LastMet != "" || before.LastMentioned != "2026-07-01" || before.NeglectBasis != "mentions" {
		t.Fatalf("before link: met=%q mentioned=%q basis=%q", before.LastMet, before.LastMentioned, before.NeglectBasis)
	}

	// ---- link dabir@anfavc.com → shoumik's TRUE last-met is the 07-02 meeting ----
	if err := svc.ConfirmEmail("shoumik dabir", "shoumik dabir", "dabir@anfavc.com"); err != nil {
		t.Fatal(err)
	}
	p, _ := svc.Page("shoumik dabir", now)
	if p.LastMet != "2026-07-02" {
		t.Fatalf("calendar last-met = %q, want 2026-07-02", p.LastMet)
	}
	if p.LastMentioned != "2026-07-01" {
		t.Fatalf("last-mentioned (notes) must stay 2026-07-01, got %q", p.LastMentioned)
	}
	if p.NeglectBasis != "meetings" {
		t.Fatalf("with a linked email, neglect basis must be meetings, got %q", p.NeglectBasis)
	}
	if len(p.Meetings) == 0 || p.Meetings[0].Date != "2026-07-02" {
		t.Fatalf("meetings section (newest-first) = %+v", p.Meetings)
	}
	if len(p.Emails) != 1 || !strings.EqualFold(p.Emails[0], "dabir@anfavc.com") {
		t.Fatalf("emails section = %+v", p.Emails)
	}

	// linking removed it from review; dismissed stays gone; michael still pending
	rev3 := mustReview(t, svc)
	if candFor(rev3, "dabir@anfavc.com") != nil {
		t.Fatal("a linked email must leave the review queue")
	}
	if candFor(rev3, "michaeltrinh19@gmail.com") == nil {
		t.Fatal("michael's unlinked email should still be in review")
	}

	// ---- link michael by his local-part match → last-met is the group meeting ----
	if err := svc.ConfirmEmail("michael trinh", "michael trinh", "michaeltrinh19@gmail.com"); err != nil {
		t.Fatal(err)
	}
	mp, _ := svc.Page("michael trinh", now)
	if mp.LastMet != "2026-03-26" {
		t.Fatalf("michael calendar last-met = %q, want 2026-03-26 (email beats title/name)", mp.LastMet)
	}
}

func TestNameTokenMatch(t *testing.T) {
	// real false positives the old substring match produced (must now be false)
	bad := [][2]string{
		{"eli front", "Cornelius Payne"}, {"eli", "Cornelius Payne"}, {"eli", "Angelina Smith"},
		{"eli", "Lauren Selig"}, {"rico meinl", "Villani Federico"}, {"sean thiessen", "Sean Spencer"},
		{"jim o'neill", "Jim Curran"}, {"andrew schlack", "Andrew"},
		// lone-first-name aliases must not match a different person who shares it
		{"sean", "Sean Spencer"}, {"andrew", "Andrew"}, {"jim", "Jim Curran"},
	}
	for _, c := range bad {
		if nameTokenMatch(c[0], c[1]) {
			t.Errorf("nameTokenMatch(%q,%q) = true, want false (mid-word/partial)", c[0], c[1])
		}
	}
	// legitimate matches (order-independent; apostrophes/initials tolerated)
	good := [][2]string{
		{"shoumik dabir", "Shoumik Dabir"}, {"sam koplar", "Sam Koplar"}, {"john sledge", "John Sledge"},
		{"michael levin", "Levin, Michael"}, {"jim o'neill", "Jim O'Neill"}, {"eli front", "Eli Front"},
	}
	for _, c := range good {
		if !nameTokenMatch(c[0], c[1]) {
			t.Errorf("nameTokenMatch(%q,%q) = false, want true", c[0], c[1])
		}
	}
}

func mustReview(t *testing.T, svc *Service) []EmailCandidate {
	t.Helper()
	rev, err := svc.EmailReview(now)
	if err != nil {
		t.Fatal(err)
	}
	return rev
}

func candFor(rev []EmailCandidate, email string) *EmailCandidate {
	for i := range rev {
		if strings.EqualFold(rev[i].Email, email) {
			return &rev[i]
		}
	}
	return nil
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
