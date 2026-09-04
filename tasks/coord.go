package tasks

import "strings"

// Coordination state (manifest P1 Phase 1): the thin task learns two
// cross-cutting FIELDS — what must finish first, and how much it matters —
// and everything else is DERIVED from them on read. There is no stored
// `blocked` that can drift: a task is blocked exactly while one of its
// dependencies is still open. Rank stays what it was (position in a list);
// priority is importance regardless of position. No project graph, no
// subtasks — a dependency is one id in a list, nothing more.

// The closed priority set, in descending importance. Words, not numbers, so
// a hand-edited line can never be misread as a rank.
const (
	PriorityHigh = "high"
	PriorityMed  = "med"
	PriorityLow  = "low"
)

// Priorities lists the closed set in order (high first).
var Priorities = []string{PriorityHigh, PriorityMed, PriorityLow}

// NormalizePriority maps a raw value onto the closed set ("" clears). ok is
// false for anything outside it — the parser keeps such values verbatim as
// an unrecognized field rather than guessing.
func NormalizePriority(v string) (string, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "":
		return "", true
	case PriorityHigh, PriorityMed, PriorityLow:
		return v, true
	case "medium":
		return PriorityMed, true
	}
	return "", false
}

// ValidPriority reports whether v is "" or a member of the closed set.
func ValidPriority(v string) bool {
	_, ok := NormalizePriority(v)
	return ok
}

// PriorityN is the ordinal (high 3 · med 2 · low 1 · unset 0) — a sort key,
// never something that is stored.
func (t *Task) PriorityN() int {
	switch t.Priority {
	case PriorityHigh:
		return 3
	case PriorityMed:
		return 2
	case PriorityLow:
		return 1
	}
	return 0
}

// SetDepends replaces the dependency list: trimmed, de-duplicated, order
// kept, and never the task itself.
func (t *Task) SetDepends(ids []string) {
	t.Depends = cleanDepends(ids, t.ID, t.explicitID())
}

// AddDepends appends id (no-op when present, empty, or self).
func (t *Task) AddDepends(id string) {
	t.SetDepends(append(append([]string{}, t.Depends...), id))
}

// RemoveDepends drops id from the list (no-op when absent).
func (t *Task) RemoveDepends(id string) {
	id = strings.TrimSpace(id)
	var keep []string
	for _, d := range t.Depends {
		if d != id {
			keep = append(keep, d)
		}
	}
	t.Depends = keep
}

// cleanDepends is the one normalizer behind parse + SetDepends.
func cleanDepends(ids []string, self ...string) []string {
	seen := map[string]bool{}
	for _, s := range self {
		if s != "" {
			seen[s] = true
		}
	}
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// splitDepends reads the `[depends:: a, b]` value.
func splitDepends(val string) []string {
	return cleanDepends(strings.Split(val, ","))
}

// joinDepends renders the list back ("" when empty).
func joinDepends(ids []string) string { return strings.Join(ids, ", ") }

// Resolver answers, for a dependency id, whether it names something known
// and whether that something is still open. The doc resolves within its own
// file; the server's projection resolves across every task source.
type Resolver func(id string) (open, known bool)

// ResolveDepends partitions a dependency list: ids that are known and still
// open BLOCK; ids nobody knows are surfaced (tolerated — the target may live
// in another domain or not exist yet) but never block; satisfied ones drop.
func ResolveDepends(depends []string, resolve Resolver) (blockedBy, unresolved []string) {
	for _, id := range depends {
		open, known := resolve(id)
		switch {
		case !known:
			unresolved = append(unresolved, id)
		case open:
			blockedBy = append(blockedBy, id)
		}
	}
	return blockedBy, unresolved
}

// BlockedBy lists the open dependencies (plus the unknown ones, separately).
func (t *Task) BlockedBy(resolve Resolver) (blockedBy, unresolved []string) {
	if t.Checked || len(t.Depends) == 0 {
		return nil, nil
	}
	return ResolveDepends(t.Depends, resolve)
}

// Blocked is the derived state: open with at least one open dependency.
func (t *Task) Blocked(resolve Resolver) bool {
	b, _ := t.BlockedBy(resolve)
	return len(b) > 0
}

// StateWith is State() extended with `blocked` — done wins, then blocked
// (the other task's state is the harder constraint), then waiting, then open.
func (t *Task) StateWith(resolve Resolver) string {
	if !t.Checked && t.Blocked(resolve) {
		return "blocked"
	}
	return t.State()
}

// Resolver is the file-local lookup: tasks (loose + bucket) and issues share
// one id space, so a task may depend on a decision being made.
func (d *Doc) Resolver() Resolver {
	return func(id string) (open, known bool) {
		if _, t := d.Find(id); t != nil {
			return !t.Checked, true
		}
		if _, is := d.FindIssue(id); is != nil {
			return !is.Checked, true
		}
		return false, false
	}
}

// StateOf is the doc-resolved state projection (open | waiting | blocked | done).
func (d *Doc) StateOf(t *Task) string { return t.StateWith(d.Resolver()) }

// Dependents answers "what depends on this": the OPEN tasks in the file
// whose depends list names id (done dependents are history, not a constraint).
func (d *Doc) Dependents(id string) []string {
	return d.dependentsIndex()[id]
}

// dependentsIndex inverts every open task's depends list (id → dependents).
func (d *Doc) dependentsIndex() map[string][]string {
	idx := map[string][]string{}
	for _, dom := range d.Domains {
		dom.AllTasks(func(_ *Bucket, t *Task) {
			if t.Checked {
				return
			}
			for _, dep := range t.Depends {
				idx[dep] = append(idx[dep], t.ID)
			}
		})
	}
	return idx
}

// Coord is the per-task coordination projection: the state, who blocks it,
// what it could not resolve, and what waits on it.
type Coord struct {
	State      string   `json:"state"`
	BlockedBy  []string `json:"blockedBy,omitempty"`
	Unresolved []string `json:"unresolved,omitempty"`
	Dependents []string `json:"dependents,omitempty"`
}

// CoordOf computes one task's coordination projection within the doc.
func (d *Doc) CoordOf(t *Task) Coord {
	blockedBy, unresolved := t.BlockedBy(d.Resolver())
	return Coord{
		State: t.StateWith(d.Resolver()), BlockedBy: blockedBy,
		Unresolved: unresolved, Dependents: d.Dependents(t.ID),
	}
}
