package aion

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"manifest/record"
)

// fieldTok is one parsed field occurrence IN SOURCE ORDER — the key to the
// fixpoint on the owner's hand-authored corpus: recognized values are
// rebuilt from struct state at emit, but every field keeps ITS OWN position
// and spelling (done vs done_on), so a parse→emit round-trip is
// byte-identical for any field ordering the owner wrote.
type fieldTok struct {
	Key  string // as written (case preserved for unknowns; lowered for dispatch)
	Wiki bool   // a [source:: [[note]]] occurrence (value lives in Sources)
}

// parseLineFields tokenizes a line's content: wikilink-valued [source::]
// fields (kernel affordance — FieldRe cannot carry "]]") and generic fields,
// merged in source order. Returns clean text, the sources, the unknown
// field values (parse order), and the ordered token stream.
func parseLineFields(content string) (text string, sources []string, unknown []record.Field, toks []fieldTok) {
	type span struct {
		start, end int
		tok        fieldTok
		value      string
	}
	var spans []span
	re := record.WikilinkValueRe("source")
	for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
		spans = append(spans, span{m[0], m[1], fieldTok{Key: "source", Wiki: true},
			strings.TrimSuffix(strings.TrimPrefix(content[m[2]:m[3]], "[["), "]]")})
	}
	for _, m := range record.FieldRe.FindAllStringSubmatchIndex(content, -1) {
		// skip anything inside an already-claimed wikilink span
		inside := false
		for _, s := range spans {
			if m[0] >= s.start && m[1] <= s.end {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		key := strings.TrimSpace(content[m[2]:m[3]])
		val := strings.TrimSpace(content[m[4]:m[5]])
		spans = append(spans, span{m[0], m[1], fieldTok{Key: key}, val})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })

	var clean strings.Builder
	last := 0
	for _, s := range spans {
		if s.start > last {
			clean.WriteString(content[last:s.start])
		}
		last = s.end
		if s.tok.Wiki {
			sources = append(sources, s.value)
		} else if strings.EqualFold(s.tok.Key, "source") {
			// plain (non-wikilink) source value
			if s.value != "" {
				sources = append(sources, s.value)
			}
			s.tok.Wiki = true // treated as a source slot on emit
		} else if !recognizedBacklogKey(s.tok.Key) {
			unknown = append(unknown, record.Field{Key: s.tok.Key, Value: s.value})
		}
		toks = append(toks, s.tok)
	}
	if last < len(content) {
		clean.WriteString(content[last:])
	}
	return strings.Join(strings.Fields(clean.String()), " "), sources, unknown, toks
}

func recognizedBacklogKey(key string) bool {
	switch strings.ToLower(key) {
	case "kind", "owner", "captured", "rock", "due", "status",
		"done", "done_on", "needed_by", "decided", "outcome", "rank", "id":
		return true
	}
	return false
}

// setBacklogField routes one recognized field into the item struct.
func setBacklogField(it *BacklogItem, key, value string) {
	switch strings.ToLower(key) {
	case "id":
		it.ID = value
		it.IDPersisted = value != ""
	case "kind":
		it.Kind = strings.ToLower(value)
	case "owner":
		it.Owner = value
	case "captured":
		it.Captured = value
	case "rock":
		it.Rock = value
	case "due":
		it.Due = value
	case "status":
		it.Status = value
	case "done", "done_on":
		it.DoneOn = value
	case "needed_by":
		it.NeededBy = value
	case "decided":
		it.Decided = value
	case "outcome":
		it.Outcome = value
	case "rank":
		it.Rank = value
	}
}

// backlogFieldValue reads the CURRENT struct value for a recognized key —
// edits take effect in place, at the field's original position.
func backlogFieldValue(it *BacklogItem, key string) string {
	switch strings.ToLower(key) {
	case "id":
		if it.IDPersisted {
			return it.ID
		}
		return ""
	case "kind":
		return it.Kind
	case "owner":
		return it.Owner
	case "captured":
		return it.Captured
	case "rock":
		return it.Rock
	case "due":
		return it.Due
	case "status":
		return it.Status
	case "done", "done_on":
		return it.DoneOn
	case "needed_by":
		return it.NeededBy
	case "decided":
		return it.Decided
	case "outcome":
		return it.Outcome
	case "rank":
		return it.Rank
	}
	return ""
}

