package construction

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// TemplateRoofMasonry seeds the illustrative roof-to-masonry junction draft
// (§4): a 2400 mm × 1800 mm roof detail at a nominal 10° slope meeting two
// nominal 100 mm wythes. Every number is a labelled user-assumption for test
// and illustration only — never a prescribed design. Wall condition starts
// unknown and headwall/sidewall orientation unresolved.
const TemplateRoofMasonry = "roof-masonry-junction"

func init() {
	templates[TemplateRoofMasonry] = true
	templateSeeders[TemplateRoofMasonry] = func(tx *Tx) error {
		a := RoofMasonryTemplate(tx.Next.Problem.ID, NewID)
		tx.Next.Assemblies[a.ID] = a
		tx.Next.Problem.Alternatives = append(tx.Next.Problem.Alternatives, a.ID)
		tx.Next.Problem.ActiveAssembly = a.ID
		tx.Record("SeedAssembly", "assembly:"+a.ID, nil, map[string]any{"template": TemplateRoofMasonry, "components": len(a.Components)})
		tx.Summary("seeded draft roof-to-masonry assembly")
		return nil
	}
}

// Component types (controlled vocabulary).
const (
	TypeRafterArray     = "rafter-array"
	TypeDecking         = "timber-decking"
	TypeControlLayer    = "control-layer"
	TypeInsulation      = "insulation-board"
	TypeUnderlayment    = "underlayment"
	TypeBattenArray     = "batten-array"
	TypeCounterBattens  = "counter-batten-array"
	TypeCorrugatedSheet = "corrugated-sheet"
	TypeProfileClosure  = "profile-closure"
	TypeApronFlashing   = "apron-flashing"
	TypeSidewallFlash   = "sidewall-flashing"
	TypeCounterflashing = "counterflashing"
	TypeThroughWall     = "through-wall-flashing"
	TypeSealant         = "sealant-bead"
	TypeFastenerSet     = "fastener-set"
	TypeMasonryWythe    = "masonry-wythe"
	TypeCavitySpace     = "cavity-space"
	TypeWeepSet         = "weep-set"
	TypeEndDams         = "end-dams"
)

// Shape kinds and their allowlisted parameters: unit and inclusive range.
// Nothing outside these lists can be stored, so no script, expression or
// vertex list can ride in a component.
type paramSpec struct {
	Unit     string
	Min, Max float64
}

var shapeParams = map[string]map[string]paramSpec{
	"layer-panel":      {"thickness": {"mm", 0.2, 600}},
	"framing-array":    {"width": {"mm", 10, 400}, "depth": {"mm", 10, 600}, "spacing": {"mm", 100, 1500}, "offset": {"mm", 0, 1500}},
	"corrugated-panel": {"pitch": {"mm", 20, 300}, "depth": {"mm", 5, 80}, "thickness": {"mm", 0.2, 3}, "wallGap": {"mm", 0, 200}, "overhang": {"mm", 0, 300}},
	"profile-closure":  {"length": {"mm", 10, 200}, "topThickness": {"mm", 1, 30}, "setback": {"mm", 0, 200}},
	"bent-flashing":    {"thickness": {"mm", 0.3, 3}, "upstand": {"mm", 50, 400}, "leg": {"mm", 50, 400}, "bendRadius": {"mm", 0, 20}},
	"counterflashing":  {"thickness": {"mm", 0.3, 3}, "lap": {"mm", 25, 200}, "height": {"mm", 30, 300}, "embed": {"mm", 0, 60}, "clearance": {"mm", 0, 20}},
	"through-wall":     {"thickness": {"mm", 0.3, 3}, "upturn": {"mm", 50, 300}, "drip": {"mm", 10, 120}, "aboveUpstand": {"mm", 25, 300}},
	"sealant-bead":     {"size": {"mm", 3, 30}},
	"fastener-array":   {"diameter": {"mm", 2, 20}, "length": {"mm", 10, 600}, "headDiameter": {"mm", 4, 40}, "minEmbedment": {"mm", 0, 200}, "designStack": {"mm", 0, 1000}, "every": {"count", 1, 8}},
	"masonry-wythe":    {"thickness": {"mm", 50, 400}},
	"cavity":           {"width": {"mm", 10, 200}},
	"weep-array":       {"spacing": {"mm", 200, 2000}, "width": {"mm", 5, 30}, "height": {"mm", 20, 120}},
	"end-dam-pair":     {"height": {"mm", 10, 100}, "thickness": {"mm", 0.3, 3}},
}

