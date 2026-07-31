// Command parcel-pull is a one-shot, RE-RUNNABLE importer that pulls tax-parcel
// research data from the St. Louis City ArcGIS Assessor service and writes one
// research-parcel record per parcel under system/realestate/parcels/ — a
// separate map/spreadsheet layer distinct from the owner's owned deals.
//
// The study area (owner-confirmed 2026-07-31): every parcel fronting Bayard Ave
// and N Euclid Ave NORTH of Fountain Ave, plus Page Blvd between them — scoped
// by Assessor neighborhood 53 (Fountain Park) with a Fountain latitude cut for
// the two N-S streets. One bulk query per street, no 1-by-1 scraping.
//
// DRY-RUN by default: prints every would-be parcel with owner + tax flag and
// writes nothing until -apply. RE-RUNNABLE + NOTES-PRESERVING: on -apply it
// refreshes each parcel's frontmatter facts + .source.json (verbatim ArcGIS
// attributes) + .geo.json (parcel polygon), but NEVER rewrites the record's
// `## log` section — the owner's per-parcel notes survive every re-pull.
//
// Every vault write goes through a declared §A3 system-zone capability scoped to
// system/realestate/parcels/** and lands in the shared write-audit.log.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"manifest/record"
	"manifest/vaultwriter"
)

const arcgisBase = "https://maps8.stlouis-mo.gov/arcgis/rest/services/ASSESSOR/Assessor_Public_Parcels/MapServer/11/query"

const outFields = "ParcelId,Handle,CityBlock,Parcel,OwnerName,OwnerName2,OwnerAddr,OwnerCity,OwnerState,OwnerZIP,ResSaleDate,RecDailyDate,TaxBalDue,AsrLandUse1,AsdTotal,AprLand,SITEADDR,Nbrhd,Ward20"

// studyStreet is one leg of the confirmed study area: a SITEADDR street filter
// within neighborhood 53, optionally floored at a latitude (north-of-Fountain).
type studyStreet struct {
	name   string  // label
	like   string  // SITEADDR LIKE '%<like>%'
	latMin float64 // keep parcels whose centroid lat exceeds this (0 = keep all)
}

// The owner-confirmed extent (175 parcels): Bayard & Euclid north of Fountain
// (lat > 38.6556, just above Fountain's north frontage), all nbrhd-53 Page.
var study = []studyStreet{
	{"Bayard Ave (N of Fountain)", "BAYARD", 38.6556},
	{"N Euclid Ave (N of Fountain)", "EUCLID", 38.6556},
	{"Page Blvd (Bayard↔Euclid)", "PAGE", 0},
}

func main() {
	configPath := flag.String("config", "config.json", "manifest config (for vaultPath + systemRoot)")
	vaultOverride := flag.String("vault", "", "override the vault path from config")
	apply := flag.Bool("apply", false, "write records (default: dry-run report only)")
	flag.Parse()

	cfg := loadConfig(*configPath)
	if *vaultOverride != "" {
		cfg.VaultPath = *vaultOverride
	}
	if cfg.VaultPath == "" {
		fatal("no vaultPath — pass -vault or a -config with one")
	}
	if cfg.SystemRoot == "" {
		cfg.SystemRoot = "system"
	}
	dataDir := cfg.DataDir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".config", "manifest")
	}
	parcelsRel := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate", "parcels"))
	vaultWrite := vaultwriter.New(cfg.VaultPath).
		WithZoneRoots(cfg.SystemRoot, "").
		WithAudit(dataDir).
		Grant(vaultwriter.Capability{
			Name: "parcel-pull", Zone: record.ZoneSystem,
			Pattern: parcelsRel + "/**",
			Actor:   vaultwriter.ActorUserAction,
		}).BindAbs("parcel-pull")

	parcels, err := fetchStudyParcels()
	if err != nil {
		fatal("fetch: %v", err)
	}
	sort.Slice(parcels, func(i, j int) bool { return parcels[i].sortKey() < parcels[j].sortKey() })

	// Report.
	byStreet := map[string]int{}
	lra, delinq := 0, 0
	for _, p := range parcels {
		byStreet[p.streetLabel]++
		switch p.taxStatus() {
		case "lra":
			lra++
		case "delinquent":
			delinq++
		}
		fmt.Printf("  %-16s %-14s %-18s %s\n", p.Site, "blk "+p.blockStr(), p.taxFlag(), truncate(p.Owner, 34))
	}
	fmt.Printf("\n%d parcels  (%d LRA land-bank · %d delinquent)\n", len(parcels), lra, delinq)
	for _, s := range study {
		fmt.Printf("  %s: %d\n", s.name, byStreet[s.name])
	}

	if !*apply {
		fmt.Println("\nDRY RUN — nothing written. Re-run with -apply to write.")
		return
	}

	vaultRoot := cfg.VaultPath
	written, notesKept := 0, 0
	for _, p := range parcels {
		slug := record.Slug(p.Site, 80)
		base := filepath.Join(vaultRoot, filepath.FromSlash(parcelsRel), slug)
		// Preserve any existing `## log` (owner notes) across the re-pull.
		logBlock := existingLog(base + ".md")
		if strings.TrimSpace(logBlock) != "## log" && logBlock != "" {
			notesKept++
		}
		if err := vaultWrite(base+".md", []byte(p.markdown(logBlock))); err != nil {
			fatal("write %s.md: %v", slug, err)
		}
		if err := vaultWrite(base+".source.json", p.rawAttrs); err != nil {
			fatal("write %s.source.json: %v", slug, err)
		}
		if err := vaultWrite(base+".geo.json", p.geoFeature()); err != nil {
			fatal("write %s.geo.json: %v", slug, err)
		}
		written++
	}
	fmt.Printf("\napplied: %d parcels written to %s/ (%d retained existing notes)\n", written, parcelsRel, notesKept)
}

