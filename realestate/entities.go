package realestate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"manifest/mdfm"
	"manifest/vaultindex"
)

// Entity records (pass-5 §5/§6): `system/realestate/entities/<slug>.md`,
// `categories: [entity]`. Frontmatter carries the ownership links and the
// admin-category list; the body is free prose. Hand-editable like every record.
type Entity struct {
	Path            string   `json:"path"`
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`            // frontmatter name, else the slug
	Trade           string   `json:"trade,omitempty"` // contractors: masonry · roofing · plumbing …
	Owners          []Owner  `json:"owners,omitempty"`
	AdminCategories []string `json:"adminCategories,omitempty"`
	// Partnered splits the accountant handoff: personal books (Anderson
	// Organization, IGS MO) vs partner entities (Garden SPE, ODA Group, Fund I).
	Partnered bool `json:"partnered,omitempty"`
	// Accounts is the bank-account scaffold (RE spec §3 Settings/Entities):
	// state is informational until a live pull exists — statements bind via
	// the ImportMemory label→entity map, which stays the source of truth.
	Accounts []EntityAccount `json:"accounts,omitempty"`
}

// EntityAccount is one bank-account row on an entity: frontmatter list items
// "label | kind | state" (kind: operating | construction-draw | card;
// state: not-connected | live | needs-reauth).
type EntityAccount struct {
	Label string `json:"label"`
	Kind  string `json:"kind"`
	State string `json:"state"`
}

// EntityHoldings are DERIVED from property records ({entity, from}), never
// typed: owned = books it sits on today; acquiring = destination while under
// contract (from names the seller). The two stay separate, never summed —
// accountant reads owned; the pipeline stays visible via acquiring.
type EntityHoldings struct {
	Owned     int `json:"owned"`
	Acquiring int `json:"acquiring"`
}

// Holdings derives per-entity holdings (keyed by entity NAME) from the
// property list. Hidden records are skipped; a property with no entity has no
// destination and counts nowhere (the modelling gap the spec warns about —
// surface those in the UI rather than papering over them here).
func Holdings(props []Property) map[string]EntityHoldings {
	out := map[string]EntityHoldings{}
	for _, p := range props {
		if p.Hidden || p.Entity == "" {
			continue
		}
		h := out[p.Entity]
		if p.From != "" {
			h.Acquiring++
		} else {
			h.Owned++
		}
		out[p.Entity] = h
	}
	return out
}

// Owner is one ownership edge: a [[wikilink]] target + a percentage.
type Owner struct {
	Ref     string  `json:"ref"` // entity slug or person name (wikilink target)
	Percent float64 `json:"percent"`
}

var ownerRe = regexp.MustCompile(`\[\[([^\]]+)\]\]\s*([\d.]+)\s*%`)

// Entities returns every entity record, sorted by name.
func (s *Service) Entities() []Entity {
	refs, err := s.ix.Category("entity", vaultindex.SortNameAsc)
	if err != nil {
		return nil
	}
	var out []Entity
	for _, r := range refs {
		raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(r.Path)))
		if err != nil {
			continue
		}
		fm, _ := mdfm.Split(string(raw))
		e := Entity{Path: r.Path, Slug: r.Name, Name: r.Name}
		if n := unquote(fm["name"]); n != "" {
			e.Name = n
		}
		for _, item := range quotedList(fm["owners"]) {
			if m := ownerRe.FindStringSubmatch(item); m != nil {
				var pctv float64
				if f, err := parseMoney(m[2]); err == nil {
					pctv = f
				}
				e.Owners = append(e.Owners, Owner{Ref: strings.TrimSpace(m[1]), Percent: pctv})
			}
		}
		e.AdminCategories = mdfm.List(fm["admin-categories"])
		e.Partnered = strings.EqualFold(strings.TrimSpace(fm["partnered"]), "true")
		for _, item := range quotedList(fm["accounts"]) {
			parts := strings.Split(item, "|")
			acc := EntityAccount{State: "not-connected"}
			if len(parts) > 0 {
				acc.Label = strings.TrimSpace(parts[0])
			}
			if len(parts) > 1 {
				acc.Kind = strings.TrimSpace(parts[1])
			}
			if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
				acc.State = strings.TrimSpace(parts[2])
			}
			if acc.Label != "" {
				e.Accounts = append(e.Accounts, acc)
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// OwnershipCycle reports whether adding `owner` above `entity` would create a
// cycle (walking parent links). Owner refs may be entity NAMES or slugs —
// both resolve; person refs (no entity record) can never cycle.
func OwnershipCycle(entities []Entity, entitySlug, ownerRef string) bool {
	resolve := map[string]string{} // lower(name|slug) → slug
	for _, e := range entities {
		resolve[strings.ToLower(e.Name)] = e.Slug
		resolve[strings.ToLower(e.Slug)] = e.Slug
	}
	parents := map[string][]string{} // slug → parent slugs
	for _, e := range entities {
		for _, o := range e.Owners {
			if p, ok := resolve[strings.ToLower(o.Ref)]; ok {
				parents[e.Slug] = append(parents[e.Slug], p)
			}
		}
	}
	start, ok := resolve[strings.ToLower(ownerRef)]
	if !ok {
		return false // a person or unknown ref — can't cycle
	}
	seen := map[string]bool{}
	var walk func(slug string) bool
	walk = func(slug string) bool {
		if strings.EqualFold(slug, entitySlug) {
			return true
		}
		if seen[slug] {
			return false
		}
		seen[slug] = true
		for _, p := range parents[slug] {
			if walk(p) {
				return true
			}
		}
		return false
	}
	return walk(start)
}

// Contractors returns every contractor record (name + slug), for autocomplete.
func (s *Service) Contractors() []Entity {
	refs, err := s.ix.Category("contractor", vaultindex.SortNameAsc)
	if err != nil {
		return nil
	}
	var out []Entity
	for _, r := range refs {
		raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(r.Path)))
		if err != nil {
			continue
		}
		fm, _ := mdfm.Split(string(raw))
		e := Entity{Path: r.Path, Slug: r.Name, Name: r.Name}
		if n := unquote(fm["name"]); n != "" {
			e.Name = n
		}
		e.Trade = strings.ToLower(strings.TrimSpace(unquote(fm["trade"])))
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}
