package recruiting

import (
	"strings"

	"manifest/recruiting/sources"
)

// EDGE IDENTITY — one fact, one spelling (intake plan §5 stage 2).
//
// A coauthor edge names a person who is usually not a record here: the other
// twenty-five authors on the paper are strangers, and most will stay that
// way. Naming them by display name would be the recurring bug this codebase
// has met before — two spellings of one fact — in its worst form: namesakes
// silently merged, and one person spelled two ways silently split. So an
// edge endpoint is a DURABLE EXTERNAL KEY (`ext/orcid/…`, `ext/openalex/…`,
// `ext/github/…`) until the person becomes a record here, at which point
// every edge that named them is repointed at their record id.
//
// The resolution happens in exactly one place — this file — and it runs in
// both directions:
//
//	inbound   saveDraftEdges maps an endpoint onto a record id when the
//	          person is ALREADY here.
//	outbound  accepting a draft repoints edges that named that person by an
//	          external key, so the graph heals itself as the board fills.

// extIndex maps every external identity the vault knows onto the record that
// carries it. Built per write; the corpus is small (tens of records) and a
// stale index would mean a graph that quietly disagrees with itself.
func (s *Store) extIndex() map[string]string {
	out := map[string]string{}
	for _, slug := range s.CandidateSlugs() {
		doc := s.LoadCandidate(slug)
		id := doc.Get("id")
		if id == "" {
			id = CandidateID(slug)
		}
		for _, k := range extKeysOfRecord(doc.Get("source_ref"), doc.Profile()) {
			out[k] = id
		}
	}
	for _, p := range s.LoadNetworkPeople().People() {
		if strings.TrimSpace(p.ID) == "" {
			continue
		}
		for _, k := range extKeysOfRecord("", map[string]string{
			"github": p.GitHub, "linkedin": p.LinkedIn,
		}) {
			out[k] = p.ID
		}
	}
	return out
}

// extKeysOfRecord derives every external key a record answers to: its
// adapter source_ref, plus any identifying profile link it carries.
func extKeysOfRecord(sourceRef string, profile map[string]string) []string {
	var out []string
	if k := extKeyFromSourceRef(sourceRef); k != "" {
		out = append(out, k)
	}
	for _, v := range profile {
		if k := ExtKeyFromURL(v); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// extKeyFromSourceRef turns "openalex:A123" / "orcid:0000-…" / "github:torvalds"
// into the graph key for that identity.
func extKeyFromSourceRef(ref string) string {
	ref = strings.TrimSpace(ref)
	source, id, ok := strings.Cut(ref, ":")
	if !ok || strings.TrimSpace(id) == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "openalex":
		return sources.ExtNodePrefix + "openalex/" + strings.TrimSpace(id)
	case "orcid":
		return sources.ExtNodePrefix + "orcid/" + strings.TrimSpace(id)
	case "github":
		return sources.ExtNodePrefix + "github/" + strings.ToLower(strings.TrimSpace(id))
	}
	return ""
}

// ExtKeyFromURL recognizes the identity links a profile carries. Anything
// else — a personal site, a lab page — identifies nobody on its own and
// returns "".
func ExtKeyFromURL(raw string) string {
	u := strings.TrimSpace(strings.ToLower(raw))
	if u == "" {
		return ""
	}
	for _, p := range []struct{ host, kind string }{
		{"orcid.org/", "orcid"},
		{"openalex.org/", "openalex"},
		{"github.com/", "github"},
	} {
		i := strings.Index(u, p.host)
		if i < 0 {
			continue
		}
		id := strings.Trim(u[i+len(p.host):], "/")
		if id == "" || strings.Contains(id, "/") && p.kind != "github" {
			continue
		}
		if p.kind == "github" {
			id = strings.SplitN(id, "/", 2)[0] // a repo URL still names its owner
			if id == "" {
				continue
			}
		}
		if p.kind == "openalex" {
			id = strings.ToUpper(id)
			if !strings.HasPrefix(id, "A") {
				continue // a work id is not a person
			}
		}
		return sources.ExtNodePrefix + p.kind + "/" + id
	}
	return ""
}

// extKeysOfDraft is every external key the accepted person answers to, so
// edges written before they were a record can be repointed at them.
func extKeysOfDraft(d sources.CandidateDraft) []string {
	var out []string
	if k := extKeyFromSourceRef(SourceRef(d)); k != "" {
		out = append(out, k)
	}
	for _, l := range d.Links {
		if k := ExtKeyFromURL(l); k != "" {
			out = append(out, k)
		}
	}
	return dedupeStrings(out)
}

// repointEdges rewrites every endpoint in `from` to `to`, and returns how
// many rows changed. Called when an external identity becomes a record.
func repointEdges(doc *EdgesDoc, from []string, to string) int {
	if to == "" || len(from) == 0 {
		return 0
	}
	want := map[string]bool{}
	for _, f := range from {
		if f = strings.TrimSpace(f); f != "" {
			want[f] = true
		}
	}
	n := 0
	for _, ln := range doc.Lines {
		if ln.Row == nil {
			continue
		}
		for _, key := range []string{"from", "to"} {
			if want[strings.TrimSpace(ln.Row.Get(key))] {
				ln.Row.Set(key, to)
				n++
			}
		}
	}
	return n
}

// edgeKey is the identity of a relationship claim for dedupe purposes: the
// same two people, the same kind. A second run of the same paper must not
// double every edge it already wrote.
func edgeKey(from, to, kind string) string {
	a, b := strings.TrimSpace(from), strings.TrimSpace(to)
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b + "\x00" + strings.TrimSpace(kind)
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
