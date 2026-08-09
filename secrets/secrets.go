// Package secrets is the credential scanner guarding the aion extraction
// pipeline (aion-domain spec §3 safety): proposal payloads, rendered record
// lines, and backfill imports are scanned before rendering and again before
// persistence; a hit refuses loudly, naming the class — NEVER the value.
//
// BYTE CONTRACT: the excalibur engine carries a twin of this table
// (engine internal/secrets) so proposals are blocked at authoring too.
// Parity is pinned by the shared fixture list in secrets_test.go — change
// one side only with its twin.
package secrets

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// Finding names a matched secret class and where it matched — the value is
// deliberately absent.
type Finding struct {
	Class  string // aws-access-key | aws-secret-key | private-key | bearer-token | credential-assignment | high-entropy
	Offset int
	End    int
}

var (
	awsAccessRe = regexp.MustCompile(`\b(AKIA|ASIA|ABIA|ACCA)[0-9A-Z]{16}\b`)
	// a 40-char base64ish token near an aws/secret keyword
	awsSecretRe = regexp.MustCompile(`(?i)(aws|secret)[^\n]{0,40}?[=:]\s*['"]?([A-Za-z0-9/+]{40})['"]?`)
	pemRe       = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	bearerRe    = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-]{20,}`)
	credAssign  = regexp.MustCompile(`(?i)\b(password|passwd|token|api[_-]?key|secret[_-]?key|auth[_-]?token)\b\s*[=:]\s*['"]?([^\s'"]{8,})`)
)

// Scan returns every secret-class hit in text (empty = clean).
func Scan(text string) []Finding {
	var out []Finding
	add := func(class string, locs [][]int) {
		for _, l := range locs {
			out = append(out, Finding{Class: class, Offset: l[0], End: l[1]})
		}
	}
	add("aws-access-key", awsAccessRe.FindAllStringIndex(text, -1))
	add("aws-secret-key", awsSecretRe.FindAllStringIndex(text, -1))
	add("private-key", pemRe.FindAllStringIndex(text, -1))
	add("bearer-token", bearerRe.FindAllStringIndex(text, -1))
	for _, m := range credAssign.FindAllStringSubmatchIndex(text, -1) {
		val := text[m[4]:m[5]]
		// wikilinks/ids/prose are not credentials; demand some entropy
		if entropy(val) >= 3.0 {
			out = append(out, Finding{Class: "credential-assignment", Offset: m[0], End: m[1]})
		}
	}
	// standalone high-entropy blobs ≥ 24 chars (keys pasted bare)
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '+' || r == '/' || r == '=' || r == '_' || r == '-' ||
			('0' <= r && r <= '9') || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z'))
	}) {
		if len(tok) >= 24 && entropy(tok) > 4.5 && !gitShaLike(tok) {
			if at := strings.Index(text, tok); at >= 0 {
				out = append(out, Finding{Class: "high-entropy", Offset: at, End: at + len(tok)})
			}
		}
	}
	return out
}

// Mask replaces every matched secret span with "•••" — display-side defense
// so a slipped-through credential never renders in a card.
func Mask(text string) string {
	fs := Scan(text)
	if len(fs) == 0 {
		return text
	}
	// apply spans right-to-left (position order, not class order) so earlier
	// offsets stay valid; overlaps degrade harmlessly to double-masking
	sort.Slice(fs, func(i, j int) bool { return fs[i].Offset > fs[j].Offset })
	for i := 0; i < len(fs); i++ {
		f := fs[i]
		if f.Offset < 0 || f.End > len(text) || f.Offset >= f.End {
			continue
		}
		text = text[:f.Offset] + "•••" + text[f.End:]
	}
	return text
}

// Classes summarizes findings as a deduped class list for error messages.
func Classes(fs []Finding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fs {
		if !seen[f.Class] {
			seen[f.Class] = true
			out = append(out, f.Class)
		}
	}
	return out
}

// gitShaLike excludes lowercase-hex blobs (commit hashes) from the entropy
// net — they are ubiquitous in this system and never credentials.
func gitShaLike(s string) bool {
	for _, r := range s {
		if !(('0' <= r && r <= '9') || ('a' <= r && r <= 'f')) {
			return false
		}
	}
	return true
}

func entropy(s string) float64 {
	if s == "" {
		return 0
	}
	freq := map[rune]float64{}
	for _, r := range s {
		freq[r]++
	}
	var h float64
	n := float64(len([]rune(s)))
	for _, c := range freq {
		p := c / n
		h -= p * math.Log2(p)
	}
	return h
}
