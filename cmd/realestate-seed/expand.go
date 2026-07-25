package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// -expand (admin-portal plan §A): the one-time re-sync that promotes the vault
// to the editable canonical. Reads the CURRENT re-portal deals.json (10 deals —
// picks up bayard's 31→28 re-carve and the new rehab trio) and:
//
//   - creates a full property record for EVERY bundle member parcel (~49 new),
//     each with md + empty ledger + geo.json + a PROPERTY-SLICE source.json;
//   - rewrites each multi-parcel deal's sidecars: source.json becomes
//     deal-level assumptions + property_slugs (members live in their own
//     records now), geo.json becomes the member FeatureCollection, and the md
//     frontmatter gains status + the member address list (body preserved);
//   - refreshes single-parcel records' source.json to the current FULL deal
//     object (deal-level + its one property — the property page edits both)
//     and re-syncs their md status;
//   - refreshes the 9 tracked parcels' source.json from neighborhood.json.
//
// md + ledger are write-once (never clobbered); source.json / geo.json are
// REFRESHED (re-portal is still the truth for this one sync — after this build
// manifest becomes the editing surface). Dry-run by default; idempotent.
func runExpand(cfg miniConfig, srcDir string, apply bool) {
	reRoot := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate"))
	// -expand reads the LIVE re-portal tree (never the vendored context copy —
	// that snapshot is the stale 9-deal era).
	dealsPath := filepath.Join(srcDir, "src/data/deals.json")
	nbPath := filepath.Join(srcDir, "src/data/neighborhood.json")
	dealsRaw := readJSONArray(dealsPath, "deals.json")
	nbRaw := readJSONArray(nbPath, "neighborhood.json")
	for _, d := range dealsRaw {
		assertNoPII(d, "deals.json")
	}

	var ops []expandOp
	for _, draw := range dealsRaw {
		var d rawDeal
		if err := json.Unmarshal(draw, &d); err != nil {
			fatal("deals.json: bad deal: %v", err)
		}
		if len(d.Properties) > 1 {
			ops = append(ops, expandMulti(reRoot, draw, d)...)
		} else if len(d.Properties) == 1 {
			ops = append(ops, expandSingle(reRoot, draw, d)...)
		}
	}
	// tracked parcels: refresh source.json only
	for _, nraw := range nbRaw {
		var n rawParcel
		if err := json.Unmarshal(nraw, &n); err != nil {
			continue
		}
		slug := slugify(n.Address)
		if slug == "" {
			slug = "parcel-" + n.ParcelID
		}
		ops = append(ops, expandOp{
			Rel: reRoot + "/properties/" + slug + ".source.json", Bytes: pretty(nraw),
			Mode: opRefresh, Note: "tracked " + slug + " source refresh",
		})
	}

	report(ops, cfg.VaultPath, apply)
	if !apply {
		fmt.Println("\nDRY RUN — nothing written. Re-run with -expand -apply to write.")
		return
	}
	for _, op := range ops {
		if err := op.write(cfg.VaultPath); err != nil {
			fatal("writing %s: %v", op.Rel, err)
		}
	}
	fmt.Println("\napplied.")
}

type opMode int

const (
	opCreate  opMode = iota // write-once — skip when the file exists
	opRefresh               // overwrite when content differs
	opMDSync                // md: create if missing, else upsert frontmatter keys + preserve body
)

type expandOp struct {
	Rel   string
	Bytes []byte
	Mode  opMode
	Note  string
	FM    map[string]string // opMDSync: frontmatter keys to upsert on an existing file
}

func (o expandOp) status(vault string) string {
	full := filepath.Join(vault, filepath.FromSlash(o.Rel))
	existing, err := os.ReadFile(full)
	switch o.Mode {
	case opCreate:
		if err == nil {
			return "skip  "
		}
		return "create"
	case opRefresh:
		if err != nil {
			return "create"
		}
		if bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(o.Bytes)) {
			return "same  "
		}
		return "update"
	case opMDSync:
		if err != nil {
			return "create"
		}
		next := upsertFM(string(existing), o.FM)
		if next == string(existing) {
			return "same  "
		}
		return "update"
	}
	return "?"
}

