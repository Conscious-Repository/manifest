package construction

// Typed assembly operations (§3.4). Inspector controls and agent proposals
// both arrive here as the same structs and reduce through the same code; the
// commit then recompiles and revalidates server-side. There is no free-text
// apply, no JSON Patch, no script and no vertex edit anywhere in this file.

import (
	"fmt"
	"sort"
	"strings"
)

func init() {
	registerOp("SetDimension", TargetAssembly, false, func() Operation { return &SetDimension{} })
	registerOp("SetPitch", TargetAssembly, false, func() Operation { return &SetPitch{} })
	registerOp("SetTransform", TargetAssembly, false, func() Operation { return &SetTransform{} })
	registerOp("SetMaterial", TargetAssembly, false, func() Operation { return &SetMaterial{} })
	registerOp("SetProduct", TargetAssembly, false, func() Operation { return &SetProduct{} })
	registerOp("SetAppearance", TargetAssembly, false, func() Operation { return &SetAppearance{} })
	registerOp("SetLayerOrder", TargetAssembly, false, func() Operation { return &SetLayerOrder{} })
	registerOp("SetJunctionStrategy", TargetAssembly, false, func() Operation { return &SetJunctionStrategy{} })
	registerOp("SetWallCondition", TargetAssembly, false, func() Operation { return &SetWallCondition{} })
	registerOp("SetAssumption", TargetAssembly, false, func() Operation { return &SetAssumption{} })
	registerOp("SetAssemblyText", TargetAssembly, false, func() Operation { return &SetAssemblyText{} })
	registerOp("SetAssemblyLifecycle", TargetAssembly, false, func() Operation { return &SetAssemblyLifecycle{} })
	registerOp("AddComponent", TargetAssembly, false, func() Operation { return &AddComponent{} })
	registerOp("RemoveComponent", TargetAssembly, false, func() Operation { return &RemoveComponent{} })
	registerOp("CreateVariant", TargetAssembly, false, func() Operation { return &CreateVariant{} })
	registerOp("RestoreRevision", TargetAssembly, false, func() Operation { return &RestoreRevision{} })
	registerOp("AcknowledgeIssue", TargetAssembly, true, func() Operation { return &AcknowledgeIssue{} })
	registerOp("SelectVariant", TargetProblem, true, func() Operation { return &SelectVariant{} })
}

// asm is the command's target assembly in the transaction's next state.
func asm(tx *Tx, c *ApplyContext) (*Assembly, error) {
	a := tx.Next.Assemblies[c.Command.AssemblyID]
	if a == nil {
		return nil, NotFound("no such assembly in this problem")
	}
	return a, nil
}

func comp(a *Assembly, id string) (*Component, error) {
	c, _ := a.component(id)
	if c == nil {
		return nil, NotFound("no component " + id + " in this assembly")
	}
	return c, nil
}

func newCompID(a *Assembly, id string) error {
	if !ValidID(KindComponent, id) {
		return Invalid("new component ids must be cmp-<32 hex>, chosen by the caller")
	}
	if c, _ := a.component(id); c != nil {
		return Invalid("component id " + id + " already exists")
	}
	for _, t := range a.Tombstones {
		if t.ID == id {
			return Invalid("component id " + id + " belonged to a removed component; ids are never reused")
		}
	}
	return nil
}

// stackToRafter is the stack thickness from the rafter top to the batten top
// (the thickness a structural screw must cross), from current parameters.
func stackToRafter(a *Assembly) float64 {
	sum := 0.0
	for _, l := range a.layers() {
		if l.Applicability == "inapplicable" {
			continue
		}
		switch l.Type {
		case TypeCorrugatedSheet:
		case TypeBattenArray, TypeCounterBattens:
			sum += l.param("depth")
		default:
			sum += l.param("thickness")
		}
	}
	return sum
}

// ---- SetDimension / SetPitch ------------------------------------------------------------

// SetDimension sets one component shape parameter or assembly parameter.
// value null makes it unknown (the previous number stays as the labelled
// placeholder for geometry). Units convert once, deterministically.
type SetDimension struct {
	Op          string   `json:"op"`
	ComponentID string   `json:"componentId,omitempty"`
	Dimension   string   `json:"dimension,omitempty"`
	Parameter   string   `json:"parameter,omitempty"`
	Value       *float64 `json:"value"`
	Unit        string   `json:"unit"`
	State       string   `json:"state,omitempty"`
	Provenance  string   `json:"provenance,omitempty"`
	Note        string   `json:"note,omitempty"`
}

func (o *SetDimension) Name() string { return "SetDimension" }
func (o *SetDimension) Check() []string {
	var out []string
	if (o.ComponentID == "") == (o.Parameter == "") {
		out = append(out, "set either componentId+dimension or parameter")
	}
	if o.ComponentID != "" && (!ValidID(KindComponent, o.ComponentID) || o.Dimension == "") {
		out = append(out, "componentId must be a cmp- id with a dimension name")
	}
	if o.Value != nil && !finite(*o.Value) {
		out = append(out, "value must be finite")
	}
	if o.State != "" && o.State != StateKnown && o.State != StateAssumed && o.State != StateUnknown {
		out = append(out, "state must be known, assumed or unknown")
	}
	if o.Provenance != "" && !provenances[o.Provenance] {
		out = append(out, "provenance is not in the taxonomy")
	}
	out = append(out, checkText("note", o.Note, 500, false)...)
	return out
}

