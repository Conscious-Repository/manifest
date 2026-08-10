// Command owner-pull is a one-shot, RE-RUNNABLE importer that looks up the
// CURRENT OWNER OF RECORD for every property record from the St. Louis City
// ArcGIS Assessor service and stamps it into the property's frontmatter
// (`owner:` / `owner-addr:` / `owner-since:`). The research-parcel layer
// (cmd/parcel-pull) covers its confirmed study area only; this command covers
// the portfolio itself — one bulk query per portfolio street, matched back to
// each property by house number (single numbers and "742-44"-style ranges).
//
// DRY-RUN by default: prints every would-be stamp and writes nothing until
// -apply. Frontmatter-surgical: SetFrontmatterField touches only the three
// owner lines — the record body (## todos, ## work, ## log) is never rewritten.
// A property whose `owner:` is already set is SKIPPED unless -force, so a
// hand-corrected owner survives re-pulls.
//
// Writes go through the vaultwriter zone guard + shared write-audit.log.
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
	"strconv"
	"strings"
	"time"

	"manifest/mdfm"
	"manifest/realestate"
	"manifest/vaultwriter"
)

const arcgisBase = "https://maps8.stlouis-mo.gov/arcgis/rest/services/ASSESSOR/Assessor_Public_Parcels/MapServer/11/query"

const outFields = "SITEADDR,OwnerName,OwnerName2,OwnerAddr,OwnerCity,OwnerState,OwnerZIP,ResSaleDate"

func main() {
	configPath := flag.String("config", "config.json", "manifest config (for vaultPath + systemRoot)")
	vaultOverride := flag.String("vault", "", "override the vault path from config")
	apply := flag.Bool("apply", false, "write frontmatter (default: dry-run report only)")
	force := flag.Bool("force", false, "overwrite an owner that is already set")
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
	propsRel := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate", "properties"))
	writer := vaultwriter.New(cfg.VaultPath).WithZoneRoots(cfg.SystemRoot, "").WithAudit(dataDir)

	props := readProperties(filepath.Join(cfg.VaultPath, filepath.FromSlash(propsRel)))
	if len(props) == 0 {
		fatal("no property records under %s", propsRel)
	}

	// One assessor query per distinct street name (the last non-suffix word of
	// the address — BAYARD, EUCLID, FOUNTAIN, AUBERT…), then match locally.
	streets := map[string][]siteRec{}
	for _, p := range props {
		st := streetWord(p.addrKey)
		if st == "" {
			continue
		}
		if _, done := streets[st]; done {
			continue
		}
		recs, err := queryStreet(st)
		if err != nil {
			fatal("assessor query %s: %v", st, err)
		}
		streets[st] = recs
		fmt.Fprintf(os.Stderr, "  fetched %s: %d parcels\n", st, len(recs))
	}

	matched, missed := 0, []string{}
	skipped := 0
	sort.Slice(props, func(i, j int) bool { return props[i].slug < props[j].slug })
	for i := range props {
		p := &props[i]
		rec, ok := matchSite(streets[streetWord(p.addrKey)], p.addrKey)
		if !ok {
			missed = append(missed, p.slug)
			continue
		}
		matched++
		p.hit = rec
		mark := "→"
		if p.owner != "" && !*force {
			skipped++
			mark = "· (owner already set — skipped)"
		}
		since := ""
		if rec.saleDate != "" {
			since = "  since " + rec.saleDate
		}
		fmt.Printf("  %-22s %-18s %s %-38s %s%s\n", p.slug, rec.site, mark, truncate(rec.owner, 38), truncate(rec.ownerAddr, 40), since)
	}
	fmt.Printf("\n%d/%d matched (%d already set", matched, len(props), skipped)
	if len(missed) > 0 {
		fmt.Printf("; unmatched: %s", strings.Join(missed, ", "))
	}
	fmt.Println(")")

	if !*apply {
		fmt.Println("\nDRY RUN — nothing written. Re-run with -apply to stamp frontmatter.")
		return
	}

	written := 0
	for _, p := range props {
		if p.hit == nil || (p.owner != "" && !*force) {
			continue
		}
		rel := propsRel + "/" + p.slug + ".md"
		set := func(key, val string) {
			if val == "" {
				return
			}
			if err := writer.SetFrontmatterField(rel, key, yamlScalar(val)); err != nil {
				fatal("write %s %s: %v", p.slug, key, err)
			}
		}
		set("owner", p.hit.owner2Join())
		set("owner-addr", p.hit.ownerAddr)
		set("owner-since", p.hit.saleDate)
		written++
	}
	fmt.Printf("\napplied: %d properties stamped\n", written)
}

// ---- property records (frontmatter peek only) ----

type prop struct {
	slug    string
	addrKey string // realestate.AddrKey of the record address
	owner   string // existing frontmatter owner
	hit     *siteRec
}

