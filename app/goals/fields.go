package goals

import (
	"regexp"
	"strings"
)

// inlineFieldRe matches Dataview-style inline fields: [key:: value].
var inlineFieldRe = regexp.MustCompile(`\[([A-Za-z][\w-]*)\s*::\s*([^\]]*)\]`)

// Field is one inline [key:: value] pair, preserving the original key case so
// unrecognized fields round-trip unchanged.
type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// parseFields strips every [key:: value] field out of text and returns the
// cleaned text (fields removed, surrounding whitespace collapsed) plus the
// fields in source order.
func parseFields(text string) (string, []Field) {
	var fields []Field
	clean := inlineFieldRe.ReplaceAllStringFunc(text, func(m string) string {
		sm := inlineFieldRe.FindStringSubmatch(m)
		fields = append(fields, Field{Key: strings.TrimSpace(sm[1]), Value: strings.TrimSpace(sm[2])})
		return ""
	})
	return strings.Join(strings.Fields(clean), " "), fields
}

// canonicalFields returns the fields to emit for a goal, in a deterministic
// order: owner, due, goal, then any unrecognized fields in their original order.
// owner/due/goal are rebuilt from the goal's resolved values so edits take
// effect; the implicit "me" owner is never written out.
func canonicalFields(g *Goal) []Field {
	var out []Field
	if g.Owner != "" {
		out = append(out, Field{Key: "owner", Value: g.Owner})
	}
	if g.Due != "" {
		out = append(out, Field{Key: "due", Value: g.Due})
	}
	if id := g.explicitID(); id != "" {
		out = append(out, Field{Key: "goal", Value: id})
	}
	for _, f := range g.Fields {
		if isRecognizedField(f.Key) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func isRecognizedField(key string) bool {
	return strings.EqualFold(key, "owner") ||
		strings.EqualFold(key, "due") ||
		strings.EqualFold(key, "goal")
}
