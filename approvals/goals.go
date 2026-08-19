package approvals

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/goals"
	"manifest/secrets"
)

// The GOALS placement proposal type (§12 amendment 2026-08-19): a candidate
// rock/milestone placement into the owner's goals.md — the FIRST knowledge-
// zone target an approved proposal may write. The words are owner-sourced
// (spoken to his agent on Telegram or in a thread); the agent only places
// them, and nothing lands until the owner confirms the card in FEED. The body
// holds a human evidence line (the owner's words, quoted) + a ```goals JSON
// fence; on Confirm the app runs goals.ApplyPlacement — a whole-file fixpoint
// transform with a one-line structural budget — and writes through the
// goals-approved capability.
const TypeGoalsItem = "goals-item"

// GoalsPath is the ONLY apply-path a goals-item may write. The capability's
// pattern is a basename match anywhere in the knowledge zone
// (vaultwriter/capability.go), so this exact-match guard IS the vault-root
// pin — without it, any file named goals.md in any folder would qualify.
const GoalsPath = "goals.md"

// GoalsPathAllowed is the goals-item apply-path allow-list.
func GoalsPathAllowed(rel string) bool { return rel == GoalsPath }

// WithGoalsCapability names the vaultwriter capability goals applies write
// under. Without it, goals applies refuse (the lane is dark, never partial).
func (s *Store) WithGoalsCapability(name string) *Store {
	s.goalsCap = name
	return s
}

// GoalsPayload parses the structured payload out of a goals proposal.
func GoalsPayload(p Proposal) (goals.PlacementPayload, bool) {
	if p.Type != TypeGoalsItem {
		return goals.PlacementPayload{}, false
	}
	return goals.ParsePlacement(p.Body)
}

// SetGoalsPayload rewrites a PENDING goals proposal's fence in place (the
// pre-Confirm edit lane). The id never changes — a re-filed extraction still
// dedupes, and a rejected edit never resurfaces. No apply-path retarget is
// needed: every mode writes the one file.
func (s *Store) SetGoalsPayload(id string, payload goals.PlacementPayload) error {
	src := filepath.Join(s.dir, "pending", id+".md")
	p, err := s.parse(src)
	if err != nil {
		return err
	}
	if p.Type != TypeGoalsItem {
		return fmt.Errorf("proposal %s is not a goals proposal", id)
	}
	if err := payload.Validate(); err != nil {
		return err
	}
	if err := goalsScan(payload); err != nil {
		return fmt.Errorf("edit refused: %w", err)
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	body, ok := replaceFencedBlock(p.Body, goals.PayloadFence, string(out))
	if !ok {
		return fmt.Errorf("proposal %s carries no %s payload fence", id, goals.PayloadFence)
	}
	p.Body = body
	return os.WriteFile(src, []byte(serialize(p)), 0o644)
}

// applyGoalsItem lands exactly one confirmed placement in goals.md. Guards,
// in order: fixed path · payload present+valid · capability/root/writer ·
// file exists · secret scan · the fixpoint transform with its structural
// budget. Any refusal leaves the proposal pending and writes nothing.
func (s *Store) applyGoalsItem(p Proposal) error {
	if !GoalsPathAllowed(p.ApplyPath) {
		return fmt.Errorf("apply refused: %q is not the vault-root goals.md", p.ApplyPath)
	}
	payload, ok := GoalsPayload(p)
	if !ok {
		return fmt.Errorf("apply refused: goals-item %s carries no payload fence", p.ID)
	}
	if s.vaultRoot == "" {
		return fmt.Errorf("apply refused: no vault root configured for goals")
	}
	if s.vw == nil || !s.vw.Enabled() {
		return fmt.Errorf("apply refused: no vault writer configured for goals")
	}
	if s.goalsCap == "" {
		return fmt.Errorf("apply refused: no goals capability granted")
	}
	// Unlike the aion/re corpora (where a missing file seeds itself), a
	// missing goals.md refuses: there is nothing to place INTO — no areas, no
	// rocks — and an approved proposal must never be the thing that creates
	// the owner's goals file.
	b, err := os.ReadFile(filepath.Join(s.vaultRoot, filepath.FromSlash(p.ApplyPath)))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("apply refused: goals.md does not exist at the vault root")
		}
		return err
	}
	if err := goalsScan(payload); err != nil {
		return fmt.Errorf("apply refused: %w — never persisted", err)
	}
	next, err := goals.ApplyPlacement(string(b), payload, time.Now())
	if err != nil {
		return fmt.Errorf("apply refused: %w", err)
	}
	return s.vw.WriteCap(s.goalsCap, p.ApplyPath, []byte(next))
}

// goalsScan is the secret gate over every owner-visible payload string (the
// engine/agent scanned at authoring; the edit and apply paths scan again).
func goalsScan(p goals.PlacementPayload) error {
	text := strings.Join([]string{p.Title, p.Owner, p.Until, p.Verify, p.Kpi, p.AnchorText}, "\n")
	if fs := secrets.Scan(text); len(fs) > 0 {
		return fmt.Errorf("content matches secret pattern(s) %s", strings.Join(secrets.Classes(fs), ", "))
	}
	return nil
}
