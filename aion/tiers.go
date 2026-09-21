package aion

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Transcript tiers (owner taxonomy, 2026-09-21). The tiering of the
// aion-category transcript notes under the vault's log/ is DATA — the
// checked-in tier-map.json beside this file — never a runtime heuristic. A
// rule that decides disclosure must be auditable and diffable, and the owner
// moves one note between tiers with a one-line change to that file.
//
//	open      shareable with the team  → portal ARTIFACTS + kairos
//	internal  company-internal         → kairos only
//	held      private (salary, comp, personal, legal) → nowhere
//
// A note the map does not name is UNMAPPED: it is treated exactly like held
// (excluded everywhere) and surfaces only as a count, so a new transcript can
// never leak by default while it waits for the owner to tier it.
type Tier string

const (
	TierOpen     Tier = "open"
	TierInternal Tier = "internal"
	TierHeld     Tier = "held"
)

// TierEntry is one line of the tier map: the tier, the owner's reason, and the
// note's size at classification time (a drift hint, never a gate).
type TierEntry struct {
	Tier   Tier   `json:"tier"`
	Reason string `json:"reason"`
	Bytes  int    `json:"bytes"`
}

// TierMap is note filename (basename, with .md) → entry.
type TierMap map[string]TierEntry

//go:embed tier-map.json
var tierMapJSON []byte

// LoadTierMap parses the embedded map and validates it (every entry names
// exactly one known tier).
func LoadTierMap() (TierMap, error) { return ParseTierMap(tierMapJSON) }

// ParseTierMap parses a tier map from JSON bytes and validates it.
func ParseTierMap(b []byte) (TierMap, error) {
	var m TierMap
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("tier map: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// Validate refuses a map with an empty name, a non-.md name, or a tier outside
// the closed vocabulary. There is no "default" tier to fall back to on purpose.
func (m TierMap) Validate() error {
	if len(m) == 0 {
		return fmt.Errorf("tier map: empty")
	}
	for name, e := range m {
		if strings.TrimSpace(name) == "" || !strings.HasSuffix(name, ".md") || strings.ContainsAny(name, "/\\") {
			return fmt.Errorf("tier map: %q is not a note basename", name)
		}
		switch e.Tier {
		case TierOpen, TierInternal, TierHeld:
		default:
			return fmt.Errorf("tier map: %q has tier %q (want open|internal|held)", name, e.Tier)
		}
	}
	return nil
}

// Tier reports a note's tier and whether the map names it at all.
func (m TierMap) Tier(name string) (Tier, bool) {
	e, ok := m[name]
	return e.Tier, ok
}

// KairosEligible: open or internal — what the standing context pack may carry.
func (m TierMap) KairosEligible(name string) bool {
	t, ok := m.Tier(name)
	return ok && (t == TierOpen || t == TierInternal)
}

// PortalEligible: open only — what portal.aion.bio ARTIFACTS may publish.
func (m TierMap) PortalEligible(name string) bool {
	t, ok := m.Tier(name)
	return ok && t == TierOpen
}

// TierCounts is the map's census, for the pack's coverage lines.
type TierCounts struct{ Open, Internal, Held int }

func (m TierMap) Counts() TierCounts {
	var c TierCounts
	for _, e := range m {
		switch e.Tier {
		case TierOpen:
			c.Open++
		case TierInternal:
			c.Internal++
		case TierHeld:
			c.Held++
		}
	}
	return c
}

// Names returns every mapped filename, sorted — the deterministic iteration
// order every renderer over the map uses.
func (m TierMap) Names() []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
