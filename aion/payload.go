package aion

import (
	"encoding/json"
	"fmt"
	"strings"

	"manifest/mdfm"
)

// HeuristicModeNew / Reinforce are the two dispositions of a heuristic
// proposal — the owner can flip between them in the editable card (spec §3).
const (
	HeuristicModeNew       = "new"
	HeuristicModeReinforce = "reinforce"
)

// HeuristicIntent carries the heuristic-matching outcome inside a payload.
type HeuristicIntent struct {
	Mode   string `json:"mode"`   // "" (non-heuristic) | new | reinforce
	Target string `json:"target"` // existing heuristic statement (reinforce)
}

// ProposalPayload is the structured content of an aion extraction proposal —
// the ````aion fenced JSON block inside the proposal body. The engine
// authors it; the app validates, edits (SetAionPayload), and renders the ONE
// markdown line that lands in the record at apply time. quote/confidence
// stay in the proposal for review and are NEVER emitted into the vault.
type ProposalPayload struct {
	Kind      string          `json:"kind"` // task | decision | heuristic
	Title     string          `json:"title"`
	Owner     string          `json:"owner,omitempty"`
	Rock      string          `json:"rock,omitempty"`
	Due       string          `json:"due,omitempty"`
	Status    string          `json:"status,omitempty"`
	DoneOn    string          `json:"done_on,omitempty"`
	NeededBy  string          `json:"needed_by,omitempty"`
	Decided   string          `json:"decided,omitempty"`
	Outcome   string          `json:"outcome,omitempty"`
	Sources   []string        `json:"sources,omitempty"` // note names, no brackets
	Captured  string          `json:"captured,omitempty"`
	Review    string          `json:"review,omitempty"` // e.g. "confirm-open" (backfill)
	Heuristic HeuristicIntent `json:"heuristic,omitempty"`

	Confidence float64 `json:"confidence,omitempty"`
	Quote      string  `json:"quote,omitempty"`
}

// KindHeuristic is the payload-only third kind (heuristics are deliberately
// NOT a backlog kind — they route to heuristics.md).
const KindHeuristic = "heuristic"

// Validate checks the payload's closed vocabularies and shape. people may be
// nil (owner check skipped).
func (p *ProposalPayload) Validate(people []*Person) error {
	switch p.Kind {
	case KindTask, KindDecision, KindHeuristic:
	default:
		return fmt.Errorf("kind must be task, decision or heuristic (got %q)", p.Kind)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("title is required")
	}
	for _, d := range []struct{ name, v string }{
		{"due", p.Due}, {"done_on", p.DoneOn}, {"captured", p.Captured}, {"decided", p.Decided},
	} {
		if d.v != "" && !isISODate(d.v) {
			return fmt.Errorf("%s must be YYYY-MM-DD (got %q)", d.name, d.v)
		}
	}
	if p.Kind == KindHeuristic {
		switch p.Heuristic.Mode {
		case HeuristicModeNew:
		case HeuristicModeReinforce:
			if strings.TrimSpace(p.Heuristic.Target) == "" {
				return fmt.Errorf("reinforce needs a target heuristic")
			}
		default:
			return fmt.Errorf("heuristic mode must be new or reinforce (got %q)", p.Heuristic.Mode)
		}
	}
	if people != nil && !ValidOwner(p.Owner, people) {
		return fmt.Errorf("owner %q has initials not in people.md", p.Owner)
	}
	return nil
}

func isISODate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// PayloadFence is the fenced-block language tag for aion proposals.
const PayloadFence = "aion"

// REPayloadFence is the fenced-block language tag for real-estate proposals —
// same payload shape, different domain (system/realestate/backlog.md).
const REPayloadFence = "re"

// ParsePayload extracts and decodes the ````aion fence from a proposal body.
func ParsePayload(body string) (ProposalPayload, bool) { return ParsePayloadFence(body, PayloadFence) }

// ParsePayloadFence is the fence-parameterized parse (aion | re).
func ParsePayloadFence(body, fence string) (ProposalPayload, bool) {
	block, ok := mdfm.ExtractFencedBlock(body, fence)
	if !ok {
		return ProposalPayload{}, false
	}
	var p ProposalPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(block)), &p); err != nil {
		return ProposalPayload{}, false
	}
	return p, true
}

// RenderPayloadFence renders the canonical fenced block for a payload.
func RenderPayloadFence(p ProposalPayload) string { return RenderPayloadFenceIn(PayloadFence, p) }

// RenderPayloadFenceIn is the fence-parameterized render.
func RenderPayloadFenceIn(fence string, p ProposalPayload) string {
	b, _ := json.Marshal(p)
	return "````" + fence + "\n" + string(b) + "\n````"
}

// ReplacePayloadFence swaps the ````aion fence inside a proposal body for
// the canonical rendering of p, leaving the rest of the body untouched.
// ok=false when the body carries no fence.
func ReplacePayloadFence(body string, p ProposalPayload) (string, bool) {
	return ReplacePayloadFenceIn(body, PayloadFence, p)
}