// ---- ArcGIS fetch ----

type esriResp struct {
	Features []struct {
		Attributes map[string]any `json:"attributes"`
		Geometry   struct {
			Rings [][][]float64 `json:"rings"`
		} `json:"geometry"`
	} `json:"features"`
	ExceededTransferLimit bool `json:"exceededTransferLimit"`
}

func fetchStudyParcels() ([]parcel, error) {
	var out []parcel
	for _, s := range study {
		feats, err := queryStreet(s.like)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.name, err)
		}
		kept := 0
		for _, f := range feats {
			lat, lng, ok := centroid(f.Geometry.Rings)
			if !ok || lat <= s.latMin {
				continue
			}
			raw, _ := json.MarshalIndent(f.Attributes, "", "  ")
			out = append(out, parcel{
				attrs: f.Attributes, rawAttrs: raw, rings: f.Geometry.Rings,
				lat: lat, lng: lng, streetLabel: s.name,
				Site:  normSpace(str(f.Attributes["SITEADDR"])),
				Owner: normSpace(str(f.Attributes["OwnerName"])),
			})
			kept++
		}
		fmt.Fprintf(os.Stderr, "  fetched %s: %d parcels\n", s.name, kept)
	}
	return out, nil
}

func queryStreet(like string) ([]struct {
	Attributes map[string]any `json:"attributes"`
	Geometry   struct {
		Rings [][][]float64 `json:"rings"`
	} `json:"geometry"`
}, error) {
	var all []struct {
		Attributes map[string]any `json:"attributes"`
		Geometry   struct {
			Rings [][][]float64 `json:"rings"`
		} `json:"geometry"`
	}
	offset := 0
	for {
		q := url.Values{}
		q.Set("where", fmt.Sprintf("SITEADDR LIKE '%%%s%%' AND Nbrhd=53", like))
		q.Set("outFields", outFields)
		q.Set("returnGeometry", "true")
		q.Set("outSR", "4326")
		q.Set("f", "json")
		q.Set("resultOffset", fmt.Sprint(offset))
		q.Set("resultRecordCount", "1000")
		resp, err := http.Get(arcgisBase + "?" + q.Encode())
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var r esriResp
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("bad response: %v", err)
		}
		all = append(all, r.Features...)
		if !r.ExceededTransferLimit || len(r.Features) == 0 {
			break
		}
		offset += len(r.Features)
	}
	return all, nil
}

// ---- parcel record ----

type parcel struct {
	attrs       map[string]any
	rawAttrs    []byte
	rings       [][][]float64
	lat, lng    float64
	streetLabel string
	Site        string
	Owner       string
}

func (p parcel) blockStr() string { return trimZeros(str(p.attrs["CityBlock"])) }

func (p parcel) taxBal() float64 {
	if v, ok := p.attrs["TaxBalDue"].(float64); ok {
		return v
	}
	return 0
}

// taxStatus: lra (owner is the Land Reutilization Authority land bank) >
// delinquent (owes back tax) > current.
func (p parcel) taxStatus() string {
	o := strings.ToUpper(strings.Join(strings.Fields(p.Owner), " "))
	if o == "LRA" || strings.HasPrefix(o, "LRA ") || o == "L R A" || strings.Contains(o, "LAND REUTILIZATION") {
		return "lra"
	}
	if p.taxBal() > 0 {
		return "delinquent"
	}
	return "current"
}

func (p parcel) taxFlag() string {
	switch p.taxStatus() {
	case "lra":
		return "LRA land-bank"
	case "delinquent":
		return fmt.Sprintf("DELINQUENT $%.0f", p.taxBal())
	}
	return "current"
}

func (p parcel) sortKey() string {
	// group by street label, then by numeric-ish address prefix
	return p.streetLabel + fmt.Sprintf("%08d", addrNum(p.Site)) + p.Site
}

