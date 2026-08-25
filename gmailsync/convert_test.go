package gmailsync

import (
	"strings"
	"testing"
	"time"
)

type fakeRoster map[string]string

func (f fakeRoster) PersonByEmail(email string) (string, bool) {
	d, ok := f[strings.ToLower(email)]
	return d, ok
}

func day(s string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04", s)
	return t
}

func TestCleanEmailBodyCutsHistoryAndSignature(t *testing.T) {
	body := "Sounds good — let's start Tuesday.\r\n\r\n" +
		"Inline answer:\n> what about permits?\nAlready filed.\n\n" +
		"-- \nOlga Sobkiv\nArchitect\n\n" +
		"On Tue, Aug 19, 2026 at 9:00 AM Benjamin <me@benjaminbanderson.com> wrote:\n" +
		"> earlier text\n> more earlier text\n"
	got := cleanEmailBody(body)
	if strings.Contains(got, "wrote:") || strings.Contains(got, "earlier text") {
		t.Fatalf("quoted history kept:\n%s", got)
	}
	if strings.Contains(got, "Architect") {
		t.Fatalf("signature kept:\n%s", got)
	}
	if !strings.Contains(got, "> what about permits?") || !strings.Contains(got, "Already filed.") {
		t.Fatalf("interior deliberate quote lost:\n%s", got)
	}
}

func TestThreadNoteShape(t *testing.T) {
	res := fakeRoster{"olga@sobkiv.com": "olga sobkiv"}
	msgs := []Msg{
		{From: "Olga Sobkiv <olga@sobkiv.com>", To: "ben@ooda.group", Internal: day("2026-08-11 09:00"), Body: "Permit sets attached."},
		{From: "Benjamin <ben@ooda.group>", To: "olga@sobkiv.com", Internal: day("2026-08-18 10:00"), Body: "Approved, proceed."},
	}
	note := ThreadNote(msgs, res, "ben@ooda.group")
	for _, want := range []string{
		"olga sobkiv\n",                 // participants line, owner excluded
		"## 2026-08-11 — olga sobkiv\n", // resolved sender
		"## 2026-08-18 — Benjamin\n",    // header display-name fallback
		"Permit sets attached.",
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("note missing %q:\n%s", want, note)
		}
	}
}

func TestNoteFilenameDateRange(t *testing.T) {
	msgs := []Msg{
		{Internal: day("2026-08-11 09:00")},
		{Internal: day("2026-08-18 10:00")},
	}
	if got := NoteFilename(msgs, "Re: Re: Roof / Scope?"); got != "2026-08-11 - 2026-08-18 roof scope.md" {
		t.Fatalf("range filename = %q", got)
	}
	same := []Msg{{Internal: day("2026-08-11 09:00")}}
	if got := NoteFilename(same, "Fwd: Budget"); got != "2026-08-11 budget.md" {
		t.Fatalf("single-day filename = %q", got)
	}
	if got := NoteFilename(same, ""); got != "2026-08-11 email thread.md" {
		t.Fatalf("empty-subject filename = %q", got)
	}
}

func TestCalendarThreadDetection(t *testing.T) {
	invite := Msg{Subject: "Invitation: walkthrough @ Tue", HasCalendar: true, Internal: day("2026-08-11 09:00")}
	rsvp := Msg{Subject: "Accepted: walkthrough @ Tue", Internal: day("2026-08-11 10:00")}
	human := Msg{Subject: "Re: walkthrough", Body: "bringing the drawings", Internal: day("2026-08-11 11:00")}
	if !IsCalendarThread([]Msg{invite, rsvp}) {
		t.Fatal("pure calendar machinery must be skipped")
	}
	if IsCalendarThread([]Msg{invite, human}) {
		t.Fatal("a human reply keeps the thread in")
	}
}

func TestStripHTML(t *testing.T) {
	got := stripHTML(`<div><style>p{}</style><p>Line one</p><p>Line &amp; two</p></div>`)
	if !strings.Contains(got, "Line one") || !strings.Contains(got, "Line & two") {
		t.Fatalf("stripHTML = %q", got)
	}
	if strings.Contains(got, "<") {
		t.Fatalf("tags kept: %q", got)
	}
}
