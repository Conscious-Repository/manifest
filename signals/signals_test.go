package signals

import (
	"testing"
	"time"

	"manifest/contacts"
	"manifest/goals"
)

var now = time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC) // Q3, past midpoint (Aug 15)

type fakeContacts struct {
	rows []contacts.Contact
	err  error
}

func (f fakeContacts) List(time.Time) ([]contacts.Contact, error) { return f.rows, f.err }

func TestColdEmitter(t *testing.T) {
	src := fakeContacts{rows: []contacts.Contact{
		{Key: "fred lee", Display: "Fred Lee", Cold: true, DaysSince: 31, NeglectBasis: "meetings", LastMet: "2026-07-20"},
		{Key: "warm gal", Display: "Warm Gal", Cold: false},
	}}
	sigs, err := ColdContacts(src).Emit(now)
	if err != nil || len(sigs) != 1 {
		t.Fatalf("want 1 cold signal, got %d err=%v", len(sigs), err)
	}
	s := sigs[0]
	if s.ID != "contact-cold:fred lee" || s.ActHref != "#/contacts/fred%20lee" || s.Hash != "meetings|2026-07-20" {
		t.Fatalf("signal = %+v", s)
	}
}

// The dismissal re-arm is the load-bearing behavior: suppressed while the hash
// holds, re-fires the instant the underlying interaction date changes.
func TestDismissReArmsOnHashChange(t *testing.T) {
	src := &fakeContacts{rows: []contacts.Contact{
		{Key: "fred lee", Display: "Fred Lee", Cold: true, DaysSince: 31, NeglectBasis: "meetings", LastMet: "2026-07-20"},
	}}
	store, _ := NewStore(t.TempDir())
	svc := New(store, ColdContacts(src))

	if len(svc.Active(now)) != 1 {
		t.Fatal("cold contact should surface before dismissal")
	}
	// dismiss at the current hash → suppressed
	if err := svc.Dismiss("contact-cold:fred lee", "meetings|2026-07-20"); err != nil {
		t.Fatal(err)
	}
	if len(svc.Active(now)) != 0 {
		t.Fatal("dismissed cold contact must be suppressed while the hash holds")
	}
	// a transient degraded read (basis flips) must NOT resurface it — the hash
	// changed, but so did the state; re-arm is correct here...
	src.rows[0].LastMet = "2026-08-18" // they met again, then re-cooled: NEW date
	if len(svc.Active(now)) != 1 {
		t.Fatal("a genuinely changed condition (new interaction date) must re-arm")
	}
}

func TestSnoozeLapses(t *testing.T) {
	src := fakeContacts{rows: []contacts.Contact{
		{Key: "x", Display: "X", Cold: true, DaysSince: 40, NeglectBasis: "mentions", LastMentioned: "2026-07-01"},
	}}
	store, _ := NewStore(t.TempDir())
	svc := New(store, ColdContacts(src))
	_ = svc.Snooze("contact-cold:x", now.Add(48*time.Hour))
	if len(svc.Active(now)) != 0 {
		t.Fatal("snoozed signal hidden")
	}
	if len(svc.Active(now.Add(72*time.Hour))) != 1 {
		t.Fatal("snooze must lapse")
	}
}

// An emitter that errors contributes nothing but must not be read as "all
// conditions absent" (which would wipe dismissals) — Active just skips it.
func TestEmitterErrorIsNotAllClear(t *testing.T) {
	src := fakeContacts{err: errBoom{}}
	store, _ := NewStore(t.TempDir())
	svc := New(store, ColdContacts(src))
	if got := svc.Active(now); len(got) != 0 {
		t.Fatalf("errored emitter must yield no signals, got %d", len(got))
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

// rock-stalled over a small in-memory goals doc.
type fakeGoals struct{ doc *goals.Doc }

func (f fakeGoals) Load() *goals.Doc { return f.doc }

func TestStalledRocks(t *testing.T) {
	doc := &goals.Doc{Areas: []*goals.Area{{
		Name: "Aion",
		Rocks: []*goals.Goal{
			// stalled: current quarter, no movement, incomplete
			{ID: "aion/series-a", Text: "Series A", Quarter: "2026-Q3", Moved: "2026-07-01",
				Children: []*goals.Goal{{Children: []*goals.Goal{{Checked: false}}}}},
			// not stalled: moved recently
			{ID: "aion/fresh", Text: "Fresh", Quarter: "2026-Q3", Moved: "2026-08-18",
				Children: []*goals.Goal{{Children: []*goals.Goal{{Checked: false}}}}},
			// not stalled: complete
			{ID: "aion/done", Text: "Done", Quarter: "2026-Q3", Moved: "2026-07-01",
				Children: []*goals.Goal{{Children: []*goals.Goal{{Checked: true}}}}},
			// not stalled: last quarter
			{ID: "aion/old", Text: "Old", Quarter: "2026-Q2", Moved: "2026-05-01",
				Children: []*goals.Goal{{Children: []*goals.Goal{{Checked: false}}}}},
		},
	}}}
	sigs, _ := StalledRocks(fakeGoals{doc}).Emit(now)
	if len(sigs) != 1 || sigs[0].ID != "rock-stalled:aion/series-a" {
		t.Fatalf("want only the stalled current-quarter rock, got %+v", sigs)
	}
	if sigs[0].GoalID != "aion/series-a" || sigs[0].ActHref != "#/goals/aion%2Fseries-a" {
		t.Fatalf("rock signal fields = %+v", sigs[0])
	}
}

func TestPastMidpoint(t *testing.T) {
	if pastMidpoint("2026-Q3", time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("early July is before the Q3 midpoint")
	}
	if !pastMidpoint("2026-Q3", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("September is past the Q3 midpoint")
	}
}
