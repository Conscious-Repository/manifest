package construction

// Profiled flashing parts (2026-10-09): a bent-metal piece whose cross-section
// the steward (or the owner) draws as a short polyline — a receiver hooked
// into a cut joint, a hemmed drip, a counterflashing with a kick-out — for
// details the template's fixed parts can't show.
//
// It follows how CAD generation is made reliable: the model writes a small
// typed program (a sketch extruded along the wall), never a mesh; the
// sketch is anchored to a named datum instead of free coordinates (models
// are poor at exact alignment); a deterministic kernel refuses what can't be
// built with an exact reason, and measures what it builds (laps, cuts into
// the brick) into ordinary validation issues the model can read and fix.
//
// The profile is the only vertex list a component may carry: at most
// cxProfileMax points of two bounded numbers, no expressions.
//
// Datum: u is millimetres out from the wall face (negative = into the
// wall), v is millimetres up from the top of the roof where it meets the
// wall (where the apron's corner sits). The profile is the centreline of the
// metal; thickness is added half each side, and the section is extruded
// along the whole junction.

import (
	"fmt"
	"math"
	"strings"
)

const (
	TypeProfiledFlashing = "profiled-flashing"
	cxProfileMax         = 32
)

func init() {
	registerOp("AddProfiledPart", TargetAssembly, false, func() Operation { return &AddProfiledPart{} })
	registerOp("SetProfile", TargetAssembly, false, func() Operation { return &SetProfile{} })
}

// checkProfile refuses a section that can't be built, naming where.
func checkProfile(f string, pts [][2]float64) []string {
	var out []string
	if len(pts) < 2 || len(pts) > cxProfileMax {
		return []string{fmt.Sprintf("%s: a profile has 2 to %d points (got %d)", f, cxProfileMax, len(pts))}
	}
	total := 0.0
	for i, p := range pts {
		if math.IsNaN(p[0]) || math.IsNaN(p[1]) || math.IsInf(p[0], 0) || math.IsInf(p[1], 0) {
			out = append(out, fmt.Sprintf("%s: point %d is not a number", f, i+1))
			continue
		}
		if p[0] < -600 || p[0] > 1000 || p[1] < -300 || p[1] > 1500 {
			out = append(out, fmt.Sprintf("%s: point %d (%.1f, %.1f) is outside the junction (u −600…1000 mm out from the wall face, v −300…1500 mm above the roof at the wall)", f, i+1, p[0], p[1]))
		}
		if i > 0 {
			d := math.Hypot(p[0]-pts[i-1][0], p[1]-pts[i-1][1])
			if d < 1 {
				out = append(out, fmt.Sprintf("%s: points %d and %d are %.2f mm apart; leave at least 1 mm between points", f, i, i+1, d))
			}
			total += d
		}
		if i > 0 && i < len(pts)-1 {
			a, b, c := pts[i-1], p, pts[i+1]
			d1 := [2]float64{b[0] - a[0], b[1] - a[1]}
			d2 := [2]float64{c[0] - b[0], c[1] - b[1]}
			l1, l2 := math.Hypot(d1[0], d1[1]), math.Hypot(d2[0], d2[1])
			if l1 > 0 && l2 > 0 {
				turn := math.Acos(math.Max(-1, math.Min(1, (d1[0]*d2[0]+d1[1]*d2[1])/(l1*l2)))) * 180 / math.Pi
				if turn > 165 {
					out = append(out, fmt.Sprintf("%s: the fold at point %d turns %.0f°; a closed hem can't be drawn — open it to 165° or less (a hem with a few mm gap)", f, i+1, turn))
				}
			}
		}
	}
	if total > 3000 {
		out = append(out, fmt.Sprintf("%s: the profile is %.0f mm of metal; at most 3000 mm", f, total))
	}
	// the centreline may not cross itself
	for i := 0; i+1 < len(pts); i++ {
		for j := i + 2; j+1 < len(pts); j++ {
			if x, ok := segCross(pts[i], pts[i+1], pts[j], pts[j+1]); ok {
				out = append(out, fmt.Sprintf("%s: the segment from point %d to %d crosses the segment from point %d to %d at (%.1f, %.1f)", f, i+1, i+2, j+1, j+2, x[0], x[1]))
			}
		}
	}
	return out
}

