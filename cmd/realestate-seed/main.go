// Command realestate-seed is a one-shot importer (real-estate-management plan §6)
// that populates system/realestate/ from the re-portal source files: the
// controlled deals (deals.json), the acquisition-pipeline parcels
// (neighborhood.json), and the background parcels (bgParcels.json → dataDir).
//
// DRY-RUN by default: it prints every would-be record and the derived field
// mapping, and writes nothing until -apply. Idempotent — a record whose file
// already exists is skipped, never overwritten, so a second -apply is a no-op.
// Empty or unparseable source files fail loudly. Source objects are preserved
// byte-for-byte as <slug>.source.json; no owner/contact PII is ever copied (the
// importer refuses to run if a source object carries a PII-named field).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"manifest/record"
)

func main() {
	configPath := flag.String("config", "config.json", "manifest config (for vaultPath + systemRoot)")
	vaultOverride := flag.String("vault", "", "override the vault path from config")
	apply := flag.Bool("apply", false, "write files (default: dry-run report only)")
	expand := flag.Bool("expand", false, "admin-portal migration: member records for every parcel + deal-level sources (re-sync from the live re-portal tree)")
	migrateBudget := flag.Bool("migrate-budget", false, "pass-5: seed stage [est::] from legacy ## budget rows with matching names (tables left verbatim)")
	normalizeAddr := flag.Bool("normalize-addresses", false, "data hygiene: full standard addresses (street, Saint Louis, MO zip) — zips reverse-geocoded from parcel centroids")
	flag.Parse()
	srcDir := flag.Arg(0)
	if srcDir == "" {
		srcDir = "/Users/benjamin/re-portal"
	}

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
	if *normalizeAddr {
		runNormalizeAddresses(cfg, *apply)
		return
	}
	if *migrateBudget {
		runMigrateBudget(cfg, *apply)
		return
	}
	if *expand {
		runExpand(cfg, srcDir, *apply)
		return
	}
	dataDir := cfg.DataDir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".config", "manifest")
	}
	reRoot := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate"))

	p := buildPlan(srcDir, reRoot)
	p.report(os.Stdout, cfg.VaultPath, dataDir, *apply)
	if !*apply {
		fmt.Println("\nDRY RUN — nothing written. Re-run with -apply to write.")
		return
	}
	if err := p.write(cfg.VaultPath, dataDir); err != nil {
		fatal("writing: %v", err)
	}
	fmt.Println("\napplied.")
}

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

// file is one write the plan will make: vault-relative path (or dataDir-relative
// when DataDir), the exact bytes, and a one-line mapping note for the report.
type file struct {
	Rel     string // forward-slash relative path
	DataDir bool   // true → under dataDir, false → under the vault
	Bytes   []byte
	Note    string // report label
}

type plan struct {
	files []file
	warn  []string
}

// ---- source shapes (RawMessage keeps the verbatim bytes for source.json) ----

type rawDeal struct {
	Slug        string            `json:"slug"`
	Name        string            `json:"name"`
	Status      string            `json:"status"`
	ProjectType string            `json:"project_type"`
	Opportunity bool              `json:"opportunity"`
	Properties  []json.RawMessage `json:"properties"`
}

type rawProp struct {
	Address        string             `json:"address"`
	FullAddress    string             `json:"fullAddress"`
	ParcelID       string             `json:"parcel_id"`
	HardCosts      float64            `json:"hard_costs"`
	SoftCostItems  map[string]float64 `json:"soft_cost_items"`
	ClosingCosts   float64            `json:"closing_costs"`
	ContingencyPct float64            `json:"contingency_pct"`
	Geo            json.RawMessage    `json:"geo"`
}

type rawParcel struct {
	ParcelID        string          `json:"parcel_id"`
	Address         string          `json:"address"`
	Status          string          `json:"status"`
	ProjectType     string          `json:"project_type"`
	AcquisitionCost float64         `json:"acquisition_cost"`
	RehabCost       float64         `json:"rehab_cost"`
	Geo             json.RawMessage `json:"geo"`
}

