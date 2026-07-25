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

// Service reads property records from the vault index + their frontmatter,
// sections, and ledger sidecar from disk. Index-backed (like reading.Service) so
// the running index watcher keeps it live with no dedicated watcher.
type Service struct{ ix *vaultindex.Index }

func New(ix *vaultindex.Index) *Service { return &Service{ix: ix} }

var wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// Properties returns every property record, sorted by entity then address so the
// Board's entity grouping is stable. The frontend re-sorts/filters as needed.
func (s *Service) Properties() ([]Property, error) {
	refs, err := s.ix.Category("property", vaultindex.SortNameAsc)
	if err != nil {
		return nil, err
	}
	out := make([]Property, 0, len(refs))
	for _, r := range refs {
		if p, ok := s.parse(r.Path, r.Name); ok {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := groupKey(out[i].Entity), groupKey(out[j].Entity); a != b {
			return a < b
		}
		return strings.ToLower(out[i].Address) < strings.ToLower(out[j].Address)
	})
	return out, nil
}

// Get returns the single property record at slug (basename), freshly parsed.
func (s *Service) Get(slug string) (Property, bool) {
	for _, r := range mustRefs(s.ix.Category("property", vaultindex.SortNameAsc)) {
		if strings.EqualFold(r.Name, slug) {
			return s.parse(r.Path, r.Name)
		}
	}
	return Property{}, false
}

func mustRefs(refs []vaultindex.NoteRef, _ error) []vaultindex.NoteRef { return refs }

// parse reads one record's frontmatter + `## budget`/`## log` sections + its
// ledger csv sidecar into a Property, then derives the rollup. Tolerant: missing
// or garbled fields are simply omitted (a hand edit in Obsidian reads back the same).
func (s *Service) parse(rel, name string) (Property, bool) {
	full := filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(rel))
	raw, err := os.ReadFile(full)
	if err != nil {
		return Property{}, false
	}
	fm, body := mdfm.Split(string(raw))
	p := Property{
		Path: rel, Slug: name, Name: name,
		Address: unquote(fm["address"]),
		Entity:  unquote(fm["entity"]),
		Status:  strings.ToLower(strings.TrimSpace(fm["status"])),
		Kind:    strings.ToLower(strings.TrimSpace(fm["kind"])),
		Control: strings.ToLower(strings.TrimSpace(fm["control"])),
		Hidden:  strings.EqualFold(strings.TrimSpace(fm["hidden"]), "true"),
	}
	if m := wikilinkRe.FindStringSubmatch(fm["deal"]); m != nil {
		p.Deal = strings.TrimSpace(m[1])
	}
	sections := parseSections(body)
	p.Budget = parseBudget(sections["budget"])
	p.Log = parseLog(sections["log"])
	if len(p.Log) > 0 {
		p.LastLog = p.Log[0]
	}
	if led, err := os.ReadFile(ledgerPath(full)); err == nil {
		p.Ledger = parseLedger(led)
	}
	p.Rollup = computeRollup(p.Budget, p.Ledger)
	return p, true
}

// ledgerPath is the csv sidecar beside a property's .md (…/<slug>.md → …/<slug>.ledger.csv).
func ledgerPath(mdFull string) string {
	return strings.TrimSuffix(mdFull, ".md") + ".ledger.csv"
}

// LedgerRel maps a vault-relative .md path to its vault-relative ledger csv path.
func LedgerRel(mdRel string) string {
	return strings.TrimSuffix(mdRel, ".md") + ".ledger.csv"
}

// groupKey sorts blank entities ("unassigned") last on the Board.
func groupKey(entity string) string {
	if strings.TrimSpace(entity) == "" {
		return "￿" // sorts after any real name
	}
	return strings.ToLower(entity)
}

// unquote strips a YAML-quoted scalar (addresses carry commas the importer may quote).
func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"') {
		return strings.ReplaceAll(v[1:len(v)-1], `\"`, `"`)
	}
	if len(v) >= 2 && (v[0] == '\'' && v[len(v)-1] == '\'') {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}
