package approvals

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"manifest/aion"
	"manifest/secrets"
)

// The AION extraction proposal types (aion-domain spec §3): candidates the
// extractor mined from `aion`-category vault notes. The proposal body holds
// a human summary + the justifying quote + a ````aion JSON fence (the
// structured payload); on Confirm the APP renders the one markdown line and
// writes it through the aion-approved vaultwriter capability — the §4
// approved-proposal lane. quote/confidence never leave the proposal file.
const (
	TypeAionBacklog   = "aion-backlog"   // append one task/decision to system/aion/backlog.md
	TypeAionHeuristic = "aion-heuristic" // new heuristic OR reinforce an existing one
)

// AionBacklogPath / AionHeuristicPath are the ONLY apply-paths the aion
// types may write. Fixed conventions (like XPostsFileName) so the
// byte-contract with the engine needs no shared config.
const (
	AionBacklogPath   = "system/aion/backlog.md"
	AionHeuristicPath = "system/aion/heuristics.md"
)

// AionBacklogPathAllowed is the aion-backlog apply-path allow-list.
// Byte-identical to engine audit.AionBacklogPathAllowed.
func AionBacklogPathAllowed(rel string) bool { return rel == AionBacklogPath }

// AionHeuristicPathAllowed is the aion-heuristic apply-path allow-list.
// Byte-identical to engine audit.AionHeuristicPathAllowed.
func AionHeuristicPathAllowed(rel string) bool { return rel == AionHeuristicPath }

// WithAionCapability names the vaultwriter capability the aion applies write
// under (main grants it with Actor approved-proposal — the first live wiring
// of that actor). Without it, aion applies refuse.
func (s *Store) WithAionCapability(name string) *Store {
	s.aionCap = name
	return s
}

// AionPayload parses the structured payload out of a proposal ("", false
// for non-aion or malformed bodies).
func AionPayload(p Proposal) (aion.ProposalPayload, bool) {
	switch p.Type {
	case TypeAionBacklog, TypeAionHeuristic:
		return aion.ParsePayload(p.Body)
	case TypeReBacklog:
		return aion.ParsePayloadFence(p.Body, aion.REPayloadFence)
	}
	return aion.ProposalPayload{}, false
}

// SetAionPayload rewrites a PENDING aion proposal's ````aion fence in place
// (the editable-proposal flow: kind/title/owner/rock/due/outcome, heuristic
// new⇄reinforce flip). The id is unchanged — a re-run of the extractor
// reproduces the original body and still dedupes; a rejected edit never
// resurfaces. The edit is validated and secret-scanned before it lands.
func (s *Store) SetAionPayload(id string, payload aion.ProposalPayload) error {
	src := filepath.Join(s.dir, "pending", id+".md")
	p, err := s.parse(src)
	if err != nil {
		return err
	}
	if p.Type != TypeAionBacklog && p.Type != TypeAionHeuristic && p.Type != TypeReBacklog {
		return fmt.Errorf("proposal %s is not an aion proposal", id)
	}
	if err := payload.Validate(nil); err != nil {
		return err
	}
	if fs := secrets.Scan(payload.Title + "\n" + payload.Outcome + "\n" + payload.Owner); len(fs) > 0 {
		return fmt.Errorf("edit refused: payload matches secret pattern(s) %s", strings.Join(secrets.Classes(fs), ", "))
	}
	if p.Type == TypeReBacklog {
		// real estate: type/apply-path are FIXED (no heuristics file to flip to)
		if payload.Kind == aion.KindHeuristic {
			return fmt.Errorf("edit refused: real estate has no heuristics file")
		}
		body, ok := aion.ReplacePayloadFenceIn(p.Body, aion.REPayloadFence, payload)
		if !ok {
			return fmt.Errorf("proposal %s carries no re payload fence", id)
		}
		p.Body = body
		return os.WriteFile(src, []byte(serialize(p)), 0o644)
	}
	// a kind flip may change which file the accept writes — keep them in sync
	if payload.Kind == aion.KindHeuristic {
		p.Type, p.ApplyPath = TypeAionHeuristic, AionHeuristicPath
	} else {
		p.Type, p.ApplyPath = TypeAionBacklog, AionBacklogPath
	}
	body, ok := aion.ReplacePayloadFence(p.Body, payload)
	if !ok {
		return fmt.Errorf("proposal %s carries no aion payload fence", id)
	}
	p.Body = body
	return os.WriteFile(src, []byte(serialize(p)), 0o644)
}