// typeShapes: which shape kind a component type must use.
var typeShapes = map[string]string{
	TypeRafterArray: "framing-array", TypeDecking: "layer-panel", TypeControlLayer: "layer-panel",
	TypeInsulation: "layer-panel", TypeUnderlayment: "layer-panel", TypeBattenArray: "framing-array",
	TypeCounterBattens: "framing-array", TypeCorrugatedSheet: "corrugated-panel", TypeProfileClosure: "profile-closure",
	TypeApronFlashing: "bent-flashing", TypeSidewallFlash: "bent-flashing", TypeCounterflashing: "counterflashing",
	TypeThroughWall: "through-wall", TypeSealant: "sealant-bead", TypeFastenerSet: "fastener-array",
	TypeMasonryWythe: "masonry-wythe", TypeCavitySpace: "cavity", TypeWeepSet: "weep-array", TypeEndDams: "end-dam-pair",
}

// layerTypes may sit in the ordered roof stack.
var layerTypes = map[string]bool{TypeDecking: true, TypeControlLayer: true, TypeInsulation: true, TypeUnderlayment: true,
	TypeCounterBattens: true, TypeBattenArray: true, TypeCorrugatedSheet: true}

// Assembly-level parameters.
var assemblyParams = map[string]paramSpec{
	"pitch":           {"deg", 1, 60},
	"widthAlongWall":  {"mm", 300, 6000},
	"depthFromWall":   {"mm", 300, 6000},
	"wallHeightAbove": {"mm", 200, 3000},
	"wallDepthBelow":  {"mm", 100, 2000},
}

// Junction strategies by orientation; applicability depends on the wall.
var strategies = map[string]struct {
	Orientation string
	Wall        string // required wall condition ("" any)
	Supported   bool   // geometry exists in this compiler
	Note        string
}{
	"unresolved":                       {"", "", true, "strategy not chosen; counterflashing attachment unresolved"},
	"apron-surface-counterflashing":    {"headwall", "", true, "surface-held counterflashing over the apron upstand; durability and sealant dependence need review"},
	"apron-reglet-counterflashing":     {"headwall", "solid-bonded", true, "reglet counterflashing cut into solid masonry — conditional on masonry condition, permissible cutting and the roof manufacturer's detail"},
	"apron-through-wall-flashing":      {"headwall", "cavity", true, "cavity tray through the outer wythe with weeps and end dams; roof flashing does not replace cavity drainage"},
	"sidewall-surface-counterflashing": {"sidewall", "", true, "sidewall flashing over the first corrugation with surface-held counterflashing"},
	"sidewall-reglet-counterflashing":  {"sidewall", "solid-bonded", true, "sidewall flashing with raking reglet counterflashing in solid masonry — conditional"},
	"sidewall-through-wall-flashing":   {"sidewall", "cavity", false, "stepped cavity trays at a raking junction are not modelled in schema v1"},
	"step-flashing":                    {"sidewall", "", false, "step flashing is a shingle/tile detail; it is not valid on corrugated metal without the roof system's sourced detail"},
}

