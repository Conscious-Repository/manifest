package realestate

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Underwriting measurables + the estimate-vintage lock (overhaul §3.1/§3.6,
// decisions 13/14). Measurables live in property FRONTMATTER, hand-editable
// in Obsidian, progressively refined from rough to exact:
//
//	units: ["A | 2bd/1ba | 850sqft | rent 1100", "B | 2bd/1ba | 850sqft | rent 1100"]
//	windows: 14
//	roof-squares: 22
//	locked: 2026-09-01
//
// The set is FREE: any numeric frontmatter key that isn't a reserved
// property field reads as a measurable (tolerant parse, all optional).
// Everything entered is an ESTIMATE until the explicit "lock underwriting"
// action snapshots measurables + rock ests + underwriting inputs +
// assumption values into a frozen `<slug>.underwrite.json` sidecar — a
// record of an owner decision at a moment, so storing it is doctrine-clean
// (§10). After lock, current values are canon and the UI shows
// initial-vs-real. One deliberate moment; no automatic flips.

// Unit is one line of the unit mix: "A | 2bd/1ba | 850sqft | rent 1100".
type Unit struct {
	Label string  `json:"label"`
	Beds  float64 `json:"beds,omitempty"`
	Baths float64 `json:"baths,omitempty"`
	Sqft  float64 `json:"sqft,omitempty"`
	Rent  float64 `json:"rent,omitempty"` // monthly rent est
}

var (
	unitBedBathRe = regexp.MustCompile(`(?i)^([\d.]+)\s*bd\s*/\s*([\d.]+)\s*ba$`)
	unitSqftRe    = regexp.MustCompile(`(?i)^([\d,.]+)\s*sq\s*ft$|^([\d,.]+)\s*sqft$`)
	unitRentRe    = regexp.MustCompile(`(?i)^rent\s+\$?([\d,.]+)$`)
)

// ParseUnits reads the frontmatter `units:` list (tolerant: a segment that
// matches no pattern rides into the label; missing segments stay zero).
func ParseUnits(v string) []Unit {
	var out []Unit
	for _, item := range quotedList(v) {
		u := Unit{}
		var labelParts []string
		for _, seg := range strings.Split(item, "|") {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			if m := unitBedBathRe.FindStringSubmatch(seg); m != nil {
				u.Beds, _ = strconv.ParseFloat(m[1], 64)
				u.Baths, _ = strconv.ParseFloat(m[2], 64)
				continue
			}
			if m := unitSqftRe.FindStringSubmatch(seg); m != nil {
				n := m[1]
				if n == "" {
					n = m[2]
				}
				u.Sqft, _ = parseMoney(n)
				continue
			}
			if m := unitRentRe.FindStringSubmatch(seg); m != nil {
				u.Rent, _ = parseMoney(m[1])
				continue
			}
			labelParts = append(labelParts, seg)
		}
		u.Label = strings.Join(labelParts, " · ")
		if u.Label == "" && u.Beds == 0 && u.Sqft == 0 && u.Rent == 0 {
			continue
		}
		out = append(out, u)
	}
	return out
}

// EmitUnit renders one unit line in the canonical segment order (the shape
// ParseUnits reads back — fixpoint for canonical lines).
func EmitUnit(u Unit) string {
	segs := []string{strings.TrimSpace(u.Label)}
	if segs[0] == "" {
		segs[0] = "unit"
	}
	if u.Beds > 0 || u.Baths > 0 {
		segs = append(segs, strconv.FormatFloat(u.Beds, 'f', -1, 64)+"bd/"+strconv.FormatFloat(u.Baths, 'f', -1, 64)+"ba")
	}
	if u.Sqft > 0 {
		segs = append(segs, strconv.FormatFloat(u.Sqft, 'f', -1, 64)+"sqft")
	}
	if u.Rent > 0 {
		segs = append(segs, "rent "+strconv.FormatFloat(u.Rent, 'f', -1, 64))
	}
	return strings.Join(segs, " | ")
}

// EmitUnitsList renders the frontmatter inline flow list.
func EmitUnitsList(units []Unit) string {
	items := make([]string, 0, len(units))
	for _, u := range units {
		items = append(items, strconv.Quote(EmitUnit(u)))
	}
	return "[" + strings.Join(items, ", ") + "]"
}

// MeasurableKeyOK gates page-written measurable names: kebab-case, not a
// reserved property field.
var measurableKeyRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

func MeasurableKeyOK(key string) bool {
	return measurableKeyRe.MatchString(key) && !reservedPropertyKeys[key]
}

// RentMonthly is Σ unit rents (0 = not yet estimated).
func RentMonthly(units []Unit) float64 {
	var n float64
	for _, u := range units {
		n += u.Rent
	}
	return n
}

// reservedPropertyKeys are frontmatter fields that are NOT measurables.
var reservedPropertyKeys = map[string]bool{
	"categories": true, "address": true, "status": true, "kind": true,
	"control": true, "entity": true, "deal": true, "hidden": true,
	"lat": true, "lng": true, "work-start": true, "owner": true,
	"owner-addr": true, "owner-since": true, "from": true, "until": true,
	"drive": true, "agc": true, "name": true, "locked": true, "units": true,
	"tags": true, "aliases": true,
}

// ParseMeasurables collects the free numeric frontmatter keys (window count,
// roof squares, linear feet of fence — whatever the owner measures).
func ParseMeasurables(fm map[string]string) map[string]float64 {
	out := map[string]float64{}
	for k, v := range fm {
		key := strings.ToLower(strings.TrimSpace(k))
		if reservedPropertyKeys[key] {
			continue
		}
		n, err := parseMoney(unquote(v))
		if err != nil {
			continue
		}
		out[key] = n
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// UnderwriteRock is one rock's est picture at lock time.
type UnderwriteRock struct {
	ID       string  `json:"id"`
	Text     string  `json:"text"`
	EstTotal float64 `json:"estTotal"`
}

// UnderwriteLock is the frozen `<slug>.underwrite.json` sidecar. Written
// ONCE by the explicit lock action; never edited after (re-lock = the owner
// deliberately overwrites, behind a confirm).
type UnderwriteLock struct {
	LockedAt    string             `json:"locked_at"` // RFC3339
	Units       []Unit             `json:"units,omitempty"`
	Measurables map[string]float64 `json:"measurables,omitempty"`
	Rocks       []UnderwriteRock   `json:"rocks,omitempty"`
	NodeEsts    map[string]float64 `json:"node_ests,omitempty"`   // work id → [est::] (own, not rolled)
	HardTotal   float64            `json:"hard_total"`            // Σ rock est totals at lock
	Source      SourceMoney        `json:"source"`                // deal-slice inputs at lock
	Assumptions map[string]float64 `json:"assumptions,omitempty"` // values in effect (global + overrides)
}

// BuildUnderwriteSnapshot freezes the property's current estimate picture.
func BuildUnderwriteSnapshot(p Property, src SourceMoney, assumptions map[string]float64, now time.Time) UnderwriteLock {
	lock := UnderwriteLock{
		LockedAt:    now.Format(time.RFC3339),
		Units:       p.UnitMix,
		Measurables: p.Measurables,
		Source:      src,
		Assumptions: assumptions,
	}
	nodeEsts := map[string]float64{}
	for _, st := range p.Work {
		lock.Rocks = append(lock.Rocks, UnderwriteRock{ID: st.ID, Text: st.Text, EstTotal: st.EstTotal})
		lock.HardTotal += st.EstTotal
		if st.Est > 0 {
			nodeEsts[st.ID] = st.Est
		}
	}
	WalkNodes(p.Work, func(_ *WorkStage, n *WorkNode) {
		if n.Est > 0 {
			nodeEsts[n.ID] = n.Est
		}
	})
	if len(nodeEsts) > 0 {
		lock.NodeEsts = nodeEsts
	}
	return lock
}

// UnderwriteRel maps a vault-relative .md path to its lock-sidecar path.
func UnderwriteRel(mdRel string) string {
	return strings.TrimSuffix(mdRel, ".md") + ".underwrite.json"
}

// ParseSourceMoneyBytes exposes the source-sidecar money parse for callers
// holding the raw object (the lock snapshot).
func ParseSourceMoneyBytes(raw []byte) SourceMoney {
	type slice struct {
		PurchasePrice  float64 `json:"purchase_price"`
		ClosingCosts   float64 `json:"closing_costs"`
		HardCosts      float64 `json:"hard_costs"`
		CarryCost      float64 `json:"carry_cost"`
		ContingencyPct float64 `json:"contingency_pct"`
	}
	var v struct {
		slice
		Properties []slice `json:"properties"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return SourceMoney{}
	}
	pick := v.slice
	if pick.PurchasePrice == 0 && pick.HardCosts == 0 && len(v.Properties) > 0 {
		pick = v.Properties[0]
	}
	return SourceMoney{
		PurchasePrice: pick.PurchasePrice, ClosingCosts: pick.ClosingCosts,
		HardCosts: pick.HardCosts, CarryCost: pick.CarryCost, ContingencyPct: pick.ContingencyPct,
	}
}

// EffectiveAssumptions overlays a property's source-sidecar overrides (the
// existing per-record override pattern, assumptions.go) onto the globals.
func EffectiveAssumptions(global map[string]float64, sourceRaw []byte) map[string]float64 {
	out := map[string]float64{}
	for k, v := range global {
		out[k] = v
	}
	var v map[string]any
	if json.Unmarshal(sourceRaw, &v) == nil {
		for _, key := range AssumptionKeys {
			if n, ok := v[key].(float64); ok {
				out[key] = n
			}
		}
	}
	return out
}

// ReadUnderwriteLock reads the sidecar (nil when absent/garbled — tolerant).
func ReadUnderwriteLock(path string) *UnderwriteLock {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lock UnderwriteLock
	if json.Unmarshal(raw, &lock) != nil || lock.LockedAt == "" {
		return nil
	}
	return &lock
}
