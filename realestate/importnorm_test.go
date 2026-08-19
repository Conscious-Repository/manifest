package realestate

import (
	"strings"
	"testing"
)

func TestNormalizeDateForms(t *testing.T) {
	cases := []struct {
		in       string
		dayFirst bool
		want     string
	}{
		{"08/18/2026", false, "2026-08-18"}, // the export that started this
		{"8/4/2026", false, "2026-08-04"},
		{"01/05/26", false, "2026-01-05"},
		{"2026-08-18", false, "2026-08-18"}, // already ISO — the bank feed's form
		{"2026/08/18", false, "2026-08-18"},
		{"18/08/2026", true, "2026-08-18"}, // day-first file
		{"08-18-2026", false, "2026-08-18"},
		{"2026-08-18 14:45:00", false, "2026-08-18"}, // clock dropped
		{"08/18/2026 2:45 PM", false, "2026-08-18"},
		{"Aug 18, 2026", false, "2026-08-18"},
		{"August 18, 2026", false, "2026-08-18"},
		{"18 Aug 2026", false, "2026-08-18"},
	}
	for _, c := range cases {
		got, ok := NormalizeDate(c.in, c.dayFirst)
		if !ok || got != c.want {
			t.Errorf("NormalizeDate(%q, %v) = %q,%v want %q", c.in, c.dayFirst, got, ok, c.want)
		}
	}
	// a non-date must refuse rather than land an unmatchable key
	for _, bad := range []string{"", "Balance", "13/13/2026", "02/30/2026", "not a date", "2026"} {
		if got, ok := NormalizeDate(bad, false); ok {
			t.Errorf("NormalizeDate(%q) should refuse, got %q", bad, got)
		}
	}
}

func TestDateOrderNeedsUnambiguousEvidence(t *testing.T) {
	// a US export: the 18 can only be a day, and it sits second
	if DateOrder([]string{"08/18/2026", "01/05/2026"}) {
		t.Error("month-first column read as day-first")
	}
	// a day-first export: the 18 sits first
	if !DateOrder([]string{"18/08/2026", "05/01/2026"}) {
		t.Error("day-first column not detected")
	}
	// no evidence at all → month-first (the US default)
	if DateOrder([]string{"01/05/2026", "02/03/2026"}) {
		t.Error("ambiguous column should default to month-first")
	}
	// contradictory evidence → month-first, never a half-flipped column
	if DateOrder([]string{"18/08/2026", "08/18/2026"}) {
		t.Error("conflicting evidence should default to month-first")
	}
	// ISO rows carry no ordering evidence
	if DateOrder([]string{"2026-08-18"}) {
		t.Error("ISO column should not read as day-first")
	}
}

func TestSuggestSignFollowsTheMajority(t *testing.T) {
	// the real export: 279 charges negative, 14 deposits positive
	if got := SuggestSign([]float64{-1075, -60.96, -937.21, 1750}); got != SignExpenseNegative {
		t.Errorf("mostly-negative file should suggest %q, got %q", SignExpenseNegative, got)
	}
	if got := SuggestSign([]float64{1075, 60.96, 937.21, -1750}); got != SignExpensePositive {
		t.Errorf("mostly-positive file should suggest %q, got %q", SignExpensePositive, got)
	}
	if !SignConventionOK(SignExpenseNegative) || SignConventionOK("whatever") {
		t.Error("SignConventionOK is wrong")
	}
}

func TestTidyVendorStripsBankNoise(t *testing.T) {
	cases := []struct{ in, want string }{
		{"MY CPA GUY               MY CPA GUY", "MY CPA GUY"},
		{"THE HOME DEPOT THE HOME DEPOT #   BRENTWOOD     MO", "THE HOME DEPOT # BRENTWOOD MO"},
		{"Spectrum          SAINT LOU IS  MOSpectrum", "Spectrum SAINT LOU IS MOSpectrum"},
		{"CUP            HBS       2700226        9281085585ACHPARNBR =    026219007886305   /ORGNAME  ANDERSON", "CUP HBS 2700226 9281085585"},
		{"WM SUPERCENTER 1900 MAPLEWOOD COM MAPLEWOOD     MO", "WM SUPERCENTER 1900 MAPLEWOOD COM MAPLEWOOD MO"},
	}
	for _, c := range cases {
		if got := TidyVendor(c.in); got != c.want {
			t.Errorf("TidyVendor(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	if got := TidyVendor(strings.Repeat("X", 200)); len(got) != 60 {
		t.Errorf("vendor should cap at 60, got %d", len(got))
	}
}
