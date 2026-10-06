package server

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"manifest/aion"
	"manifest/domainextract"
	"manifest/jev"
)

// WHICH NOTES MAY LEAVE THE LAB (owner decision 2026-10-06: run extraction on
// Claude while the Sparks are down, "use jev to determine what to use
// where"). The extractor's whole design kept transcripts on local compute; a
// stand-in model is a third party, so each job is routed note by note:
//
//   - the transcript tier map is the authority: open and internal notes may
//     go (the owner's `tiers`), held notes never do;
//   - an UNMAPPED note (treated as held everywhere else) asks Jev for its
//     tier, under jev.AdviseTier's restrictive policy — unsure, a held-class
//     signal or P(held) ≥ 0.30 all come back held. Nothing is written to the
//     map; the advice only decides where this one job runs;
//   - no Jev (no key, an error, an oversize note) → the job waits;
//   - a note outside log/ has no tier at all → the job waits.
//
// A job that may not go simply waits, queued, for the lab (domainextract).

// ExtractionRoute returns the per-job routing decision.
func (s *Server) ExtractionRoute(rituals, tiers []string) func(domainextract.Input) (bool, string) {
	allowedRitual := map[string]bool{}
	for _, r := range rituals {
		allowedRitual[strings.TrimSpace(r)] = true
	}
	allowedTier := map[aion.Tier]bool{}
	for _, t := range tiers {
		if t := aion.Tier(strings.ToLower(strings.TrimSpace(t))); t != aion.TierHeld {
			allowedTier[t] = true
		}
	}
	return func(in domainextract.Input) (bool, string) {
		if !allowedRitual[in.Ritual] {
			return false, in.Ritual + " notes stay on the lab model"
		}
		if len(in.Documents) == 0 {
			return false, "nothing to route"
		}
		tm := s.aionTierMap()
		if tm == nil {
			var err error
			if tm, err = aion.LoadTierMap(); err != nil {
				return false, "the tier map is unreadable"
			}
		}
		for _, d := range in.Documents {
			name := path.Base(d.Name)
			if !strings.HasPrefix(d.Name, "log/") {
				return false, "“" + name + "” has no transcript tier"
			}
			tier, known := tm.Tier(name)
			basis := "tier map"
			if !known {
				var why string
				if tier, why = s.extractionJevTier(name, d.Text); tier == "" {
					return false, "“" + name + "” is unmapped and " + why
				}
				basis = "Jev"
			}
			if !allowedTier[tier] {
				return false, "“" + name + "” is " + string(tier) + " (" + basis + ")"
			}
		}
		return true, ""
	}
}

const jevKindExtractTier = "extract-tier"

// extractionJevTier is Jev's policy tier for an unmapped transcript, cached by
// content hash. "" with the reason when there is no usable answer.
func (s *Server) extractionJevTier(name, text string) (aion.Tier, string) {
	if !s.jevEnabled() {
		return "", "Jev is unavailable"
	}
	if strings.TrimSpace(text) == "" {
		return "", "has no text to judge"
	}
	if len(text) > jev.MaxTierText {
		return "", fmt.Sprintf("is over Jev's %d-byte limit (never truncated)", jev.MaxTierText)
	}
	hash := jevHash(text)
	e, hit := s.jevLookup(jevKindExtractTier, name, hash)
	if !hit || (e.State == jevStateError && time.Since(e.At) > jevErrorRetry) {
		e = jevCached{Kind: jevKindExtractTier, ID: name, Hash: hash, At: time.Now()}
		ctx, cancel := context.WithTimeout(context.Background(), jevAutoTimeout)
		adv, err := s.jevAdvisor().AdviseTier(ctx, jev.TierInput{Name: name, Text: text})
		cancel()
		if err != nil {
			e = jevErrState(jevKindExtractTier, name, hash, err)
		} else {
			e.State, e.Tier = jevStateAdvised, adv
		}
		s.jevStore(e)
	}
	if e.State != jevStateAdvised || e.Tier == nil {
		return "", "Jev did not answer"
	}
	switch e.Tier.Tier {
	case aion.TierOpen, aion.TierInternal, aion.TierHeld:
		return e.Tier.Tier, ""
	}
	return "", "Jev's answer was unrecognised"
}