// segCross reports a proper crossing of segments ab and cd.
func segCross(a, b, c, d [2]float64) ([2]float64, bool) {
	r := [2]float64{b[0] - a[0], b[1] - a[1]}
	s := [2]float64{d[0] - c[0], d[1] - c[1]}
	den := r[0]*s[1] - r[1]*s[0]
	if math.Abs(den) < 1e-9 {
		return [2]float64{}, false
	}
	qp := [2]float64{c[0] - a[0], c[1] - a[1]}
	t := (qp[0]*s[1] - qp[1]*s[0]) / den
	u := (qp[0]*r[1] - qp[1]*r[0]) / den
	if t > 1e-6 && t < 1-1e-6 && u > 1e-6 && u < 1-1e-6 {
		return [2]float64{a[0] + t*r[0], a[1] + t*r[1]}, true
	}
	return [2]float64{}, false
}

// AddProfiledPart adds a bent-metal part drawn by its section.
type AddProfiledPart struct {
	Op             string       `json:"op"`
	NewComponentID string       `json:"newComponentId"`
	PartName       string       `json:"name"`
	Thickness      float64      `json:"thickness,omitempty"` // mm, default 0.6
	Points         [][2]float64 `json:"points"`              // [u, v] mm from the datum
	Replaces       string       `json:"replaces,omitempty"`  // a template part this one stands in for (set inapplicable)
	Notes          string       `json:"notes,omitempty"`
}

func (o *AddProfiledPart) Name() string { return "AddProfiledPart" }
func (o *AddProfiledPart) Check() []string {
	out := checkText("name", o.PartName, MaxTitle, true)
	out = append(out, checkText("notes", o.Notes, 2000, false)...)
	if !ValidID(KindComponent, o.NewComponentID) {
		out = append(out, "newComponentId must be a new cmp- id")
	}
	if o.Thickness != 0 && (o.Thickness < 0.3 || o.Thickness > 3) {
		out = append(out, "thickness is 0.3 to 3 mm")
	}
	if o.Replaces != "" && !ValidID(KindComponent, o.Replaces) {
		out = append(out, "replaces must be a cmp- id")
	}
	return append(out, checkProfile("points", o.Points)...)
}
func (o *AddProfiledPart) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	if err := newCompID(a, o.NewComponentID); err != nil {
		return err
	}
	t := o.Thickness
	if t == 0 {
		t = 0.6
	}
	nc := Component{ID: o.NewComponentID, Type: TypeProfiledFlashing, Role: "flashing:custom", Name: strings.TrimSpace(o.PartName),
		Shape:     Shape{Kind: "bent-profile", Params: map[string]Quantity{"thickness": {Value: &t, Unit: "mm", State: StateAssumed, Provenance: ProvInference}}, Profile: o.Points},
		Transform: IdentityTransform(), Material: &PinRef{ID: genericMaterialID("metal-flashing"), Revision: 1}, Appearance: "metal-flashing",
		Attachments: []string{}, Applicability: "applicable", Notes: strings.TrimSpace(o.Notes)}
	if o.Replaces != "" {
		old, _ := a.component(o.Replaces)
		if old == nil {
			return NotFound("no component " + o.Replaces + " to replace")
		}
		tx.Record(o.Name(), old.ID+".applicability", old.Applicability, "inapplicable")
		old.Applicability = "inapplicable"
	}
	a.Components = append(a.Components, nc)
	refreshJunction(a)
	tx.Record(o.Name(), nc.ID, nil, nc)
	return nil
}

// SetProfile redraws a profiled part's section and/or thickness.
type SetProfile struct {
	Op          string       `json:"op"`
	ComponentID string       `json:"componentId"`
	Points      [][2]float64 `json:"points,omitempty"`
	Thickness   float64      `json:"thickness,omitempty"`
}

