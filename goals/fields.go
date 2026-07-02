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

// fieldRole is the emitting goal's place in the ladder — it decides which
// canonical fields are written (§1).
type fieldRole int

const (
	roleAnnual    fieldRole = iota // a 1-year goal: identity only
	roleRock                       // a 90-day Rock: identity + quarter/serves/status/rolled-from
	roleStageTask                  // a stage or task: identity only when explicit
)

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

// canonicalFields returns the fields to emit for a goal, in a deterministic order:
// goal (identity), then Rock metadata (quarter, serves, status, rolled-from), then
// owner (only when not "me"), then any unrecognized fields in original order.
// Recognized fields are rebuilt from struct state so edits take effect; `due` is
// recognized-but-never-emitted (retired — §0/§1).
func canonicalFields(g *Goal, role fieldRole) []Field {
	var out []Field

	switch role {
	case roleRock, roleAnnual:
		if id := g.identity(); id != "" {
			out = append(out, Field{Key: "goal", Value: id})
		}
	default: // stage / task: only pin identity when explicitly set
		if id := g.explicitID(); id != "" {
			out = append(out, Field{Key: "goal", Value: id})
		}
	}

	if role == roleRock {
		if g.Quarter != "" {
			out = append(out, Field{Key: "quarter", Value: g.Quarter})
		}
		if g.Serves != "" {
			out = append(out, Field{Key: "serves", Value: g.Serves})
		}
		if g.Status != "" && !strings.EqualFold(g.Status, "active") {
			out = append(out, Field{Key: "status", Value: g.Status})
		}
		if g.RolledFrom != "" {
			out = append(out, Field{Key: "rolled-from", Value: g.RolledFrom})
		}
		if g.Moved != "" {
			out = append(out, Field{Key: "moved", Value: g.Moved})
		}
	}

	if g.Owner != "" && !strings.EqualFold(g.Owner, "me") {
		out = append(out, Field{Key: "owner", Value: g.Owner})
	}

	for _, f := range g.Fields {
		if isRecognizedField(f.Key) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// isRecognizedField reports keys the model owns (rebuilt from struct state, not
// passed through as unknown). `due` is included so any legacy dates are dropped on
// the next save rather than round-tripped as junk.
func isRecognizedField(key string) bool {
	switch strings.ToLower(key) {
	case "owner", "goal", "quarter", "serves", "status", "rolled-from", "moved", "due":
		return true
	}
	return false
}
