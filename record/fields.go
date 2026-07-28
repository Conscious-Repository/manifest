package record

import (
	"regexp"
	"strings"
	"sync"
)

// THE inline-field grammar (ARCHITECTURE §3): [key:: value], Dataview-style.
// This is the only bracketed-field regex in the codebase — every domain
// imports it. key = letter-led word chars/hyphens; value = anything up to the
// closing bracket (a value can therefore never CONTAIN "]" — wikilink values
// use the dedicated affordances below).
var FieldRe = regexp.MustCompile(`\[([A-Za-z][\w-]*)\s*::\s*([^\]]*)\]`)

// Field is one inline [key:: value] pair, key case preserved so unrecognized
// fields round-trip unchanged.
type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ParseFields strips every [key:: value] field out of text and returns the
// cleaned text (fields removed, whitespace collapsed) plus the fields in
// source order. Keys and values are trimmed.
func ParseFields(text string) (string, []Field) {
	var fields []Field
	clean := FieldRe.ReplaceAllStringFunc(text, func(m string) string {
		sm := FieldRe.FindStringSubmatch(m)
		fields = append(fields, Field{Key: strings.TrimSpace(sm[1]), Value: strings.TrimSpace(sm[2])})
		return ""
	})
	return strings.Join(strings.Fields(clean), " "), fields
}

// EmitField renders one canonical field.
func EmitField(key, value string) string { return "[" + key + ":: " + value + "]" }

// ---- wikilink-valued fields (first-class affordance, not per-site hacks) ----
//
// A field value that IS a [[wikilink]] cannot pass through FieldRe (the "]]"
// terminates the match early). Two supported shapes:
//   - MATCH-FIRST: WikilinkValueRe(key) matches `[key:: [[target]]]` and must
//     be applied BEFORE the generic scan (todos `waiting`).
//   - EMIT-LAST: ExtractTrailingField pulls a `[key:: …]` emitted as the LAST
//     field on a line, value taken greedily so "]]" survives (goals archive
//     `evidence`).

var (
	wlMu    sync.Mutex
	wlCache = map[string]*regexp.Regexp{}
)

// WikilinkValueRe returns the match-first pattern for `[key:: [[target]]]`.
func WikilinkValueRe(key string) *regexp.Regexp {
	wlMu.Lock()
	defer wlMu.Unlock()
	if re, ok := wlCache[key]; ok {
		return re
	}
	re := regexp.MustCompile(`\[` + regexp.QuoteMeta(key) + `::\s*(\[\[[^\]]+\]\])\s*\]`)
	wlCache[key] = re
	return re
}

// ExtractTrailingField pulls a trailing `[key:: value]` off content, value
// taken greedily to the final bracket (so a [[wikilink]] value survives).
// Returns the value and the remaining content; value "" when absent.
func ExtractTrailingField(content, key string) (value, rest string) {
	marker := "[" + key + ":: "
	i := strings.LastIndex(content, marker)
	if i < 0 {
		return "", content
	}
	v := content[i+len(marker):]
	value = strings.TrimSuffix(strings.TrimRight(v, " "), "]")
	rest = strings.TrimRight(content[:i], " ")
	return value, rest
}

// StripBracket keeps a value from breaking the field grammar on emit: a full
// [[wikilink]] passes when allowed; anything else loses lone brackets.
func StripBracket(v string, allowWikilink bool) string {
	v = strings.TrimSpace(v)
	if allowWikilink && strings.HasPrefix(v, "[[") && strings.HasSuffix(v, "]]") {
		return v
	}
	return strings.NewReplacer("[", "", "]", "").Replace(v)
}
