package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"manifest/record"
)

// -normalize-addresses (data hygiene): every property's `address:` becomes the
// full standard form `<street>, Saint Louis, MO <zip>`. Records missing
// city/state/zip get their zip by REVERSE-GEOCODING the parcel polygon's
// centroid (OSM Nominatim, 1 req/s per policy). The street part — the display
// name — keeps the record's existing casing. Dry-run by default; slugs, links
// and tethers are untouched (only the frontmatter scalar changes).
func runNormalizeAddresses(cfg miniConfig, apply bool) {
	reRoot := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate"))
	files, _ := filepath.Glob(filepath.Join(cfg.VaultPath, filepath.FromSlash(reRoot), "properties", "*.md"))
	fullRe := regexp.MustCompile(`(?i),\s*(saint|st\.?)\s+louis\s*,\s*MO\s+\d{5}`)
	updated, already, noGeo, failed := 0, 0, 0, 0
	for _, full := range files {
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		m := regexp.MustCompile(`(?m)^address:\s*(.+)$`).FindStringSubmatch(string(raw))
		if m == nil {
			continue
		}
		addr := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		slug := strings.TrimSuffix(filepath.Base(full), ".md")
		if fullRe.MatchString(addr) {
			// standardize any "St. Louis" spelling to "Saint Louis", keep the rest
			std := regexp.MustCompile(`(?i)\bst\.?\s+louis\b`).ReplaceAllString(addr, "Saint Louis")
			if std != addr {
				fmt.Printf("  [%s] spelling: %q → %q\n", slug, addr, std)
				if apply {
					writeAddr(full, string(raw), std)
				}
				updated++
			} else {
				already++
			}
			continue
		}
		// short address — need the zip from the parcel centroid
		lat, lng, ok := centroidOf(record.Sidecar(full, record.SidecarGeo))
		if !ok {
			fmt.Printf("  [%s] SKIP — no geometry to reverse-geocode (%q left as-is)\n", slug, addr)
			noGeo++
			continue
		}
		zip := ""
		if apply || true { // dry-run geocodes too, so the report shows the real outcome
			zip = reverseZip(lat, lng)
			time.Sleep(1100 * time.Millisecond) // Nominatim: 1 req/s
		}
		if zip == "" {
			fmt.Printf("  [%s] FAILED reverse geocode (%q left as-is)\n", slug, addr)
			failed++
			continue
		}
		std := addr + ", Saint Louis, MO " + zip
		fmt.Printf("  [%s] %q → %q\n", slug, addr, std)
		if apply {
			writeAddr(full, string(raw), std)
		}
		updated++
	}
	fmt.Printf("\nnormalize-addresses: %d updated · %d already standard · %d no-geometry · %d geocode failures\n",
		updated, already, noGeo, failed)
	if !apply {
		fmt.Println("DRY RUN — re-run with -normalize-addresses -apply to write.")
	}
}

func writeAddr(fullPath, raw, addr string) {
	next := regexp.MustCompile(`(?m)^address:\s*.+$`).ReplaceAllString(raw, "address: "+addr)
	_ = os.WriteFile(fullPath, []byte(next), 0o644)
}

// centroidOf averages the first ring's vertices ([lng, lat] GeoJSON order).
func centroidOf(geoPath string) (lat, lng float64, ok bool) {
	raw, err := os.ReadFile(geoPath)
	if err != nil {
		return 0, 0, false
	}
	var probe struct {
		Type     string `json:"type"`
		Geometry struct {
			Type        string        `json:"type"`
			Coordinates [][][]float64 `json:"coordinates"`
		} `json:"geometry"`
		Features []json.RawMessage `json:"features"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return 0, 0, false
	}
	if probe.Type == "FeatureCollection" && len(probe.Features) > 0 {
		return centroidOfBytes(probe.Features[0])
	}
	return centroidRing(probe.Geometry.Coordinates)
}

func centroidOfBytes(raw []byte) (float64, float64, bool) {
	var f struct {
		Geometry struct {
			Coordinates [][][]float64 `json:"coordinates"`
		} `json:"geometry"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return 0, 0, false
	}
	return centroidRing(f.Geometry.Coordinates)
}

func centroidRing(coords [][][]float64) (float64, float64, bool) {
	if len(coords) == 0 || len(coords[0]) == 0 {
		return 0, 0, false
	}
	var sLng, sLat float64
	for _, c := range coords[0] {
		if len(c) < 2 {
			continue
		}
		sLng += c[0]
		sLat += c[1]
	}
	n := float64(len(coords[0]))
	return sLat / n, sLng / n, true
}

// reverseZip asks Nominatim for the postcode at a point (descriptive UA per policy).
func reverseZip(lat, lng float64) string {
	q := url.Values{
		"lat": {fmt.Sprintf("%f", lat)}, "lon": {fmt.Sprintf("%f", lng)},
		"format": {"json"}, "zoom": {"18"},
	}
	req, err := http.NewRequest(http.MethodGet, "https://nominatim.openstreetmap.org/reverse?"+q.Encode(), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "manifest-dashboard/1.0 (local personal cockpit)")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var out struct {
		Address struct {
			Postcode string `json:"postcode"`
		} `json:"address"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return ""
	}
	// zips sometimes come back as "63108-2765" — keep the 5-digit part
	if i := strings.Index(out.Address.Postcode, "-"); i > 0 {
		return out.Address.Postcode[:i]
	}
	return out.Address.Postcode
}
