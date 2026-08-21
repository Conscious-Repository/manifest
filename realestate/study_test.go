package realestate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const studyFixture = `{
  "type": "FeatureCollection",
  "metadata": {"snapshot_date": "2026-07-31", "count": 3, "areas": {"53": "Fountain Park"}},
  "features": [
    {"type": "Feature",
     "geometry": {"type": "Polygon", "coordinates": [[[-90.26,38.65],[-90.26,38.66],[-90.25,38.66],[-90.26,38.65]]]},
     "properties": {"address": "1113 BAYARD AV", "owner": "SHACKLEFORD, KEITH",
       "owner_addr": "400 4TH ST", "rec_date": "2018-05-31", "tax_status": "current",
       "tax_bal_due": 0, "assessed": 7700, "land_use": "1110", "parcel_id": "37709540000",
       "ward": "10", "nbrhd": "53", "lat": 38.655, "lng": -90.258}},
    {"type": "Feature",
     "geometry": {"type": "Polygon", "coordinates": [[[-90.27,38.65],[-90.27,38.66],[-90.26,38.66],[-90.27,38.65]]]},
     "properties": {"address": "1161 BAYARD AV", "owner": "LRA", "tax_status": "lra",
       "assessed": 360, "parcel_id": "37709470000"}},
    {"type": "Feature",
     "geometry": {"type": "Polygon", "coordinates": [[[-90.28,38.65],[-90.28,38.66],[-90.27,38.66],[-90.28,38.65]]]},
     "properties": {"address": "1115 BAYARD AV", "owner": "MILLER, PATRICIA A",
       "tax_status": "delinquent", "tax_bal_due": "1,234.56", "assessed": "$360",
       "parcel_id": "37709530000"}}
  ]
}`

func writeStudy(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "study-parcels.geojson")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The study geojson projects into the SAME Parcel shape the vault records use,
// so one client renders both layers — and a study parcel has NO slug, which is
// what tells the UI there is no record to append a note to.
func TestStudyParcelsProjectIntoParcels(t *testing.T) {
	layer, err := LoadStudyParcels(writeStudy(t, studyFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(layer.Parcels) != 3 {
		t.Fatalf("parcels = %d, want 3", len(layer.Parcels))
	}
	if layer.Meta.SnapshotDate != "2026-07-31" || layer.Key == "" {
		t.Fatalf("meta = %+v, key = %q", layer.Meta, layer.Key)
	}
	byID := map[string]Parcel{}
	for _, p := range layer.Parcels {
		if p.Slug != "" {
			t.Fatalf("a study parcel must have no slug (no record behind it): %+v", p)
		}
		if len(p.Features) != 1 {
			t.Fatalf("%s: features = %d, want 1", p.Address, len(p.Features))
		}
		byID[p.ParcelID] = p
	}
	if p := byID["37709540000"]; p.Owner != "SHACKLEFORD, KEITH" || p.Assessed != 7700 ||
		p.TaxStatus != "current" || p.RecDate != "2018-05-31" {
		t.Fatalf("current parcel = %+v", p)
	}
	// the export is not consistent about numbers: "1,234.56" and "$360" are
	// both real shapes, and reading either as 0 would say "owes nothing"
	if p := byID["37709530000"]; p.TaxBalDue != 1234.56 || p.Assessed != 360 {
		t.Fatalf("delinquent parcel numbers = %v / %v, want 1234.56 / 360", p.TaxBalDue, p.Assessed)
	}
	if p := byID["37709470000"]; p.TaxStatus != "lra" || p.Owner != "LRA" {
		t.Fatalf("lra parcel = %+v", p)
	}
}

// A lot already drawn by a richer layer — a research record with the owner's
// notes, or a property we hold — must not draw a second time underneath.
func TestStudyParcelsExceptDropsAlreadyDrawn(t *testing.T) {
	layer, err := LoadStudyParcels(writeStudy(t, studyFixture))
	if err != nil {
		t.Fatal(err)
	}
	out := StudyParcelsExcept(layer, map[string]bool{"37709540000": true, "37709470000": true})
	if len(out) != 1 || out[0].ParcelID != "37709530000" {
		t.Fatalf("survivors = %+v, want only 37709530000", out)
	}
	if all := StudyParcelsExcept(layer, nil); len(all) != 3 {
		t.Fatalf("no exclusions should keep all 3, got %d", len(all))
	}
	if StudyParcelsExcept(nil, nil) != nil {
		t.Fatal("a nil layer must yield nil, not panic")
	}
}

// The study layer is OPTIONAL. A host with no re-portal checkout must get an
// empty layer and a working map, never an error and never a blank page.
func TestStudyParcelsMissingFileIsNotAnError(t *testing.T) {
	for _, p := range []string{"", "/nope/does-not-exist.geojson"} {
		layer, err := LoadStudyParcels(p)
		if err != nil || layer == nil || len(layer.Parcels) != 0 {
			t.Fatalf("LoadStudyParcels(%q) = %v, %v", p, layer, err)
		}
	}
	// malformed JSON is a real error — it means the file is there and wrong
	if _, err := LoadStudyParcels(writeStudy(t, "{not json")); err == nil {
		t.Fatal("malformed study geojson must report an error")
	}
}

func TestStudyAge(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ date, want string }{
		{"2026-08-21", "today"},
		{"2026-08-20", "1 day old"},
		{"2026-07-31", "21 days old"},
		{"2026-01-01", "7 months old"},
		{"", ""},
		{"garbage", ""},
	} {
		if got := (StudyMeta{SnapshotDate: tc.date}).StudyAge(now); got != tc.want {
			t.Errorf("StudyAge(%q) = %q, want %q", tc.date, got, tc.want)
		}
	}
}
