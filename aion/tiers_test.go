package aion

import (
	"sort"
	"strings"
	"testing"
)

// The owner's 16 held notes (2026-09-21). Names only — the test never opens
// a held body, and the assertion is that the DATA file holds exactly these.
var heldNotes2026_09_21 = []string{
	"2026-08-31 - 2026-09-04 v job offer.md",
	"2026-08-26 heye equity david sync.md",
	"2026-06-01 heye immigration sync.md",
	"2026-04-07 heye immigration sync.md",
	"2026-03-25 heye & benjamin sync.md",
	"2026-03-25 georges sync.md",
	"2026-03-27 rich and moji butterfly.md",
	"2026-07-22 specialt mri visit.md",
	"2026-01-20 moufeed intro fda feedback.md",
	"2026-08-11 jackson sync.md",
	"2026-09-08 johnny and tucker sync.md",
	"2026-05-27 steven reintro.md",
	"2026-09-03 ljv raise feedback.md",
	"2026-08-07 rj sync.md",
	"2026-05-05 artemy sync.md",
	"2026-05-05 jack ruhl sync.md",
}

// Tier-map completeness: every one of the 230 notes carries exactly one of
// the three tiers, the census matches the owner's counts, and the held set is
// exactly the owner's list.
func TestTierMapCompleteness(t *testing.T) {
	tm, err := LoadTierMap()
	if err != nil {
		t.Fatal(err)
	}
	if len(tm) != 230 {
		t.Fatalf("tier map has %d entries, want 230", len(tm))
	}
	c := tm.Counts()
	if c.Open != 83 || c.Internal != 131 || c.Held != 16 {
		t.Fatalf("census = %+v, want open 83 · internal 131 · held 16", c)
	}
	if c.Open+c.Internal+c.Held != len(tm) {
		t.Fatal("an entry carries no tier — Validate should have refused it")
	}
	var held []string
	for name, e := range tm {
		if e.Tier == TierHeld {
			held = append(held, name)
		}
		if e.Reason == "" {
			t.Errorf("%s has no reason", name)
		}
	}
	sort.Strings(held)
	want := append([]string(nil), heldNotes2026_09_21...)
	sort.Strings(want)
	if strings.Join(held, "\n") != strings.Join(want, "\n") {
		t.Fatalf("held set differs from the owner's list:\n got %v\nwant %v", held, want)
	}
	for _, name := range want {
		if tm.KairosEligible(name) || tm.PortalEligible(name) {
			t.Fatalf("%s is held but reads as eligible", name)
		}
	}
	if !tm.PortalEligible("2025-04-13 ultrasound memo.md") || !tm.KairosEligible("2025-04-13 ultrasound memo.md") {
		t.Fatal("an open note must be eligible for both channels")
	}
	if tm.PortalEligible("2025-10-29 shadowing ellie with melyne.md") || !tm.KairosEligible("2025-10-29 shadowing ellie with melyne.md") {
		t.Fatal("an internal note is kairos-only")
	}
	if tm.KairosEligible("2099-01-01 not in the map.md") || tm.PortalEligible("2099-01-01 not in the map.md") {
		t.Fatal("an unmapped note must never be eligible")
	}
}

// The map is closed: a fourth tier, a missing tier, or a non-basename key
// is refused at load rather than treated as some default.
func TestTierMapRefusesUnknownTier(t *testing.T) {
	for _, bad := range []string{
		`{"2026-01-01 x.md": {"tier": "public", "reason": "", "bytes": 1}}`,
		`{"2026-01-01 x.md": {"reason": "no tier", "bytes": 1}}`,
		`{"log/2026-01-01 x.md": {"tier": "open", "reason": "", "bytes": 1}}`,
		`{"2026-01-01 x.txt": {"tier": "open", "reason": "", "bytes": 1}}`,
		`{}`,
	} {
		if _, err := ParseTierMap([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if _, err := ParseTierMap([]byte(`{"2026-01-01 x.md": {"tier": "held", "reason": "r", "bytes": 1}}`)); err != nil {
		t.Fatal(err)
	}
}
