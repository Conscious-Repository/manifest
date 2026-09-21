package aion

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
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

// SourceTier resolves a backlog item's `source:` reference to a transcript
// tier. A source is a wikilink target (brackets already stripped) such as
// "log/2026-06-01 heye immigration sync"; only a bare "log/<name>" names a
// transcript this map governs. The rule, exactly:
//
//   - trim; drop a "|alias" or "#heading" wikilink suffix
//   - require the "log/" prefix — "intrinsic/…", "updates for justin" and
//     any other path are not transcripts (no tier → the caller keeps them)
//   - the remainder must be a bare basename (no further "/")
//   - append ".md" when absent, then look the basename up verbatim
//
// A log/ note the map does not name resolves to no tier (ok=false): the
// export filter suppresses only a KNOWN held source, never a guess.
func (m TierMap) SourceTier(source string) (Tier, bool) {
	s := strings.TrimSpace(source)
	if i := strings.IndexAny(s, "|#"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	name, ok := strings.CutPrefix(s, "log/")
	if !ok || name == "" || strings.ContainsAny(name, "/\\") {
		return "", false
	}
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	return m.Tier(name)
}

// HeldSource: the source resolves to a transcript the map holds. This is the
// portal export's suppression predicate (a held transcript's backlog items
// disclose the subject in their titles).
func (m TierMap) HeldSource(source string) bool {
	t, ok := m.SourceTier(source)
	return ok && t == TierHeld
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

// TierMapPath is the data file's checkout-relative path — the ONE tier store.
// An owner decision taken in the approvals inbox is recorded here (in the
// coding checkout), exactly where a hand edit would go, and ships with the
// next build like any other tier change.
const TierMapPath = "aion/tier-map.json"

// tierEntryJSON is the on-disk field order (alphabetical, as the file is
// kept) so a rewrite is byte-stable against the checked-in data.
type tierEntryJSON struct {
	Bytes  int    `json:"bytes"`
	Reason string `json:"reason"`
	Tier   Tier   `json:"tier"`
}

// MarshalTierMap renders the map in the data file's own format: keys sorted,
// one-space indent, HTML left unescaped ("&" stays "&"), no trailing newline.
// Round-trips the checked-in file byte-for-byte.
func MarshalTierMap(m TierMap) ([]byte, error) {
	out := make(map[string]tierEntryJSON, len(m))
	for name, e := range m {
		out[name] = tierEntryJSON{Bytes: e.Bytes, Reason: e.Reason, Tier: e.Tier}
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(b.String(), "\n")), nil
}

// WriteTierMapEntry records one owner decision in the data file at path:
// read, validate, set the note's entry, validate again, write atomically.
// name is the note basename (with .md). The file must already exist and
// parse — a missing or corrupt map is an error, never a fresh one-line map.
func WriteTierMapEntry(path, name string, e TierEntry) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("tier map: %w", err)
	}
	m, err := ParseTierMap(raw)
	if err != nil {
		return err
	}
	m[name] = e
	if err := m.Validate(); err != nil {
		return err
	}
	b, err := MarshalTierMap(m)
	if err != nil {
		return fmt.Errorf("tier map: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("tier map: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("tier map: %w", err)
	}
	return nil
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
