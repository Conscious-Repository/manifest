package hermes

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Phase 2 (approval-gated execution): a fired ("go"-phase) Hermes turn cannot
// change the world directly — its toolset scope stays read-only. Instead the
// go-phase prompt teaches it to EMIT fenced `manifest-proposal` JSON blocks in
// its reply; manifest parses them here and files each as a pending proposal in
// the approvals inbox (FEED), where the owner confirms or rejects. This file is
// the pure parsing/validation half — the server maps Specs onto
// approvals.Proposal and files them (dependency direction: this package knows
// nothing of the approvals store).

// ProposalFence is the fenced-block language tag Hermes uses.
const ProposalFence = "manifest-proposal"

// ProposalSpec is one validated world-change request from a Hermes reply.
// Exactly one JSON object per fenced block; fields beyond the type's schema are
// ignored (forward tolerance).
type ProposalSpec struct {
	Type string `json:"type"` // create-vault-note | run-errand | aion-backlog | re-backlog

	// create-vault-note
	Title string `json:"title"` // also the backlog item title
	Date  string `json:"date"`  // YYYY-MM-DD; empty → today (server fills)
	Body  string `json:"body"`  // note content / backlog evidence

	// run-errand
	Errand  string `json:"errand"`
	Account string `json:"account"`
	Goal    string `json:"goal"`

	// aion-backlog / re-backlog
	Kind    string   `json:"kind"` // task | decision
	Owner   string   `json:"owner"`
	Rock    string   `json:"rock"`
	Due     string   `json:"due"`
	Sources []string `json:"sources"`

	// goals-item (§12 2026-08-19): a placement into the owner's goals.md.
	// Title/Owner/Due are shared with the fields above.
	Mode       string   `json:"mode"`       // add | edit | move
	Level      string   `json:"level"`      // rock | milestone
	Area       string   `json:"area"`       // "Home", "Real Estate", …
	ParentID   string   `json:"parentId"`   // milestone add/move: the rock it goes under
	TargetID   string   `json:"targetId"`   // edit/move: the goal being changed
	AnchorText string   `json:"anchorText"` // edit/move: the target's current text (staleness guard)
	Serves     []string `json:"serves"`
}

// specTypes is the v1 allowlist — every type maps onto an EXISTING confirm
// lane in the approvals package; nothing here invents a new effector.
var specTypes = map[string]bool{
	"create-vault-note": true,
	"run-errand":        true,
	"aion-backlog":      true,
	"re-backlog":        true,
	"goals-item":        true,
}

// validate enforces the per-type required fields. Errors are worded for the
// thread (the owner reads them), not for a stack trace.
func (p ProposalSpec) validate() error {
	switch p.Type {
	case "create-vault-note":
		if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Body) == "" {
			return fmt.Errorf("create-vault-note needs title and body")
		}
		if strings.ContainsAny(p.Title, `/\`) || strings.Contains(p.Title, "..") {
			return fmt.Errorf("create-vault-note title %q may not contain path separators", p.Title)
		}
	case "run-errand":
		if strings.TrimSpace(p.Errand) == "" {
			return fmt.Errorf("run-errand needs errand text")
		}
	case "aion-backlog", "re-backlog":
		if strings.TrimSpace(p.Title) == "" {
			return fmt.Errorf("%s needs a title", p.Type)
		}
		if p.Kind != "task" && p.Kind != "decision" {
			return fmt.Errorf("%s kind must be task or decision (got %q)", p.Type, p.Kind)
		}
	case "goals-item":
		switch p.Mode {
		case "add", "edit", "move":
		default:
			return fmt.Errorf("goals-item mode must be add, edit or move (got %q)", p.Mode)
		}
		if p.Level != "rock" && p.Level != "milestone" {
			return fmt.Errorf("goals-item level must be rock or milestone (got %q)", p.Level)
		}
		if strings.TrimSpace(p.Area) == "" {
			return fmt.Errorf("goals-item needs the area (the ## heading in goals.md)")
		}
		if p.Mode == "add" && strings.TrimSpace(p.Title) == "" {
			return fmt.Errorf("goals-item add needs a title")
		}
		if p.Mode != "add" && (strings.TrimSpace(p.TargetID) == "" || strings.TrimSpace(p.AnchorText) == "") {
			return fmt.Errorf("goals-item %s needs targetId AND anchorText (the goal's current text)", p.Mode)
		}
	default:
		return fmt.Errorf("unknown proposal type %q (allowed: create-vault-note, run-errand, aion-backlog, re-backlog, goals-item)", p.Type)
	}
	return nil
}

// ParseProposals extracts every `manifest-proposal` fenced block from a reply.
// It returns the reply with the blocks REPLACED by one-line placeholders (the
// thread shows "→ proposed: …", never raw JSON), the validated specs, and
// human-readable warnings for blocks that were dropped (bad JSON, unknown type,
// missing fields). Fence matching mirrors mdfm.ExtractFencedBlock: 3+ backticks
// followed by exactly the language tag, closed by a bare fence of >= as many
// backticks; an unclosed opener swallows the rest of the reply (tolerated).
func ParseProposals(reply string) (clean string, specs []ProposalSpec, warnings []string) {
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
		var spec ProposalSpec
		if err := json.Unmarshal([]byte(raw), &spec); err != nil {
			warnings = append(warnings, "a proposal block was dropped — not valid JSON: "+err.Error())
		} else if err := spec.validate(); err != nil {
			warnings = append(warnings, "a proposal block was dropped — "+err.Error())
		} else {
			specs = append(specs, spec)
			out = append(out, "→ proposed: "+specLabel(spec))
		}
		if !closed {
			i = len(lines) // unclosed fence ate the tail
		} else {
			i = j
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n")), specs, warnings
}

// specLabel is the one-line human name a filed proposal shows in the thread.
func specLabel(p ProposalSpec) string {
	switch p.Type {
	case "run-errand":
		return "errand — " + snip(p.Errand, 80)
	case "aion-backlog":
		return "AION " + p.Kind + " — " + snip(p.Title, 80)
	case "re-backlog":
		return "RE " + p.Kind + " — " + snip(p.Title, 80)
	case "goals-item":
		what := p.Title
		if what == "" {
			what = p.TargetID
		}
		return "goals " + p.Mode + " " + p.Level + " — " + snip(what, 80)
	default:
		return "vault note — " + snip(p.Title, 80)
	}
}

func snip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func countBackticks(s string) int {
	i := 0
	for i < len(s) && s[i] == '`' {
		i++
	}
	return i
}
