package construction

// Deterministic validation (§3.5). Two layers:
//
//   - ValidateAssembly: structure (ids, vocabulary, units, ranges, references,
//     host/lap cycles, supported transforms). Any problem refuses the commit.
//   - Evaluate: physical/relationship rules over the compiled geometry and
//     the problem's catalog/evidence. Blocking findings (layer gaps,
//     disconnected parts, reverse laps, unsupported geometry) refuse the
//     commit with the prior assembly intact; every other finding is stored as
//     a ValidationIssue with a stable id across revisions.
//
// Rules cite their key and version and the inputs they read. Geometric
// validity is not physical appropriateness: structural, hygrothermal and
// manufacturer questions stay specialist-review issues whatever the geometry.

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const RuleSet = "construction-rules/1"

func init() {
	preCommitHooks = append(preCommitHooks, compileAssembliesHook)
	postStampHooks = append(postStampHooks, validationReportsHook)
	docValidators[DocAssembly] = func(st *State, key string, doc any) error {
		a := doc.(*Assembly)
		pid := ""
		if st.Problem != nil {
			pid = st.Problem.ID
		}
		if errs := ValidateAssembly(a, pid, st); len(errs) > 0 {
			return Invalid(errs...)
		}
		return nil
	}
}

// ValidateAssembly checks an assembly's structure against its problem state.
func ValidateAssembly(a *Assembly, problemID string, st *State) []string {
	out := checkEnvelope(a.Envelope, DocAssembly, KindAssembly)
	if a.ProblemID != problemID {
		out = append(out, "problemId must be the owning problem")
	}
	out = append(out, checkText("name", a.Name, MaxTitle, true)...)
	out = append(out, checkText("summary", a.Summary, MaxText, false)...)
	if a.Units != "mm" || a.Convention != CoordinateConvention {
		out = append(out, "units must be mm in the "+CoordinateConvention+" convention")
	}
	if !templates[a.Template] {
		out = append(out, "template is not recognised")
	}
	if d := a.DerivedFrom; d != nil && (!ValidID(KindAssembly, d.ID) || !ValidToken(d.Revision)) {
		out = append(out, "derivedFrom must pin an assembly id and revision")
	}
	if !assemblyLifecycles[a.Lifecycle] {
		out = append(out, "lifecycle is not recognised")
	}
	if !applicabilityStatuses[a.Applicability.Status] {
		out = append(out, "applicability status is not recognised")
	}
	j := a.Junction
	if !ValidID(KindJunction, j.ID) {
		out = append(out, "junction id must be jct-")
	}
	if !orientations[j.Orientation] {
		out = append(out, "junction orientation must be unresolved, headwall or sidewall")
	}
	if !wallConditions[j.WallCondition.Value] || !provenances[j.WallCondition.Provenance] {
		out = append(out, "wall condition must be unknown, solid-bonded, cavity or other with a provenance")
	}
	if j.WallCondition.Value == "unknown" && j.WallCondition.Provenance != ProvUnknown {
		out = append(out, "an unknown wall condition has provenance unknown")
	}
	if s, ok := strategies[j.Strategy]; !ok {
		out = append(out, "junction strategy is not recognised")
	} else {
		if j.Orientation == "unresolved" && j.Strategy != "unresolved" {
			out = append(out, "a strategy needs a resolved headwall/sidewall orientation")
		}
		if s.Orientation != "" && s.Orientation != j.Orientation {
			out = append(out, "strategy "+j.Strategy+" belongs to a "+s.Orientation+" junction")
		}
	}
	for name, spec := range assemblyParams {
		q, ok := a.Parameters[name]
		if !ok {
			out = append(out, "parameter "+name+" is required")
			continue
		}
		out = append(out, checkQuantity("parameters."+name, q, spec)...)
	}
	for name := range a.Parameters {
		if _, ok := assemblyParams[name]; !ok {
			out = append(out, "parameter "+name+" is not allowlisted")
		}
	}
	if len(a.Components) > MaxComponents {
		out = append(out, fmt.Sprintf("at most %d components", MaxComponents))
	}
	ids := map[string]*Component{}
	tomb := map[string]bool{}
	for _, t := range a.Tombstones {
		tomb[t.ID] = true
	}
	orders := map[int]bool{}
	lowest := math.MaxInt
	deckOrder := -1
	for i := range a.Components {
		c := &a.Components[i]
		f := fmt.Sprintf("components[%d]", i)
		if !ValidID(KindComponent, c.ID) {
			out = append(out, f+": id must be cmp-")
		}
		if ids[c.ID] != nil {
			out = append(out, f+": duplicate id "+c.ID)
		}
		if tomb[c.ID] {
			out = append(out, f+": id "+c.ID+" belongs to a removed component and is never reused")
		}
		ids[c.ID] = c
		shape, ok := typeShapes[c.Type]
		if !ok {
			out = append(out, f+": type "+c.Type+" is not in the controlled vocabulary")
			continue
		}
		if c.Shape.Kind != shape {
			out = append(out, f+": a "+c.Type+" uses shape "+shape)
		}
		out = append(out, checkText(f+".name", c.Name, 200, true)...)
		out = append(out, checkText(f+".role", c.Role, 120, true)...)
		out = append(out, checkText(f+".notes", c.Notes, 2000, false)...)
		specs := shapeParams[shape]
		for name, spec := range specs {
			q, ok := c.Shape.Params[name]
			if !ok {
				out = append(out, f+": shape parameter "+name+" is required")
				continue
			}
			out = append(out, checkQuantity(f+"."+name, q, spec)...)
		}
		for name := range c.Shape.Params {
			if _, ok := specs[name]; !ok {
				out = append(out, f+": shape parameter "+name+" is not allowlisted")
			}
		}
		if c.Layer != nil {
			if !layerTypes[c.Type] {
				out = append(out, f+": a "+c.Type+" is not a roof-stack layer")
			}
			if orders[c.Layer.Order] {
				out = append(out, f+": duplicate layer order")
			}
			orders[c.Layer.Order] = true
			if c.Layer.Order < lowest {
				lowest = c.Layer.Order
			}
			if c.Type == TypeDecking {
				deckOrder = c.Layer.Order
			}
		} else if layerTypes[c.Type] {
			out = append(out, f+": a "+c.Type+" needs a layer position")
		}
		if c.Applicability != "applicable" && c.Applicability != "conditional" && c.Applicability != "inapplicable" {
			out = append(out, f+": applicability must be applicable, conditional or inapplicable")
		}
		out = append(out, checkTransform(f, c, a)...)
		if c.Material != nil {
			if _, ok := st.Catalog.material(c.Material.ID, c.Material.Revision); !ok {
				out = append(out, f+": material "+c.Material.ID+" revision is not in the problem catalog")
			}
		}
		if c.Product != nil {
			if _, ok := st.Catalog.product(c.Product.ID, c.Product.Revision); !ok {
				out = append(out, f+": product "+c.Product.ID+" revision is not in the problem catalog")
			}
		}
	}
	if deckOrder >= 0 && deckOrder != lowest {
		out = append(out, "the exposed timber deck must remain the bottom of the roof stack")
	}
	ref := func(field, id string) {
		if id != "" && ids[id] == nil && id != a.ID && id != j.ID {
			out = append(out, field+": unknown component "+id)
		}
	}
	for i := range a.Components {
		c := &a.Components[i]
		if c.HostID == c.ID && c.HostID != "" {
			out = append(out, c.ID+": a component cannot host itself")
		}
		ref(c.ID+".hostId", c.HostID)
		for _, at := range c.Attachments {
			ref(c.ID+".attachments", at)
		}
	}
	// host chains: bounded, acyclic
	for id := range ids {
		seen := map[string]bool{}
		cur := id
		for depth := 0; cur != ""; depth++ {
			if seen[cur] || depth > MaxDependencyDepth {
				out = append(out, "host chain from "+id+" has a cycle or exceeds the depth budget")
				break
			}
			seen[cur] = true
			if c := ids[cur]; c != nil {
				cur = c.HostID
			} else {
				cur = ""
			}
		}
	}
	laps := map[string][]string{}
	for _, r := range a.Relationships {
		if !relationshipKinds[r.Kind] {
			out = append(out, "relationship kind "+r.Kind+" is not recognised")
		}
		ref("relationships.from", r.From)
		ref("relationships.to", r.To)
		if r.Kind == "laps-over" {
			laps[r.From] = append(laps[r.From], r.To)
		}
	}
	if cyc := lapCycle(laps); cyc != "" {
		out = append(out, "laps-over relationships form a cycle through "+cyc)
	}
	for _, id := range j.Components {
		ref("junction.components", id)
	}
	for _, id := range j.AttachmentPath {
		ref("junction.attachmentPath", id)
	}
	for _, t := range j.Transitions {
		if t.Kind != "air" && t.Kind != "water" && t.Kind != "vapor" {
			out = append(out, "transition kind must be air, water or vapor")
		}
		ref("transition.from", t.From)
		ref("transition.to", t.To)
		for _, v := range t.Via {
			ref("transition.via", v)
		}
	}
	evidenceIDs := map[string]bool{}
	if st.Evidence != nil {
		for _, e := range st.Evidence.Evidence {
			evidenceIDs[e.ID] = true
		}
	}
	for _, l := range a.EvidenceLinks {
		if !evidenceRelations[l.Relation] {
			out = append(out, "evidence relation must be supports, contradicts or informs")
		}
		if !evidenceIDs[l.EvidenceID] {
			out = append(out, "evidence link names unknown evidence "+l.EvidenceID)
		}
		if !strings.HasPrefix(l.Target, "param:") {
			ref("evidenceLinks.target", l.Target)
		}
	}
	for k, ack := range a.IssueStates {
		if !strings.Contains(k, "|") || ack.Status != "acknowledged" {
			out = append(out, "issue state "+k+" must be an acknowledgement keyed rule|target")
		}
	}
	for _, as := range a.Assumptions {
		out = append(out, checkText("assumption", as.Text, 1000, true)...)
		if !provenances[as.Provenance] || (as.Status != "open" && as.Status != "confirmed" && as.Status != "withdrawn") {
			out = append(out, "assumption "+as.Key+" needs a provenance and status open/confirmed/withdrawn")
		}
	}
	for to, from := range a.Correspondence {
		if !ValidID(KindComponent, to) || !ValidID(KindComponent, from) {
			out = append(out, "correspondence maps component ids")
		}
	}
	return out
}

