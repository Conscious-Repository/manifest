package realestate

import (
	"testing"
	"time"
)

// The unit-mix grammar is tolerant: labeled segments parse, unknown segments
// ride the label, missing segments stay zero.
func TestParseUnits(t *testing.T) {
	units := ParseUnits(`["A | 2bd/1ba | 850sqft | rent 1100", "B | 2bd/1ba | 850sqft | rent 1100", "garden studio | rent 750"]`)
	if len(units) != 3 {
		t.Fatalf("want 3 units, got %d: %+v", len(units), units)
	}
	a := units[0]
	if a.Label != "A" || a.Beds != 2 || a.Baths != 1 || a.Sqft != 850 || a.Rent != 1100 {
		t.Fatalf("unit A: %+v", a)
	}
	if units[2].Label != "garden studio" || units[2].Rent != 750 {
		t.Fatalf("free-label unit: %+v", units[2])
	}
	if RentMonthly(units) != 2950 {
		t.Fatalf("rent total: %v", RentMonthly(units))
	}
	if got := ParseUnits(""); got != nil {
		t.Fatalf("empty units: %+v", got)
	}
}

// Free numeric frontmatter keys read as measurables; reserved fields never do.
func TestParseMeasurables(t *testing.T) {
	m := ParseMeasurables(map[string]string{
		"windows": "14", "roof-squares": "22.5", "lat": "38.65",
		"address": "751 Bayard", "status": "construction", "fence-lf": "120",
		"work-start": "2026-07-25", "notes": "not a number",
	})
	if len(m) != 3 || m["windows"] != 14 || m["roof-squares"] != 22.5 || m["fence-lf"] != 120 {
		t.Fatalf("measurables: %+v", m)
	}
}

// The snapshot freezes rocks, node ests, measurables, inputs, assumptions.
func TestBuildUnderwriteSnapshot(t *testing.T) {
	p := Property{
		UnitMix:     ParseUnits(`["A | rent 1100"]`),
		Measurables: map[string]float64{"windows": 14},
		Work: ParseWork([]string{
			"- [ ] Exterior [est:: 2000] [work:: exterior]",
			"    - [ ] roof [est:: 9000] [work:: exterior/roof]",
			"- [ ] Demo [work:: demo]",
		}),
	}
	src := SourceMoney{PurchasePrice: 18000, HardCosts: 100000, ContingencyPct: 0.1}
	lock := BuildUnderwriteSnapshot(p, src, map[string]float64{"exit_cap_rate": 0.07}, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	if lock.HardTotal != 11000 {
		t.Fatalf("hard total: %v", lock.HardTotal)
	}
	if len(lock.Rocks) != 2 || lock.Rocks[0].EstTotal != 11000 {
		t.Fatalf("rocks: %+v", lock.Rocks)
	}
	if lock.NodeEsts["exterior"] != 2000 || lock.NodeEsts["exterior/roof"] != 9000 {
		t.Fatalf("node ests: %+v", lock.NodeEsts)
	}
	if lock.Source.PurchasePrice != 18000 || lock.Assumptions["exit_cap_rate"] != 0.07 {
		t.Fatalf("inputs: %+v", lock)
	}
	if lock.Measurables["windows"] != 14 || len(lock.Units) != 1 {
		t.Fatalf("measurables/units: %+v", lock)
	}
}