func (o *SetProfile) Name() string { return "SetProfile" }
func (o *SetProfile) Check() []string {
	var out []string
	if !ValidID(KindComponent, o.ComponentID) {
		out = append(out, "componentId must be a cmp- id")
	}
	if o.Points == nil && o.Thickness == 0 {
		out = append(out, "set points and/or thickness")
	}
	if o.Thickness != 0 && (o.Thickness < 0.3 || o.Thickness > 3) {
		out = append(out, "thickness is 0.3 to 3 mm")
	}
	if o.Points != nil {
		out = append(out, checkProfile("points", o.Points)...)
	}
	return out
}
func (o *SetProfile) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	cp, _ := a.component(o.ComponentID)
	if cp == nil || cp.Type != TypeProfiledFlashing {
		return NotFound("no profiled part " + o.ComponentID)
	}
	before := cp.Shape
	if o.Points != nil {
		cp.Shape.Profile = o.Points
	}
	if o.Thickness != 0 {
		t := o.Thickness
		cp.Shape.Params = map[string]Quantity{"thickness": {Value: &t, Unit: "mm", State: StateAssumed, Provenance: ProvInference}}
	}
	tx.Record(o.Name(), cp.ID+".shape", before, cp.Shape)
	return nil
}

// profiledParts builds every profiled part on the junction datum and
// measures it: a lap over the base flashing's upstand, and how far it goes
// into the wall (the outer wythe is then cut to receive it).
func (c *compiler) profiledParts(g sheetGeom) {
	f := c.f
	for i := range c.a.Components {
		comp := &c.a.Components[i]
		if comp.Type != TypeProfiledFlashing || !c.active(comp) {
			continue
		}
		t := comp.param("thickness")
		var pts []V2
		for _, p := range comp.Shape.Profile {
			pts = append(pts, V2{p[0], c.jg.datum + p[1]})
		}
		if len(pts) < 2 {
			continue
		}
		A, B := mitreRibbon(pts, t)
		if f.orient == "sidewall" && g.ok {
			fr := frame2{O: vmul(f.S, g.tTop), U: f.A, V: f.N}
			c.add(comp, ribbonPrism(comp.ID+"#0", A, B, fr, vmul(f.S, g.tBot-g.tTop)))
		} else {
			c.add(comp, ribbonPrism(comp.ID+"#0", A, B, frame2{O: V3{}, U: V3{0, 1, 0}, V: V3{0, 0, 1}}, vmul(f.A, f.Wa)))
		}
		// over the upstand: the lowest metal outside the upstand's face,
		// below its top, is how far this part laps it
		if c.jg.ok {
			lo, out, found := math.Inf(1), math.Inf(1), false
			for _, p := range pts {
				if p[0] > c.jg.flashT+0.01 && p[0] < c.jg.flashT+80 && p[1] < c.jg.upTop[1] {
					found = true
					lo = math.Min(lo, p[1])
					out = math.Min(out, p[0])
				}
			}
			if found {
				under := firstID(c.a.firstOf(TypeApronFlashing), c.a.firstOf(TypeSidewallFlash))
				c.facts.Laps = append(c.facts.Laps, LapFact{Kind: "custom-over-upstand", Over: comp.ID, Under: under,
					Lap: round4(c.jg.upTop[1] - lo), Clear: round4(out - t/2 - c.jg.flashT), OK: out-t/2 >= c.jg.flashT-0.01})
			}
		}
		// into the wall: cut the outer wythe to receive it
		if f.orient == "headwall" {
			zLo, zHi, deep := math.Inf(1), math.Inf(-1), 0.0
			for _, p := range pts {
				if p[0] < -0.5 {
					zLo, zHi, deep = math.Min(zLo, p[1]), math.Max(zHi, p[1]), math.Max(deep, -p[0])
				}
			}
			if deep > 0 {
				lo, hi := zLo-t, zHi+t
				if hi-lo < 10 { // a cut is at least a mortar joint's height
					mid := (lo + hi) / 2
					lo, hi = mid-5, mid+5
				}
				if c.jg.embed > 0 {
					lo, hi, deep = math.Min(lo, c.jg.grooveLo), math.Max(hi, c.jg.grooveHi), math.Max(deep, c.jg.embed)
				}
				c.jg.grooveLo, c.jg.grooveHi, c.jg.embed = lo, hi, deep+0.5
				c.facts.WallCuts = append(c.facts.WallCuts, WallCut{Component: comp.ID, Depth: round4(deep), Height: round4(hi - lo), Above: round4(lo - c.jg.datum)})
			}
		}
	}
}