func lapCycle(g map[string][]string) string {
	state := map[string]int{}
	var bad string
	var visit func(n string) bool
	visit = func(n string) bool {
		if state[n] == 1 {
			bad = n
			return true
		}
		if state[n] == 2 {
			return false
		}
		state[n] = 1
		for _, m := range g[n] {
			if visit(m) {
				return true
			}
		}
		state[n] = 2
		return false
	}
	keys := make([]string, 0, len(g))
	for k := range g {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if visit(k) {
			return bad
		}
	}
	return ""
}

// supported transforms: junction parts move freely within ±500 mm; roof-stack
// layers only along the roof normal; everything else (arrays, masonry) not
// at all. Rotation, scale, shear and mirroring do not exist to set.
var movableTypes = map[string]bool{TypeApronFlashing: true, TypeSidewallFlash: true, TypeCounterflashing: true,
	TypeThroughWall: true, TypeProfileClosure: true, TypeSealant: true}

func checkTransform(f string, c *Component, a *Assembly) []string {
	t := c.Transform
	if t.Rotation != [4]float64{0, 0, 0, 1} {
		return []string{f + ": rotation is not a supported positioning; only translation is"}
	}
	for _, v := range t.Translation {
		if !finite(v) || math.Abs(v) > 500 {
			return []string{f + ": translation must be finite and within ±500 mm"}
		}
	}
	if t.Translation == [3]float64{} {
		return nil
	}
	if movableTypes[c.Type] {
		return nil
	}
	if layerTypes[c.Type] {
		n := roofNormal(a)
		tv := V3{t.Translation[0], t.Translation[1], t.Translation[2]}
		along := vdot(tv, n)
		if vlen(vsub(tv, vmul(n, along))) > 0.01 {
			return []string{f + ": a roof-stack layer moves only along the roof normal"}
		}
		return nil
	}
	return []string{f + ": a " + c.Type + " cannot be repositioned (unsupported)"}
}

