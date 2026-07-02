package vaultindex

import (
	"sort"
	"strings"
)

// VocabVariant is one exact category spelling with its note count + examples.
type VocabVariant struct {
	Value    string
	Count    int
	Examples []string // up to a few note names using this exact value
}

// VocabCluster groups near-duplicate category values (same stem, different exact
// spelling) so drift is visible. A cluster only exists when a stem has TWO OR
// MORE distinct spellings — a single clean value is not drift.
type VocabCluster struct {
	Stem     string
	Total    int
	Variants []VocabVariant
}

// Vocabulary clusters near-duplicate category values (audit §3): it lowercases,
// strips punctuation, and folds a trailing plural 's', then reports any stem
// with more than one exact spelling, each with counts + example notes. The
// engine NEVER rewrites values — this view exists so the user standardizes them
// in Obsidian. Since the vault was hand-unified, this should come back nearly
// empty; an empty result is itself the validation that no drift exists.
func (ix *Index) Vocabulary() ([]VocabCluster, error) {
	rows, err := ix.db.Query(`SELECT category, COUNT(DISTINCT path) FROM note_categories GROUP BY category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type v struct {
		count int
	}
	byStem := map[string]map[string]int{} // stem -> value -> count
	for rows.Next() {
		var value string
		var count int
		if err := rows.Scan(&value, &count); err != nil {
			return nil, err
		}
		k := stemKey(value)
		if byStem[k] == nil {
			byStem[k] = map[string]int{}
		}
		byStem[k][value] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var clusters []VocabCluster
	for stem, values := range byStem {
		if len(values) < 2 {
			continue // one clean spelling — not drift
		}
		c := VocabCluster{Stem: stem}
		for value, count := range values {
			c.Total += count
			c.Variants = append(c.Variants, VocabVariant{
				Value: value, Count: count, Examples: ix.categoryExamples(value, 3),
			})
		}
		sort.Slice(c.Variants, func(i, j int) bool { return c.Variants[i].Count > c.Variants[j].Count })
		clusters = append(clusters, c)
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Total != clusters[j].Total {
			return clusters[i].Total > clusters[j].Total
		}
		return clusters[i].Stem < clusters[j].Stem
	})
	return clusters, nil
}

func (ix *Index) categoryExamples(value string, n int) []string {
	rows, err := ix.db.Query(
		`SELECT n.name FROM note_categories c JOIN notes n ON n.path=c.path
		 WHERE c.category = ? ORDER BY n.name ASC LIMIT ?`, value, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			out = append(out, name)
		}
	}
	return out
}

// stemKey folds a category value to its comparison stem: lowercase, drop every
// non-alphanumeric character (so `first-meeting`/`first_meeting` agree), and
// remove a single trailing plural 's' on stems long enough that it is unlikely
// to be significant. This is used ONLY to detect drift, never to match queries.
func stemKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	k := b.String()
	if len(k) > 3 && strings.HasSuffix(k, "s") {
		k = k[:len(k)-1]
	}
	return k
}