func (o expandOp) write(vault string) error {
	full := filepath.Join(vault, filepath.FromSlash(o.Rel))
	if !isUnder(full, filepath.Join(vault, "system")) {
		return fmt.Errorf("outside system zone")
	}
	existing, err := os.ReadFile(full)
	switch o.Mode {
	case opCreate:
		if err == nil {
			return nil // write-once
		}
	case opRefresh:
		if err == nil && bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(o.Bytes)) {
			return nil
		}
	case opMDSync:
		if err == nil {
			next := upsertFM(string(existing), o.FM)
			if next == string(existing) {
				return nil
			}
			return os.WriteFile(full, []byte(next), 0o644)
		}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, o.Bytes, 0o644)
}

func report(ops []expandOp, vault string, apply bool) {
	counts := map[string]int{}
	fmt.Printf("realestate-seed -expand — %d file ops\n\n", len(ops))
	for _, o := range ops {
		st := o.status(vault)
		counts[strings.TrimSpace(st)]++
		if strings.TrimSpace(st) != "same" { // quiet on no-ops; the count line covers them
			fmt.Printf("  [%s] %s\n", st, o.Note)
		}
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println()
	for _, k := range keys {
		fmt.Printf("  %s: %d\n", k, counts[k])
	}
}

// expandMulti: member records + the deal record's three sidecars.
func expandMulti(reRoot string, draw json.RawMessage, d rawDeal) []expandOp {
	var ops []expandOp
	dealStatus := strings.ToLower(strings.TrimSpace(d.Status))
	if dealStatus == "" {
		dealStatus = "negotiating"
	}
	memberStatus := statusForDeal(d)
	// members of an un-underwritten opportunity bundle aren't controlled yet
	control := "owned"
	if dealStatus == "opportunity" || d.Opportunity {
		control = "tracked"
	}

	var slugs, addrs []string
	var feats []json.RawMessage
	for _, praw := range d.Properties {
		var pr rawProp
		if err := json.Unmarshal(praw, &pr); err != nil {
			fatal("deals.json %s: bad property: %v", d.Slug, err)
		}
		slug := slugify(pr.Address)
		if slug == "" {
			slug = "parcel-" + pr.ParcelID
		}
		slugs = append(slugs, slug)
		addr := pr.FullAddress
		if addr == "" {
			addr = pr.Address
		}
		addrs = append(addrs, addr)
		if len(pr.Geo) > 0 {
			feats = append(feats, pr.Geo)
		}
		base := reRoot + "/properties/" + slug
		body := propBody(addr, dealPropBudget(pr))
		md := memberRecord(addr, memberStatus, kindFor(d.ProjectType), control, d.Slug, body)
		ops = append(ops,
			expandOp{Rel: base + ".md", Bytes: []byte(md), Mode: opCreate, Note: "member " + slug + "  [" + memberStatus + " · " + control + " · " + d.Slug + "]"},
			expandOp{Rel: base + ".ledger.csv", Bytes: []byte(ledgerHeader), Mode: opCreate, Note: "  ledger.csv"},
			expandOp{Rel: base + ".source.json", Bytes: pretty(praw), Mode: opRefresh, Note: "  source.json (property slice)"},
		)
		if len(pr.Geo) > 0 {
			ops = append(ops, expandOp{Rel: base + ".geo.json", Bytes: pretty(pr.Geo), Mode: opRefresh, Note: "  geo.json"})
		}
	}

	dealName := d.Name
	if dealName == "" {
		dealName = titleize(d.Slug)
	}
	dealBase := reRoot + "/deals/" + d.Slug
	ops = append(ops,
		expandOp{
			Rel: dealBase + ".md", Mode: opMDSync,
			Bytes: []byte(dealMD(dealName, dealStatus, addrs)),
			FM:    map[string]string{"status": dealStatus, "properties": quotedFMList(addrs)},
			Note:  "deal   " + d.Slug + "  [" + dealStatus + " · " + itoa(len(slugs)) + " members]",
		},
		expandOp{Rel: dealBase + ".source.json", Bytes: dealLevelSource(draw, slugs), Mode: opRefresh, Note: "  source.json (deal-level + property_slugs)"},
	)
	if len(feats) > 0 {
		ops = append(ops, expandOp{Rel: dealBase + ".geo.json", Bytes: featureCollection(feats), Mode: opRefresh, Note: "  geo.json (members)"})
	}
	return ops
}

// expandSingle: refresh the one record's source to the FULL current deal object
// + re-sync md status; create the record when it's a brand-new single deal.
func expandSingle(reRoot string, draw json.RawMessage, d rawDeal) []expandOp {
	var pr rawProp
	_ = json.Unmarshal(d.Properties[0], &pr)
	addr := pr.FullAddress
	if addr == "" {
		addr = pr.Address
	}
	base := reRoot + "/properties/" + d.Slug
	status := statusForDeal(d)
	body := propBody(addr, dealPropBudget(pr))
	ops := []expandOp{
		{Rel: base + ".md", Mode: opMDSync,
			Bytes: []byte(propRecord(addr, status, kindFor(d.ProjectType), "owned", "", body)),
			FM:    map[string]string{"status": status},
			Note:  "single " + d.Slug + "  [status→" + status + "]"},
		{Rel: base + ".ledger.csv", Bytes: []byte(ledgerHeader), Mode: opCreate, Note: "  ledger.csv"},
		{Rel: base + ".source.json", Bytes: pretty(draw), Mode: opRefresh, Note: "  source.json (full deal object)"},
	}
	if len(pr.Geo) > 0 {
		ops = append(ops, expandOp{Rel: base + ".geo.json", Bytes: pretty(pr.Geo), Mode: opRefresh, Note: "  geo.json"})
	}
	return ops
}

// memberRecord is propRecord with a deal:: slug link.
func memberRecord(address, status, kind, control, dealSlug, body string) string {
	var b strings.Builder
	b.WriteString("---\ncategories: [property]\n")
	b.WriteString("address: " + address + "\n")
	b.WriteString("status: " + status + "\n")
	b.WriteString("kind: " + kind + "\n")
	b.WriteString("control: " + control + "\n")
	b.WriteString("deal: \"[[" + dealSlug + "]]\"\n")
	b.WriteString("hidden: false\n")
	b.WriteString("---\n")
	b.WriteString(body)
	return b.String()
}

func dealMD(name, status string, addrs []string) string {
	var b strings.Builder
	b.WriteString("---\ncategories: [deal]\n")
	b.WriteString("status: " + status + "\n")
	b.WriteString("properties: " + quotedFMList(addrs) + "\n")
	b.WriteString("---\n\n# " + name + "\n")
	return b.String()
}

func quotedFMList(items []string) string {
	return "[" + strings.Join(quoteAll(items), ", ") + "]"
}

// dealLevelSource strips properties[] from the deal object and adds
// property_slugs — the members now live in their own records.
func dealLevelSource(draw json.RawMessage, slugs []string) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(draw, &m); err != nil {
		fatal("deal source: %v", err)
	}
	delete(m, "properties")
	sl, _ := json.Marshal(slugs)
	m["property_slugs"] = sl
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fatal("deal source marshal: %v", err)
	}
	return out
}

// upsertFM upserts keys into an md file's frontmatter, preserving every other
// line and the body byte-for-byte.
func upsertFM(raw string, kv map[string]string) string {
	if !strings.HasPrefix(raw, "---\n") {
		return raw
	}
	end := strings.Index(raw[4:], "\n---")
	if end < 0 {
		return raw
	}
	block, rest := raw[4:4+end], raw[4+end:]
	lines := strings.Split(block, "\n")
	seen := map[string]bool{}
	for i, l := range lines {
		k := strings.TrimSpace(strings.SplitN(l, ":", 2)[0])
		if v, ok := kv[k]; ok {
			lines[i] = k + ": " + v
			seen[k] = true
		}
	}
	var keys []string
	for k := range kv {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, k+": "+kv[k])
	}
	return "---\n" + strings.Join(lines, "\n") + rest
}

// pretty re-indents raw JSON stably (2-space) so refresh comparisons are stable.
func pretty(raw json.RawMessage) []byte {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return out
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