func roofNormal(a *Assembly) V3 {
	th := a.aparam("pitch") * math.Pi / 180
	if a.Junction.Orientation == "sidewall" {
		return V3{math.Sin(th), 0, math.Cos(th)}
	}
	return V3{0, math.Sin(th), math.Cos(th)}
}

// ---- rules ---------------------------------------------------------------------------

type ruleInput struct {
	a   *Assembly
	ir  *GeometryIR
	cat *Catalog
	ev  *EvidenceBundle
	p   *Problem
}

type finding struct {
	key, target, severity, category, message, observed, expected string
	comps                                                        []string
	specialist                                                   bool
	inputs                                                       []string
}

// Evaluate runs every rule. Output order is deterministic (key, target).
func Evaluate(in ruleInput) []finding {
	a, ir := in.a, in.ir
	var fs []finding
	add := func(f finding) { fs = append(fs, f) }
	j := a.Junction
	// geometry: blocking
	for _, g := range ir.Facts.LayerGaps {
		if math.Abs(g.Gap) > 0.01 {
			kind := "gap"
			if g.Gap < 0 {
				kind = "overlap"
			}
			add(finding{key: "geometry.layer-continuity", target: g.Above, severity: SevBlocking, category: "geometry",
				message: fmt.Sprintf("layer %s of %.2f mm between stacked layers", kind, math.Abs(g.Gap)), observed: fmt.Sprintf("%.3f mm", g.Gap), expected: "0 ± 0.01 mm",
				comps: []string{g.Below, g.Above}, inputs: []string{"layer thickness", "layer transform"}})
		}
	}
	for _, id := range ir.Facts.Disconnected {
		add(finding{key: "geometry.disconnected", target: id, severity: SevBlocking, category: "geometry",
			message: "part does not touch the rest of the assembly", observed: "isolated bounds", expected: "contact within 0.05 mm", comps: []string{id}, inputs: []string{"geometry"}})
	}
	for _, l := range ir.Facts.Laps {
		if !l.OK {
			add(finding{key: "geometry.reverse-lap", target: l.Over, severity: SevBlocking, category: "moisture",
				message: "reverse lap: " + l.Kind + " runs under the part it must lap over", observed: fmt.Sprintf("separation %.2f mm", l.Clear), expected: "over-lapping part outside/above",
				comps: []string{l.Over, l.Under}, inputs: []string{"transform", "layer stack"}})
		}
	}
	// wall condition and orientation
	if j.WallCondition.Value == "unknown" {
		add(finding{key: "wall.condition.unknown", target: j.ID, severity: SevCritical, category: "masonry",
			message:  "Wall type unknown: solid/bonded vs cavity is unresolved; two wythes do not establish a cavity. Alternatives stay conditional.",
			observed: "unknown", expected: "verified solid-bonded or cavity (inspection/measurement)", specialist: true, inputs: []string{"junction.wallCondition"}})
	}
	if j.WallCondition.Value == "other" {
		add(finding{key: "wall.condition.other", target: j.ID, severity: SevCritical, category: "masonry",
			message: "Wall type 'other' is not modelled; strategies cannot be qualified.", observed: "other", expected: "solid-bonded or cavity", specialist: true, inputs: []string{"junction.wallCondition"}})
	}
	if j.WallCondition.Value != "unknown" && j.WallCondition.Provenance != ProvVerifiedFact {
		add(finding{key: "wall.condition.unverified", target: j.ID, severity: SevCritical, category: "masonry",
			message: "Wall type is " + j.WallCondition.Value + " by " + j.WallCondition.Provenance + ", not a verified fact.", observed: j.WallCondition.Provenance, expected: "verified-fact", specialist: true, inputs: []string{"junction.wallCondition.provenance"}})
	}
	if j.Orientation == "unresolved" {
		add(finding{key: "junction.orientation.unresolved", target: j.ID, severity: SevCritical, category: "geometry",
			message: "Headwall vs sidewall orientation unresolved; the model shows the headwall arrangement only as an illustration.", observed: "unresolved", expected: "headwall or sidewall", inputs: []string{"junction.orientation"}})
	}
	st := strategies[j.Strategy]
	if j.Strategy == "unresolved" {
		add(finding{key: "junction.strategy.unresolved", target: j.ID, severity: SevCritical, category: "moisture",
			message: "No flashing strategy chosen: counterflashing attachment to the wall is unresolved.", observed: "unresolved", expected: "a qualified strategy for the verified wall type", inputs: []string{"junction.strategy"}})
	} else if st.Wall != "" {
		switch {
		case j.WallCondition.Value == st.Wall:
		case j.WallCondition.Value == "unknown":
			add(finding{key: "junction.strategy.conditional", target: j.ID, severity: SevCritical, category: "moisture",
				message: "Strategy " + j.Strategy + " applies only to a " + st.Wall + " wall, which is not established.", observed: "wall unknown", expected: st.Wall, specialist: true, inputs: []string{"junction.strategy", "junction.wallCondition"}})
		default:
			add(finding{key: "junction.strategy.inapplicable", target: j.ID, severity: SevCritical, category: "moisture",
				message: "Strategy " + j.Strategy + " is inapplicable to a " + j.WallCondition.Value + " wall.", observed: j.WallCondition.Value, expected: st.Wall, inputs: []string{"junction.strategy", "junction.wallCondition"}})
		}
	}
	if strings.Contains(j.Strategy, "reglet") {
		add(finding{key: "masonry.reglet.cutting", target: j.ID, severity: SevCritical, category: "masonry",
			message:  "A reglet means cutting masonry: masonry condition, permissible cutting, profile and the manufacturer's detail need review. Nothing here approves cutting.",
			observed: "reglet strategy", expected: "masonry assessment + manufacturer detail", specialist: true, inputs: []string{"junction.strategy"}})
	}
	if j.WallCondition.Value == "cavity" {
		cv := a.firstOf(TypeCavitySpace)
		if j.Strategy != "apron-through-wall-flashing" {
			add(finding{key: "masonry.cavity.drainage", target: j.ID, severity: SevCritical, category: "moisture",
				message:  "Cavity wall: masonry cavity drainage is not addressed. Roof flashing does not replace a through-wall cavity tray with weeps and end dams.",
				observed: j.Strategy, expected: "apron-through-wall-flashing (or a sourced equivalent)", specialist: true, inputs: []string{"junction.strategy", "junction.wallCondition"}})
		} else {
			for _, need := range []struct{ typ, what string }{{TypeThroughWall, "through-wall flashing"}, {TypeWeepSet, "weeps"}, {TypeEndDams, "end dams"}} {
				if c := a.firstOf(need.typ); c == nil || c.Applicability == "inapplicable" {
					add(finding{key: "masonry.cavity.component-missing", target: j.ID + ":" + need.typ, severity: SevCritical, category: "moisture",
						message: "Cavity drainage needs " + need.what + ".", observed: "missing", expected: need.what, inputs: []string{"components"}})
				}
			}
		}
		if cv == nil {
			add(finding{key: "masonry.cavity.space-missing", target: j.ID, severity: SevCritical, category: "masonry",
				message: "Cavity wall without a modelled cavity space.", observed: "no cavity-space component", expected: "cavity-space", inputs: []string{"components"}})
		} else if q := cv.Shape.Params["width"]; q.Value == nil {
			add(finding{key: "masonry.cavity.width-unknown", target: cv.ID, severity: SevCritical, category: "masonry",
				message: "Cavity width unknown: the drawing uses a placeholder and labels it unresolved.", observed: "unknown", expected: "measured cavity width", comps: []string{cv.ID}, inputs: []string{cv.ID + ".width"}})
		}
	}
	// laps
	for _, l := range ir.Facts.Laps {
		min := map[string]float64{"apron-over-sheet": 75, "counter-over-upstand": 25, "drip-over-upstand": 25}[l.Kind]
		if l.OK && l.Lap < min {
			add(finding{key: "flashing.lap." + l.Kind, target: l.Over, severity: SevCritical, category: "moisture",
				message:  fmt.Sprintf("%s lap %.1f mm is below the illustrative %g mm check; confirm against the manufacturer detail.", l.Kind, l.Lap, min),
				observed: fmt.Sprintf("%.1f mm", l.Lap), expected: fmt.Sprintf("≥ %g mm (illustrative, unsourced)", min), comps: []string{l.Over, l.Under}, inputs: []string{"lap geometry"}})
		}
	}
	// attachment and structure
	fastenerIDs := []string{}
	for _, ff := range ir.Facts.Fasteners {
		fastenerIDs = append(fastenerIDs, ff.Component)
		if ff.Host == "" || ff.Count == 0 {
			add(finding{key: "attachment.host-missing", target: ff.Component, severity: SevCritical, category: "structure",
				message: "Fastener set has no resolvable structural host; the attachment path is incomplete.", observed: "no host placement", expected: "named support", comps: []string{ff.Component}, inputs: []string{"hostId"}})
			continue
		}
		if !ff.ReachesHost || ff.Embedment < ff.MinEmbedment {
			add(finding{key: "structure.fastener.embedment", target: ff.Component, severity: SevCritical, category: "structure",
				message:  fmt.Sprintf("Fastener embedment %.1f mm into its host is below the illustrative %.0f mm check (reaches host: %v).", ff.Embedment, ff.MinEmbedment, ff.ReachesHost),
				observed: fmt.Sprintf("%.1f mm", ff.Embedment), expected: fmt.Sprintf("≥ %.0f mm and a qualified design", ff.MinEmbedment), comps: []string{ff.Component, ff.Host}, specialist: true, inputs: []string{"length", "layer stack"}})
		}
		if ff.StackBased && math.Abs(ff.DesignStack-ff.CurrentStack) > 0.5 {
			add(finding{key: "structure.fastener.stack-changed", target: ff.Component, severity: SevCritical, category: "structure",
				message:  fmt.Sprintf("The layer stack changed from %.0f to %.0f mm since these fasteners were specified: review length, embedment, support and thermal bridging. Adequacy is not assumed unchanged.", ff.DesignStack, ff.CurrentStack),
				observed: fmt.Sprintf("%.0f mm", ff.CurrentStack), expected: fmt.Sprintf("%.0f mm (as specified)", ff.DesignStack), comps: []string{ff.Component}, specialist: true, inputs: []string{"designStack", "layer thickness"}})
		}
		if ff.StackBased {
			want := []string{}
			for _, l := range a.layers() {
				if l.Type != TypeCorrugatedSheet && l.Applicability != "inapplicable" {
					want = append(want, l.ID)
				}
			}
			passes := map[string]bool{}
			for _, p := range ff.Passes {
				passes[p] = true
			}
			for _, w := range want {
				if !passes[w] {
					add(finding{key: "attachment.path.incomplete", target: ff.Component + ":" + w, severity: SevCritical, category: "structure",
						message: "The structural fastener axis does not pass through every modelled layer to its host.", observed: "misses " + w, expected: "batten → layers → rafter", comps: []string{ff.Component, w}, inputs: []string{"fastener axis"}})
				}
			}
			add(finding{key: "thermal.bridging", target: ff.Component, severity: SevAdvisory, category: "thermal",
				message: fmt.Sprintf("%d fasteners bridge the insulation; their thermal effect is not quantified.", ff.Count), observed: "penetrations", expected: "quantified or mitigated", comps: []string{ff.Component}, inputs: []string{"fastener count"}})
		}
		if !ff.StackBased && ff.Embedment > 0 {
			if host, _ := a.component(ff.Host); host != nil && ff.Embedment > host.param("depth")+0.01 {
				add(finding{key: "attachment.penetrates-underlayment", target: ff.Component, severity: SevAdvisory, category: "moisture",
					message: "Sheet fasteners run past the batten into the underlayment.", observed: fmt.Sprintf("%.1f mm into a %.0f mm batten", ff.Embedment, host.param("depth")), expected: "within the batten", comps: []string{ff.Component}, inputs: []string{"length"}})
			}
		}
		if fs, _ := a.component(ff.Component); fs != nil && fs.Product == nil {
			add(finding{key: "fastener.specification", target: ff.Component, severity: SevCritical, category: "structure",
				message: "Fastener type, coating and capacity are unspecified (no sourced product).", observed: "generic", expected: "sourced fastener product + qualified design", comps: []string{ff.Component}, specialist: true, inputs: []string{"product"}})
		}
	}
	add(finding{key: "structure.specification", target: a.ID, severity: SevCritical, category: "structure",
		message:  "Unverified structural specification — qualified review required: fastener spacing, pull-out, uplift, load path, rafter bearing at the masonry and structural capacity. Geometry cannot clear these.",
		observed: "no structural design", expected: "qualified structural review", comps: fastenerIDs, specialist: true, inputs: []string{"fasteners", "rafters"}})
	// moisture transitions
	for i, t := range j.Transitions {
		if !t.Required {
			continue
		}
		missing := []string{}
		for _, v := range append([]string{t.From, t.To}, t.Via...) {
			if c, _ := a.component(v); c == nil || c.Applicability == "inapplicable" {
				missing = append(missing, v)
			}
		}
		if len(missing) > 0 || len(t.Via) == 0 {
			add(finding{key: "moisture.transition." + t.Kind, target: fmt.Sprintf("%s:%d", j.ID, i), severity: SevCritical, category: "moisture",
				message: "Required " + t.Kind + " transition is not made: " + t.Note, observed: strings.Join(missing, ","), expected: "continuous " + t.Kind + " control", inputs: []string{"junction.transitions"}})
		}
	}
	climate := "unknown"
	if in.p != nil {
		climate = in.p.Climate.State
	}
	if avcl := a.firstOf(TypeControlLayer); avcl != nil {
		sev := SevCritical
		if climate == StateKnown {
			sev = SevAdvisory
		}
		add(finding{key: "moisture.vapour-control", target: avcl.ID, severity: sev, category: "moisture",
			message:  "Air/vapour-control placement, condensation and drying depend on climate (" + climate + "); no hygrothermal calculation has been performed. Do not place a vapour barrier from a generic climate guess.",
			observed: "climate " + climate, expected: "climate-specific hygrothermal review", comps: []string{avcl.ID}, specialist: true, inputs: []string{"problem.climate"}})
	}
	for _, typ := range []string{TypeDecking, TypeRafterArray} {
		if c := a.firstOf(typ); c == nil || c.Applicability == "inapplicable" {
			add(finding{key: "timber.exposure", target: a.ID + ":" + typ, severity: SevCritical, category: "timber",
				message: "The exposed rafters and finish-grade deck underside must remain in every viable alternative.", observed: typ + " missing", expected: typ + " present", inputs: []string{"components"}})
		}
	}
	add(finding{key: "timber.protection", target: a.ID, severity: SevAdvisory, category: "timber",
		message: "Exposed timber protection (finish, moisture at the bearing, fire) is not assessed.", observed: "not assessed", expected: "specialist review", specialist: true, inputs: []string{}})
	// products, materials, sources
	sheet := a.firstOf(TypeCorrugatedSheet)
	for _, c := range []*Component{sheet, a.firstOf(TypeApronFlashing), a.firstOf(TypeSidewallFlash), a.firstOf(TypeCounterflashing), a.firstOf(TypeThroughWall)} {
		if c == nil || c.Applicability == "inapplicable" {
			continue
		}
		if c.Product == nil {
			add(finding{key: "product.manufacturer-reference", target: c.ID, severity: SevAdvisory, category: "sourcing",
				message: c.Name + ": no manufacturer product selected; the manufacturer's detail is required before specification.", observed: "generic material", expected: "sourced product + detail", comps: []string{c.ID}, inputs: []string{"product"}})
		} else if p, ok := in.cat.product(c.Product.ID, c.Product.Revision); ok {
			verified := false
			for _, fct := range p.Facts {
				verified = verified || (fct.Verified && fct.EvidenceID != "")
			}
			if !verified || p.Fictional {
				add(finding{key: "product.evidence-missing", target: c.ID, severity: SevCritical, category: "sourcing",
					message:  c.Name + ": product " + p.Manufacturer + " " + p.Model + " has no verified, evidence-backed fact" + map[bool]string{true: " (fictional fixture product)", false: ""}[p.Fictional] + ".",
					observed: "unverified product", expected: "checked manufacturer document", comps: []string{c.ID}, inputs: []string{"product facts"}})
			}
			if p.Lifecycle == "stale" || p.Lifecycle == "withdrawn" {
				add(finding{key: "product.stale", target: c.ID, severity: SevCritical, category: "sourcing",
					message: "Product " + p.Model + " is " + p.Lifecycle + "; re-check the source.", observed: p.Lifecycle, expected: "active", comps: []string{c.ID}, inputs: []string{"product lifecycle"}})
			}
		}
	}
	if fl := firstActive(a, TypeApronFlashing, TypeSidewallFlash); fl != nil && sheet != nil {
		add(finding{key: "material.compatibility", target: fl.ID + ":" + sheet.ID, severity: SevAdvisory, category: "materials",
			message: "Galvanic/chemical compatibility of the flashing and roof-sheet metals is unknown until both are sourced.", observed: "unknown", expected: "documented compatibility", comps: []string{fl.ID, sheet.ID}, inputs: []string{"materials"}})
	}
	if sheet != nil {
		add(finding{key: "roof.minimum-pitch", target: sheet.ID, severity: SevAdvisory, category: "roofing",
			message:  fmt.Sprintf("The corrugated profile's minimum pitch and end/side lap rules are manufacturer-specific; %.1f° is unverified.", a.aparam("pitch")),
			observed: fmt.Sprintf("%.1f°", a.aparam("pitch")), expected: "manufacturer minimum", comps: []string{sheet.ID}, inputs: []string{"pitch"}})
	}
	linked := map[string]bool{}
	for _, l := range a.EvidenceLinks {
		if l.Relation == "supports" {
			linked[l.Target] = true
		}
	}
	if !linked[j.ID] && !linked[a.ID] {
		add(finding{key: "source.completeness", target: a.ID, severity: SevAdvisory, category: "sourcing",
			message: "No evidence supports this assembly's junction strategy yet.", observed: "0 supporting evidence", expected: "sourced detail / guidance", inputs: []string{"evidenceLinks"}})
	}
	illus := 0
	for _, l := range ir.Labels {
		if l == "illustrative" {
			illus++
		}
	}
	if illus > 0 {
		add(finding{key: "dimensions.illustrative", target: a.ID, severity: SevInfo, category: "geometry",
			message: fmt.Sprintf("%d dimensions are illustrative test-only assumptions; drawings label them.", illus), observed: fmt.Sprint(illus), expected: "sourced or measured", inputs: []string{"parameters"}})
	}
	for _, c := range a.Components {
		if c.Applicability == "inapplicable" {
			add(finding{key: "geometry.part-inapplicable", target: c.ID, severity: SevInfo, category: "geometry",
				message: c.Name + " is switched off for this orientation/strategy/wall condition (kept for comparison, not drawn).", observed: "inapplicable", expected: "", comps: []string{c.ID}, inputs: []string{"applicability"}})
		}
	}
	for _, id := range ir.Facts.Inapplicable {
		add(finding{key: "geometry.part-not-used", target: id, severity: SevInfo, category: "geometry",
			message: "This part has no geometry in the current orientation/condition.", observed: "not placed", expected: "", comps: []string{id}, inputs: []string{"orientation"}})
	}
	add(finding{key: "geometry.compiled", target: a.ID, severity: SevInfo, category: "geometry",
		message: fmt.Sprintf("Compiled %d triangles; corrugation/bend chord error %.3f mm.", ir.Triangles, ir.ChordErrorMM), observed: ir.Hash[:16], expected: "", inputs: []string{"geometry"}})
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].key != fs[j].key {
			return fs[i].key < fs[j].key
		}
		return fs[i].target < fs[j].target
	})
	return fs
}