// ReplacePayloadFenceIn is the fence-parameterized replace.
func ReplacePayloadFenceIn(body, fence string, p ProposalPayload) (string, bool) {
	lines := strings.Split(body, "\n")
	start, end := -1, -1
	for i, ln := range lines {
		n := 0
		for n < len(ln) && ln[n] == '`' {
			n++
		}
		if start < 0 {
			if n >= 3 && strings.TrimSpace(ln[n:]) == fence {
				start = i
			}
			continue
		}
		if n >= 3 && strings.TrimSpace(ln) == strings.Repeat("`", n) {
			end = i
			break
		}
	}
	if start < 0 {
		return body, false
	}
	if end < 0 {
		end = len(lines) - 1
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, strings.Split(RenderPayloadFenceIn(fence, p), "\n")...)
	out = append(out, lines[end+1:]...)
	return strings.Join(out, "\n"), true
}

// BacklogItemFromPayload builds the record item a task/decision payload
// lands as. quote/confidence are deliberately dropped (audit stays in the
// proposal file).
func BacklogItemFromPayload(p ProposalPayload) *BacklogItem {
	it := &BacklogItem{
		Kind:     p.Kind,
		Text:     strings.TrimSpace(p.Title),
		Owner:    p.Owner,
		Sources:  p.Sources,
		Captured: p.Captured,
	}
	if p.Kind == KindDecision {
		it.Plain = true // new decisions land as plain bullets (canon)
		// A decision tethers to a rock exactly like a task: every rock-scoped
		// surface (portal cone, scoped work/archive, "decided here") reads
		// [rock::] for both kinds, and an untethered decision falls out of all
		// of them (owner call 2026-08-18).
		it.Rock = p.Rock
		it.Status = p.Status
		if it.Status == "" {
			it.Status = StatusOpen
		}
		it.NeededBy = p.NeededBy
		it.Decided = p.Decided
		it.Outcome = p.Outcome
		if it.Decided != "" {
			it.Status = StatusDecided
		}
	} else {
		it.Rock = p.Rock
		it.Due = p.Due
		it.Status = p.Status
		if it.Status == "" {
			it.Status = StatusOpen
		}
		it.DoneOn = p.DoneOn
		it.Checked = it.Status == StatusDone
	}
	it.ID = ItemID(it.Kind, it.Text)
	return it
}

// RenderItemLine renders the single markdown line a payload writes into
// backlog.md — THE one renderer both the accept path and its diff preview
// share.
func RenderItemLine(p ProposalPayload) string {
	return strings.Join(emitBacklogItem(BacklogItemFromPayload(p), 0), "\n")
}

// AppendBacklogItem is the pure accept transform for tasks/decisions: parse
// current backlog.md content, append exactly one item line under its kind's
// section, serialize. No I/O — the approvals store owns the write.
func AppendBacklogItem(current string, p ProposalPayload) (string, error) {
	if err := p.Validate(nil); err != nil {
		return "", err
	}
	if p.Kind == KindHeuristic {
		return "", fmt.Errorf("heuristic payloads apply to heuristics.md")
	}
	doc := ParseBacklog(current)
	it := BacklogItemFromPayload(p)
	// Dupe-check by kind+title, which is what the check MEANS. It used to
	// lean on Find(legacy-hash) colliding — which stopped holding the moment
	// AppendItem started minting persisted portal ids.
	for _, have := range doc.AllItems() {
		if have.Kind == it.Kind && NormalizeTitle(have.Text) == NormalizeTitle(it.Text) {
			return "", fmt.Errorf("item already in backlog: %q", it.Text)
		}
	}
	doc.AppendItem(it)
	return SerializeBacklog(doc), nil
}

// ApplyHeuristic is the pure accept transform for heuristics: mode new
// appends a fresh statement (with [first::] and one reinforcement per
// source); mode reinforce locates the target by normalized statement and
// unions in a source+date child — loudly failing when the target is gone.
func ApplyHeuristic(current string, p ProposalPayload) (string, error) {
	if err := p.Validate(nil); err != nil {
		return "", err
	}
	if p.Kind != KindHeuristic {
		return "", fmt.Errorf("not a heuristic payload")
	}
	doc := ParseHeuristics(current)
	switch p.Heuristic.Mode {
	case HeuristicModeNew:
		if doc.FindLive(HeuristicID(p.Title)) != nil {
			return "", fmt.Errorf("heuristic already present: %q — flip to reinforce", p.Title)
		}
		h := &Heuristic{Text: strings.TrimSpace(p.Title), First: p.Captured}
		for _, src := range p.Sources {
			h.Sources = append(h.Sources, Reinforcement{Note: src, Date: p.Captured})
		}
		doc.AppendLive(h)
	case HeuristicModeReinforce:
		target := doc.FindLive(HeuristicID(p.Heuristic.Target))
		if target == nil {
			return "", fmt.Errorf("reinforce target not found in heuristics.md: %q", p.Heuristic.Target)
		}
		for _, src := range p.Sources {
			if !target.HasReinforcement(src, p.Captured) {
				target.Sources = append(target.Sources, Reinforcement{Note: src, Date: p.Captured})
			}
		}
	default:
		return "", fmt.Errorf("heuristic mode must be new or reinforce")
	}
	return SerializeHeuristics(doc), nil
}
