package chatthreads

import (
	"encoding/json"
	"strings"
)

// ProposalFence is the fenced block a delegate turn emits, one per change. The
// grammar mirrors hermes/proposals.go (same tag, same fence rules) but the spec
// types are chat's own — item-field patches and plan-section swaps that apply
// IN-PORTAL through the item/plan write paths, not the FEED approvals lane.
const ProposalFence = "manifest-proposal"

// ParseProposals extracts every manifest-proposal block from a kairos brief.
// Returns the brief with blocks replaced by "→ proposed: …" placeholders, the
// structured proposals (State pre-set to "pending"), and human warnings for
// blocks that were dropped. Two types:
//
//	set-field        {"type":"set-field","item":"<id>","field":"status","value":"done"}
//	replace-section  {"type":"replace-section","item":"<id>","section":"plan","body":"1. …"}
func ParseProposals(reply string) (clean string, props []Proposal, warnings []string) {
	lines := strings.Split(reply, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		n := countBackticks(lines[i])
		if n < 3 || strings.TrimSpace(lines[i][n:]) != ProposalFence {
			out = append(out, lines[i])
			continue
		}
		var body []string
		closed := false
		j := i + 1
		for ; j < len(lines); j++ {
			m := countBackticks(lines[j])
			if m >= n && strings.TrimSpace(lines[j]) == strings.Repeat("`", m) {
				closed = true
				break
			}
			body = append(body, lines[j])
		}
		raw := strings.TrimSpace(strings.Join(body, "\n"))
		var spec struct {
			Type    string `json:"type"`
			Item    string `json:"item"`
			Field   string `json:"field"`
			Value   string `json:"value"`
			Section string `json:"section"`
			Body    string `json:"body"`
		}
		if err := json.Unmarshal([]byte(raw), &spec); err != nil {
			warnings = append(warnings, "a proposal block was dropped — not valid JSON: "+err.Error())
		} else if p, err := toProposal(spec.Type, spec.Item, spec.Field, spec.Value, spec.Section, spec.Body); err != "" {
			warnings = append(warnings, "a proposal block was dropped — "+err)
		} else {
			props = append(props, p)
			out = append(out, "→ proposed: "+p.Verb)
		}
		if !closed {
			i = len(lines)
		} else {
			i = j
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n")), props, warnings
}

func toProposal(typ, item, field, value, section, body string) (Proposal, string) {
	item = strings.TrimPrefix(strings.TrimSpace(item), "aion:") // normalize to the bare portal id
	if item == "" {
		return Proposal{}, "missing item id"
	}
	switch typ {
	case "set-field":
		field = strings.TrimSpace(field)
		if field == "" || strings.TrimSpace(value) == "" {
			return Proposal{}, "set-field needs a field and value"
		}
		return Proposal{
			Verb: "set " + field, Type: "set-field", ItemID: item, Target: item,
			Field: field, Value: strings.TrimSpace(value), State: "pending",
		}, ""
	case "replace-section":
		section = strings.TrimSpace(section)
		if section != "plan" && section != "description" {
			return Proposal{}, "replace-section needs section plan|description"
		}
		if strings.TrimSpace(body) == "" {
			return Proposal{}, "replace-section needs a body"
		}
		return Proposal{
			Verb: "replace ## " + section, Type: "replace-section", ItemID: item,
			Target:  "system/todo-plans/" + planSlug("aion:"+item) + ".md",
			Section: section, Body: body, State: "pending",
		}, ""
	default:
		return Proposal{}, "unknown proposal type " + typ
	}
}

// planSlug mirrors server/todo_plans.go planSlug (:/ → -, 64 cap) so the target
// path a proposal shows matches the real record file.
func planSlug(id string) string {
	s := strings.NewReplacer(":", "-", "/", "-").Replace(id)
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func countBackticks(s string) int {
	i := 0
	for i < len(s) && s[i] == '`' {
		i++
	}
	return i
}
