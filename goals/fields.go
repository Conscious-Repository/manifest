package goals

import (
	"strings"

	"manifest/record"
)

// inlineFieldRe is THE kernel grammar (record.FieldRe) — no local copy.
var inlineFieldRe = record.FieldRe

// Field is the kernel's inline-field pair.
type Field = record.Field

// fieldRole is the emitting goal's place in the ladder — it decides which
// canonical fields are written (§1).
type fieldRole int

const (
	roleAnnual    fieldRole = iota // a 1-year goal: identity only
	roleRock                       // a 90-day Rock: identity + quarter/serves/status/rolled-from
	roleStageTask                  // a stage or task: identity only when explicit
)

// parseFields is the kernel scan (record.ParseFields).
func parseFields(text string) (string, []Field) { return record.ParseFields(text) }

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

	// Aliases (portal-matcher vocabulary) on an ANNUAL — a backlog item's
	// rock can resolve to a 1-year goal. Rocks emit alias after serves
	// (below) to preserve the established field order.
	if role == roleAnnual {
		for _, al := range g.Aliases {
			out = append(out, Field{Key: "alias", Value: al})
		}
	}

	if role == roleRock {
		if g.Quarter != "" {
			out = append(out, Field{Key: "quarter", Value: g.Quarter})
		}
		for _, sv := range g.Serves {
			out = append(out, Field{Key: "serves", Value: sv})
		}
		for _, al := range g.Aliases {
			out = append(out, Field{Key: "alias", Value: al})
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

	// Finish-line fields (goals-finish-lines §1): until/verify on every role,
	// kpi on Rocks + stages (not annuals). Emitted after the Rock metadata,
	// before owner.
	if g.Until != "" {
		out = append(out, Field{Key: "until", Value: g.Until})
	}
	if g.Verify != "" {
		out = append(out, Field{Key: "verify", Value: g.Verify})
	}
	if g.Kpi != "" && role != roleAnnual {
		out = append(out, Field{Key: "kpi", Value: g.Kpi})
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
	case "owner", "goal", "quarter", "serves", "alias", "aliases", "status", "rolled-from", "moved", "due",
		"until", "verify", "kpi":
		return true
	}
	return false
}
