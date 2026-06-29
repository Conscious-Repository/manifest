package calendar

import (
	"testing"
	"time"
)

func at(h, m int) time.Time { return time.Date(2026, 6, 29, h, m, 0, 0, time.UTC) }

func tokens(slots []Slot) []string {
	out := make([]string, len(slots))
	for i, s := range slots {
		out[i] = s.Token
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestEventsToSlots(t *testing.T) {
	day := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		ev   Event
		want []string
	}{
		{"all-day skipped", Event{AllDay: true, Start: day, End: day.Add(24 * time.Hour)}, nil},
		{"multi-day >=24h skipped", Event{Start: at(9, 0), End: at(9, 0).Add(48 * time.Hour)}, nil},
		{"midnight-spanning block skipped", Event{Start: at(22, 0), End: time.Date(2026, 6, 30, 2, 0, 0, 0, time.UTC)}, nil},
		{"30-min meeting -> one slot", Event{Start: at(9, 30), End: at(10, 0)}, []string{"9:30A"}},
		{"90-min meeting -> three slots", Event{Start: at(9, 0), End: at(10, 30)}, []string{"9:00A", "9:30A", "10:00A"}},
		{"non-aligned start floors down", Event{Start: at(9, 40), End: at(10, 0)}, []string{"9:30A"}},
		{"ends exactly at midnight is not multi-day", Event{Start: at(23, 0), End: time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)}, []string{"11:00P", "11:30P"}},
		{"afternoon PM tokens", Event{Start: at(14, 0), End: at(14, 30)}, []string{"2:00P"}},
	}
	for _, c := range cases {
		got := tokens(EventsToSlots([]Event{c.ev}, day, time.UTC))
		if !eq(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestEventsToSlotsOffDayClamped(t *testing.T) {
	day := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	// An event entirely on a different date contributes nothing.
	other := Event{Start: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 7, 1, 9, 30, 0, 0, time.UTC)}
	if got := EventsToSlots([]Event{other}, day, time.UTC); len(got) != 0 {
		t.Fatalf("off-day event should map to no slots, got %v", tokens(got))
	}
}

func TestEventsToSlotsFirstWinsCollision(t *testing.T) {
	day := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	a := Event{ID: "a", Title: "Standup", Start: at(9, 0), End: at(9, 30)}
	b := Event{ID: "b", Title: "Other", Start: at(9, 15), End: at(9, 45)}
	slots := EventsToSlots([]Event{b, a}, day, time.UTC) // pass out of order
	if len(slots) == 0 || slots[0].Token != "9:00A" || slots[0].EventID != "a" {
		t.Fatalf("earliest event should win the 9:00A slot: %+v", slots)
	}
}