func readProperties(dir string) []prop {
	ents, err := os.ReadDir(dir)
	if err != nil {
		fatal("read %s: %v", dir, err)
	}
	var out []prop
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		fm, _ := mdfm.Split(string(raw))
		if !strings.Contains(fm["categories"], "property") {
			continue
		}
		addr := strings.Trim(strings.TrimSpace(fm["address"]), `"'`)
		if addr == "" {
			continue
		}
		out = append(out, prop{
			slug:    strings.TrimSuffix(e.Name(), ".md"),
			addrKey: realestate.AddrKey(addr),
			owner:   strings.Trim(strings.TrimSpace(fm["owner"]), `"'`),
		})
	}
	return out
}

// streetWord is the query token: the last word of the AddrKey (suffix already
// dropped) — "751 BAYARD" → BAYARD, "736 N EUCLID" → EUCLID.
func streetWord(addrKey string) string {
	f := strings.Fields(addrKey)
	if len(f) < 2 {
		return ""
	}
	return f[len(f)-1]
}

// ---- ArcGIS fetch + match ----

type siteRec struct {
	site      string // normalized SITEADDR
	key       string // realestate.AddrKey(site)
	lo, hi    int    // house-number range ("742-44 BAYARD AV" → 742..744)
	owner     string
	owner2    string
	ownerAddr string
	saleDate  string
}

func (r siteRec) owner2Join() string {
	if r.owner2 != "" {
		return r.owner + " & " + r.owner2
	}
	return r.owner
}

type esriResp struct {
	Features []struct {
		Attributes map[string]any `json:"attributes"`
	} `json:"features"`
	ExceededTransferLimit bool `json:"exceededTransferLimit"`
}

func queryStreet(street string) ([]siteRec, error) {
	var out []siteRec
	offset := 0
	for {
		q := url.Values{}
		q.Set("where", fmt.Sprintf("SITEADDR LIKE '%%%s%%'", street))
		q.Set("outFields", outFields)
		q.Set("returnGeometry", "false")
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
		for _, f := range r.Features {
			a := f.Attributes
			site := normSpace(str(a["SITEADDR"]))
			if site == "" {
				continue
			}
			rec := siteRec{
				site:      site,
				key:       realestate.AddrKey(site),
				owner:     normSpace(str(a["OwnerName"])),
				owner2:    normSpace(str(a["OwnerName2"])),
				ownerAddr: ownerAddr(a),
				saleDate:  epochDate(a["ResSaleDate"]),
			}
			rec.lo, rec.hi = numRange(site)
			out = append(out, rec)
		}
		if !r.ExceededTransferLimit || len(r.Features) == 0 {
			break
		}
		offset += len(r.Features)
	}
	return out, nil
}

// matchSite finds the assessor record for one property: exact AddrKey first,
// then a house-number-in-range match on the same street tail.
func matchSite(recs []siteRec, addrKey string) (*siteRec, bool) {
	for i := range recs {
		if recs[i].key == addrKey {
			return &recs[i], true
		}
	}
	f := strings.Fields(addrKey)
	if len(f) < 2 {
		return nil, false
	}
	num, err := strconv.Atoi(f[0])
	if err != nil {
		return nil, false
	}
	tail := strings.Join(f[1:], " ")
	for i := range recs {
		kf := strings.Fields(recs[i].key)
		if len(kf) < 2 || strings.Join(kf[1:], " ") != tail {
			continue
		}
		if recs[i].lo != 0 && num >= recs[i].lo && num <= recs[i].hi {
			return &recs[i], true
		}
	}
	return nil, false
}

// numRange parses a SITEADDR house-number token: "742" → 742..742,
// "742-744" → 742..744, "742-44" (short form) → 742..744.
func numRange(site string) (lo, hi int) {
	f := strings.Fields(site)
	if len(f) == 0 {
		return 0, 0
	}
	parts := strings.SplitN(f[0], "-", 2)
	lo, err := strconv.Atoi(strings.TrimFunc(parts[0], func(r rune) bool { return r < '0' || r > '9' }))
	if err != nil || lo == 0 {
		return 0, 0
	}
	hi = lo
	if len(parts) == 2 {
		if h, err := strconv.Atoi(strings.TrimFunc(parts[1], func(r rune) bool { return r < '0' || r > '9' })); err == nil && h > 0 {
			if h < lo { // short form: graft the high digits of lo
				mag := 1
				for m := h; m > 0; m /= 10 {
					mag *= 10
				}
				h = lo - lo%mag + h
			}
			if h >= lo {
				hi = h
			}
		}
	}
	return lo, hi
}

// ---- helpers (mirrors cmd/parcel-pull) ----

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

func epochDate(v any) string {
	ms, ok := v.(float64)
	if !ok || ms == 0 {
		return ""
	}
	return time.Unix(int64(ms)/1000, 0).UTC().Format("2006-01-02")
}

// yamlScalar quotes a value for a frontmatter scalar line when it contains
// YAML-significant characters (the assessor loves "LAST, FIRST" commas).
func yamlScalar(s string) string {
	if strings.ContainsAny(s, ",:#'\"[]{}") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func normSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func str(v any) string {
	s, _ := v.(string)
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "owner-pull: "+format+"\n", args...)
	os.Exit(1)
}
