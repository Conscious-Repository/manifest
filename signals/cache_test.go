package signals

import (
	"testing"
	"time"

	"manifest/contacts"
)

type countingLister struct {
	rows  []contacts.Contact
	calls int
}

func (c *countingLister) List(time.Time) ([]contacts.Contact, error) { c.calls++; return c.rows, nil }

// WithCache serves one pass for its ttl; a verdict drops the pass at once.
func TestActiveCacheServesOnePassUntilVerdict(t *testing.T) {
	src := &countingLister{rows: []contacts.Contact{{Key: "fred lee", Display: "Fred Lee", Cold: true, DaysSince: 31, NeglectBasis: "meetings", LastMet: "2026-07-20"}}}
	store, _ := NewStore(t.TempDir())
	svc := New(store, ColdContacts(src)).WithCache(time.Minute)
	if len(svc.Active(now)) != 1 || len(svc.Active(now)) != 1 || svc.Count(now) != 1 {
		t.Fatal("cold contact should surface")
	}
	if src.calls != 1 {
		t.Fatalf("cached pass recomputed: %d emitter calls", src.calls)
	}
	if err := svc.Dismiss("contact-cold:fred lee", "meetings|2026-07-20"); err != nil {
		t.Fatal(err)
	}
	if len(svc.Active(now)) != 0 || src.calls != 2 {
		t.Fatalf("dismiss must drop the cached pass: %d signals, %d calls", len(svc.Active(now)), src.calls)
	}
	// a traced read is always a fresh compute and never replaces the cache
	var trace []Timing
	svc.ActiveTraced(now, &trace)
	if len(trace) != 1 || src.calls != 3 {
		t.Fatalf("traced pass: %d timings, %d calls", len(trace), src.calls)
	}
	svc.Invalidate()
	svc.Active(now)
	if src.calls != 4 {
		t.Fatalf("invalidate must force a compute: %d calls", src.calls)
	}
}

// The cached lister holds a contacts read for its ttl and never caches a failure.
func TestColdContactsCachedHoldsTheList(t *testing.T) {
	src := &countingLister{rows: []contacts.Contact{{Key: "a", Display: "A", Cold: true, DaysSince: 40}}}
	e := ColdContactsCached(src, time.Minute)
	for i := 0; i < 3; i++ {
		if sigs, err := e.Emit(now); err != nil || len(sigs) != 1 {
			t.Fatal(sigs, err)
		}
	}
	if src.calls != 1 {
		t.Fatalf("list re-read within ttl: %d", src.calls)
	}
}