func buildPlan(srcDir, reRoot string) *plan {
	p := &plan{}
	dealsRaw := readJSONArray(resolveSrc(srcDir, "src/data/deals.json"), "deals.json")
	parcelsRaw := readJSONArray(resolveSrc(srcDir, "src/data/neighborhood.json"), "neighborhood.json")

	// Privacy: refuse to run if any source object carries a PII-named field.
	for _, d := range dealsRaw {
		assertNoPII(d, "deals.json")
	}
	for _, n := range parcelsRaw {
		assertNoPII(n, "neighborhood.json")
	}

	for _, draw := range dealsRaw {
		p.mapDeal(draw, reRoot)
	}
	for _, nraw := range parcelsRaw {
		p.mapParcel(nraw, reRoot)
	}

	// Background parcels → dataDir (derived map context; not the vault).
	if bg, err := os.ReadFile(resolveSrc(srcDir, "public/bgParcels.json")); err == nil {
		p.files = append(p.files, file{
			Rel: "realestate/bgParcels.json", DataDir: true, Bytes: bg,
			Note: "bgParcels.json → dataDir (map context)",
		})
	} else {
		p.warn = append(p.warn, "bgParcels.json not found — the map's background layer will be empty")
	}
	return p
}

func (p *plan) mapDeal(draw json.RawMessage, reRoot string) {
	var d rawDeal
	if err := json.Unmarshal(draw, &d); err != nil {
		fatal("deals.json: bad deal object: %v", err)
	}
	if d.Slug == "" {
		fatal("deals.json: a deal has no slug")
	}
	dealName := d.Name
	if dealName == "" {
		dealName = titleize(d.Slug)
	}

	// Multi-parcel deal → ONE deal record; the parcels live in its geo.json for
	// the map (the spec's rule — the seed doesn't decide splitting). Benjamin
	// splits out per-parcel property records later, for the ones that need their
	// own ledger/log. Single-parcel deal → the deal IS the property record.
	if len(d.Properties) > 1 {
		var addrs []string
		var feats []json.RawMessage
		for _, praw := range d.Properties {
			var pr rawProp
			_ = json.Unmarshal(praw, &pr)
			a := pr.FullAddress
			if a == "" {
				a = pr.Address
			}
			addrs = append(addrs, a)
			if len(pr.Geo) > 0 {
				feats = append(feats, pr.Geo)
			}
		}
		var fm strings.Builder
		fm.WriteString("---\ncategories: [deal]\n")
		fm.WriteString("properties: [" + strings.Join(quoteAll(addrs), ", ") + "]\n")
		fm.WriteString("---\n\n# " + dealName + "\n")
		dealRel := reRoot + "/deals/" + d.Slug
		p.add(dealRel+".md", false, []byte(fm.String()),
			"deal    "+d.Slug+"  ("+strconv.Itoa(len(d.Properties))+" parcels — bundle, no per-parcel rows)")
		p.add(dealRel+".source.json", false, draw, "  source.json (verbatim)")
		if len(feats) > 0 {
			p.add(dealRel+".geo.json", false, featureCollection(feats), "  geo.json (all parcels)")
		}
		return
	}

	// Single-parcel deal → one property record (slug = deal slug).
	var pr rawProp
	if err := json.Unmarshal(d.Properties[0], &pr); err != nil {
		fatal("deals.json: bad property in %s: %v", d.Slug, err)
	}
	addr := pr.FullAddress
	if addr == "" {
		addr = pr.Address
	}
	body := propBody(addr, dealPropBudget(pr))
	md := propRecord(addr, statusForDeal(d), kindFor(d.ProjectType), "owned", "", body)
	rel := reRoot + "/properties/" + d.Slug
	p.add(rel+".md", false, []byte(md), "property "+d.Slug+"  ["+statusForDeal(d)+" · "+kindFor(d.ProjectType)+" · owned]")
	p.add(rel+".ledger.csv", false, []byte(ledgerHeader), "  ledger.csv (empty)")
	p.add(rel+".source.json", false, draw, "  source.json (verbatim)")
	if len(pr.Geo) > 0 {
		p.add(rel+".geo.json", false, pr.Geo, "  geo.json (verbatim)")
	}
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = "\"" + s + "\""
	}
	return out
}

