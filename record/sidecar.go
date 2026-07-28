package record

import "strings"

// Sidecar file kinds a record can declare: data that belongs to a record but
// never lives in its markdown (money facts, geometry, verbatim source objects).
const (
	SidecarLedger = "ledger.csv"  // money facts, one csv row per entry
	SidecarGeo    = "geo.json"    // verbatim GeoJSON Feature/FeatureCollection
	SidecarSource = "source.json" // verbatim external source object (export contract)
)

// Sidecar derives THE sidecar path for a record: `…/<slug>.md` →
// `…/<slug>.<kind>`. Works on absolute and vault-relative paths alike.
func Sidecar(mdPath, kind string) string {
	return strings.TrimSuffix(mdPath, ".md") + "." + kind
}
