package server

import (
	"fmt"
	"log"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"manifest/aion"
	"manifest/approvals"
	"manifest/mdfm"
	"manifest/record"
)

// Visibility suggestion on a transcript approval (owner ask 2026-09-21).
//
// A create-vault-note that arrived from Granola, HeyPocket or email is a
// transcript the tier map (aion/tiers.go) will govern the moment it carries
// the `aion` category: the corpus channel, the portal ARTIFACTS and — since
// the held-source gate — the backlog export all read that one data file. The
// card therefore proposes a tier the same way it proposes people names:
// pre-filled, accept-by-confirm, overridable, never auto-applied elsewhere.
//
// The proposal is the DATA FILE's answer when the note is already tiered
// (a re-proposed or re-synced transcript) and `held` otherwise: an unknown
// transcript must never default to more exposure. The owner's answer is
// written back into the data file in the coding checkout — the one tier
// store — where a hand edit would go; it ships with the next build.

// aionVisibilitySuggestion is the card's proposal: the tier, whether the
// data file already names the note (known) or this is the conservative
// default, the note basename the answer will be filed under, and which
// connector the transcript came from.
type aionVisibilitySuggestion struct {
	Suggested aion.Tier `json:"suggested"`
	Known     bool      `json:"known"`
	Note      string    `json:"note"`
	Source    string    `json:"source"` // granola | pocket | email
}

// transcriptSource names the connector a proposed note carries identity for,
// "" when the note is not a synced transcript (a hand-filed note, a
// spirit's own draft).
func transcriptSource(fm map[string]string) string {
	switch {
	case strings.TrimSpace(fm["granola-id"]) != "" || strings.TrimSpace(fm["granola_id"]) != "":
		return "granola"
	case strings.TrimSpace(fm["pocket-id"]) != "" || strings.TrimSpace(fm["pocket_id"]) != "":
		return "pocket"
	case strings.TrimSpace(fm["gmail-thread-id"]) != "" || strings.TrimSpace(fm["gmail_thread_id"]) != "":
		return "email"
	}
	return ""
}

// noteCategories reads a proposed note's frontmatter categories in both
// YAML shapes the converters and the card write — inline `categories: [a, b]`
// and block `categories:\n  - a` (mdfm.Split is flat and drops the block
// form; this mirrors the card's parseCategories exactly).
func noteCategories(proposed string) []string {
	block, _, ok := record.SplitFrontmatter(proposed)
	if !ok {
		return nil
	}
	for i, line := range block {
		k, v, found := strings.Cut(line, ":")
		if !found || !strings.EqualFold(strings.TrimSpace(k), "categories") {
			continue
		}
		if v = strings.TrimSpace(v); v != "" {
			return record.ParseList(v)
		}
		var out []string
		for _, ln := range block[i+1:] {
			item, ok := strings.CutPrefix(strings.TrimSpace(ln), "-")
			if !ok || record.IndentWidth(ln) == 0 {
				break
			}
			if item = record.Unquote(strings.TrimSpace(item)); item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	return nil
}

// noteHasAionCategory: the frontmatter categories name the aion pipeline.
func noteHasAionCategory(proposed string) bool {
	return slices.ContainsFunc(noteCategories(proposed), func(c string) bool {
		return strings.EqualFold(strings.TrimSpace(c), "aion")
	})
}

// tierMapNoteName is the basename the data file keys a written note by: the
// apply path (already a bare "YYYY-MM-DD <title>.md"), lowercased as the
// create-vault-note apply lowercases it.
func tierMapNoteName(applyPath string) string {
	return strings.ToLower(path.Base(strings.TrimSpace(applyPath)))
}

// aionVisibilitySuggestion returns the card's proposal for a pending
// create-vault-note, nil when the note is not a synced transcript. It does
// not gate on the aion category: the owner may add `aion` on the card, and
// the editor shows itself the moment the category is present.
func (s *Server) aionVisibilitySuggestion(p approvals.Proposal) *aionVisibilitySuggestion {
	if p.Type != approvals.TypeCreateVaultNote {
		return nil
	}
	fm, _ := mdfm.Split(p.Proposed)
	src := transcriptSource(fm)
	if src == "" {
		return nil
	}
	name := tierMapNoteName(p.ApplyPath)
	out := &aionVisibilitySuggestion{Suggested: aion.TierHeld, Note: name, Source: src}
	tm := s.aionTierMap()
	if tm == nil {
		var err error
		if tm, err = aion.LoadTierMap(); err != nil {
			return out // the conservative default stands
		}
	}
	if t, ok := tm.Tier(name); ok {
		out.Suggested, out.Known = t, true
	}
	return out
}

// aionTierMapPath is the data file in the coding checkout; "" when no
// checkout is configured (boardRepo).
func (s *Server) aionTierMapPath() string {
	if s.terminal == nil || s.terminal.codingRepo == "" {
		return ""
	}
	return filepath.Join(s.terminal.codingRepo, filepath.FromSlash(aion.TierMapPath))
}

// aionRecordVisibility files the owner's accepted tier for a just-confirmed
// transcript note. It refuses a tier outside the closed vocabulary and skips
// (with a log line) a note that no longer carries the aion category — the
// tier map governs aion transcripts only. The written note is unaffected.
func (s *Server) aionRecordVisibility(approved approvals.Proposal, tier string) error {
	t := aion.Tier(strings.ToLower(strings.TrimSpace(tier)))
	switch t {
	case aion.TierOpen, aion.TierInternal, aion.TierHeld:
	default:
		return fmt.Errorf("visibility %q is not open|internal|held", tier)
	}
	fm, _ := mdfm.Split(approved.Proposed)
	src := transcriptSource(fm)
	if src == "" || !noteHasAionCategory(approved.Proposed) {
		log.Printf("approval %s: visibility %s not recorded — note is not an aion transcript", approved.ID, t)
		return nil
	}
	p := s.aionTierMapPath()
	if p == "" {
		return fmt.Errorf("visibility %s not recorded: coding checkout is not configured (boardRepo)", t)
	}
	name := tierMapNoteName(approved.ApplyPath)
	return aion.WriteTierMapEntry(p, name, aion.TierEntry{
		Tier: t, Reason: "owner · approvals inbox (" + src + ")", Bytes: len(approved.Proposed),
	})
}
