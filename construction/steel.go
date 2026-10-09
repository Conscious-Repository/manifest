package construction

// Steel framing (2026-10-09). The 761 drawings frame the addition in steel:
// rafters of back-to-back channels (2C3×3.5 at 16 in) on W8×13 beams
// (A2.07, A3.14). The template's rafter array is timber by default; giving it
// a steel material draws it as two channels back to back, and a W-shape beam
// is its own part, running along the wall under the rafters.
//
// Sizes come from a small table of standard AISC shapes (nominal
// dimensions, rounded to 0.1 mm), so neither the owner nor the agent has to
// supply flange and web thicknesses; capacity, connections and protection
// remain a qualified engineer's (the validation says so).

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const TypeSteelBeam = "steel-beam"

// channel sections: depth, flange width, web, flange thickness (mm)
var channelSizes = map[string][4]float64{
	"C3x3.5":  {76.2, 34.8, 3.4, 6.9},
	"C3x4.1":  {76.2, 35.8, 4.3, 6.9},
	"C4x5.4":  {101.6, 40.1, 4.7, 7.5},
	"C5x6.7":  {127.0, 44.5, 4.8, 8.1},
	"C6x8.2":  {152.4, 48.8, 5.1, 8.7},
	"C8x11.5": {203.2, 57.4, 5.6, 9.9},
}

// wide-flange sections: depth, flange width, web, flange thickness (mm)
var beamSizes = map[string][4]float64{
	"W6x9":   {150.4, 100.1, 4.3, 5.5},
	"W8x10":  {200.4, 100.1, 4.3, 5.2},
	"W8x13":  {202.9, 101.6, 5.8, 6.5},
	"W8x18":  {206.5, 133.4, 5.8, 8.4},
	"W10x12": {250.7, 101.1, 4.8, 5.3},
	"W12x14": {302.5, 100.6, 5.1, 5.7},
}

func sizeNames(m map[string][4]float64) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ", ")
}

// channelFor finds the standard channel a steel rafter array's depth and pair
// width describe, else proportions it.
func channelFor(depth, pairWidth float64) [4]float64 {
	for _, c := range channelSizes {
		if math.Abs(c[0]-depth) < 1 && math.Abs(2*c[1]-pairWidth) < 2 {
			return c
		}
	}
	return [4]float64{depth, pairWidth / 2, math.Max(3, math.Min(12, depth*0.045)), math.Max(4, math.Min(20, depth*0.09))}
}

func isSteel(cat *Catalog, c *Component) bool {
	if c == nil || c.Material == nil {
		return false
	}
	m, ok := cat.material(c.Material.ID, c.Material.Revision)
	return ok && m.Family == "structural-steel"
}

func init() {
	registerOp("UseSteelRafters", TargetAssembly, false, func() Operation { return &UseSteelRafters{} })
	registerOp("AddSteelBeam", TargetAssembly, false, func() Operation { return &AddSteelBeam{} })
}

// UseSteelRafters makes the rafter array back-to-back steel channels.
type UseSteelRafters struct {
	Op      string  `json:"op"`
	Size    string  `json:"size"`              // a channel, e.g. C3x3.5 (drawn as a back-to-back pair)
	Spacing float64 `json:"spacing,omitempty"` // mm on centre (default: unchanged)
	Note    string  `json:"note,omitempty"`    // where the size came from
}

func (o *UseSteelRafters) Name() string { return "UseSteelRafters" }
func (o *UseSteelRafters) Check() []string {
	var out []string
	if _, ok := channelSizes[o.Size]; !ok {
		out = append(out, "size must be one of "+sizeNames(channelSizes))
	}
	if o.Spacing != 0 && (o.Spacing < 100 || o.Spacing > 1500) {
		out = append(out, "spacing is 100 to 1500 mm")
	}
	return append(out, checkText("note", o.Note, 500, false)...)
}
func (o *UseSteelRafters) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	r := a.firstOf(TypeRafterArray)
	if r == nil {
		return NotFound("this approach has no rafters")
	}
	before := *r
	sz := channelSizes[o.Size]
	note := "2 × " + o.Size + " back to back" + map[bool]string{true: "; " + strings.TrimSpace(o.Note), false: ""}[strings.TrimSpace(o.Note) != ""]
	set := func(k string, v float64) {
		q := r.Shape.Params[k]
		q.Value, q.Unit, q.State, q.Provenance, q.Illustrative, q.Placeholder, q.Note = &v, "mm", StateAssumed, ProvUserAssumption, false, nil, note
		r.Shape.Params[k] = q
	}
	set("depth", sz[0])
	set("width", round4(2*sz[1]))
	if o.Spacing != 0 {
		set("spacing", o.Spacing)
	}
	r.Material = &PinRef{ID: genericMaterialID("steel-channel"), Revision: 1}
	r.Appearance = "steel-channel"
	if strings.Contains(r.Name, "rafters") && !strings.Contains(r.Name, "steel") {
		r.Name = "Steel rafters (2 × " + o.Size + ")"
	}
	tx.Record(o.Name(), r.ID, before, *r)
	return nil
}

