package recruiting

import (
	"path"
	"strings"
)

// Corpora is the record-SHAPE → parse∘serialize round-trip registry: the hook
// the kernel's central property test (record/corpus_test.go) and this
// domain's own fixpoint tests drive. Byte-identical output on canonical input
// is the fixpoint guarantee (ARCHITECTURE §3).
//
// Unlike aion.Corpora, the keys here are SHAPES rather than filenames,
// because roles and candidates are one file per record. `roles/*.md` and
// `candidates/*.md` are globs; the rest are exact vault-relative paths.
var Corpora = map[string]func(raw string) string{
	"seeds.md":          func(raw string) string { return SerializeSeeds(ParseSeeds(raw)) },
	"network/people.md": func(raw string) string { return SerializeNetworkPeople(ParseNetworkPeople(raw)) },
	"network/edges.md":  func(raw string) string { return SerializeEdges(ParseEdges(raw)) },
	"roles/*.md":        func(raw string) string { return SerializeRole(ParseRole(raw)) },
	"candidates/*.md":   func(raw string) string { return SerializeCandidate(ParseCandidate(raw)) },
}

// Files are the fixed, non-glob records — the ones Ensure seeds by name.
var Files = []string{"seeds.md", "network/people.md", "network/edges.md"}

// RoundTrip resolves a recruiting-root-relative path (slash form) to its
// declared round-trip, or nil when the path is not part of this domain.
func RoundTrip(rel string) func(string) string {
	rel = strings.TrimPrefix(path.Clean(strings.TrimSpace(rel)), "./")
	if fn, ok := Corpora[rel]; ok {
		return fn
	}
	for shape, fn := range Corpora {
		if !strings.Contains(shape, "*") {
			continue
		}
		if ok, err := path.Match(shape, rel); err == nil && ok {
			return fn
		}
	}
	return nil
}