func (p *plan) mapParcel(nraw json.RawMessage, reRoot string) {
	var n rawParcel
	if err := json.Unmarshal(nraw, &n); err != nil {
		fatal("neighborhood.json: bad parcel: %v", err)
	}
	slug := slugify(n.Address)
	if slug == "" {
		slug = "parcel-" + n.ParcelID
	}
	var budget []budgetLine
	if n.AcquisitionCost > 0 {
		budget = append(budget, budgetLine{"acquisition", n.AcquisitionCost})
	}
	if n.RehabCost > 0 {
		budget = append(budget, budgetLine{"rehab", n.RehabCost})
	}
	body := propBody(n.Address, budget)
	// Neighborhood free-text status → the record's first log line, verbatim.
	if s := strings.TrimSpace(n.Status); s != "" {
		body += "\n## log\n- " + s + "\n"
	}
	md := propRecord(n.Address, "negotiating", kindFor(n.ProjectType), "tracked", "", body)
	rel := reRoot + "/properties/" + slug
	p.add(rel+".md", false, []byte(md), "parcel  "+slug+"  [tracked · negotiating · "+kindFor(n.ProjectType)+"]")
	p.add(rel+".ledger.csv", false, []byte(ledgerHeader), "  ledger.csv (empty)")
	p.add(rel+".source.json", false, nraw, "  source.json (verbatim)")
	if len(n.Geo) > 0 {
		p.add(rel+".geo.json", false, n.Geo, "  geo.json (verbatim)")
	}
}

func (p *plan) add(rel string, dataDir bool, b []byte, note string) {
	p.files = append(p.files, file{Rel: rel, DataDir: dataDir, Bytes: b, Note: note})
}

// ---- record construction ----

type budgetLine struct {
	cat string
	amt float64
}

const ledgerHeader = "date,type,category,vendor,contractor,amount,status,note,doc\n"

func propRecord(address, status, kind, control, deal, body string) string {
	var b strings.Builder
	b.WriteString("---\ncategories: [property]\n")
	// entity intentionally BLANK (assigned by hand later) — the Board groups by it.
	b.WriteString("address: " + address + "\n")
	b.WriteString("status: " + status + "\n")
	b.WriteString("kind: " + kind + "\n")
	b.WriteString("control: " + control + "\n")
	if deal != "" {
		b.WriteString("deal: \"[[" + deal + "]]\"\n")
	}
	b.WriteString("hidden: false\n")
	b.WriteString("---\n")
	b.WriteString(body)
	return b.String()
}

func propBody(title string, budget []budgetLine) string {
	var b strings.Builder
	b.WriteString("\n# " + title + "\n\n## budget\n")
	if len(budget) > 0 {
		b.WriteString("| category | budget |\n")
		for _, l := range budget {
			b.WriteString("| " + l.cat + " | " + money(l.amt) + " |\n")
		}
	}
	return b.String()
}

func dealPropBudget(pr rawProp) []budgetLine {
	var soft float64
	for _, v := range pr.SoftCostItems {
		soft += v
	}
	var out []budgetLine
	if pr.HardCosts > 0 {
		out = append(out, budgetLine{"hard costs", pr.HardCosts})
	}
	if soft > 0 {
		out = append(out, budgetLine{"soft costs", soft})
	}
	if pr.ClosingCosts > 0 {
		out = append(out, budgetLine{"closing", pr.ClosingCosts})
	}
	if c := pr.ContingencyPct * (pr.HardCosts + soft); c > 0 {
		out = append(out, budgetLine{"contingency", math.Round(c)})
	}
	return out
}