func firstActive(a *Assembly, types ...string) *Component {
	for _, t := range types {
		if c := a.firstOf(t); c != nil && c.Applicability != "inapplicable" {
			return c
		}
	}
	return nil
}

// deriveApplicability states whether the alternative applies under the
// currently known conditions. It never upgrades an unverified wall.
func deriveApplicability(a *Assembly) Applicability {
	j := a.Junction
	ap := Applicability{Status: "unknown", Conditions: []string{}, Reasons: []string{}}
	st := strategies[j.Strategy]
	switch {
	case j.Strategy == "unresolved" || j.Orientation == "unresolved":
		ap.Reasons = append(ap.Reasons, "orientation or strategy unresolved")
		ap.Conditions = append(ap.Conditions, "orientation", "strategy")
	case st.Wall == "":
		ap.Status = "conditional"
		ap.Conditions = append(ap.Conditions, "manufacturer detail", "wall attachment review")
		ap.Reasons = append(ap.Reasons, st.Note)
	case j.WallCondition.Value == st.Wall:
		ap.Status = "conditional"
		ap.Conditions = append(ap.Conditions, st.Wall+" wall ("+j.WallCondition.Provenance+")", "manufacturer detail", "specialist review")
		ap.Reasons = append(ap.Reasons, st.Note)
		if j.WallCondition.Provenance == ProvVerifiedFact {
			ap.Status = "applicable"
		}
	case j.WallCondition.Value == "unknown":
		ap.Status = "conditional"
		ap.Conditions = append(ap.Conditions, "requires a "+st.Wall+" wall")
		ap.Reasons = append(ap.Reasons, "wall type unknown")
	default:
		ap.Status = "inapplicable"
		ap.Reasons = append(ap.Reasons, j.Strategy+" needs a "+st.Wall+" wall; the wall is "+j.WallCondition.Value)
	}
	return ap
}

