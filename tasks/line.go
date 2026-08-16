package tasks

import (
	"strconv"
	"strings"
)

// The exported line grammar (redesign stage 4): other domains — the property
// `## todos` section — declare their checkbox lines over THIS grammar instead
// of growing a second parser (kernel doctrine §3: one regex in one file).

// ParseLine parses one checkbox line's body (everything after "- [x] ") into a
// Task. ID is left to the caller's id scheme (an explicit [todo:: id] pin is
// available via ExplicitID).
func ParseLine(checked bool, rest string) *Task { return parseTask(checked, rest) }

// EmitLine renders a todo back to its canonical markdown line ("- [ ] …").
func EmitLine(t *Task) string { return emitTask(t) }

// ExplicitID returns the [todo:: id] identity pin, or "".
func (t *Task) ExplicitID() string { return t.explicitID() }

// PinID freezes id as the explicit [todo:: id] identity (idempotent).
func (t *Task) PinID(id string) {
	if t.explicitID() == "" && id != "" {
		t.Fields = append(t.Fields, Field{Key: "todo", Value: id})
	}
}

// FieldValue reads an unrecognized field's value ("" when absent) — the
// property todo's [work:: id] back-tether reads through this.
func (t *Task) FieldValue(key string) string {
	for _, f := range t.Fields {
		if strings.EqualFold(f.Key, key) {
			return f.Value
		}
	}
	return ""
}

// RankN parses [rank:: n] as an int (0 = unranked / unparsable).
func (t *Task) RankN() int {
	n, err := strconv.Atoi(t.Rank)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// LineSlug exposes the kernel slug at the shared id cap for peer id schemes.
func LineSlug(s string) string { return slug(s) }
