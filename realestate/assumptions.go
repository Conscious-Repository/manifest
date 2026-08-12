// The single global assumption set (RE spec §3 Settings / §4): the 15 values
// every pro forma inherits, stored as one vault record —
// system/realestate/assumptions.md — and PUBLISHED to the ooda portal's
// src/engine/defaults.js. Manifest owns the values; the portal runs the calc.
// Per-property/per-deal overrides live on their source.json sidecars; the
// Settings "who overrides it" index derives from those, never from here.
//
// Fixpoint contract: Parse→Emit is byte-identical for a well-formed file
// (unknown keys and prose survive verbatim in order).
package realestate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// AssumptionKeys is the canonical order — mirrors the portal engine's
// defaults.js so the published module diffs cleanly.
var AssumptionKeys = []string{
	"vacancy_rate", "opex_rate", "rent_growth", "opex_growth",
	"capex_per_unit_year", "construction_period_months", "hold_years",
	"exit_cap_rate", "perm_interest_rate", "perm_amort_years", "perm_ltv",
	"construction_interest_rate", "construction_loan_ltc",
	"selling_cost_pct", "contingency_pct",
}

// DefaultAssumptions seeds assumptions.md from the portal's live defaults
// (re-portal src/engine/defaults.js, verified 2026-08-12).
var DefaultAssumptions = map[string]float64{
	"vacancy_rate":               0.08,
	"opex_rate":                  0.35,
	"rent_growth":                0.04,
	"opex_growth":                0.02,
	"capex_per_unit_year":        350,
	"construction_period_months": 10,
	"hold_years":                 5,
	"exit_cap_rate":              0.0725,
	"perm_interest_rate":         0.0625,
	"perm_amort_years":           25,
	"perm_ltv":                   0.75,
	"construction_interest_rate": 0.10,
	"construction_loan_ltc":      0.70,
	"selling_cost_pct":           0.015,
	"contingency_pct":            0.05,
}

// AssumptionLabels are the human names the Settings panel shows.
var AssumptionLabels = map[string]string{
	"vacancy_rate":               "Vacancy",
	"opex_rate":                  "Operating expenses (% EGI)",
	"rent_growth":                "Rent growth /yr",
	"opex_growth":                "OpEx growth /yr",
	"capex_per_unit_year":        "CapEx reserve $/unit/yr",
	"construction_period_months": "Construction period (months)",
	"hold_years":                 "Hold period (years)",
	"exit_cap_rate":              "Exit cap rate",
	"perm_interest_rate":         "Perm interest rate",
	"perm_amort_years":           "Perm amortization (years)",
	"perm_ltv":                   "Perm LTV",
	"construction_interest_rate": "Construction interest rate",
	"construction_loan_ltc":      "Construction LTC",
	"selling_cost_pct":           "Selling cost",
	"contingency_pct":            "Contingency",
}

// AssumptionsFile is the vault-relative record (under the realestate root).
const AssumptionsFile = "assumptions.md"

var assumptionLineRe = regexp.MustCompile(`^- \[([a-z_]+):: ([0-9.]+)\]\s*$`)

// Assumptions is the parsed record: the value map plus the verbatim lines so
// emission is a fixpoint (prose/unknown lines survive untouched, in place).
type Assumptions struct {
	Values map[string]float64 `json:"values"`
	lines  []string           // full file, verbatim
}

// ParseAssumptions reads the record. Missing file/keys fall back to defaults
// at READ time (the caller sees a complete value map) without inventing lines.
func ParseAssumptions(raw string) Assumptions {
	a := Assumptions{Values: map[string]float64{}}
	if raw != "" {
		a.lines = strings.Split(strings.TrimRight(raw, "\n"), "\n")
	}
	for _, ln := range a.lines {
		if m := assumptionLineRe.FindStringSubmatch(ln); m != nil {
			if v, err := strconv.ParseFloat(m[2], 64); err == nil {
				a.Values[m[1]] = v
			}
		}
	}
	for _, k := range AssumptionKeys {
		if _, ok := a.Values[k]; !ok {
			a.Values[k] = DefaultAssumptions[k]
		}
	}
	return a
}