// ---- mapping helpers ----

func statusForDeal(d rawDeal) string {
	s := strings.ToLower(strings.TrimSpace(d.Status))
	if s == "opportunity" || d.Opportunity {
		return "negotiating" // un-underwritten bundle, shown as a pipeline record
	}
	if s == "" {
		return "negotiating"
	}
	return s // completed | construction | pre_development | under_contract
}

func kindFor(projectType string) string {
	switch strings.ToLower(strings.TrimSpace(projectType)) {
	case "value_add", "gut_rehab":
		return "rehab"
	case "new_construction":
		return "new-construction"
	case "mixed":
		return "mixed"
	default:
		return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(projectType)), "_", "-")
	}
}

// slugify is the kernel slug, uncapped (file basenames carry the full address).
func slugify(s string) string { return record.Slug(s, 0) }

func titleize(slug string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(slug, "-", " ")), " ")
}

func money(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func featureCollection(feats []json.RawMessage) []byte {
	b, _ := json.MarshalIndent(map[string]any{
		"type": "FeatureCollection", "features": feats,
	}, "", "  ")
	return b
}

// ---- PII guard ----

var piiKey = regexp.MustCompile(`(?i)^(owner|owner_name|ownername|phone|tel|mobile|email|contact|contact_name|contact_phone)$`)

// assertNoPII walks a source object's keys (recursively) and fatals if any is a
// PII-named field — so a future source that adds owner/contact data can never
// leak into a verbatim source.json.
func assertNoPII(raw json.RawMessage, where string) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return
	}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			for k, val := range t {
				if piiKey.MatchString(k) {
					fatal("%s: source object carries a PII-named field %q — refusing to seed (never copy owner/contact info)", where, k)
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
}

// ---- io + report ----

func resolveSrc(srcDir, relInPortal string) string {
	base := filepath.Base(relInPortal)
	if ctx := filepath.Join("context", base); fileExists(ctx) {
		return ctx // prefer a copy vendored under the repo's context/
	}
	return filepath.Join(srcDir, filepath.FromSlash(relInPortal))
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func readJSONArray(path, label string) []json.RawMessage {
	b, err := os.ReadFile(path)
	if err != nil {
		fatal("reading %s (%s): %v", label, path, err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		fatal("%s is empty — refusing a partial seed", label)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err != nil {
		fatal("%s: not a JSON array: %v", label, err)
	}
	if len(arr) == 0 {
		fatal("%s parsed to zero objects — refusing a partial seed", label)
	}
	return arr
}

func (p *plan) report(w *os.File, vault, dataDir string, apply bool) {
	fmt.Fprintf(w, "realestate-seed — %d files planned\n\n", len(p.files))
	create, skip := 0, 0
	for _, f := range p.files {
		full := f.fullPath(vault, dataDir)
		mark := "create"
		if fileExists(full) {
			mark, skip = "skip  ", skip+1
		} else {
			create++
		}
		fmt.Fprintf(w, "  [%s] %s\n", mark, f.Note)
	}
	fmt.Fprintf(w, "\n%d to create, %d already present (skipped).\n", create, skip)
	for _, wn := range p.warn {
		fmt.Fprintf(w, "  ! %s\n", wn)
	}
}

func (f file) fullPath(vault, dataDir string) string {
	if f.DataDir {
		return filepath.Join(dataDir, filepath.FromSlash(f.Rel))
	}
	return filepath.Join(vault, filepath.FromSlash(f.Rel))
}

func (p *plan) write(vault, dataDir string) error {
	for _, f := range p.files {
		full := f.fullPath(vault, dataDir)
		if !f.DataDir && !isUnder(full, filepath.Join(vault, "system")) {
			return fmt.Errorf("refusing write outside system zone: %s", f.Rel)
		}
		if fileExists(full) {
			continue // write-once — never clobber
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, f.Bytes, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func isUnder(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "realestate-seed: "+format+"\n", a...)
	os.Exit(1)
}
