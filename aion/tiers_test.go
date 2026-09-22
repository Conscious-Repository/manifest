package aion

import (
	"os"
	"path/filepath"
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

// SourceTier is the export's source: → tier predicate: only a bare
// "log/<basename>" (with or without .md) resolves; every other shape is
// "not a transcript" and an unmapped log/ note resolves to nothing.
func TestTierMapSourceTier(t *testing.T) {
	tm := TierMap{
		"2026-06-01 heye immigration sync.md": {Tier: TierHeld, Reason: "immigration"},
		"2026-01-19 aion team sync.md":        {Tier: TierOpen, Reason: "team sync"},
		"2025-12-15 mechanisms sync.md":       {Tier: TierInternal, Reason: "internal ops"},
	}
	cases := []struct {
		source string
		tier   Tier
		ok     bool
	}{
		{"log/2026-06-01 heye immigration sync", TierHeld, true},
		{"log/2026-06-01 heye immigration sync.md", TierHeld, true},
		{"  log/2026-06-01 heye immigration sync|the sync  ", TierHeld, true},
		{"log/2026-06-01 heye immigration sync#Transcript", TierHeld, true},
		{"log/2026-01-19 aion team sync", TierOpen, true},
		{"log/2025-12-15 mechanisms sync", TierInternal, true},
		{"log/2026-09-20 rj sync", "", false},           // unmapped log/ note → no tier
		{"2026-06-01 heye immigration sync", "", false}, // no log/ prefix → not a transcript
		{"intrinsic/2026-06-01 heye immigration sync", "", false},
		{"updates for justin", "", false},
		{"log/", "", false},
		{"log/sub/2026-06-01 heye immigration sync", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		tier, ok := tm.SourceTier(c.source)
		if ok != c.ok || tier != c.tier {
			t.Errorf("SourceTier(%q) = (%q, %v), want (%q, %v)", c.source, tier, ok, c.tier, c.ok)
		}
	}
	if !tm.HeldSource("log/2026-06-01 heye immigration sync") || tm.HeldSource("log/2026-01-19 aion team sync") || tm.HeldSource("log/2026-09-20 rj sync") {
		t.Fatal("HeldSource must be true for the held note only")
	}
}

// Tier-map completeness: every one of the 230 notes carries exactly one of
// the three tiers, the census matches the owner's counts, and the held set is
// exactly the owner's list.
func TestTierMapCompleteness(t *testing.T) {
	tm, err := LoadTierMap()
	if err != nil {
		t.Fatal(err)
	}
	// The census is NOT frozen: the approvals card legitimately grows this map when the
	// owner accepts a suggested tier (2026-09-21 onward). What must hold is that every
	// entry carries exactly one valid tier and a reason, and that the known HELD set is
	// intact — not that the totals match a snapshot from the day it was authored.
	if len(tm) < 230 {
		t.Fatalf("tier map has %d entries, want at least the 230 originally classified", len(tm))
	}
	c := tm.Counts()
	if c.Open < 83 || c.Internal < 131 || c.Held < 16 {
		t.Fatalf("census shrank: %+v (want open >=83 · internal >=131 · held >=16)", c)
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

// MarshalTierMap reproduces the checked-in data file byte-for-byte, so an
// owner decision recorded from the approvals inbox is a one-entry diff.
func TestMarshalTierMapRoundTrip(t *testing.T) {
	tm, err := LoadTierMap()
	if err != nil {
		t.Fatal(err)
	}
	b, err := MarshalTierMap(tm)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(tierMapJSON) {
		t.Fatalf("MarshalTierMap does not round-trip the data file (len %d vs %d)", len(b), len(tierMapJSON))
	}
}

// WriteTierMapEntry: adds or overwrites one note's entry in a copy of the
// data file, keeps every other entry, refuses a tier outside the vocabulary
// and a missing file (it never invents a fresh map).
func TestWriteTierMapEntry(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tier-map.json")
	if err := os.WriteFile(p, tierMapJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	const name = "2026-09-20 rj sync.md"
	if err := WriteTierMapEntry(p, name, TierEntry{Tier: TierHeld, Reason: "owner · approvals inbox (granola)", Bytes: 12}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	m, err := ParseTierMap(raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := tierMapBaseCount(t) + 1; len(m) != want {
		t.Fatalf("entries = %d, want %d", len(m), want)
	}
	if e := m[name]; e.Tier != TierHeld || e.Reason != "owner · approvals inbox (granola)" || e.Bytes != 12 {
		t.Fatalf("entry = %+v", e)
	}
	if !m.HeldSource("log/2026-09-20 rj sync") {
		t.Fatal("the recorded tier must govern the export predicate")
	}
	// override in place: same count, new tier
	if err := WriteTierMapEntry(p, name, TierEntry{Tier: TierOpen, Reason: "owner · approvals inbox (granola)", Bytes: 12}); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(p)
	m, _ = ParseTierMap(raw)
	if want := tierMapBaseCount(t) + 1; len(m) != want || m[name].Tier != TierOpen {
		t.Fatalf("override failed: %d entries, tier %q", len(m), m[name].Tier)
	}
	if strings.Contains(string(raw), "\\u0026") || !strings.Contains(string(raw), "heye & benjamin") {
		t.Fatal("rewrite HTML-escaped the file (\"&\" must stay literal)")
	}
	if strings.HasSuffix(string(raw), "\n") {
		t.Fatal("rewrite added a trailing newline the data file does not carry")
	}
	if err := WriteTierMapEntry(p, name, TierEntry{Tier: "public"}); err == nil {
		t.Fatal("accepted a tier outside open|internal|held")
	}
	if err := WriteTierMapEntry(p, "log/"+name, TierEntry{Tier: TierOpen}); err == nil {
		t.Fatal("accepted a non-basename key")
	}
	if err := WriteTierMapEntry(filepath.Join(t.TempDir(), "missing.json"), name, TierEntry{Tier: TierOpen}); err == nil {
		t.Fatal("invented a fresh map for a missing file")
	}
	if _, err := os.Stat(p + ".tmp"); err == nil {
		t.Fatal("temp file left behind")
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

// tierMapBaseCount is the entry count of the embedded fixture the write tests seed from.
// The census is not frozen (the approvals card legitimately grows the map), so the tests
// derive their expectation from the fixture rather than a number that goes stale.
func tierMapBaseCount(t *testing.T) int {
	t.Helper()
	m, err := ParseTierMap([]byte(tierMapJSON))
	if err != nil {
		t.Fatal(err)
	}
	return len(m)
}
