package approvals

import (
	"fmt"
	"time"

	"manifest/aion"
)

// The closed-loop proposal types (email-sync): the extractor found evidence —
// usually the owner's own outbound reply in an email thread note — that an
// OPEN backlog item is finished or decided. On Confirm the app flips exactly
// that one line (task → done, decision → decided + outcome) via the same
// approved-proposal capability lane the append types use. The payload rides
// the same ````aion / ````re fence; status carries the mutation verb.
const (
	TypeAionResolve = "aion-resolve" // resolve one item in system/aion/backlog.md
	TypeReResolve   = "re-resolve"   // resolve one item in system/realestate/backlog.md
)

// applyAionResolve / applyReResolve flip the matched backlog line. Any
// refusal (item not found, already resolved, missing outcome) leaves the
// proposal pending and writes nothing — the card's editable title/outcome is
// the recovery path.
func (s *Store) applyAionResolve(p Proposal) error {
	return s.applyResolve(p, aion.PayloadFence, s.aionCap, AionBacklogPathAllowed, "aion backlog")
}

func (s *Store) applyReResolve(p Proposal) error {
	return s.applyResolve(p, aion.REPayloadFence, s.reCap, ReBacklogPathAllowed, "re backlog")
}

func (s *Store) applyResolve(p Proposal, fence, cap string, allowed func(string) bool, what string) error {
	if !allowed(p.ApplyPath) {
		return fmt.Errorf("apply refused: %q is not the %s", p.ApplyPath, what)
	}
	payload, ok := aion.ParsePayloadFence(p.Body, fence)
	if !ok {
		return fmt.Errorf("apply refused: %s %s carries no payload fence", p.Type, p.ID)
	}
	current, err := s.corpusCurrent(p.ApplyPath, cap, what)
	if err != nil {
		return err
	}
	if err := s.aionScan(payload.Title + "\n" + payload.Outcome); err != nil {
		return err
	}
	out, err := aion.ResolveBacklogItem(current, payload, time.Now())
	if err != nil {
		return fmt.Errorf("apply refused: %w", err)
	}
	return s.vw.WriteCap(cap, p.ApplyPath, []byte(out))
}
