package realestate

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"manifest/mdfm"
	"manifest/vaultindex"
)

// The chart of accounts (money-workbench v2, owner call 2026-08-19): ONE
// global category registry every entity shares — the "internal quickbooks"
// vocabulary behind the workbench's category autosuggest. A hand-editable
// record at system/realestate/categories.md, categories: [money-categories],
// frontmatter list items "name | kind | class":
//
//	kind:  income | expense
//	class: operating | project — the axis that keeps a leased property's
//	       utilities OUT of its rehab budget ([cat:: operating] at apply)
//
// When no record exists yet, a built-in default set serves suggestions —
// zero vault writes until the owner's first create gesture materializes the
// record (defaults + the new entry).

type MoneyCategory struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`  // income | expense
	Class string `json:"class"` // operating | project
}

// DefaultMoneyCategories is the day-one vocabulary (also the seed content
// the first create gesture writes into the record).
func DefaultMoneyCategories() []MoneyCategory {
	mk := func(kind, class string, names ...string) []MoneyCategory {
		out := make([]MoneyCategory, 0, len(names))
		for _, n := range names {
			out = append(out, MoneyCategory{Name: n, Kind: kind, Class: class})
		}
		return out
	}
	var all []MoneyCategory
	all = append(all, mk("income", "operating", "rent", "deposit", "capital")...)
	all = append(all, mk("expense", "operating",
		"internet", "electric", "gas", "water-sewer", "trash", "insurance",
		"property-tax", "maintenance", "lawn-snow", "management", "legal", "hoa")...)
	all = append(all, mk("expense", "project",
		"materials", "labor", "closing", "drawings", "permits", "demo",
		"roof", "windows", "plumbing", "electrical", "hvac", "framing",
		"drywall", "paint", "flooring", "appliances", "landscaping")...)
	return all
}

// ParseMoneyCategories reads the registry record's items list (tolerant —
// malformed items are skipped; missing kind/class default expense/project,
// the conservative bucket that never hides money from the rehab budget).
func ParseMoneyCategories(raw string) []MoneyCategory {
	fm, _ := mdfm.Split(raw)
	var out []MoneyCategory
	for _, item := range quotedList(fm["items"]) {
		parts := strings.Split(item, "|")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		if name == "" {
			continue
		}
		c := MoneyCategory{Name: name, Kind: "expense", Class: "project"}
		if len(parts) > 1 {
			if k := strings.ToLower(strings.TrimSpace(parts[1])); k == "income" || k == "expense" {
				c.Kind = k
			}
		}
		if len(parts) > 2 {
			if cl := strings.ToLower(strings.TrimSpace(parts[2])); cl == "operating" || cl == "project" {
				c.Class = cl
			}
		}
		out = append(out, c)
	}
	return out
}

// EmitMoneyCategoryItems renders the frontmatter list value (the entity-
// accounts idiom: quoted "name | kind | class" items, inline flow).
func EmitMoneyCategoryItems(cats []MoneyCategory) string {
	items := make([]string, 0, len(cats))
	for _, c := range cats {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		items = append(items, `"`+c.Name+` | `+c.Kind+` | `+c.Class+`"`)
	}
	return "[" + strings.Join(items, ", ") + "]"
}

// MoneyCategoriesPath is the registry record's vault-relative path under root.
func MoneyCategoriesPath(root string) string { return root + "/categories.md" }

// MoneyCategories returns the registry — the record when it exists, else the
// built-in defaults — sorted by name. The bool reports whether a record
// backs it (false = defaults, nothing on disk yet).
func (s *Service) MoneyCategories() ([]MoneyCategory, bool) {
	refs, err := s.ix.Category("money-categories", vaultindex.SortNameAsc)
	if err == nil {
		for _, r := range refs {
			raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(r.Path)))
			if err != nil {
				continue
			}
			cats := ParseMoneyCategories(string(raw))
			sort.Slice(cats, func(i, j int) bool { return cats[i].Name < cats[j].Name })
			return cats, true
		}
	}
	cats := DefaultMoneyCategories()
	sort.Slice(cats, func(i, j int) bool { return cats[i].Name < cats[j].Name })
	return cats, false
}

// CategoryClass resolves a category name → operating|project ("" when the
// name isn't in the registry — apply treats unknown as project, the
// conservative default).
func (s *Service) CategoryClass(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	cats, _ := s.MoneyCategories()
	for _, c := range cats {
		if c.Name == name {
			return c.Class
		}
	}
	return ""
}