func (p parcel) markdown(logBlock string) string {
	var b strings.Builder
	b.WriteString("---\ncategories: [parcel]\n")
	b.WriteString("address: " + p.Site + "\n")
	b.WriteString("owner: " + yamlScalar(p.Owner) + "\n")
	if o2 := strings.TrimSpace(str(p.attrs["OwnerName2"])); o2 != "" {
		b.WriteString("owner2: " + yamlScalar(o2) + "\n")
	}
	b.WriteString("owner_addr: " + yamlScalar(ownerAddr(p.attrs)) + "\n")
	if d := epochDate(p.attrs["ResSaleDate"]); d != "" {
		b.WriteString("sale_date: " + d + "\n")
	}
	if d := epochDate(p.attrs["RecDailyDate"]); d != "" {
		b.WriteString("rec_date: " + d + "\n")
	}
	b.WriteString(fmt.Sprintf("tax_bal_due: %.2f\n", p.taxBal()))
	b.WriteString("tax_status: " + p.taxStatus() + "\n")
	b.WriteString(fmt.Sprintf("assessed: %s\n", numStr(p.attrs["AsdTotal"])))
	b.WriteString("land_use: " + trimZeros(str(p.attrs["AsrLandUse1"])) + "\n")
	b.WriteString("parcel_id: " + str(p.attrs["ParcelId"]) + "\n")
	b.WriteString("handle: " + str(p.attrs["Handle"]) + "\n")
	b.WriteString("cityblock: " + p.blockStr() + "\n")
	b.WriteString("ward: " + trimZeros(str(p.attrs["Ward20"])) + "\n")
	b.WriteString("nbrhd: " + trimZeros(str(p.attrs["Nbrhd"])) + "\n")
	b.WriteString(fmt.Sprintf("lat: %.6f\n", p.lat))
	b.WriteString(fmt.Sprintf("lng: %.6f\n", p.lng))
	b.WriteString("hidden: false\n")
	b.WriteString("---\n\n# " + p.Site + "\n\n")
	if strings.TrimSpace(logBlock) == "" {
		b.WriteString("## log\n")
	} else {
		b.WriteString(strings.TrimRight(logBlock, "\n") + "\n")
	}
	return b.String()
}

// geoFeature builds the parcel's GeoJSON Feature ([lng,lat] rings) for the map.
func (p parcel) geoFeature() []byte {
	feat := map[string]any{
		"type": "Feature",
		"properties": map[string]any{
			"address":    p.Site,
			"owner":      p.Owner,
			"tax_status": p.taxStatus(),
			"parcel_id":  str(p.attrs["ParcelId"]),
		},
		"geometry": map[string]any{
			"type":        "Polygon",
			"coordinates": p.rings, // ESRI rings are already [lng,lat] at outSR=4326
		},
	}
	out, _ := json.MarshalIndent(feat, "", "  ")
	return out
}

// existingLog returns the `## log` section (heading through EOF) of an existing
// record, or "" if the file/section is absent. This is what survives a re-pull.
func existingLog(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := string(raw)
	i := strings.Index(s, "\n## log")
	if i < 0 {
		return ""
	}
	return s[i+1:]
}

// ---- helpers ----

type miniConfig struct {
	VaultPath  string `json:"vaultPath"`
	SystemRoot string `json:"systemRoot"`
	DataDir    string `json:"dataDir"`
}

func loadConfig(path string) miniConfig {
	var c miniConfig
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.VaultPath != "" {
		if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(c.VaultPath, "~/") {
			c.VaultPath = filepath.Join(h, c.VaultPath[2:])
		}
	}
	return c
}

func centroid(rings [][][]float64) (lat, lng float64, ok bool) {
	if len(rings) == 0 || len(rings[0]) == 0 {
		return 0, 0, false
	}
	pts := rings[0]
	var sx, sy float64
	for _, pt := range pts {
		sx += pt[0]
		sy += pt[1]
	}
	n := float64(len(pts))
	return sy / n, sx / n, true
}

func ownerAddr(a map[string]any) string {
	parts := []string{strings.TrimSpace(str(a["OwnerAddr"]))}
	cityLine := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(str(a["OwnerCity"])),
		strings.TrimSpace(str(a["OwnerState"])),
		strings.TrimSpace(str(a["OwnerZIP"])),
	}, " "))
	if cityLine != "" {
		parts = append(parts, cityLine)
	}
	return strings.TrimSpace(strings.Join(parts, ", "))
}

// epochDate converts an ArcGIS epoch-millisecond value to YYYY-MM-DD ("" if nil).
func epochDate(v any) string {
	ms, ok := v.(float64)
	if !ok || ms == 0 {
		return ""
	}
	return time.Unix(int64(ms)/1000, 0).UTC().Format("2006-01-02")
}

// normSpace trims and collapses internal whitespace (SITEADDR/OwnerName pad).
func normSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func numStr(v any) string {
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%.0f", f)
	}
	return "0"
}

func trimZeros(s string) string {
	if i := strings.Index(s, "."); i >= 0 {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

// yamlScalar quotes an owner/address value if it contains YAML-significant chars.
func yamlScalar(s string) string {
	if s == "" {
		return "\"\""
	}
	if strings.ContainsAny(s, ":#\"'[]{}|>&*!,") || strings.HasPrefix(s, " ") {
		return "\"" + strings.ReplaceAll(s, "\"", "'") + "\""
	}
	return s
}

func addrNum(s string) int {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	n := 0
	for _, r := range f[0] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "parcel-pull: "+format+"\n", a...)
	os.Exit(1)
}