// ---- commit hooks ----------------------------------------------------------------------

// compileAssembliesHook compiles every assembly the commit changed (or whose
// catalog/evidence/problem context changed). A compile error or a blocking
// finding refuses the whole commit.
func compileAssembliesHook(tx *Tx) error {
	if tx.viewOnly {
		return nil
	}
	ctxChanged := !sameDoc(tx.Base.Catalog, tx.Next.Catalog) || !sameDoc(tx.Base.Evidence, tx.Next.Evidence) || !sameDoc(tx.Base.Problem, tx.Next.Problem)
	ids := make([]string, 0, len(tx.Next.Assemblies))
	for id := range tx.Next.Assemblies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tx.compiled = map[string]compiledAssembly{}
	for _, id := range ids {
		a := tx.Next.Assemblies[id]
		prev := tx.Base.Assemblies[id]
		if prev != nil && !ctxChanged && sameDoc(prev, a) && tx.Base.Validation[id] != nil {
			continue
		}
		ir, err := Compile(a, tx.Next.Catalog)
		if err != nil {
			if ce, ok := err.(*CompileError); ok {
				return &Error{Status: 422, Kind: "invalid", Message: "unsupported geometry — the assembly was not changed", Problems: ce.Problems}
			}
			return err
		}
		fs := Evaluate(ruleInput{a: a, ir: ir, cat: tx.Next.Catalog, ev: tx.Next.Evidence, p: tx.Next.Problem})
		var blocking []string
		for _, f := range fs {
			if f.severity == SevBlocking {
				blocking = append(blocking, f.key+" ("+f.target+"): "+f.message)
			}
		}
		if len(blocking) > 0 {
			return &Error{Status: 422, Kind: "invalid", Message: "blocking validation — the assembly was not changed", Problems: blocking}
		}
		a.ModelHash = ir.Hash
		a.CompilerVersion = CompilerVersion
		a.Applicability = deriveApplicability(a)
		tx.compiled[id] = compiledAssembly{ir: ir, findings: fs}
	}
	return nil
}

