package aion

import (
	"fmt"
	"strings"
	"time"
)

// ResolveBacklogItem is the pure closed-loop transform (email-sync): the
// extractor found evidence — usually the owner's own outbound reply — that an
// OPEN backlog item is finished or decided, and the confirmed proposal flips
// that one line. Matching is by ItemID(kind, title), so the payload's title
// must equal the backlog line verbatim. Mirrors the Store invariants:
// UpdateItem for tasks (done stamps done_on + checkbox), Decide for decisions
// (append-only, outcome required, re-decide refused). No I/O — the approvals
// store owns the write.
func ResolveBacklogItem(current string, p ProposalPayload, now time.Time) (string, error) {
	if err := p.Validate(nil); err != nil {
		return "", err
	}
	if p.Kind == KindHeuristic {
		return "", fmt.Errorf("heuristics are reinforced, not resolved")
	}
	doc := ParseBacklog(current)
	it := doc.Find(ItemID(p.Kind, strings.TrimSpace(p.Title)))
	if it == nil {
		return "", fmt.Errorf("item not found in backlog: %q — edit the title to match the backlog line", p.Title)
	}
	switch p.Kind {
	case KindTask:
		if p.Status != StatusDone {
			return "", fmt.Errorf("a task resolves to status done (got %q)", p.Status)
		}
		if it.Status == StatusDone {
			return "", fmt.Errorf("already done %s", it.DoneOn)
		}
		it.Status = StatusDone
		it.Checked = true
		it.DoneOn = p.DoneOn
		if it.DoneOn == "" {
			it.DoneOn = now.Format("2006-01-02")
		}
	case KindDecision:
		if p.Status != StatusDecided {
			return "", fmt.Errorf("a decision resolves to status decided (got %q)", p.Status)
		}
		if strings.TrimSpace(p.Outcome) == "" {
			return "", fmt.Errorf("an outcome is required to decide")
		}
		if it.Status == StatusDecided {
			return "", fmt.Errorf("already decided %s", it.Decided)
		}
		it.Status = StatusDecided
		it.Decided = p.Decided
		if it.Decided == "" {
			it.Decided = now.Format("2006-01-02")
		}
		it.Outcome = strings.TrimSpace(p.Outcome)
	}
	// union the evidence note into the item's sources (dedupe, order kept)
	for _, src := range p.Sources {
		src = strings.TrimSpace(src)
		if src == "" {
			continue
		}
		dup := false
		for _, have := range it.Sources {
			if have == src {
				dup = true
				break
			}
		}
		if !dup {
			it.Sources = append(it.Sources, src)
		}
	}
	return SerializeBacklog(doc), nil
}