// AddSteelBeam adds a wide-flange beam along the wall under the rafters.
type AddSteelBeam struct {
	Op             string  `json:"op"`
	NewComponentID string  `json:"newComponentId"`
	Size           string  `json:"size"`     // e.g. W8x13
	Position       float64 `json:"position"` // mm, horizontally from the wall face to the beam's centreline
	PartName       string  `json:"name,omitempty"`
	Note           string  `json:"note,omitempty"`
}

func (o *AddSteelBeam) Name() string { return "AddSteelBeam" }
func (o *AddSteelBeam) Check() []string {
	var out []string
	if !ValidID(KindComponent, o.NewComponentID) {
		out = append(out, "newComponentId must be a new cmp- id")
	}
	if _, ok := beamSizes[o.Size]; !ok {
		out = append(out, "size must be one of "+sizeNames(beamSizes))
	}
	if o.Position < 50 || o.Position > 6000 {
		out = append(out, "position is 50 to 6000 mm from the wall face")
	}
	out = append(out, checkText("name", o.PartName, MaxTitle, false)...)
	return append(out, checkText("note", o.Note, 500, false)...)
}
func (o *AddSteelBeam) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	if err := newCompID(a, o.NewComponentID); err != nil {
		return err
	}
	sz := beamSizes[o.Size]
	note := o.Size + map[bool]string{true: "; " + strings.TrimSpace(o.Note), false: ""}[strings.TrimSpace(o.Note) != ""]
	q := func(v float64) Quantity {
		return Quantity{Value: &v, Unit: "mm", State: StateAssumed, Provenance: ProvUserAssumption, Note: note}
	}
	name := strings.TrimSpace(o.PartName)
	if name == "" {
		name = "Steel beam (" + o.Size + ")"
	}
	nc := Component{ID: o.NewComponentID, Type: TypeSteelBeam, Role: "structure:beam", Name: name,
		Shape: Shape{Kind: "wide-flange", Params: map[string]Quantity{"depth": q(sz[0]), "flangeWidth": q(sz[1]), "webThickness": q(sz[2]),
			"flangeThickness": q(sz[3]), "position": q(o.Position)}},
		Transform: IdentityTransform(), Material: &PinRef{ID: genericMaterialID("steel-beam"), Revision: 1}, Appearance: "steel-beam",
		Attachments: []string{}, Applicability: "applicable"}
	if r := a.firstOf(TypeRafterArray); r != nil {
		nc.Attachments = []string{r.ID}
	}
	a.Components = append(a.Components, nc)
	tx.Record(o.Name(), nc.ID, nil, nc)
	return nil
}

// steelRafters draws a steel rafter array as back-to-back channels: two
// flanges and a doubled web, each a strip down the slope.
func (c *compiler) steelRafters(comp *Component, top float64) {
	f := c.f
	ch := channelFor(comp.param("depth"), comp.param("width"))
	d, bf, tw, tf := ch[0], ch[1], ch[2], ch[3]
	strip := func(key string, w0, w1, a0, width float64, x float64) {
		A := []V2{{0, f.zAt(0, w0)}, {f.Lh, f.zAt(f.Lh, w0)}}
		B := []V2{{0, f.zAt(0, w1)}, {f.Lh, f.zAt(f.Lh, w1)}}
		s := ribbonPrism(key, A, B, f.profileFrame(), vmul(f.A, width))
		c.add(comp, translateSolid(s, vmul(f.A, x+a0)))
	}
	for k, x := range c.arrayPositions(comp.param("offset"), comp.param("spacing"), 2*bf, f.Wa) {
		strip(fmt.Sprintf("%s#%d.top", comp.ID, k), top-tf, top, -bf, 2*bf, x)
		strip(fmt.Sprintf("%s#%d.bottom", comp.ID, k), top-d, top-d+tf, -bf, 2*bf, x)
		strip(fmt.Sprintf("%s#%d.web", comp.ID, k), top-d+tf, top-tf, -tw, 2*tw, x)
	}
}

// steelBeams draws each W-shape along the wall, its top flange under the
// rafters' underside at its centreline.
func (c *compiler) steelBeams(rafterBottom float64) {
	f := c.f
	for i := range c.a.Components {
		comp := &c.a.Components[i]
		if comp.Type != TypeSteelBeam || !c.active(comp) {
			continue
		}
		d, bf, tw, tf, p := comp.param("depth"), comp.param("flangeWidth"), comp.param("webThickness"), comp.param("flangeThickness"), comp.param("position")
		zTop := f.zAt(p+bf/2, rafterBottom) // the lower edge of the sloping underside it carries
		rect := func(key string, h0, h1, z0, z1 float64) {
			poly := []V2{{h0, z0}, {h1, z0}, {h1, z1}, {h0, z1}}
			c.add(comp, convexPrism(key, poly, f.profileFrame(), vmul(f.A, f.Wa)))
		}
		rect(comp.ID+"#top", p-bf/2, p+bf/2, zTop-tf, zTop)
		rect(comp.ID+"#web", p-tw/2, p+tw/2, zTop-d+tf, zTop-tf)
		rect(comp.ID+"#bottom", p-bf/2, p+bf/2, zTop-d, zTop-d+tf)
	}
}
