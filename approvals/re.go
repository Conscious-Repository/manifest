package approvals

import (
	"fmt"

	"manifest/aion"
)

// The REAL-ESTATE extraction proposal type — the aion pipeline's second
// domain (RE spec / owner decision 2026-08-12: one domain, one decision log).
// Candidates mined from `real-estate`/`ooda`/`ooda-group`-category vault notes
// by the extractor spirit's real-estate ritual; the body carries a ````re JSON fence in the SAME
// payload shape as aion (the backlog grammar is domain-neutral — package aion
// is the record library here, not a domain leak). On Confirm the app renders
// the one markdown line and appends it to the RE decision log through the
// realestate-approved capability. Real estate has NO heuristics file — the
// heuristic kind is refused at edit and at apply.
const TypeReBacklog = "re-backlog" // append one task/decision to system/realestate/backlog.md

// ReBacklogPath is the ONLY apply-path the re-backlog type may write.
// Byte-identical to the engine's audit allow-list.
const ReBacklogPath = "system/realestate/backlog.md"

// ReBacklogPathAllowed is the re-backlog apply-path allow-list.
func ReBacklogPathAllowed(rel string) bool { return rel == ReBacklogPath }

// WithReCapability names the vaultwriter capability the RE applies write
// under (main grants it with Actor approved-proposal). Without it, RE
// applies refuse.
func (s *Store) WithReCapability(name string) *Store {
	s.reCap = name
	return s
}

// applyReBacklog appends exactly one rendered item line to the RE decision
// log — the same shape as applyAionBacklog against a different file, fence,
// and capability. AppendBacklogItem already rejects the heuristic kind
// (defense in depth under the no-heuristics rule).
func (s *Store) applyReBacklog(p Proposal) error {
	if !ReBacklogPathAllowed(p.ApplyPath) {
		return fmt.Errorf("apply refused: %q is not the real-estate backlog", p.ApplyPath)
	}
	payload, ok := aion.ParsePayloadFence(p.Body, aion.REPayloadFence)
	if !ok {
		return fmt.Errorf("apply refused: re-backlog %s carries no payload fence", p.ID)
	}
	if payload.Kind == aion.KindHeuristic {
		return fmt.Errorf("apply refused: real estate has no heuristics file")
	}
	current, err := s.corpusCurrent(p.ApplyPath, s.reCap, "realestate")
	if err != nil {
		return err
	}
	if err := s.aionScan(aion.RenderItemLine(payload)); err != nil {
		return err
	}
	out, err := aion.AppendBacklogItem(current, payload)
	if err != nil {
		return fmt.Errorf("apply refused: %w", err)
	}
	return s.vw.WriteCap(s.reCap, p.ApplyPath, []byte(out))
}