// StrategyNames lists every strategy key (sorted).
func StrategyNames() []string {
	out := make([]string, 0, len(strategies))
	for k := range strategies {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// IDSource mints component/assembly ids (NewID in production; fixed ids in
// fixtures so goldens are stable).
type IDSource func(kind string) string

func qmm(v float64, note string) Quantity {
	return Quantity{Value: &v, Unit: "mm", State: StateAssumed, Provenance: ProvUserAssumption, Illustrative: true, Note: note}
}
func qdeg(v float64, note string) Quantity {
	return Quantity{Value: &v, Unit: "deg", State: StateAssumed, Provenance: ProvUserAssumption, Illustrative: true, Note: note}
}
func qcount(v float64, note string) Quantity {
	return Quantity{Value: &v, Unit: "count", State: StateAssumed, Provenance: ProvUserAssumption, Illustrative: true, Note: note}
}

// RoofMasonryTemplate builds the draft assembly. The appearance/material
// refs are generic catalog families (catalog.go); no product is assumed.
func RoofMasonryTemplate(problemID string, ids IDSource) *Assembly {
	illus := "illustrative test-only default; verify before specifying"
	comp := func(typ, role, name string, layer *LayerSpec, params map[string]Quantity, material string) Component {
		c := Component{ID: ids(KindComponent), Type: typ, Role: role, Name: name, Layer: layer,
			Shape: Shape{Kind: typeShapes[typ], Params: params}, Transform: IdentityTransform(),
			Attachments: []string{}, Applicability: "applicable", Appearance: material}
		if material != "" {
			c.Material = &PinRef{ID: genericMaterialID(material), Revision: 1}
		}
		return c
	}
	rafters := comp(TypeRafterArray, "structure:exposed-rafters", "Exposed rafters (structural host)", nil,
		map[string]Quantity{"width": qmm(50, illus), "depth": qmm(200, illus), "spacing": qmm(600, illus), "offset": qmm(300, illus)}, "timber-rafter")
	deck := comp(TypeDecking, "structure:finish-deck", "Finish-grade timber decking (exposed underside)", &LayerSpec{Order: 1, Role: "deck"},
		map[string]Quantity{"thickness": qmm(25, illus)}, "timber-decking")
	avcl := comp(TypeControlLayer, "control:air-vapour-candidate", "Air/vapour-control layer (candidate; climate-dependent)", &LayerSpec{Order: 2, Role: "air-vapour"},
		map[string]Quantity{"thickness": qmm(1, illus)}, "membrane-avcl")
	ins := comp(TypeInsulation, "thermal:insulation", "Above-deck insulation", &LayerSpec{Order: 3, Role: "insulation"},
		map[string]Quantity{"thickness": qmm(100, illus+" (100 mm is test geometry only)")}, "insulation-rigid")
	ul := comp(TypeUnderlayment, "control:water-underlayment", "Underlayment / waterproofing", &LayerSpec{Order: 4, Role: "water"},
		map[string]Quantity{"thickness": qmm(2, illus)}, "membrane-underlayment")
	battens := comp(TypeBattenArray, "support:battens", "Support battens", &LayerSpec{Order: 5, Role: "support"},
		map[string]Quantity{"width": qmm(63, illus), "depth": qmm(38, illus), "spacing": qmm(600, illus), "offset": qmm(150, illus)}, "timber-batten")
	sheet := comp(TypeCorrugatedSheet, "roof:corrugated-metal", "Corrugated metal roof sheet", &LayerSpec{Order: 6, Role: "roofing"},
		map[string]Quantity{"pitch": qmm(76, illus), "depth": qmm(18, illus), "thickness": qmm(0.5, illus), "wallGap": qmm(30, illus), "overhang": qmm(50, illus)}, "metal-corrugated")
	closure := comp(TypeProfileClosure, "roof:profile-closure", "Profile closure (under apron)", nil,
		map[string]Quantity{"length": qmm(50, illus), "topThickness": qmm(3, illus), "setback": qmm(15, illus)}, "closure-foam")
	apron := comp(TypeApronFlashing, "flashing:base-apron", "Base / apron flashing", nil,
		map[string]Quantity{"thickness": qmm(0.6, illus), "upstand": qmm(150, illus), "leg": qmm(150, illus), "bendRadius": qmm(3, illus)}, "metal-flashing")
	counter := comp(TypeCounterflashing, "flashing:counter", "Counterflashing (wall attachment unresolved)", nil,
		map[string]Quantity{"thickness": qmm(0.6, illus), "lap": qmm(75, illus), "height": qmm(90, illus), "embed": qmm(0, illus), "clearance": qmm(2, illus)}, "metal-flashing")
	sealant := comp(TypeSealant, "seal:counterflashing-to-wall", "Sealant interface (counterflashing to wall)", nil,
		map[string]Quantity{"size": qmm(10, illus)}, "sealant-generic")
	sheetFix := comp(TypeFastenerSet, "attachment:sheet-to-batten", "Sheet fasteners (crest-fixed)", nil,
		map[string]Quantity{"diameter": qmm(6.3, illus), "length": qmm(45, illus), "headDiameter": qmm(16, illus), "minEmbedment": qmm(20, illus), "designStack": qmm(0, "not stack-dependent"), "every": qcount(2, "every second crest (illustrative)")}, "fastener-steel")
	stackFix := comp(TypeFastenerSet, "attachment:batten-through-stack-to-rafter", "Structural screws: batten through insulation to rafter", nil,
		map[string]Quantity{"diameter": qmm(8, illus), "length": qmm(230, illus), "headDiameter": qmm(14, illus), "minEmbedment": qmm(50, illus), "designStack": qmm(166, "stack thickness the length was chosen for"), "every": qcount(1, "at every rafter crossing (illustrative)")}, "fastener-steel")
	outer := comp(TypeMasonryWythe, "masonry:outer-wythe", "Outer masonry wythe (nominal 100)", nil,
		map[string]Quantity{"thickness": qmm(100, illus)}, "masonry-brick")
	inner := comp(TypeMasonryWythe, "masonry:inner-wythe", "Inner masonry wythe (nominal 100)", nil,
		map[string]Quantity{"thickness": qmm(100, illus)}, "masonry-brick")
	stackFix.HostID = rafters.ID
	sheetFix.HostID = battens.ID
	sheet.HostID = battens.ID
	battens.HostID = rafters.ID
	apron.HostID = sheet.ID
	closure.HostID = sheet.ID
	counter.HostID = outer.ID
	sealant.HostID = outer.ID
	sheetFix.Attachments = []string{sheet.ID, battens.ID}
	stackFix.Attachments = []string{battens.ID, ul.ID, ins.ID, avcl.ID, deck.ID, rafters.ID}
	counter.Applicability, sealant.Applicability = "conditional", "conditional"
	comps := []Component{rafters, deck, avcl, ins, ul, battens, sheet, closure, apron, counter, sealant, sheetFix, stackFix, outer, inner}
	a := &Assembly{
		Envelope:  Envelope{SchemaVersion: SchemaVersion, Kind: DocAssembly, ID: ids(KindAssembly)},
		ProblemID: problemID,
		Name:      "Draft — roof to masonry (conditions unresolved)",
		Summary:   "Illustrative starting assembly. Wall type and headwall/sidewall orientation are unknown; every dimension is a test-only assumption.",
		Template:  TemplateRoofMasonry,
		Units:     "mm", Convention: CoordinateConvention,
		Junction: Junction{
			ID: ids(KindJunction), Type: "roof-wall-unresolved", Orientation: "unresolved",
			WallCondition: WallCondition{Value: "unknown", Provenance: ProvUnknown, Note: "two wythes do not establish a cavity"},
			Strategy:      "unresolved",
			Components:    []string{sheet.ID, apron.ID, counter.ID, sealant.ID, outer.ID, inner.ID, ul.ID},
			Transitions: []Transition{
				{Kind: "water", From: ul.ID, To: apron.ID, Via: []string{closure.ID}, Required: true, Note: "underlayment turned up behind the apron upstand (method unresolved)"},
				{Kind: "water", From: apron.ID, To: outer.ID, Via: []string{counter.ID, sealant.ID}, Required: true, Note: "counterflashing laps the upstand; wall attachment depends on wall type"},
				{Kind: "air", From: avcl.ID, To: inner.ID, Via: []string{}, Required: true, Note: "air-control continuity from roof to wall is unresolved"},
			},
			AttachmentPath:      []string{sheetFix.ID, battens.ID, stackFix.ID, rafters.ID},
			SupportedStrategies: StrategyNames(),
		},
		Parameters: map[string]Quantity{
			"pitch":           qdeg(10, "nominal 10° — illustrative"),
			"widthAlongWall":  qmm(2400, illus),
			"depthFromWall":   qmm(1800, illus),
			"wallHeightAbove": qmm(700, illus),
			"wallDepthBelow":  qmm(450, illus),
		},
		Components: comps,
		Tombstones: []Tombstone{},
		Relationships: []Relationship{
			{From: rafters.ID, To: deck.ID, Kind: "hosts"}, {From: battens.ID, To: sheet.ID, Kind: "hosts"},
			{From: apron.ID, To: sheet.ID, Kind: "laps-over"}, {From: counter.ID, To: apron.ID, Kind: "laps-over"},
			{From: sealant.ID, To: counter.ID, Kind: "seals"}, {From: sheet.ID, To: apron.ID, Kind: "drains-to"},
		},
		Assumptions: []Assumption{
			{Key: "wall-condition", Text: "Wall type unknown: solid/bonded vs cavity is not established by two wythes.", Provenance: ProvUnknown, Status: "open"},
			{Key: "orientation", Text: "Headwall vs sidewall orientation unresolved; geometry shows the headwall arrangement only as an illustration.", Provenance: ProvUnknown, Status: "open"},
			{Key: "dimensions", Text: "All dimensions are illustrative test-only defaults until sourced or measured.", Provenance: ProvUserAssumption, Status: "open"},
			{Key: "exposed-timber", Text: "Exposed rafters and the finish-grade deck underside must remain exposed in every viable alternative.", Provenance: ProvUserAssumption, Status: "confirmed"},
		},
		EvidenceLinks:   []EvidenceLink{},
		IssueStates:     map[string]IssueAck{},
		Lifecycle:       AssemblyDraft,
		Applicability:   Applicability{Status: "unknown", Conditions: []string{"wall type", "orientation"}, Reasons: []string{"wall condition and orientation unresolved"}},
		CompilerVersion: CompilerVersion,
	}
	return a
}

// ---- lookups and parameter resolution -------------------------------------------------

func (a *Assembly) component(id string) (*Component, int) {
	for i := range a.Components {
		if a.Components[i].ID == id {
			return &a.Components[i], i
		}
	}
	return nil, -1
}

// Component returns a copy of a component by id.
func (a *Assembly) Component(id string) (Component, bool) {
	c, i := a.component(id)
	if i < 0 {
		return Component{}, false
	}
	return *c, true
}

func (a *Assembly) byType(typ string) []*Component {
	var out []*Component
	for i := range a.Components {
		if a.Components[i].Type == typ {
			out = append(out, &a.Components[i])
		}
	}
	return out
}

func (a *Assembly) firstOf(typ string) *Component {
	if cs := a.byType(typ); len(cs) > 0 {
		return cs[0]
	}
	return nil
}

func (a *Assembly) byRole(prefix string) *Component {
	for i := range a.Components {
		if strings.HasPrefix(a.Components[i].Role, prefix) {
			return &a.Components[i]
		}
	}
	return nil
}

// param returns a component parameter's effective value (mm/deg/count).
func (c *Component) param(name string) float64 {
	q, ok := c.Shape.Params[name]
	if !ok {
		return 0
	}
	v, _ := q.Effective()
	return v
}

func (a *Assembly) aparam(name string) float64 {
	q, ok := a.Parameters[name]
	if !ok {
		return 0
	}
	v, _ := q.Effective()
	return v
}

// layers returns the stack components in order (bottom → top).
func (a *Assembly) layers() []*Component {
	var out []*Component
	for i := range a.Components {
		if a.Components[i].Layer != nil {
			out = append(out, &a.Components[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Layer.Order < out[j].Layer.Order })
	return out
}

// ToMM converts a user value to millimetres (or degrees for angles). Unknown
// units are refused; nothing is guessed.
func ToMM(v float64, unit string) (float64, error) {
	if !finite(v) {
		return 0, ErrNonFinite
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "mm":
		return v, nil
	case "cm":
		return v * 10, nil
	case "m":
		return v * 1000, nil
	case "in", "inch", "inches", "\"":
		return v * 25.4, nil
	case "ft", "foot", "feet", "'":
		return v * 304.8, nil
	}
	return 0, Invalid(fmt.Sprintf("unknown unit %q (use mm, cm, m, in or ft)", unit))
}

// checkQuantity validates a stored quantity against its spec.
func checkQuantity(field string, q Quantity, spec paramSpec) []string {
	var out []string
	if q.Unit != spec.Unit {
		out = append(out, fmt.Sprintf("%s: unit must be %s", field, spec.Unit))
	}
	if q.State != StateKnown && q.State != StateAssumed && q.State != StateUnknown {
		out = append(out, field+": state must be known, assumed or unknown")
	}
	if !provenances[q.Provenance] {
		out = append(out, field+": provenance is not in the taxonomy")
	}
	check := func(label string, v *float64) {
		if v == nil {
			return
		}
		if !finite(*v) {
			out = append(out, field+": "+label+" is not finite")
		} else if *v < spec.Min || *v > spec.Max {
			out = append(out, fmt.Sprintf("%s: %s %.4g outside %.4g–%.4g %s", field, label, *v, spec.Min, spec.Max, spec.Unit))
		}
	}
	// designStack/embed may legitimately be zero
	if q.Value != nil && *q.Value == 0 && spec.Min == 0 {
	} else {
		check("value", q.Value)
	}
	check("placeholder", q.Placeholder)
	if q.Value == nil && q.State != StateUnknown {
		out = append(out, field+": a null value is unknown")
	}
	if q.Value == nil && q.Placeholder == nil {
		out = append(out, field+": an unknown dimension needs a placeholder for geometry (shown unresolved)")
	}
	if q.Value != nil && q.State == StateUnknown {
		out = append(out, field+": a known value cannot be state unknown")
	}
	return out
}

func round3(x float64) float64 { return normZero(math.Round(x*1000) / 1000) }
