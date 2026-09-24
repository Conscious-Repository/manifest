package contacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeMail struct{ by map[string]MailTouch }

func (f fakeMail) LatestExchange(_ context.Context, address string) (MailTouch, bool, error) {
	t, ok := f.by[address]
	return t, ok, nil
}

// The last touch is the latest dated evidence across calendar, mail,
// transcript and note — a meeting wins a tie — and the next touch is the
// soonest confirmed-email calendar event. A note dated after today is a plan.
func TestTouchesPickLatestEvidenceAndSoonestEvent(t *testing.T) {
	cal := fakeCal{
		past:     []Event{{Start: time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC), Title: "coffee", Attendees: []Attendee{{Name: "Chris", Email: "chris@atria.vc"}}}},
		upcoming: []Event{{Start: time.Date(2026, 7, 9, 15, 0, 0, 0, time.UTC), Title: "follow-up", Attendees: []Attendee{{Email: "chris@atria.vc"}}}, {Start: time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC), Title: "someone else", Attendees: []Attendee{{Email: "x@y.z"}}}},
	}
	svc, ix, root := harnessCal(t, cal)
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("chris leiter.md", "---\ncategories: [people]\nemail: [chris@atria.vc]\n---\nAtria.\n")
	write("2026-06-25 chris sync.md", "---\ncategories: [sync]\n---\n[[chris leiter]] notes\n")
	write("2026-08-01 plan.md", "---\ncategories: [plan]\n---\nwill ping [[chris leiter]]\n") // after `now`: a plan, not a touch
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}

	last, next := svc.Touches("chris leiter", now)
	if last.Kind != "note" || last.Date != "2026-06-25" || last.PersonKey != "chris leiter" {
		t.Fatalf("note should be the latest touch: %+v", last)
	}
	if next.Kind != "upcoming" || next.Date != "2026-07-09" || next.Title != "follow-up" {
		t.Fatalf("next should be the soonest confirmed-email event: %+v", next)
	}

	// mail newer than the note takes over once the mailbox is asked
	svc.UseMail(fakeMail{by: map[string]MailTouch{"chris@atria.vc": {Date: "2026-06-30", Subject: "re: deck", Sent: true}}}, filepath.Join(t.TempDir(), "mail.json"))
	if n, err := svc.RefreshMail(context.Background(), []string{"chris@atria.vc"}); err != nil || n != 1 {
		t.Fatalf("refresh = %d, %v", n, err)
	}
	last, _ = svc.Touches("chris leiter", now)
	if last.Kind != "email" || last.Date != "2026-06-30" || last.Title != "re: deck" {
		t.Fatalf("mail should win: %+v", last)
	}
	// a meeting on the same day as the mail outranks it
	svc.UseMail(fakeMail{by: map[string]MailTouch{"chris@atria.vc": {Date: "2026-06-20"}}}, filepath.Join(t.TempDir(), "mail2.json"))
	if _, err := svc.RefreshMail(context.Background(), []string{"chris@atria.vc"}); err != nil {
		t.Fatal(err)
	}
	svc.invalidateMeetings()
	last, _ = svc.Touches("chris leiter", now)
	if last.Date != "2026-06-25" || last.Kind != "note" {
		t.Fatalf("latest wins again: %+v", last)
	}
	if a, b := (Touch{Date: "2026-06-20", Kind: "met"}), (Touch{Date: "2026-06-20", Kind: "email"}); !a.Later(b) || b.Later(a) {
		t.Fatal("a meeting outranks mail on the same day")
	}

	// a key with nothing dated yields nothing
	if l, n := svc.Touches("alice", now); l.Date != "" || n.Date != "" {
		t.Fatalf("alice has no touches: %+v %+v", l, n)
	}
}

// EnsureNote creates the lowercase people note once and returns the existing
// one afterwards; NoteFor only answers for people notes.
func TestEnsureNoteCreatesOnceAndNoteForIsPeopleOnly(t *testing.T) {
	svc, _, root := harness(t)
	if _, ok := svc.NoteFor("shoumik dabir"); ok {
		t.Fatal("a bare link target is not a note")
	}
	key, rel, err := svc.EnsureNote("shoumik dabir", "Shoumik Dabir")
	if err != nil || key != "shoumik dabir" || rel != "shoumik dabir.md" {
		t.Fatalf("ensure = %q %q %v", key, rel, err)
	}
	if _, err := os.Stat(filepath.Join(root, "shoumik dabir.md")); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddEmails(rel, []string{"Shoumik@Example.com"}); err != nil {
		t.Fatal(err)
	}
	if got := svc.EmailsOf("shoumik dabir"); len(got) != 1 || got[0] != "shoumik@example.com" {
		t.Fatalf("emails = %v", got)
	}
	again, rel2, err := svc.EnsureNote("shoumik dabir", "Someone Else")
	if err != nil || again != key || rel2 != rel {
		t.Fatalf("second ensure must return the same note: %q %q %v", again, rel2, err)
	}
	if rel, ok := svc.NoteFor("shoumik dabir"); !ok || rel != "shoumik dabir.md" {
		t.Fatalf("NoteFor = %q %v", rel, ok)
	}
}