func (o *SetDimension) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	var spec paramSpec
	var cur Quantity
	var target string
	var set func(Quantity)
	if o.Parameter != "" {
		s, ok := assemblyParams[o.Parameter]
		if !ok {
			return Invalid("unknown assembly parameter " + o.Parameter)
		}
		spec, cur, target = s, a.Parameters[o.Parameter], "param:"+o.Parameter
		set = func(q Quantity) { a.Parameters[o.Parameter] = q }
	} else {
		cp, err := comp(a, o.ComponentID)
		if err != nil {
			return err
		}
		s, ok := shapeParams[cp.Shape.Kind][o.Dimension]
		if !ok {
			return Invalid(cp.Type + " has no dimension " + o.Dimension)
		}
		spec, cur, target = s, cp.Shape.Params[o.Dimension], cp.ID+"."+o.Dimension
		set = func(q Quantity) { cp.Shape.Params[o.Dimension] = q }
		if cp.Type == TypeFastenerSet && o.Dimension == "length" && cp.HostID != "" {
			if host, _ := a.component(cp.HostID); host != nil && host.Type == TypeRafterArray {
				// re-specifying a structural screw's length records the stack it
				// was chosen for; embedment and structural review still apply
				ds := cp.Shape.Params["designStack"]
				v := round3(stackToRafter(a))
				ds.Value, ds.State, ds.Provenance, ds.Illustrative = &v, StateAssumed, ProvUserAssumption, false
				ds.Placeholder = nil
				cp.Shape.Params["designStack"] = ds
			}
		}
	}
	next := cur
	next.Unit = spec.Unit
	next.Illustrative = false
	next.Note = o.Note
	if o.Value == nil {
		if v, ok := cur.Effective(); ok {
			ph := v
			next.Placeholder = &ph
		}
		next.Value, next.State, next.Provenance = nil, StateUnknown, ProvUnknown
	} else {
		v := *o.Value
		switch spec.Unit {
		case "mm":
			mm, err := ToMM(v, orDefault(o.Unit, "mm"))
			if err != nil {
				return err
			}
			v = mm
		default:
			if o.Unit != spec.Unit {
				return Invalid(fmt.Sprintf("%s takes %s, not %q", target, spec.Unit, o.Unit))
			}
		}
		v = round3(v)
		if v < spec.Min || v > spec.Max {
			if !(v == 0 && spec.Min == 0) {
				return Invalid(fmt.Sprintf("%s: %.4g %s is outside %.4g–%.4g", target, v, spec.Unit, spec.Min, spec.Max))
			}
		}
		next.Value = &v
		next.Placeholder = nil
		next.State = orDefault(o.State, StateAssumed)
		next.Provenance = orDefault(o.Provenance, ProvUserAssumption)
		if next.State == StateUnknown {
			return Invalid("a value cannot have state unknown; send null to make it unknown")
		}
	}
	tx.Record(o.Name(), target, cur, next)
	set(next)
	tx.Summary(fmt.Sprintf("set %s", target))
	return nil
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// SetPitch sets the roof pitch in degrees.
type SetPitch struct {
	Op         string  `json:"op"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	Provenance string  `json:"provenance,omitempty"`
}

func (o *SetPitch) Name() string { return "SetPitch" }
func (o *SetPitch) Check() []string {
	if !finite(o.Value) {
		return []string{"value must be finite"}
	}
	if o.Unit != "deg" {
		return []string{"pitch unit must be deg"}
	}
	if o.Provenance != "" && !provenances[o.Provenance] {
		return []string{"provenance is not in the taxonomy"}
	}
	return nil
}
func (o *SetPitch) Apply(tx *Tx, c *ApplyContext) error {
	v := o.Value
	return (&SetDimension{Parameter: "pitch", Value: &v, Unit: "deg", Provenance: o.Provenance}).Apply(tx, c)
}

// ---- positioning, material, product, appearance, layer order -------------------------------

// SetTransform repositions a supported part by a translation (mm).
type SetTransform struct {
	Op          string     `json:"op"`
	ComponentID string     `json:"componentId"`
	Translation [3]float64 `json:"translation"`
	Unit        string     `json:"unit"`
}

func (o *SetTransform) Name() string { return "SetTransform" }
func (o *SetTransform) Check() []string {
	if !ValidID(KindComponent, o.ComponentID) {
		return []string{"componentId must be a cmp- id"}
	}
	if !finite(o.Translation[:]...) {
		return []string{"translation must be finite"}
	}
	return nil
}
func (o *SetTransform) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	cp, err := comp(a, o.ComponentID)
	if err != nil {
		return err
	}
	var t [3]float64
	for i := range t {
		mm, err := ToMM(o.Translation[i], orDefault(o.Unit, "mm"))
		if err != nil {
			return err
		}
		t[i] = round3(mm)
	}
	next := Transform{Translation: t, Rotation: [4]float64{0, 0, 0, 1}}
	if errs := checkTransform("translation", &Component{Type: cp.Type, Transform: next}, a); len(errs) > 0 {
		return Invalid(errs...)
	}
	tx.Record(o.Name(), cp.ID+".transform", cp.Transform, next)
	cp.Transform = next
	return nil
}

// SetMaterial pins a catalog material's current revision on a component.
type SetMaterial struct {
	Op          string `json:"op"`
	ComponentID string `json:"componentId"`
	MaterialID  string `json:"materialId"`
}

func (o *SetMaterial) Name() string { return "SetMaterial" }
func (o *SetMaterial) Check() []string {
	if !ValidID(KindComponent, o.ComponentID) || !ValidID(KindMaterial, o.MaterialID) {
		return []string{"componentId (cmp-) and materialId (mat-) are required"}
	}
	return nil
}
func (o *SetMaterial) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	cp, err := comp(a, o.ComponentID)
	if err != nil {
		return err
	}
	for _, m := range tx.Next.Catalog.Materials {
		if m.ID == o.MaterialID {
			next := &PinRef{ID: m.ID, Revision: m.Revision}
			tx.Record(o.Name(), cp.ID+".material", cp.Material, next)
			cp.Material = next
			return nil
		}
	}
	return NotFound("no material " + o.MaterialID + " in this problem's catalog")
}

// SetProduct pins (or clears) a sourced product on a component. A product is
// never followed to a newer catalog revision automatically.
type SetProduct struct {
	Op          string `json:"op"`
	ComponentID string `json:"componentId"`
	ProductID   string `json:"productId"` // "" clears
}

func (o *SetProduct) Name() string { return "SetProduct" }
func (o *SetProduct) Check() []string {
	if !ValidID(KindComponent, o.ComponentID) || (o.ProductID != "" && !ValidID(KindProduct, o.ProductID)) {
		return []string{"componentId (cmp-) and productId (prd- or empty) are required"}
	}
	return nil
}
func (o *SetProduct) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	cp, err := comp(a, o.ComponentID)
	if err != nil {
		return err
	}
	if o.ProductID == "" {
		tx.Record(o.Name(), cp.ID+".product", cp.Product, nil)
		cp.Product = nil
		return nil
	}
	for _, p := range tx.Next.Catalog.Products {
		if p.ID == o.ProductID {
			if p.Lifecycle == "withdrawn" {
				return Invalid("product " + p.Model + " is withdrawn")
			}
			next := &PinRef{ID: p.ID, Revision: p.Revision}
			tx.Record(o.Name(), cp.ID+".product", cp.Product, next)
			cp.Product = next
			tx.Summary("product substitution on " + cp.Name)
			return nil
		}
	}
	return NotFound("no product " + o.ProductID + " in this problem's catalog")
}

// SetAppearance changes only how a part looks (a generic appearance key);
// identity, material and specification are untouched.
type SetAppearance struct {
	Op          string `json:"op"`
	ComponentID string `json:"componentId"`
	Appearance  string `json:"appearance"`
}

func (o *SetAppearance) Name() string { return "SetAppearance" }
func (o *SetAppearance) Check() []string {
	if !ValidID(KindComponent, o.ComponentID) {
		return []string{"componentId must be a cmp- id"}
	}
	if _, ok := genericByKey[o.Appearance]; !ok {
		return []string{"appearance must be a catalog appearance key"}
	}
	return nil
}
func (o *SetAppearance) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	cp, err := comp(a, o.ComponentID)
	if err != nil {
		return err
	}
	tx.Record(o.Name(), cp.ID+".appearance", cp.Appearance, o.Appearance)
	cp.Appearance = o.Appearance
	return nil
}

// SetLayerOrder reorders the roof stack; ids must be exactly the layers.
type SetLayerOrder struct {
	Op           string   `json:"op"`
	ComponentIDs []string `json:"componentIds"`
}

func (o *SetLayerOrder) Name() string { return "SetLayerOrder" }
func (o *SetLayerOrder) Check() []string {
	if len(o.ComponentIDs) < 2 || len(o.ComponentIDs) > 32 {
		return []string{"give 2–32 layer component ids in their new order"}
	}
	return nil
}
func (o *SetLayerOrder) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	cur := a.layers()
	if len(cur) != len(o.ComponentIDs) {
		return Invalid("the new order must list every layer exactly once")
	}
	before := make([]string, len(cur))
	want := map[string]bool{}
	for i, l := range cur {
		before[i] = l.ID
		want[l.ID] = true
	}
	for _, id := range o.ComponentIDs {
		if !want[id] {
			return Invalid("the new order must list every layer exactly once")
		}
		delete(want, id)
	}
	for i, id := range o.ComponentIDs {
		cp, _ := a.component(id)
		cp.Layer.Order = i + 1
	}
	tx.Record(o.Name(), a.ID+".layers", before, o.ComponentIDs)
	return nil
}

// ---- junction strategy and wall condition ----------------------------------------------

// SetJunctionStrategy resolves orientation + strategy. Orientation-specific
// parts are switched applicable/inapplicable (never silently rotated), and
// parts a strategy needs are created with ids the caller supplied.
type SetJunctionStrategy struct {
	Op              string            `json:"op"`
	Orientation     string            `json:"orientation"`
	Strategy        string            `json:"strategy"`
	NewComponentIDs map[string]string `json:"newComponentIds,omitempty"`
}

func (o *SetJunctionStrategy) Name() string { return "SetJunctionStrategy" }
func (o *SetJunctionStrategy) Check() []string {
	var out []string
	if !orientations[o.Orientation] {
		out = append(out, "orientation must be unresolved, headwall or sidewall")
	}
	st, ok := strategies[o.Strategy]
	if !ok {
		out = append(out, "unknown strategy "+o.Strategy)
		return out
	}
	if o.Orientation == "unresolved" && o.Strategy != "unresolved" {
		out = append(out, "choose an orientation before a strategy")
	}
	if st.Orientation != "" && st.Orientation != o.Orientation {
		out = append(out, o.Strategy+" is a "+st.Orientation+" strategy")
	}
	if !st.Supported {
		out = append(out, o.Strategy+": "+st.Note)
	}
	for slot, id := range o.NewComponentIDs {
		if slot != "sidewall-flashing" && slot != "through-wall" && slot != "weeps" && slot != "end-dams" && slot != "apron" {
			out = append(out, "unknown new component slot "+slot)
		}
		if !ValidID(KindComponent, id) {
			out = append(out, "new component ids must be cmp- ids")
		}
	}
	return out
}

func (o *SetJunctionStrategy) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	before := map[string]any{"orientation": a.Junction.Orientation, "strategy": a.Junction.Strategy}
	illus := "illustrative test-only default; verify before specifying"
	if o.Strategy == "apron-through-wall-flashing" && (a.Junction.WallCondition.Value != "cavity" || a.firstOf(TypeCavitySpace) == nil) {
		return Invalid("a through-wall cavity tray needs a cavity wall: set the wall condition to cavity first (an alternative may assume it, labelled conditional)")
	}
	if strings.Contains(o.Strategy, "reglet") && a.firstOf(TypeCounterflashing) == nil {
		return Invalid("a reglet strategy needs the counterflashing component")
	}
	// 1. create every part the strategy needs (appends may move the slice,
	//    so no component pointer is held across this step)
	ensure := func(slot, typ string, build func(id string) Component) error {
		if a.firstOf(typ) != nil {
			return nil
		}
		id := o.NewComponentIDs[slot]
		if id == "" {
			return Invalid("strategy " + o.Strategy + " needs a new " + slot + " component: supply newComponentIds." + slot)
		}
		if err := newCompID(a, id); err != nil {
			return err
		}
		a.Components = append(a.Components, build(id))
		return nil
	}
	flashing := func(id, typ, role, name, host string) Component {
		return Component{ID: id, Type: typ, Role: role, Name: name,
			Shape:     Shape{Kind: "bent-flashing", Params: map[string]Quantity{"thickness": qmm(0.6, illus), "upstand": qmm(150, illus), "leg": qmm(150, illus), "bendRadius": qmm(3, illus)}},
			Transform: IdentityTransform(), HostID: host, Material: &PinRef{ID: genericMaterialID("metal-flashing"), Revision: 1}, Appearance: "metal-flashing", Attachments: []string{}, Applicability: "applicable"}
	}
	sheetID := idOf(a.firstOf(TypeCorrugatedSheet))
	outerID := idOf(a.byRole("masonry:outer"))
	headwall := o.Orientation != "sidewall"
	if headwall {
		if err := ensure("apron", TypeApronFlashing, func(id string) Component {
			return flashing(id, TypeApronFlashing, "flashing:base-apron", "Base / apron flashing", sheetID)
		}); err != nil {
			return err
		}
	} else if err := ensure("sidewall-flashing", TypeSidewallFlash, func(id string) Component {
		return flashing(id, TypeSidewallFlash, "flashing:sidewall", "Sidewall flashing (over first corrugation)", sheetID)
	}); err != nil {
		return err
	}
	if o.Strategy == "apron-through-wall-flashing" {
		if err := ensure("through-wall", TypeThroughWall, func(id string) Component {
			return Component{ID: id, Type: TypeThroughWall, Role: "flashing:through-wall-tray", Name: "Through-wall flashing / cavity tray",
				Shape:     Shape{Kind: "through-wall", Params: map[string]Quantity{"thickness": qmm(0.6, illus), "upturn": qmm(150, illus), "drip": qmm(110, illus), "aboveUpstand": qmm(75, illus)}},
				Transform: IdentityTransform(), HostID: outerID, Material: &PinRef{ID: genericMaterialID("metal-flashing"), Revision: 1}, Appearance: "metal-flashing", Attachments: []string{}, Applicability: "applicable"}
		}); err != nil {
			return err
		}
		if err := ensure("weeps", TypeWeepSet, func(id string) Component {
			return Component{ID: id, Type: TypeWeepSet, Role: "drainage:weeps", Name: "Weeps (cavity outlets)",
				Shape:     Shape{Kind: "weep-array", Params: map[string]Quantity{"spacing": qmm(600, illus), "width": qmm(10, illus), "height": qmm(65, illus)}},
				Transform: IdentityTransform(), HostID: outerID, Appearance: "cavity-air", Attachments: []string{}, Applicability: "applicable"}
		}); err != nil {
			return err
		}
		twID := idOf(a.firstOf(TypeThroughWall))
		if err := ensure("end-dams", TypeEndDams, func(id string) Component {
			return Component{ID: id, Type: TypeEndDams, Role: "drainage:end-dams", Name: "End dams",
				Shape:     Shape{Kind: "end-dam-pair", Params: map[string]Quantity{"height": qmm(25, illus), "thickness": qmm(0.6, illus)}},
				Transform: IdentityTransform(), HostID: twID, Material: &PinRef{ID: genericMaterialID("metal-flashing"), Revision: 1}, Appearance: "metal-flashing", Attachments: []string{}, Applicability: "applicable"}
		}); err != nil {
			return err
		}
	}
	// 2. switch applicability (pointers resolved after every append)
	setApp := func(typ, v string) {
		for i := range a.Components {
			if a.Components[i].Type == typ {
				a.Components[i].Applicability = v
			}
		}
	}
	on := map[bool]string{true: "applicable", false: "inapplicable"}
	setApp(TypeApronFlashing, on[headwall]) // a headwall apron is never rotated into a sidewall
	setApp(TypeProfileClosure, on[headwall])
	setApp(TypeSidewallFlash, on[!headwall])
	tray := o.Strategy == "apron-through-wall-flashing"
	setApp(TypeThroughWall, on[tray])
	setApp(TypeWeepSet, on[tray])
	setApp(TypeEndDams, on[tray])
	switch {
	case tray:
		setApp(TypeCounterflashing, "inapplicable") // the tray's drip laps the upstand instead
		setApp(TypeSealant, "inapplicable")
	case o.Strategy == "unresolved":
		setApp(TypeCounterflashing, "conditional")
		setApp(TypeSealant, "conditional")
	default:
		setApp(TypeCounterflashing, "applicable")
		setApp(TypeSealant, "applicable")
	}
	if cf := a.firstOf(TypeCounterflashing); cf != nil {
		switch {
		case strings.Contains(o.Strategy, "reglet"):
			if cf.param("embed") == 0 {
				setEmbed(cf, 25)
			}
		case !tray:
			setEmbed(cf, 0)
		}
	}
	a.Junction.Orientation, a.Junction.Strategy = o.Orientation, o.Strategy
	a.Junction.Type = map[string]string{"unresolved": "roof-wall-unresolved", "headwall": "roof-headwall", "sidewall": "roof-sidewall"}[o.Orientation]
	refreshJunction(a)
	tx.Record(o.Name(), a.Junction.ID, before, map[string]any{"orientation": o.Orientation, "strategy": o.Strategy})
	tx.Summary("junction " + o.Orientation + " / " + o.Strategy)
	return nil
}

func idOf(c *Component) string {
	if c == nil {
		return ""
	}
	return c.ID
}

func setEmbed(c *Component, v float64) {
	q := c.Shape.Params["embed"]
	q.Value, q.Placeholder = &v, nil
	if q.State == StateUnknown {
		q.State, q.Provenance = StateAssumed, ProvUserAssumption
	}
	c.Shape.Params["embed"] = q
}

// refreshJunction keeps the junction's part list and transition paths in
// step with which parts are applicable.
func refreshJunction(a *Assembly) {
	j := &a.Junction
	active := func(c *Component) bool { return c != nil && c.Applicability != "inapplicable" }
	var parts []string
	for _, c := range a.Components {
		switch c.Type {
		case TypeCorrugatedSheet, TypeApronFlashing, TypeSidewallFlash, TypeCounterflashing, TypeSealant, TypeThroughWall,
			TypeWeepSet, TypeEndDams, TypeMasonryWythe, TypeCavitySpace, TypeUnderlayment:
			if c.Applicability != "inapplicable" {
				parts = append(parts, c.ID)
			}
		}
	}
	j.Components = parts
	base := firstActive(a, TypeApronFlashing, TypeSidewallFlash)
	ul := a.firstOf(TypeUnderlayment)
	closure := a.firstOf(TypeProfileClosure)
	counter, sealant := a.firstOf(TypeCounterflashing), a.firstOf(TypeSealant)
	tw, outer, inner, avcl := a.firstOf(TypeThroughWall), a.byRole("masonry:outer"), a.byRole("masonry:inner"), a.firstOf(TypeControlLayer)
	var tr []Transition
	if ul != nil && base != nil {
		via := []string{}
		if active(closure) {
			via = append(via, closure.ID)
		} else {
			via = append(via, base.ID)
		}
		tr = append(tr, Transition{Kind: "water", From: ul.ID, To: base.ID, Via: via, Required: true, Note: "underlayment turned up behind the base flashing upstand (method unresolved)"})
	}
	if base != nil && outer != nil {
		switch {
		case active(tw):
			tr = append(tr, Transition{Kind: "water", From: base.ID, To: outer.ID, Via: []string{tw.ID}, Required: true, Note: "cavity tray drip laps the base flashing upstand"})
		case active(counter):
			via := []string{counter.ID}
			if active(sealant) {
				via = append(via, sealant.ID)
			}
			tr = append(tr, Transition{Kind: "water", From: base.ID, To: outer.ID, Via: via, Required: true, Note: "counterflashing laps the upstand; wall attachment depends on wall type"})
		default:
			tr = append(tr, Transition{Kind: "water", From: base.ID, To: outer.ID, Via: []string{}, Required: true, Note: "no counterflashing or tray covers the upstand"})
		}
	}
	if avcl != nil && inner != nil {
		tr = append(tr, Transition{Kind: "air", From: avcl.ID, To: inner.ID, Via: []string{}, Required: true, Note: "air-control continuity from roof to wall is unresolved"})
	}
	if tr == nil {
		tr = []Transition{}
	}
	j.Transitions = tr
}

// SetWallCondition records the wall type with its provenance. A cavity adds
// (or re-enables) the cavity space with a caller-supplied id; its width may
// stay unknown, drawn with a labelled placeholder.
type SetWallCondition struct {
	Op              string            `json:"op"`
	Value           string            `json:"value"`
	Provenance      string            `json:"provenance"`
	Note            string            `json:"note,omitempty"`
	CavityWidth     *float64          `json:"cavityWidth,omitempty"`
	CavityUnit      string            `json:"cavityUnit,omitempty"`
	NewComponentIDs map[string]string `json:"newComponentIds,omitempty"`
}

func (o *SetWallCondition) Name() string { return "SetWallCondition" }
func (o *SetWallCondition) Check() []string {
	var out []string
	if !wallConditions[o.Value] {
		out = append(out, "value must be unknown, solid-bonded, cavity or other")
	}
	if !provenances[o.Provenance] {
		out = append(out, "provenance is not in the taxonomy")
	}
	if o.Value == "unknown" && o.Provenance != ProvUnknown {
		out = append(out, "an unknown wall has provenance unknown")
	}
	if o.CavityWidth != nil && (o.Value != "cavity" || !finite(*o.CavityWidth)) {
		out = append(out, "cavityWidth applies only to a cavity wall and must be finite")
	}
	out = append(out, checkText("note", o.Note, 500, false)...)
	return out
}
func (o *SetWallCondition) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	before := a.Junction.WallCondition
	cv := a.firstOf(TypeCavitySpace)
	if o.Value == "cavity" {
		width := Quantity{Value: nil, Unit: "mm", State: StateUnknown, Provenance: ProvUnknown, Placeholder: fp(50), Note: "cavity width unknown: placeholder for geometry only"}
		if o.CavityWidth != nil {
			mm, err := ToMM(*o.CavityWidth, orDefault(o.CavityUnit, "mm"))
			if err != nil {
				return err
			}
			mm = round3(mm)
			width = Quantity{Value: &mm, Unit: "mm", State: StateAssumed, Provenance: orDefault(o.Provenance, ProvUserAssumption)}
		}
		if cv == nil {
			id := o.NewComponentIDs["cavity"]
			if id == "" {
				return Invalid("a cavity wall needs a cavity-space component: supply newComponentIds.cavity")
			}
			if err := newCompID(a, id); err != nil {
				return err
			}
			outer := a.byRole("masonry:outer")
			a.Components = append(a.Components, Component{ID: id, Type: TypeCavitySpace, Role: "masonry:cavity", Name: "Masonry cavity (air space)",
				Shape: Shape{Kind: "cavity", Params: map[string]Quantity{"width": width}}, Transform: IdentityTransform(), HostID: idOf(outer),
				Material: &PinRef{ID: genericMaterialID("cavity-air"), Revision: 1}, Appearance: "cavity-air", Attachments: []string{}, Applicability: "applicable"})
		} else {
			cv.Applicability = "applicable"
			if o.CavityWidth != nil {
				cv.Shape.Params["width"] = width
			}
		}
	} else if cv != nil {
		cv.Applicability = "inapplicable"
	}
	a.Junction.WallCondition = WallCondition{Value: o.Value, Provenance: o.Provenance, Note: o.Note}
	refreshJunction(a)
	tx.Record(o.Name(), a.Junction.ID+".wallCondition", before, a.Junction.WallCondition)
	tx.Summary("wall condition " + o.Value)
	return nil
}

func fp(v float64) *float64 { return &v }

// SetAssumption adds or updates a keyed assumption.
type SetAssumption struct {
	Op         string `json:"op"`
	Key        string `json:"key"`
	Text       string `json:"text"`
	Provenance string `json:"provenance"`
	Status     string `json:"status"`
}

func (o *SetAssumption) Name() string { return "SetAssumption" }
func (o *SetAssumption) Check() []string {
	out := checkText("text", o.Text, 1000, true)
	if !scopeIDRE.MatchString(o.Key) {
		out = append(out, "key must be a short identifier")
	}
	if !provenances[o.Provenance] || (o.Status != "open" && o.Status != "confirmed" && o.Status != "withdrawn") {
		out = append(out, "provenance from the taxonomy and status open/confirmed/withdrawn are required")
	}
	return out
}
func (o *SetAssumption) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	next := Assumption{Key: o.Key, Text: strings.TrimSpace(o.Text), Provenance: o.Provenance, Status: o.Status}
	for i, as := range a.Assumptions {
		if as.Key == o.Key {
			tx.Record(o.Name(), "assumption/"+o.Key, as, next)
			a.Assumptions[i] = next
			return nil
		}
	}
	tx.Record(o.Name(), "assumption/"+o.Key, nil, next)
	a.Assumptions = append(a.Assumptions, next)
	return nil
}

// SetAssemblyText renames or re-summarizes an alternative.
type SetAssemblyText struct {
	Op      string  `json:"op"`
	NewName *string `json:"name,omitempty"`
	Summary *string `json:"summary,omitempty"`
}

func (o *SetAssemblyText) Name() string { return "SetAssemblyText" }
func (o *SetAssemblyText) Check() []string {
	var out []string
	if o.NewName == nil && o.Summary == nil {
		out = append(out, "set name and/or summary")
	}
	if o.NewName != nil {
		out = append(out, checkText("name", *o.NewName, MaxTitle, true)...)
	}
	if o.Summary != nil {
		out = append(out, checkText("summary", *o.Summary, MaxText, false)...)
	}
	return out
}
func (o *SetAssemblyText) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	if o.NewName != nil {
		tx.Record("SetAssemblyText", a.ID+".name", a.Name, *o.NewName)
		a.Name = strings.TrimSpace(*o.NewName)
	}
	if o.Summary != nil {
		tx.Record("SetAssemblyText", a.ID+".summary", a.Summary, *o.Summary)
		a.Summary = *o.Summary
	}
	return nil
}

// SetAssemblyLifecycle moves an alternative between draft, proposed and
// superseded. owner-selected comes only from approving a decision.
type SetAssemblyLifecycle struct {
	Op        string `json:"op"`
	Lifecycle string `json:"lifecycle"`
}

func (o *SetAssemblyLifecycle) Name() string { return "SetAssemblyLifecycle" }
func (o *SetAssemblyLifecycle) Check() []string {
	if o.Lifecycle != AssemblyDraft && o.Lifecycle != AssemblyProposed && o.Lifecycle != AssemblySuperseded {
		return []string{"lifecycle must be draft, proposed or superseded (owner-selected comes only from an approved decision)"}
	}
	return nil
}
func (o *SetAssemblyLifecycle) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	if tx.Actor.Kind == ActorAgent && o.Lifecycle == AssemblySuperseded {
		return Forbidden("an agent cannot supersede an alternative")
	}
	tx.Record(o.Name(), a.ID+".lifecycle", a.Lifecycle, o.Lifecycle)
	a.Lifecycle = o.Lifecycle
	return nil
}

// ---- add / remove ----------------------------------------------------------------------

// AddComponent adds one controlled component with a caller-chosen new id.
type AddComponent struct {
	Op        string    `json:"op"`
	Component Component `json:"component"`
	After     string    `json:"after,omitempty"`
}

func (o *AddComponent) Name() string { return "AddComponent" }
func (o *AddComponent) Check() []string {
	var out []string
	if !ValidID(KindComponent, o.Component.ID) {
		out = append(out, "component.id must be a new cmp- id")
	}
	if typeShapes[o.Component.Type] == "" {
		out = append(out, "component.type is not in the controlled vocabulary")
	}
	return out
}
func (o *AddComponent) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	if err := newCompID(a, o.Component.ID); err != nil {
		return err
	}
	nc := o.Component
	if nc.Transform == (Transform{}) {
		nc.Transform = IdentityTransform()
	}
	if nc.Attachments == nil {
		nc.Attachments = []string{}
	}
	if nc.Applicability == "" {
		nc.Applicability = "applicable"
	}
	if nc.Shape.Kind == "" {
		nc.Shape.Kind = typeShapes[nc.Type]
	}
	idx := len(a.Components)
	if o.After != "" {
		if _, i := a.component(o.After); i >= 0 {
			idx = i + 1
		} else {
			return NotFound("no component " + o.After)
		}
	}
	a.Components = append(a.Components[:idx], append([]Component{nc}, a.Components[idx:]...)...)
	refreshJunction(a)
	tx.Record(o.Name(), nc.ID, nil, nc)
	return nil
}

// RemoveComponent removes a component. Anything that depends on it (hosted
// parts, attachments, relationships, junction paths, evidence) refuses the
// removal unless dependents is "detach", which unlinks them explicitly. The
// id is tombstoned and never reused.
type RemoveComponent struct {
	Op          string `json:"op"`
	ComponentID string `json:"componentId"`
	Dependents  string `json:"dependents,omitempty"` // refuse (default) | detach
}

func (o *RemoveComponent) Name() string { return "RemoveComponent" }
func (o *RemoveComponent) Check() []string {
	if !ValidID(KindComponent, o.ComponentID) {
		return []string{"componentId must be a cmp- id"}
	}
	if o.Dependents != "" && o.Dependents != "refuse" && o.Dependents != "detach" {
		return []string{"dependents must be refuse or detach"}
	}
	return nil
}
func (o *RemoveComponent) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	cp, idx := a.component(o.ComponentID)
	if cp == nil {
		return NotFound("no component " + o.ComponentID)
	}
	id := cp.ID
	var deps []string
	for _, other := range a.Components {
		if other.ID == id {
			continue
		}
		if other.HostID == id {
			deps = append(deps, other.ID+" (hosted)")
		}
		for _, at := range other.Attachments {
			if at == id {
				deps = append(deps, other.ID+" (attached)")
			}
		}
	}
	for _, r := range a.Relationships {
		if r.From == id || r.To == id {
			deps = append(deps, r.Kind+" relationship")
		}
	}
	for _, p := range a.Junction.AttachmentPath {
		if p == id {
			deps = append(deps, "attachment path")
		}
	}
	for _, l := range a.EvidenceLinks {
		if l.Target == id {
			deps = append(deps, "evidence "+l.EvidenceID)
		}
	}
	if len(deps) > 0 && o.Dependents != "detach" {
		sort.Strings(deps)
		return &Error{Status: 409, Kind: "conflict", Message: "the component has dependents; remove with dependents=detach to unlink them explicitly", Problems: deps}
	}
	for i := range a.Components {
		oc := &a.Components[i]
		if oc.HostID == id {
			oc.HostID = ""
		}
		var keep []string
		for _, at := range oc.Attachments {
			if at != id {
				keep = append(keep, at)
			}
		}
		if keep == nil {
			keep = []string{}
		}
		oc.Attachments = keep
	}
	var rels []Relationship
	for _, r := range a.Relationships {
		if r.From != id && r.To != id {
			rels = append(rels, r)
		}
	}
	if rels == nil {
		rels = []Relationship{}
	}
	a.Relationships = rels
	var path []string
	for _, p := range a.Junction.AttachmentPath {
		if p != id {
			path = append(path, p)
		}
	}
	if path == nil {
		path = []string{}
	}
	a.Junction.AttachmentPath = path
	var links []EvidenceLink
	for _, l := range a.EvidenceLinks {
		if l.Target != id {
			links = append(links, l)
		}
	}
	if links == nil {
		links = []EvidenceLink{}
	}
	a.EvidenceLinks = links
	removed := *cp
	a.Components = append(a.Components[:idx], a.Components[idx+1:]...)
	a.Tombstones = append(a.Tombstones, Tombstone{ID: id, Type: removed.Type, Name: removed.Name, RemovedAt: tx.Now.Format("2006-01-02T15:04:05.999999999Z07:00"), RemovedBy: tx.Actor, Revision: a.RevisionNumber + 1})
	refreshJunction(a)
	tx.Record(o.Name(), id, removed, nil)
	return nil
}

// ---- variants, restore, acknowledgement, working selection ----------------------------------

// CreateVariant copies the target assembly into a new alternative with an
// explicit derivedFrom pin. Unchanged parts keep their ids (resolved with the
// assembly id + revision); acknowledgements and approvals are not copied.
type CreateVariant struct {
	Op            string `json:"op"`
	NewAssemblyID string `json:"newAssemblyId"`
	VariantName   string `json:"name"`
	Summary       string `json:"summary,omitempty"`
}

func (o *CreateVariant) Name() string { return "CreateVariant" }
func (o *CreateVariant) Check() []string {
	out := checkText("name", o.VariantName, MaxTitle, true)
	if !ValidID(KindAssembly, o.NewAssemblyID) {
		out = append(out, "newAssemblyId must be a new asm- id")
	}
	out = append(out, checkText("summary", o.Summary, MaxText, false)...)
	return out
}
func (o *CreateVariant) Apply(tx *Tx, c *ApplyContext) error {
	src, err := asm(tx, c)
	if err != nil {
		return err
	}
	if tx.Next.Assemblies[o.NewAssemblyID] != nil {
		return Invalid("assembly id already exists")
	}
	clone, err := cloneAssembly(src)
	if err != nil {
		return err
	}
	clone.Envelope = Envelope{SchemaVersion: SchemaVersion, Kind: DocAssembly, ID: o.NewAssemblyID}
	clone.Name = strings.TrimSpace(o.VariantName)
	if o.Summary != "" {
		clone.Summary = o.Summary
	}
	clone.DerivedFrom = &VersionRef{ID: src.ID, Revision: tx.Base.Revision("assembly:" + src.ID)}
	clone.IssueStates = map[string]IssueAck{}
	clone.Lifecycle = AssemblyDraft
	clone.Correspondence = map[string]string{}
	tx.Next.Assemblies[clone.ID] = clone
	tx.Next.Problem.Alternatives = append(tx.Next.Problem.Alternatives, clone.ID)
	if tx.Next.Problem.Lifecycle == LifecycleDraft || tx.Next.Problem.Lifecycle == LifecycleInvestigating {
		tx.Next.Problem.Lifecycle = LifecycleAlternatives
	}
	tx.Record("CreateVariant", "assembly:"+clone.ID, nil, map[string]any{"derivedFrom": clone.DerivedFrom, "name": clone.Name})
	tx.Summary("created alternative " + clone.Name)
	return nil
}

func cloneAssembly(a *Assembly) (*Assembly, error) {
	st := newState(Head{})
	b, err := Canonical(a)
	if err != nil {
		return nil, err
	}
	if err := st.decode("assembly:"+a.ID, DocAssembly, b); err != nil {
		return nil, err
	}
	return st.Assemblies[a.ID], nil
}

// RestoreRevision makes an earlier revision of this same assembly the new
// head as a NEW revision (history is never rewound or deleted).
type RestoreRevision struct {
	Op       string `json:"op"`
	Revision string `json:"revision"`
}

func (o *RestoreRevision) Name() string { return "RestoreRevision" }
func (o *RestoreRevision) Check() []string {
	if !ValidToken(o.Revision) {
		return []string{"revision must be a revision token"}
	}
	return nil
}
func (o *RestoreRevision) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	old, err := tx.assemblyAt(a.ID, o.Revision)
	if err != nil {
		return err
	}
	env := a.Envelope
	*a = *old
	a.Envelope = env
	tx.Record(o.Name(), "assembly:"+a.ID, tx.Base.Revision("assembly:"+a.ID), o.Revision)
	tx.Summary("restored revision " + o.Revision[:12])
	return nil
}

// assemblyAt loads an earlier revision of an assembly by walking its own
// parent chain from the current head — a token from elsewhere is refused.
func (tx *Tx) assemblyAt(id, rev string) (*Assembly, error) {
	tok := tx.Base.Revision("assembly:" + id)
	for steps := 0; tok != "" && steps < 100000; steps++ {
		raw, err := tx.store.docBytes(DocRef{Kind: DocAssembly, Revision: tok})
		if err != nil {
			return nil, err
		}
		var a Assembly
		if err := DecodeStrict(raw, DocAssembly, &a); err != nil {
			return nil, err
		}
		if a.ID != id {
			return nil, corrupt("assembly chain left %s", id)
		}
		if tok == rev {
			return &a, nil
		}
		tok = a.ParentRevision
	}
	return nil, NotFound("that revision is not in this assembly's history")
}

// AcknowledgeIssue records the owner's acknowledgement of an issue. It
// never validates the underlying engineering; the issue stays visible.
type AcknowledgeIssue struct {
	Op      string `json:"op"`
	RuleKey string `json:"ruleKey"`
	Target  string `json:"target"`
	Reason  string `json:"reason"`
}

func (o *AcknowledgeIssue) Name() string { return "AcknowledgeIssue" }
func (o *AcknowledgeIssue) Check() []string {
	out := checkText("reason", o.Reason, 500, true)
	if o.RuleKey == "" || o.Target == "" || strings.Contains(o.RuleKey, "|") {
		out = append(out, "ruleKey and target are required")
	}
	return out
}
func (o *AcknowledgeIssue) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	rep := tx.Base.Validation[a.ID]
	found := false
	if rep != nil {
		for _, is := range rep.Issues {
			if is.RuleKey == o.RuleKey && is.Target == o.Target && is.Status != "resolved" {
				found = true
				if is.Severity == SevBlocking {
					return Invalid("blocking findings cannot be acknowledged")
				}
			}
		}
	}
	if !found {
		return NotFound("no open issue with that rule and target")
	}
	ack := IssueAck{Status: "acknowledged", Actor: tx.Actor, Reason: strings.TrimSpace(o.Reason), At: tx.Now.Format("2006-01-02T15:04:05.999999999Z07:00")}
	a.IssueStates[o.RuleKey+"|"+o.Target] = ack
	tx.Record(o.Name(), o.RuleKey+"|"+o.Target, nil, ack)
	return nil
}

// SelectVariant sets the owner's working alternative (a focus, not an
// approval; a project decision is separate).
type SelectVariant struct {
	Op         string `json:"op"`
	AssemblyID string `json:"assemblyId"`
}

func (o *SelectVariant) Name() string { return "SelectVariant" }
func (o *SelectVariant) Check() []string {
	if !ValidID(KindAssembly, o.AssemblyID) {
		return []string{"assemblyId must be an asm- id"}
	}
	return nil
}
func (o *SelectVariant) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	if !contains(p.Alternatives, o.AssemblyID) {
		return NotFound("no such alternative")
	}
	tx.Record(o.Name(), "problem.activeAssembly", p.ActiveAssembly, o.AssemblyID)
	p.ActiveAssembly = o.AssemblyID
	return nil
}