// canonicalAppendOrder is where NEW recognized fields land (fields set by an
// edit/accept that had no position in the source line) — matching the
// owner's corpus convention: kind, owner, sources, captured, then state.
// "source" is the slot where unpositioned sources flush.
var canonicalAppendOrder = []string{
	"id", "kind", "owner", "source", "captured", "status", "needed_by", "due", "done", "decided", "outcome", "rock", "rank",
}

// backlogFields renders an item's field stream: positioned tokens first
// (values re-read from struct state; emptied fields drop; the done/done_on
// spelling each token carried is preserved), then any extra sources, then
// newly-set recognized fields in canonical order, then unknowns that had no
// position (edits never invent those).
func backlogFields(it *BacklogItem) []string {
	var out []string
	emitted := map[string]bool{}
	srcN, unkN := 0, 0
	for _, tok := range it.toks {
		if tok.Wiki {
			if srcN < len(it.Sources) {
				out = append(out, emitSource(it.Sources[srcN]))
				srcN++
			}
			continue
		}
		if recognizedBacklogKey(tok.Key) {
			if v := backlogFieldValue(it, tok.Key); v != "" {
				out = append(out, record.EmitField(tok.Key, record.StripBracket(v, false)))
			}
			emitted[normKey(tok.Key)] = true
			continue
		}
		if unkN < len(it.Unknown) {
			f := it.Unknown[unkN]
			out = append(out, record.EmitField(f.Key, f.Value))
			unkN++
		}
	}
	flushSources := func() {
		for ; srcN < len(it.Sources); srcN++ {
			out = append(out, emitSource(it.Sources[srcN]))
		}
	}
	for _, key := range canonicalAppendOrder {
		if key == "source" {
			flushSources()
			continue
		}
		if emitted[normKey(key)] {
			continue
		}
		if v := backlogFieldValue(it, key); v != "" {
			out = append(out, record.EmitField(key, record.StripBracket(v, false)))
			emitted[normKey(key)] = true
		}
	}
	flushSources()
	// any unknown fields without positions ride at the end (edits never
	// invent those)
	for ; unkN < len(it.Unknown); unkN++ {
		f := it.Unknown[unkN]
		out = append(out, record.EmitField(f.Key, f.Value))
	}
	return out
}

// normKey folds the done/done_on alias so position-preserved emission never
// double-emits the completion date.
func normKey(key string) string {
	k := strings.ToLower(key)
	if k == "done_on" {
		return "done"
	}
	return k
}

// emitSource renders one canonical source field with a wikilink value.
func emitSource(note string) string {
	return "[source:: [[" + note + "]]]"
}

// NormalizeTitle is the item-identity normalization: lowercase, whitespace
// collapsed, trailing punctuation stripped.
func NormalizeTitle(s string) string {
	s = strings.Join(strings.Fields(strings.ToLower(s)), " ")
	return strings.TrimRight(s, ".!?,;: ")
}

// ItemID is the legacy identity fallback for rows that have not yet received
// a persisted portal id.
func ItemID(kind, title string) string {
	sum := sha1.Sum([]byte(kind + "|" + NormalizeTitle(title)))
	return hex.EncodeToString(sum[:])[:8]
}

// PortalItemID assigns the cross-surface identity for a new or migrated row.
func PortalItemID(title string, taken map[string]bool) string {
	base := "aion-bl/" + record.Slug(title, 60)
	if base == "aion-bl/" {
		base = "aion-bl/item"
	}
	id := base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// HeuristicID derives the stable id for a heuristic statement.
func HeuristicID(text string) string { return ItemID("heuristic", text) }

// ValidOwner reports whether owner is empty, a registered set of initials
// (slash-separated for shared ownership, e.g. "BA/RT"), or a free name.
// Only slash-separated ALL-CAPS tokens are checked against the registry —
// free-text names always pass (the handoff keeps names for non-roster people).
func ValidOwner(owner string, people []*Person) bool {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return true
	}
	known := map[string]bool{}
	for _, p := range people {
		known[strings.ToUpper(p.Initials)] = true
	}
	for _, tok := range strings.Split(owner, "/") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return false
		}
		if tok == strings.ToUpper(tok) && len(tok) <= 3 && !known[tok] {
			return false // looks like initials but not in the registry
		}
	}
	return true
}