// EmitAssumptions re-serializes: known-key lines are rebuilt from Values (so
// an edit lands), everything else passes through verbatim. Parse→Emit with no
// edits is byte-identical.
func EmitAssumptions(a Assumptions) string {
	var out []string
	for _, ln := range a.lines {
		if m := assumptionLineRe.FindStringSubmatch(ln); m != nil {
			if v, ok := a.Values[m[1]]; ok {
				out = append(out, "- ["+m[1]+":: "+trimFloat(v)+"]")
				continue
			}
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n") + "\n"
}

// SetAssumption updates one value in place (adding a line only if the key has
// none yet — appended in canonical position at the end of the list block).
func (a *Assumptions) SetAssumption(key string, v float64) error {
	known := false
	for _, k := range AssumptionKeys {
		if k == key {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("unknown assumption %q", key)
	}
	a.Values[key] = v
	for _, ln := range a.lines {
		if m := assumptionLineRe.FindStringSubmatch(ln); m != nil && m[1] == key {
			return nil // line exists; Emit rebuilds it
		}
	}
	a.lines = append(a.lines, "- ["+key+":: "+trimFloat(v)+"]")
	return nil
}

// SeedAssumptions is the freshly-created record: a titled list of the 15
// defaults in canonical order, naming its own publish path.
func SeedAssumptions() string {
	var b strings.Builder
	b.WriteString("---\ncategories: [assumptions]\n---\n\n# underwriting assumptions\n\n")
	b.WriteString("The single global set every pro forma inherits. Per-property and per-deal\noverrides live on their source.json sidecars. Published to\noodagroup/src/engine/defaults.js via PUBLISH.\n\n")
	for _, k := range AssumptionKeys {
		b.WriteString("- [" + k + ":: " + trimFloat(DefaultAssumptions[k]) + "]\n")
	}
	return b.String()
}

// trimFloat renders a float the way defaults.js writes them (no trailing
// zeros, no scientific notation) so round-trips stay stable.
func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	return s
}

// AssumptionOverride is one derived override-index row: which record overrides
// which global key with what value.
type AssumptionOverride struct {
	Key   string  `json:"key"`
	Kind  string  `json:"kind"` // property | deal
	Slug  string  `json:"slug"`
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// AssumptionOverrides derives the Settings "who overrides it" index by
// scanning every property and deal source sidecar for assumption keys — the
// index is backed by records, never hand-written (RE spec §2).
func (s *Service) AssumptionOverrides() []AssumptionOverride {
	keyset := map[string]bool{}
	for _, k := range AssumptionKeys {
		keyset[k] = true
	}
	var out []AssumptionOverride
	scan := func(mdRel, kind, slug, name string) {
		raw, ok := s.Source(mdRel)
		if !ok {
			return
		}
		var obj map[string]any
		if json.Unmarshal(raw, &obj) != nil {
			return
		}
		for k, v := range obj {
			if !keyset[k] {
				continue
			}
			if f, ok := v.(float64); ok {
				out = append(out, AssumptionOverride{Key: k, Kind: kind, Slug: slug, Name: name, Value: f})
			}
		}
	}
	if props, err := s.Properties(); err == nil {
		for _, p := range props {
			if !p.Hidden {
				scan(p.Path, "property", p.Slug, p.Short)
			}
		}
	}
	if deals, err := s.Deals(); err == nil {
		for _, d := range deals {
			scan(d.Path, "deal", d.Slug, d.Name)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// ---- the published module (oodagroup src/engine/defaults.js) ----

// portalDefaultLineRe matches one value line inside the `export const
// defaults = {` block: indent, key, numeric literal, trailing comma.
var portalDefaultLineRe = regexp.MustCompile(`^(\s*)([a-z_]+):\s*(-?[0-9.]+),\s*$`)

// RenderPortalDefaults rewrites ONLY the `export const defaults = {…}` block
// of the portal's defaults.js from the assumption values, leaving every other
// byte (opex breakdowns, capex schedules, comments) verbatim. Byte-stability
// contract: a line whose literal already equals the value is kept EXACTLY as
// written (0.10 stays 0.10) — publish with no edits diffs empty; assumption
// keys missing from the block are appended before its closing brace.
func RenderPortalDefaults(current []byte, values map[string]float64) ([]byte, error) {
	lines := strings.Split(strings.TrimRight(string(current), "\n"), "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "export const defaults = {") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("defaults.js has no `export const defaults = {` block")
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "}") {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("defaults.js block never closes")
	}
	seen := map[string]bool{}
	var out []string
	out = append(out, lines[:start+1]...)
	for _, ln := range lines[start+1 : end] {
		m := portalDefaultLineRe.FindStringSubmatch(ln)
		if m == nil {
			out = append(out, ln) // comments/unknown shapes ride verbatim
			continue
		}
		key := m[2]
		v, ok := values[key]
		if !ok {
			out = append(out, ln) // a key manifest doesn't own stays theirs
			continue
		}
		seen[key] = true
		if cur, err := strconv.ParseFloat(m[3], 64); err == nil && cur == v {
			out = append(out, ln) // numerically equal — keep the exact literal
			continue
		}
		out = append(out, m[1]+key+": "+trimFloat(v)+",")
	}
	for _, k := range AssumptionKeys {
		if _, ok := values[k]; ok && !seen[k] {
			out = append(out, "  "+k+": "+trimFloat(values[k])+",")
		}
	}
	out = append(out, lines[end:]...)
	return []byte(strings.Join(out, "\n") + "\n"), nil
}
