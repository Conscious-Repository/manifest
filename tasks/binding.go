package tasks

import "strings"

// Artifact binding (manifest P1 artifacts): the thin task learns two more
// id-list FIELDS — the artifacts it produced and the artifacts it consumed.
// Both are references into the artifact registry, never content: the task
// line stays one line, the bytes stay in the registry, and "what did this
// task produce" / "who consumes this artifact" derive from the ids on read
// (artifacts.LinkIndex). Same grammar and normalizer as [depends::].

// SetOutputs replaces the produced-artifact list (trimmed, deduped, ordered).
func (t *Task) SetOutputs(ids []string) { t.Outputs = cleanIDs(ids) }

// SetInputs replaces the consumed-artifact list.
func (t *Task) SetInputs(ids []string) { t.Inputs = cleanIDs(ids) }

// AddOutput appends an artifact id (no-op when present or empty).
func (t *Task) AddOutput(id string) {
	t.Outputs = cleanIDs(append(append([]string{}, t.Outputs...), id))
}

// AddInput appends an artifact id (no-op when present or empty).
func (t *Task) AddInput(id string) { t.Inputs = cleanIDs(append(append([]string{}, t.Inputs...), id)) }

// RemoveOutput drops an id (no-op when absent).
func (t *Task) RemoveOutput(id string) { t.Outputs = dropID(t.Outputs, id) }

// RemoveInput drops an id (no-op when absent).
func (t *Task) RemoveInput(id string) { t.Inputs = dropID(t.Inputs, id) }

// cleanIDs is cleanDepends without a self to exclude — an artifact id can
// never be the task's own id.
func cleanIDs(ids []string) []string { return cleanDepends(ids) }

// splitIDs reads a comma list field value.
func splitIDs(val string) []string { return cleanIDs(strings.Split(val, ",")) }

// joinIDs renders the list back.
func joinIDs(ids []string) string { return strings.Join(ids, ", ") }

func dropID(ids []string, id string) []string {
	id = strings.TrimSpace(id)
	var keep []string
	for _, x := range ids {
		if x != id {
			keep = append(keep, x)
		}
	}
	return keep
}