// ProposeBackfill files p unless an identical proposal already exists ANY-
// where — pending, approved, or rejected (the importer's idempotency: a
// re-run never resurrects a rejected record; Materialize's dedupe, exported
// for the backfill). Returns the proposal and its disposition.
func (s *Store) ProposeBackfill(p Proposal) (Proposal, string, error) {
	if p.ID == "" {
		p.ID = proposalID(p)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "pending", p.ID+".md")); err == nil {
		return p, "pending", nil
	}
	if s.decidedElsewhere(p.ID) {
		return p, "decided", nil
	}
	saved, err := s.Propose(p)
	if err != nil {
		return p, "", err
	}
	return saved, "filed", nil
}

// applyAionBacklog appends exactly one task/decision line to backlog.md via
// the approved-proposal capability. Any refusal leaves the proposal pending
// and writes nothing.
func (s *Store) applyAionBacklog(p Proposal) error {
	if !AionBacklogPathAllowed(p.ApplyPath) {
		return fmt.Errorf("apply refused: %q is not the aion backlog", p.ApplyPath)
	}
	payload, ok := aion.ParsePayload(p.Body)
	if !ok {
		return fmt.Errorf("apply refused: aion-backlog %s carries no payload fence", p.ID)
	}
	current, err := s.aionCurrent(p.ApplyPath)
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
	return s.vw.WriteCap(s.aionCap, p.ApplyPath, []byte(out))
}

// applyAionHeuristic lands a heuristic: mode new appends a statement; mode
// reinforce unions a source+date into the target entry (loud refusal when
// the target is gone — the owner flips to new or re-edits).
func (s *Store) applyAionHeuristic(p Proposal) error {
	if !AionHeuristicPathAllowed(p.ApplyPath) {
		return fmt.Errorf("apply refused: %q is not the aion heuristics file", p.ApplyPath)
	}
	payload, ok := aion.ParsePayload(p.Body)
	if !ok {
		return fmt.Errorf("apply refused: aion-heuristic %s carries no payload fence", p.ID)
	}
	current, err := s.aionCurrent(p.ApplyPath)
	if err != nil {
		return err
	}
	if err := s.aionScan(payload.Title + "\n" + payload.Heuristic.Target); err != nil {
		return err
	}
	out, err := aion.ApplyHeuristic(current, payload)
	if err != nil {
		return fmt.Errorf("apply refused: %w", err)
	}
	return s.vw.WriteCap(s.aionCap, p.ApplyPath, []byte(out))
}

// aionCurrent reads the target corpus file's current content ("" when the
// file does not exist yet — the transform appends into an empty doc).
func (s *Store) aionCurrent(rel string) (string, error) {
	return s.corpusCurrent(rel, s.aionCap, "aion")
}

// corpusCurrent reads the current bytes of a domain corpus ahead of an
// approved-proposal append — shared by the aion and real-estate applies.
// A missing file reads as "" (the append seeds the sections).
func (s *Store) corpusCurrent(rel, cap, domain string) (string, error) {
	if s.vaultRoot == "" {
		return "", errors.New("apply refused: no vault root configured for " + domain)
	}
	if s.vw == nil || !s.vw.Enabled() {
		return "", errors.New("apply refused: no vault writer configured for " + domain)
	}
	if cap == "" {
		return "", errors.New("apply refused: no " + domain + " capability granted")
	}
	b, err := os.ReadFile(filepath.Join(s.vaultRoot, filepath.FromSlash(rel)))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

// aionScan is the persistence-side secret gate (scan again before writing —
// the engine scanned at authoring, the edit path scanned at edit).
func (s *Store) aionScan(text string) error {
	if fs := secrets.Scan(text); len(fs) > 0 {
		return fmt.Errorf("apply refused: content matches secret pattern(s) %s — never persisted", strings.Join(secrets.Classes(fs), ", "))
	}
	return nil
}
