package sources

import (
	"strconv"
	"strings"
)

// AUTHORSHIP — the matching works a draft's publication rows cite, read back
// from the labelled snippets the adapters themselves wrote (pubmed:
// "position: last of 7 · pubdate: 2024 Mar"; openalex: "position: last
// author · pubdate: 2024-03-01 · cited_by_count: 12 · authors: 7"). Reading
// the evidence rather than a second field means a run cached before ranking
// existed ranks the same as a new one, and every number a rank reason quotes
// is a row the owner can open.

// Authorship is one matching work, as the source printed it.
type Authorship struct {
	Ref      string `json:"ref"`                // the evidence row's URL — the paper's page
	Year     int    `json:"year,omitempty"`     // 0 when the row carried no date
	Authors  int    `json:"authors,omitempty"`  // byline length; 0 when unknown
	Position string `json:"position,omitempty"` // sole | first | middle | last | "" (not said)
	Cited    int    `json:"cited,omitempty"`    // the registry's citation count, when it gave one
}

// Authorships returns the distinct works a draft's publication rows cite,
// in row order, and the registry's own count of matching works for this
// person when the source reported one (OpenAlex group_by), else 0.
func Authorships(d CandidateDraft) ([]Authorship, int) {
	var out []Authorship
	seen := map[string]bool{}
	matching := 0
	for _, ev := range d.Evidence {
		if ev.Kind != EvidencePublication {
			continue
		}
		snip := strings.TrimSpace(ev.Snippet)
		if rest, ok := strings.CutPrefix(snip, "works matching this search attributed to this author: "); ok {
			if n, err := strconv.Atoi(strings.Fields(rest + " ")[0]); err == nil && n > matching {
				matching = n
			}
			continue
		}
		ref := strings.TrimSpace(ev.URLOrFile)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		a := Authorship{Ref: ref}
		for _, part := range strings.Split(snip, " · ") {
			k, v, ok := strings.Cut(part, ": ")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch strings.TrimSpace(k) {
			case "position":
				a.Position, a.Authors = parsePosition(v, a.Authors)
			case "pubdate":
				a.Year = firstYear(v)
			case "authors":
				if n := leadingInt(v); n > 0 {
					a.Authors = n
				}
			case "cited_by_count":
				a.Cited = leadingInt(v)
			}
		}
		if a.Authors == 1 && (a.Position == "first" || a.Position == "last") {
			a.Position = "sole"
		}
		out = append(out, a)
	}
	return out, matching
}

// parsePosition reads both spellings: pubmed's "last of 7" and openalex's
// "last author" / "co-author".
func parsePosition(v string, authors int) (string, int) {
	f := strings.Fields(strings.ToLower(v))
	if len(f) == 0 {
		return "", authors
	}
	if len(f) >= 3 && f[1] == "of" {
		if n, err := strconv.Atoi(f[2]); err == nil {
			authors = n
		}
	}
	switch f[0] {
	case "sole", "first", "middle", "last":
		return f[0], authors
	case "co-author":
		return "middle", authors
	}
	return "", authors
}

func firstYear(v string) int {
	for i := 0; i+4 <= len(v); i++ {
		if y, err := strconv.Atoi(v[i : i+4]); err == nil && y >= 1900 && y <= 2100 {
			return y
		}
	}
	return 0
}

func leadingInt(v string) int {
	f := strings.Fields(v)
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(f[0])
	return n
}

// FoldName is the deterministic name fold the aggregation keys on — case,
// punctuation, spacing. Exported for the one caller that needs to SAY two
// people share a name without ever merging them on it.
func FoldName(raw string) string { return foldName(raw) }
