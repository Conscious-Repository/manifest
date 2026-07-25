package realestate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// GeoRecord is one mappable record — a property or deal plus its parcel
// polygon(s) from the .geo.json sidecar (verbatim GeoJSON Features; geometry
// never lives in markdown) and the city parcel ids from .source.json (used to
// filter already-rendered parcels out of the background layer).
type GeoRecord struct {
	Slug      string            `json:"slug"`
	Type      string            `json:"type"` // property | deal
	Title     string            `json:"title"`
	Status    string            `json:"status,omitempty"`
	Control   string            `json:"control,omitempty"`
	Path      string            `json:"path"`
	Features  []json.RawMessage `json:"features,omitempty"`
	ParcelIDs []string          `json:"-"`
}

// GeoRecords returns every property + deal with whatever geometry its sidecars
// carry (records without geometry come back with empty Features → the client's
// "unmapped" strip).
func (s *Service) GeoRecords() ([]GeoRecord, error) {
	props, err := s.Properties()
	if err != nil {
		return nil, err
	}
	var out []GeoRecord
	for _, p := range props {
		g := GeoRecord{
			Slug: p.Slug, Type: "property", Title: firstNonEmpty(p.Address, p.Name),
			Status: p.Status, Control: p.Control, Path: p.Path,
		}
		s.fillSidecars(&g)
		out = append(out, g)
	}
	deals, err := s.Deals()
	if err != nil {
		return nil, err
	}
	for _, d := range deals {
		g := GeoRecord{Slug: d.Slug, Type: "deal", Title: d.Name, Path: d.Path}
		s.fillSidecars(&g)
		out = append(out, g)
	}
	return out, nil
}

// fillSidecars loads <base>.geo.json (Feature or FeatureCollection → Features)
// and <base>.source.json (any nested parcel_id values).
func (s *Service) fillSidecars(g *GeoRecord) {
	base := strings.TrimSuffix(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(g.Path)), ".md")
	if raw, err := os.ReadFile(base + ".geo.json"); err == nil {
		g.Features = normalizeFeatures(raw)
	}
	if raw, err := os.ReadFile(base + ".source.json"); err == nil {
		g.ParcelIDs = parcelIDs(raw)
	}
}

// normalizeFeatures accepts a Feature or a FeatureCollection and returns the
// flat Feature list, verbatim bytes.
func normalizeFeatures(raw []byte) []json.RawMessage {
	var probe struct {
		Type     string            `json:"type"`
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	switch probe.Type {
	case "FeatureCollection":
		return probe.Features
	case "Feature":
		return []json.RawMessage{json.RawMessage(raw)}
	}
	return nil
}

// parcelIDs walks arbitrary source JSON for "parcel_id" string values.
func parcelIDs(raw []byte) []string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			for k, val := range t {
				if k == "parcel_id" {
					if s, ok := val.(string); ok && s != "" && !seen[s] {
						seen[s] = true
						out = append(out, s)
					}
				}
				walk(val)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