type compiledAssembly struct {
	ir       *GeometryIR
	findings []finding
}

// validationReportsHook writes each recompiled assembly's report, keyed to
// the assembly's stamped revision token, reusing issue ids by rule+target.
func validationReportsHook(tx *Tx, tokens map[string]string) error {
	ids := make([]string, 0, len(tx.compiled))
	for id := range tx.compiled {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ca := tx.compiled[id]
		a := tx.Next.Assemblies[id]
		prev := tx.Base.Validation[id]
		tx.Next.Validation[id] = buildReport(a, tokens["assembly:"+id], prev, ca, tx.Actor, tx.Now)
	}
	return nil
}

func buildReport(a *Assembly, asmToken string, prev *ValidationReport, ca compiledAssembly, actor Actor, now time.Time) *ValidationReport {
	prevIDs := map[string]Issue{}
	if prev != nil {
		for _, is := range prev.Issues {
			if is.Status != "resolved" {
				prevIDs[is.RuleKey+"|"+is.Target] = is
			}
		}
	}
	r := &ValidationReport{Envelope: Envelope{SchemaVersion: SchemaVersion, Kind: DocValidation, ID: a.ID},
		AssemblyID: a.ID, AssemblyRevision: asmToken, RuleSet: RuleSet, GeometryHash: ca.ir.Hash, Issues: []Issue{}}
	seen := map[string]bool{}
	for _, f := range ca.findings {
		key := f.key + "|" + f.target
		if seen[key] {
			continue
		}
		seen[key] = true
		is := Issue{RuleKey: f.key, RuleVersion: 1, Target: f.target, Severity: f.severity, Category: f.category,
			ComponentIDs: sortedUnique(f.comps), JunctionIDs: []string{a.Junction.ID}, Observed: f.observed, Expected: f.expected,
			Message: f.message, EvidenceIDs: []string{}, Status: "open", SpecialistReview: f.specialist, Deterministic: true, Inputs: f.inputs}
		if is.Inputs == nil {
			is.Inputs = []string{}
		}
		if p, ok := prevIDs[key]; ok {
			is.ID = p.ID
		} else {
			is.ID = NewID(KindIssue)
		}
		if ack, ok := a.IssueStates[key]; ok {
			is.Status = "acknowledged"
			copy := ack
			is.Resolution = &copy
		}
		for _, l := range a.EvidenceLinks {
			for _, c := range is.ComponentIDs {
				if l.Target == c {
					is.EvidenceIDs = append(is.EvidenceIDs, l.EvidenceID)
				}
			}
		}
		is.EvidenceIDs = sortedUnique(is.EvidenceIDs)
		r.Issues = append(r.Issues, is)
		switch f.severity {
		case SevBlocking:
			r.Counts.Blocking++
		case SevCritical:
			r.Counts.Critical++
		case SevAdvisory:
			r.Counts.Advisory++
		default:
			r.Counts.Informational++
		}
	}
	// issues that stopped firing in this revision are shown once as resolved
	keys := make([]string, 0, len(prevIDs))
	for k := range prevIDs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if seen[k] {
			continue
		}
		is := prevIDs[k]
		is.Status = "resolved"
		is.Resolution = &IssueAck{Status: "resolved", Actor: actor, Reason: "condition no longer detected by rule " + is.RuleKey, At: now.UTC().Format(time.RFC3339Nano)}
		r.Issues = append(r.Issues, is)
	}
	return r
}
