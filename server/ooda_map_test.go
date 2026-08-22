package server

import "testing"

// The MAP view draws and lists exactly three tax statuses. An unrecognized
// one (audit B13 — "exempt", a typo in an assessor pull) used to pass
// through mapPayload: counted in the legend, never drawn — a parcel silently
// missing from the map. Every unknown clamps to "current".
func TestOodaMapTaxStatusClampsUnknown(t *testing.T) {
	for in, want := range map[string]string{
		"current":    "current",
		"delinquent": "delinquent",
		"lra":        "lra",
		"":           "current",
		"exempt":     "current",
		"Delinquent": "current", // the vocabulary is exact — case included
	} {
		if got := oodaMapTaxStatus(in); got != want {
			t.Errorf("oodaMapTaxStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
