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
		if g.Start != "" {
			out = append(out, Field{Key: "start", Value: g.Start})
		}
		if g.Due != "" {
			out = append(out, Field{Key: "due", Value: g.Due})
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

	if g.Owner != "" && !record.OwnerIsMe(g.Owner) {
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
// passed through as unknown). `start`/`due` are timeline dates on rocks (portal §7).
func isRecognizedField(key string) bool {
	switch strings.ToLower(key) {
	case "owner", "goal", "quarter", "start", "due", "serves", "alias", "aliases", "status", "rolled-from", "moved":
		return true
	case "until", "verify", "kpi":
		// retired (owner call 2026-08-19): recognized so they NEVER pass
		// through as unknown fields, rebuilt from nothing — a save drops them.
		// The startup migration strips them from the live file once.
		return true
	}
	return false
}
