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
	Name            string   `json:"name"` // frontmatter name, else the slug
	Owners          []Owner  `json:"owners,omitempty"`
	AdminCategories []string `json:"adminCategories,omitempty"`
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
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}
