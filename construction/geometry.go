package construction

// Deterministic, renderer-neutral geometry compilation (§3.3). The canonical
// assembly — typed parameters on controlled primitives — compiles in pure Go
// to a GeometryIR: closed, consistently wound indexed meshes keyed by semantic
// component id, plus overlay polylines (roof water path, masonry drainage,
// fastener axes) and the derived facts validation reads (stack thickness,
// fastener embedment, laps, layer gaps). The browser, GLB, sections and
// drawings all consume this IR; nothing else generates geometry. Identical
// assembly content gives byte-identical IR.
//
// World frame: right-handed millimetres, +X along the wall, +Y outward from
// the wall, +Z up; origin at the top of the timber deck at the wall face.
// Every solid is a prism swept from a two-rail ("ribbon") or convex profile,
// so caps triangulate without a general polygon triangulator.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
)

type V2 [2]float64
type V3 [3]float64

func vadd(a, b V3) V3         { return V3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func vsub(a, b V3) V3         { return V3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func vmul(a V3, s float64) V3 { return V3{a[0] * s, a[1] * s, a[2] * s} }
func vdot(a, b V3) float64    { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func vcross(a, b V3) V3 {
	return V3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func vlen(a V3) float64 { return math.Sqrt(vdot(a, a)) }
func vunit(a V3) V3 {
	l := vlen(a)
	if l == 0 {
		return a
	}
	return vmul(a, 1/l)
}
func vlerp(a, b V3, t float64) V3 { return vadd(a, vmul(vsub(b, a), t)) }

// ---- solids -------------------------------------------------------------------------

// Solid is one closed mesh with shared vertices (sections key intersection
// points by shared edges, so neighbouring triangles agree exactly).
type Solid struct {
	Key  string
	Pos  []V3
	Tris [][3]int
}

type frame2 struct{ O, U, V V3 }

func (f frame2) at(p V2) V3 { return vadd(f.O, vadd(vmul(f.U, p[0]), vmul(f.V, p[1]))) }

func signedArea2(poly []V2) float64 {
	s := 0.0
	for i := range poly {
		j := (i + 1) % len(poly)
		s += poly[i][0]*poly[j][1] - poly[j][0]*poly[i][1]
	}
	return s / 2
}

func reverse2(p []V2) []V2 {
	out := make([]V2, len(p))
	for i := range p {
		out[len(p)-1-i] = p[i]
	}
	return out
}

// ribbonPrism sweeps the polygon A ++ reverse(B) along e. Rails must have
// equal length; caps are quad strips between matching rail points.
func ribbonPrism(key string, A, B []V2, f frame2, e V3) Solid {
	n := len(A)
	if n < 2 || len(B) != n {
		panic("construction: ribbon rails must have equal length ≥ 2")
	}
	poly := append(append([]V2{}, A...), reverse2(B)...)
	m := len(poly)
	flip := (signedArea2(poly) > 0) != (vdot(e, vcross(f.U, f.V)) > 0)
	s := Solid{Key: key, Pos: make([]V3, 0, 2*m), Tris: make([][3]int, 0, 4*(n-1)+2*m)}
	for _, p := range poly {
		s.Pos = append(s.Pos, f.at(p))
	}
	for _, p := range poly {
		s.Pos = append(s.Pos, vadd(f.at(p), e))
	}
	tri := func(a, b, c int) {
		if flip {
			b, c = c, b
		}
		s.Tris = append(s.Tris, [3]int{a, b, c})
	}
	for i := 0; i+1 < n; i++ {
		a0, a1, b0, b1 := i, i+1, m-1-i, m-2-i
		tri(b0, a1, a0)
		tri(b0, b1, a1)
		tri(m+a0, m+a1, m+b0)
		tri(m+a1, m+b1, m+b0)
	}
	for i := 0; i < m; i++ {
		j := (i + 1) % m
		tri(i, j, m+j)
		tri(i, m+j, m+i)
	}
	return s
}

// convexPrism sweeps a convex polygon (fan caps) along e.
func convexPrism(key string, poly []V2, f frame2, e V3) Solid {
	m := len(poly)
	flip := (signedArea2(poly) > 0) != (vdot(e, vcross(f.U, f.V)) > 0)
	s := Solid{Key: key}
	for _, p := range poly {
		s.Pos = append(s.Pos, f.at(p))
	}
	for _, p := range poly {
		s.Pos = append(s.Pos, vadd(f.at(p), e))
	}
	tri := func(a, b, c int) {
		if flip {
			b, c = c, b
		}
		s.Tris = append(s.Tris, [3]int{a, b, c})
	}
	for i := 1; i+1 < m; i++ {
		tri(0, i+1, i)
		tri(m, m+i, m+i+1)
	}
	for i := 0; i < m; i++ {
		j := (i + 1) % m
		tri(i, j, m+j)
		tri(i, m+j, m+i)
	}
	return s
}

func boxSolid(key string, min, max V3) Solid {
	return ribbonPrism(key, []V2{{min[0], min[1]}, {max[0], min[1]}}, []V2{{min[0], max[1]}, {max[0], max[1]}},
		frame2{O: V3{0, 0, min[2]}, U: V3{1, 0, 0}, V: V3{0, 1, 0}}, V3{0, 0, max[2] - min[2]})
}

const cylinderSegs = 12

// cylinderSolid: a cylinder of radius r from c0 along unit d for length l.
func cylinderSolid(key string, c0, d V3, l, r float64) Solid {
	ref := V3{1, 0, 0}
	if math.Abs(vdot(ref, d)) > 0.9 {
		ref = V3{0, 1, 0}
	}
	u := vunit(vcross(d, ref))
	v := vcross(d, u)
	poly := make([]V2, cylinderSegs)
	for i := range poly {
		a := 2 * math.Pi * float64(i) / cylinderSegs
		poly[i] = V2{r * math.Cos(a), r * math.Sin(a)}
	}
	return convexPrism(key, poly, frame2{O: c0, U: u, V: v}, vmul(d, l))
}

func translateSolid(s Solid, t V3) Solid {
	if t == (V3{}) {
		return s
	}
	out := Solid{Key: s.Key, Tris: s.Tris, Pos: make([]V3, len(s.Pos))}
	for i, p := range s.Pos {
		out.Pos[i] = vadd(p, t)
	}
	return out
}

func (s Solid) bounds() (V3, V3) {
	mn := V3{math.Inf(1), math.Inf(1), math.Inf(1)}
	mx := V3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, p := range s.Pos {
		for i := 0; i < 3; i++ {
			mn[i] = math.Min(mn[i], p[i])
			mx[i] = math.Max(mx[i], p[i])
		}
	}
	return mn, mx
}

// ---- profiles: fillet L and mitred polyline ------------------------------------------

// filletL builds the ribbon of a bent sheet lying inside the corner C between
// two outer lines leaving C along u1 and u2 (unit, in 2D), with inward unit
// normals n1, n2: outer face on the lines, thickness t, inner bend radius r.
// Returns rails (outer, inner) and the arc's stated chord error.
func filletL(C, u1, n1, u2, n2 V2, len1, len2, t, r float64, segs int) ([]V2, []V2, float64) {
	cosPhi := u1[0]*u2[0] + u1[1]*u2[1]
	phi := math.Acos(math.Max(-1, math.Min(1, cosPhi)))
	half := phi / 2
	rc := r + t
	dT := rc / math.Tan(half)
	bis := V2{u1[0] + u2[0], u1[1] + u2[1]}
	bl := math.Hypot(bis[0], bis[1])
	bis = V2{bis[0] / bl, bis[1] / bl}
	q := V2{C[0] + bis[0]*rc/math.Sin(half), C[1] + bis[1]*rc/math.Sin(half)}
	at := func(base V2, dir V2, d float64) V2 { return V2{base[0] + dir[0]*d, base[1] + dir[1]*d} }
	T1o, T2o := at(C, u1, dT), at(C, u2, dT)
	a1 := math.Atan2(T1o[1]-q[1], T1o[0]-q[0])
	a2 := math.Atan2(T2o[1]-q[1], T2o[0]-q[0])
	da := a2 - a1
	for da > math.Pi {
		da -= 2 * math.Pi
	}
	for da < -math.Pi {
		da += 2 * math.Pi
	}
	var outer, inner []V2
	push := func(o, i V2) { outer = append(outer, o); inner = append(inner, i) }
	push(at(C, u1, len1), at(at(C, u1, len1), n1, t))
	if r > 0 || t > 0 {
		for k := 0; k <= segs; k++ {
			ang := a1 + da*float64(k)/float64(segs)
			cs, sn := math.Cos(ang), math.Sin(ang)
			push(V2{q[0] + rc*cs, q[1] + rc*sn}, V2{q[0] + r*cs, q[1] + r*sn})
		}
	}
	push(at(C, u2, len2), at(at(C, u2, len2), n2, t))
	chord := 0.0
	if segs > 0 {
		chord = rc * (1 - math.Cos(math.Abs(da)/float64(2*segs)))
	}
	return outer, inner, chord
}

// mitreRibbon offsets a polyline centreline by ±t/2 with mitred joins.
func mitreRibbon(pts []V2, t float64) ([]V2, []V2) {
	n := len(pts)
	normal := func(a, b V2) V2 {
		dx, dy := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(dx, dy)
		return V2{-dy / l, dx / l}
	}
	var A, B []V2
	for i := 0; i < n; i++ {
		var nv V2
		switch {
		case i == 0:
			nv = normal(pts[0], pts[1])
		case i == n-1:
			nv = normal(pts[n-2], pts[n-1])
		default:
			n0, n1 := normal(pts[i-1], pts[i]), normal(pts[i], pts[i+1])
			m := V2{n0[0] + n1[0], n0[1] + n1[1]}
			ml := math.Hypot(m[0], m[1])
			m = V2{m[0] / ml, m[1] / ml}
			scale := 1 / (m[0]*n0[0] + m[1]*n0[1])
			nv = V2{m[0] * scale, m[1] * scale}
		}
		A = append(A, V2{pts[i][0] - nv[0]*t/2, pts[i][1] - nv[1]*t/2})
		B = append(B, V2{pts[i][0] + nv[0]*t/2, pts[i][1] + nv[1]*t/2})
	}
	return A, B
}

// ---- the IR -----------------------------------------------------------------------------

// GeometryIR is the compiled model of one assembly revision.
type GeometryIR struct {
	SchemaVersion int               `json:"schemaVersion"`
	Compiler      string            `json:"compiler"`
	Units         string            `json:"units"`
	Convention    string            `json:"convention"`
	AssemblyID    string            `json:"assemblyId"`
	Orientation   string            `json:"orientation"`
	Strategy      string            `json:"strategy"`
	WallCondition string            `json:"wallCondition"`
	ChordErrorMM  float64           `json:"chordErrorMm"`
	Bounds        [6]float64        `json:"bounds"`
	Triangles     int               `json:"triangles"`
	Parts         []IRPart          `json:"parts"`
	Overlays      []IROverlay       `json:"overlays"`
	Facts         IRFacts           `json:"facts"`
	Labels        map[string]string `json:"labels"`
	Notice        string            `json:"notice"`
	Hash          string            `json:"hash"`
}

type IRPart struct {
	Component  string     `json:"component"`
	Type       string     `json:"type"`
	Role       string     `json:"role"`
	Name       string     `json:"name"`
	Material   string     `json:"material"`
	Appearance Appearance `json:"appearance"`
	Void       bool       `json:"void"`
	Solids     []IRSolid  `json:"solids"`
}

type IRSolid struct {
	Key       string    `json:"key"`
	Positions []float64 `json:"positions"`
	Indices   []int     `json:"indices"`
}

type IROverlay struct {
	Kind    string       `json:"kind"`    // roof-water | masonry-water | surface-water | attachment
	Network string       `json:"network"` // roof | masonry | attachment
	Label   string       `json:"label"`
	Source  string       `json:"source"`
	Outlet  string       `json:"outlet"`
	Points  [][3]float64 `json:"points"`
	Refs    []string     `json:"refs"`
}

// IRFacts are geometric facts the deterministic rules read.
type IRFacts struct {
	StackAboveDeck float64        `json:"stackAboveDeckMm"`
	StackToRafter  float64        `json:"stackToRafterMm"`
	Fasteners      []FastenerFact `json:"fasteners"`
	LayerGaps      []LayerGap     `json:"layerGaps"`
	Laps           []LapFact      `json:"laps"`
	Disconnected   []string       `json:"disconnected"`
	WallTopNeeded  float64        `json:"wallTopNeededMm"`
	WallTop        float64        `json:"wallTopMm"`
	CavityWidth    float64        `json:"cavityWidthMm"`
	Inapplicable   []string       `json:"inapplicable"`
}

type FastenerFact struct {
	Component    string   `json:"component"`
	Host         string   `json:"host"`
	Count        int      `json:"count"`
	Length       float64  `json:"lengthMm"`
	Embedment    float64  `json:"embedmentMm"`
	MinEmbedment float64  `json:"minEmbedmentMm"`
	ReachesHost  bool     `json:"reachesHost"`
	Passes       []string `json:"passes"`
	DesignStack  float64  `json:"designStackMm"`
	CurrentStack float64  `json:"currentStackMm"`
	StackBased   bool     `json:"stackBased"`
}

type LayerGap struct {
	Below string  `json:"below"`
	Above string  `json:"above"`
	Gap   float64 `json:"gapMm"` // + gap, − overlap
}

type LapFact struct {
	Kind  string  `json:"kind"` // apron-over-sheet | counter-over-upstand | drip-over-upstand
	Over  string  `json:"over"`
	Under string  `json:"under"`
	Lap   float64 `json:"lapMm"`   // positive = laps correctly by this much
	Clear float64 `json:"clearMm"` // vertical separation for over/under (negative = reversed)
	OK    bool    `json:"ok"`
}

// ---- compile ---------------------------------------------------------------------------

type roofFrame struct {
	orient     string
	theta      float64
	S, N, A, H V3
	Lh, Wa     float64 // horizontal downslope run, across run
}

func (f roofFrame) at(t, a, w float64) V3 {
	return vadd(vadd(vmul(f.S, t), vmul(f.A, a)), vmul(f.N, w))
}

// hz maps (slope t, normal w) to the (horizontal-downslope h, z) profile plane.
func (f roofFrame) hz(t, w float64) V2 {
	return V2{t*math.Cos(f.theta) + w*math.Sin(f.theta), -t*math.Sin(f.theta) + w*math.Cos(f.theta)}
}

// tAt is the slope distance where a surface at normal offset w reaches
// horizontal distance h.
func (f roofFrame) tAt(h, w float64) float64 {
	return (h - w*math.Sin(f.theta)) / math.Cos(f.theta)
}

// zAt is the height of surface w at horizontal distance h.
func (f roofFrame) zAt(h, w float64) float64 {
	return (w - h*math.Sin(f.theta)) / math.Cos(f.theta)
}

func (f roofFrame) profileFrame() frame2 { return frame2{O: V3{}, U: f.H, V: V3{0, 0, 1}} }

type compiler struct {
	a      *Assembly
	cat    *Catalog
	f      roofFrame
	parts  map[string]*IRPart
	order  []string
	ov     []IROverlay
	facts  IRFacts
	labels map[string]string
	chord  float64
	layerW map[string][2]float64 // component → nominal [w0, w1]
	jg     junctionGeom
	errs   []string
}

// CompileError means the assembly cannot be compiled (unsupported geometry or
// a resource budget); the commit is refused with the prior assembly intact.
type CompileError struct{ Problems []string }

func (e *CompileError) Error() string { return "geometry: " + strings.Join(e.Problems, "; ") }

// Compile turns an assembly into its GeometryIR. cat may be nil (generic
// appearance only).
func Compile(a *Assembly, cat *Catalog) (*GeometryIR, error) {
	if len(a.Components) > MaxComponents {
		return nil, &CompileError{[]string{fmt.Sprintf("%d components exceed the %d budget", len(a.Components), MaxComponents)}}
	}
	c := &compiler{a: a, cat: cat, parts: map[string]*IRPart{}, labels: map[string]string{}, layerW: map[string][2]float64{}}
	c.frame()
	if len(c.errs) > 0 {
		return nil, &CompileError{c.errs}
	}
	c.stack()
	c.roofParts()
	c.wallParts()
	c.fasteners()
	c.overlays()
	if len(c.errs) > 0 {
		return nil, &CompileError{c.errs}
	}
	return c.finish()
}

func (c *compiler) frame() {
	a := c.a
	theta := a.aparam("pitch") * math.Pi / 180
	W, D := a.aparam("widthAlongWall"), a.aparam("depthFromWall")
	orient := a.Junction.Orientation
	if orient == "unresolved" || orient == "" {
		orient = "headwall" // illustration only; rules keep it unresolved
	}
	f := roofFrame{orient: orient, theta: theta}
	switch orient {
	case "headwall":
		f.S, f.N, f.A, f.H = V3{0, math.Cos(theta), -math.Sin(theta)}, V3{0, math.Sin(theta), math.Cos(theta)}, V3{1, 0, 0}, V3{0, 1, 0}
		f.Lh, f.Wa = D, W
	case "sidewall":
		f.S, f.N, f.A, f.H = V3{math.Cos(theta), 0, -math.Sin(theta)}, V3{math.Sin(theta), 0, math.Cos(theta)}, V3{0, 1, 0}, V3{1, 0, 0}
		f.Lh, f.Wa = W, D
	default:
		c.errs = append(c.errs, "unknown orientation "+orient)
	}
	if st, ok := strategies[a.Junction.Strategy]; ok && !st.Supported {
		c.errs = append(c.errs, "strategy "+a.Junction.Strategy+" is not modelled: "+st.Note)
	}
	c.f = f
	for name, q := range a.Parameters {
		if l := q.Label(); l != "" {
			c.labels["param:"+name] = l
		}
	}
	for _, comp := range a.Components {
		for name, q := range comp.Shape.Params {
			if l := q.Label(); l != "" {
				c.labels[comp.ID+"."+name] = l
			}
		}
	}
}

func (c *compiler) part(comp *Component) *IRPart {
	if p, ok := c.parts[comp.ID]; ok {
		return p
	}
	mat := ""
	if comp.Material != nil {
		mat = comp.Material.ID
	}
	p := &IRPart{Component: comp.ID, Type: comp.Type, Role: comp.Role, Name: comp.Name, Material: mat,
		Appearance: appearanceFor(c.cat, comp), Void: comp.Type == TypeCavitySpace || comp.Type == TypeWeepSet, Solids: []IRSolid{}}
	c.parts[comp.ID] = p
	c.order = append(c.order, comp.ID)
	return p
}

func (c *compiler) add(comp *Component, s Solid) {
	t := comp.Transform.Translation
	s = translateSolid(s, V3{t[0], t[1], t[2]})
	p := c.part(comp)
	ir := IRSolid{Key: s.Key, Positions: make([]float64, 0, 3*len(s.Pos)), Indices: make([]int, 0, 3*len(s.Tris))}
	for _, v := range s.Pos {
		ir.Positions = append(ir.Positions, round4(v[0]), round4(v[1]), round4(v[2]))
	}
	for _, tr := range s.Tris {
		ir.Indices = append(ir.Indices, tr[0], tr[1], tr[2])
	}
	p.Solids = append(p.Solids, ir)
}

func round4(x float64) float64 { return normZero(math.Round(x*1e4) / 1e4) }

func (c *compiler) active(comp *Component) bool { return comp.Applicability != "inapplicable" }

// stack computes each layer's nominal normal-offset range. The deck's top is
// the datum (w = 0); layers above stack upward in order, rafters hang below.
func (c *compiler) stack() {
	ls := c.a.layers()
	w := 0.0
	deckSeen := false
	for _, l := range ls {
		if !c.active(l) {
			continue
		}
		var th float64
		switch l.Type {
		case TypeBattenArray, TypeCounterBattens:
			th = l.param("depth")
		case TypeCorrugatedSheet:
			th = 0 // the sheet rests on the layer below; its own extent is computed in roofParts
		default:
			th = l.param("thickness")
		}
		if l.Type == TypeDecking && !deckSeen {
			c.layerW[l.ID] = [2]float64{-th, 0}
			deckSeen = true
			continue
		}
		c.layerW[l.ID] = [2]float64{w, w + th}
		w += th
	}
	c.facts.StackAboveDeck = w
	deck := c.a.firstOf(TypeDecking)
	td := 0.0
	if deck != nil && c.active(deck) {
		td = deck.param("thickness")
	}
	c.facts.StackToRafter = w + td
	// layer gaps from supported normal-direction offsets
	prev := ""
	prevTop := 0.0
	for _, l := range ls {
		r, ok := c.layerW[l.ID]
		if !ok || l.Type == TypeCorrugatedSheet {
			continue
		}
		dn := vdot(V3{l.Transform.Translation[0], l.Transform.Translation[1], l.Transform.Translation[2]}, c.f.N)
		bottom, top := r[0]+dn, r[1]+dn
		if prev != "" {
			c.facts.LayerGaps = append(c.facts.LayerGaps, LayerGap{Below: prev, Above: l.ID, Gap: round4(bottom - prevTop)})
		}
		prev, prevTop = l.ID, top
	}
}

// sheetTop is the top of the layer stack the sheet bears on (w).
func (c *compiler) sheetBase() float64 { return c.facts.StackAboveDeck }

func (c *compiler) roofParts() {
	f := c.f
	cosT := math.Cos(f.theta)
	for i := range c.a.Components {
		comp := &c.a.Components[i]
		if !c.active(comp) {
			continue
		}
		switch comp.Type {
		case TypeDecking, TypeControlLayer, TypeInsulation, TypeUnderlayment:
			r := c.layerW[comp.ID]
			A := []V2{{0, f.zAt(0, r[0])}, {f.Lh, f.zAt(f.Lh, r[0])}}
			B := []V2{{0, f.zAt(0, r[1])}, {f.Lh, f.zAt(f.Lh, r[1])}}
			c.add(comp, ribbonPrism(comp.ID+"#0", A, B, f.profileFrame(), vmul(f.A, f.Wa)))
		case TypeRafterArray:
			deck := c.a.firstOf(TypeDecking)
			top := 0.0
			if deck != nil {
				top = c.layerW[deck.ID][0]
			}
			b, d := comp.param("width"), comp.param("depth")
			A := []V2{{0, f.zAt(0, top-d)}, {f.Lh, f.zAt(f.Lh, top-d)}}
			B := []V2{{0, f.zAt(0, top)}, {f.Lh, f.zAt(f.Lh, top)}}
			for k, x := range c.arrayPositions(comp.param("offset"), comp.param("spacing"), b, f.Wa) {
				s := ribbonPrism(fmt.Sprintf("%s#%d", comp.ID, k), A, B, f.profileFrame(), vmul(f.A, b))
				c.add(comp, translateSolid(s, vmul(f.A, x-b/2)))
			}
		case TypeBattenArray, TypeCounterBattens:
			r := c.layerW[comp.ID]
			bw := comp.param("width")
			slopeLen := f.Lh / cosT
			if comp.Type == TypeCounterBattens {
				// counter-battens run down the slope, spaced across it
				A := []V2{{0, f.zAt(0, r[0])}, {f.Lh, f.zAt(f.Lh, r[0])}}
				B := []V2{{0, f.zAt(0, r[1])}, {f.Lh, f.zAt(f.Lh, r[1])}}
				for k, x := range c.arrayPositions(comp.param("offset"), comp.param("spacing"), bw, f.Wa) {
					s := ribbonPrism(fmt.Sprintf("%s#%d", comp.ID, k), A, B, f.profileFrame(), vmul(f.A, bw))
					c.add(comp, translateSolid(s, vmul(f.A, x-bw/2)))
				}
				continue
			}
			for k, t := range c.arrayPositions(comp.param("offset"), comp.param("spacing"), bw, slopeLen) {
				A := []V2{f.hz(t-bw/2, r[0]), f.hz(t+bw/2, r[0])}
				B := []V2{f.hz(t-bw/2, r[1]), f.hz(t+bw/2, r[1])}
				c.add(comp, ribbonPrism(fmt.Sprintf("%s#%d", comp.ID, k), A, B, f.profileFrame(), vmul(f.A, f.Wa)))
			}
		case TypeCorrugatedSheet:
			c.sheet(comp)
		}
	}
	c.closureAndFlashing()
}

// arrayPositions: centre positions offset + k·spacing whose full width fits.
func (c *compiler) arrayPositions(offset, spacing, width, run float64) []float64 {
	var out []float64
	if spacing <= 0 {
		return out
	}
	for x := offset; x+width/2 <= run+1e-9 && len(out) < 400; x += spacing {
		if x-width/2 >= -1e-9 {
			out = append(out, x)
		}
	}
	return out
}

// sheet geometry state shared with closures, flashings and fasteners.
type sheetGeom struct {
	ok                   bool
	comp                 *Component
	w0, depth, pitch, th float64
	a0, a1               float64 // across extent
	tTop, tBot           float64 // slope extent
	crestTop             float64 // w of the crest top surface
}

func (c *compiler) sheetInfo() sheetGeom {
	comp := c.a.firstOf(TypeCorrugatedSheet)
	if comp == nil || !c.active(comp) {
		return sheetGeom{}
	}
	f := c.f
	g := sheetGeom{ok: true, comp: comp, w0: c.sheetBase(), depth: comp.param("depth"), pitch: comp.param("pitch"), th: comp.param("thickness")}
	gap, over := comp.param("wallGap"), comp.param("overhang")
	if f.orient == "headwall" {
		g.a0, g.a1 = 0, f.Wa
		g.tTop, g.tBot = f.tAt(gap, g.w0), f.tAt(f.Lh+over, g.w0)
	} else {
		g.a0, g.a1 = gap, f.Wa
		g.tTop, g.tBot = f.tAt(0, g.w0), f.tAt(f.Lh+over, g.w0)
	}
	g.crestTop = g.w0 + g.depth + g.th
	return g
}

const segsPerWave = 16

func (g sheetGeom) bottomAt(a float64) float64 {
	return g.w0 + g.depth/2*(1-math.Cos(2*math.Pi*(a-g.a0)/g.pitch))
}

func (g sheetGeom) samples() []float64 {
	steps := int(math.Ceil((g.a1 - g.a0) / g.pitch * segsPerWave))
	if steps < 2 {
		steps = 2
	}
	out := make([]float64, steps+1)
	for i := range out {
		out[i] = g.a0 + (g.a1-g.a0)*float64(i)/float64(steps)
	}
	return out
}

func (c *compiler) sheet(comp *Component) {
	g := c.sheetInfo()
	f := c.f
	var A, B []V2
	for _, a := range g.samples() {
		w := g.bottomAt(a)
		A = append(A, V2{a, w})
		B = append(B, V2{a, w + g.th})
	}
	fr := frame2{O: vmul(f.S, g.tTop), U: f.A, V: f.N}
	c.add(comp, ribbonPrism(comp.ID+"#0", A, B, fr, vmul(f.S, g.tBot-g.tTop)))
	h := g.pitch / segsPerWave
	k := g.depth / 2 * math.Pow(2*math.Pi/g.pitch, 2)
	c.chord = math.Max(c.chord, h*h*k/8)
}

// closureAndFlashing builds the roof-side junction parts and records laps.
func (c *compiler) closureAndFlashing() {
	f := c.f
	g := c.sheetInfo()
	support := c.sheetBase()
	if g.ok {
		support = g.crestTop
	}
	closure := c.a.firstOf(TypeProfileClosure)
	if closure != nil && c.active(closure) && g.ok {
		if f.orient == "headwall" {
			top := g.crestTop + closure.param("topThickness")
			var A, B []V2
			for _, a := range g.samples() {
				A = append(A, V2{a, g.bottomAt(a) + g.th})
				B = append(B, V2{a, top})
			}
			t0 := g.tTop + closure.param("setback")
			fr := frame2{O: vmul(f.S, t0), U: f.A, V: f.N}
			c.add(closure, ribbonPrism(closure.ID+"#0", A, B, fr, vmul(f.S, closure.param("length"))))
			support = top
		} else {
			c.facts.Inapplicable = append(c.facts.Inapplicable, closure.ID)
		}
	}
	c.flashings(support, g)
}

// junction geometry recorded for the wall side.
type junctionGeom struct {
	upTop      V2      // (h|a, z|w) top of the base flashing upstand, outer face
	flashT     float64 // base flashing thickness
	ok         bool
	counterTop float64 // z (headwall) or w (sidewall) of the counterflashing return top
	counterBot float64
	grooveLo   float64 // reglet groove band (z or w)
	grooveHi   float64
	embed      float64
	twZ        float64 // through-wall bed joint height
	twTh       float64
}

func (c *compiler) flashings(support float64, g sheetGeom) {
	f := c.f
	var jg junctionGeom
	cosT, sinT := math.Cos(f.theta), math.Sin(f.theta)
	apron := c.a.firstOf(TypeApronFlashing)
	side := c.a.firstOf(TypeSidewallFlash)
	switch {
	case f.orient == "headwall" && apron != nil && c.active(apron):
		t, r := apron.param("thickness"), apron.param("bendRadius")
		C := V2{0, support / cosT} // wall face × support line, in (y, z)
		outer, inner, ch := filletL(C, V2{0, 1}, V2{1, 0}, V2{cosT, -sinT}, V2{sinT, cosT}, apron.param("upstand"), apron.param("leg"), t, r, 6)
		c.chord = math.Max(c.chord, ch)
		c.add(apron, ribbonPrism(apron.ID+"#0", outer, inner, f.profileFrame(), vmul(f.A, f.Wa)))
		jg = junctionGeom{upTop: V2{0, C[1] + apron.param("upstand")}, flashT: t, ok: true}
		dn := vdot(trans(apron), f.N)
		if g.ok {
			// the apron must lap OVER the sheet: its underside (support + dn)
			// at or above the sheet crest, and its leg must reach past the
			// sheet's upper end (lap length measured along the slope)
			tC := f.tAt(0, support)
			c.facts.Laps = append(c.facts.Laps, LapFact{Kind: "apron-over-sheet", Over: apron.ID, Under: g.comp.ID,
				Lap: round4(tC + apron.param("leg") - g.tTop), Clear: round4(support + dn - g.crestTop), OK: support+dn >= g.crestTop-0.01})
		}
	case f.orient == "sidewall" && side != nil && c.active(side) && g.ok:
		t, r := side.param("thickness"), side.param("bendRadius")
		C := V2{0, g.crestTop}
		outer, inner, ch := filletL(C, V2{0, 1}, V2{1, 0}, V2{1, 0}, V2{0, 1}, side.param("upstand"), side.param("leg"), t, r, 6)
		c.chord = math.Max(c.chord, ch)
		fr := frame2{O: vmul(f.S, g.tTop), U: f.A, V: f.N}
		c.add(side, ribbonPrism(side.ID+"#0", outer, inner, fr, vmul(f.S, g.tBot-g.tTop)))
		jg = junctionGeom{upTop: V2{0, g.crestTop + side.param("upstand")}, flashT: t, ok: true}
		dn := vdot(trans(side), f.N)
		c.facts.Laps = append(c.facts.Laps, LapFact{Kind: "apron-over-sheet", Over: side.ID, Under: g.comp.ID,
			Lap: round4(side.param("leg")), Clear: round4(dn), OK: dn >= -0.01})
	default:
		if apron != nil && f.orient == "sidewall" && c.active(apron) {
			c.errs = append(c.errs, "a headwall apron flashing cannot be placed at a sidewall; choose a sidewall strategy")
		}
	}
	c.jg = jg
	cf := c.a.firstOf(TypeCounterflashing)
	tw := c.a.firstOf(TypeThroughWall)
	if cf != nil && c.active(cf) && jg.ok {
		t := cf.param("thickness")
		inFace := jg.flashT + cf.param("clearance")
		outFace := inFace + t
		bot := jg.upTop[1] - cf.param("lap")
		top := bot + cf.param("height")
		embed := cf.param("embed")
		A := []V2{{outFace, bot}, {outFace, top}, {-embed, top}}
		B := []V2{{inFace, bot}, {inFace, top - t}, {-embed, top - t}}
		if f.orient == "headwall" {
			c.add(cf, ribbonPrism(cf.ID+"#0", A, B, frame2{O: V3{}, U: V3{0, 1, 0}, V: V3{0, 0, 1}}, vmul(f.A, f.Wa)))
		} else {
			fr := frame2{O: vmul(f.S, c.sheetInfo().tTop), U: f.A, V: f.N}
			c.add(cf, ribbonPrism(cf.ID+"#0", A, B, fr, vmul(f.S, c.sheetInfo().tBot-c.sheetInfo().tTop)))
		}
		c.jg.counterTop, c.jg.counterBot, c.jg.embed = top, bot, embed
		tr := trans(cf)
		dy := tr[1] // across the wall face in both orientations
		dz := vdot(tr, V3{0, 0, 1})
		if f.orient == "sidewall" {
			dz = vdot(tr, f.N)
		}
		lap := jg.upTop[1] - (bot + dz)
		clear := inFace + dy - jg.flashT
		c.facts.Laps = append(c.facts.Laps, LapFact{Kind: "counter-over-upstand", Over: cf.ID, Under: firstID(apron, side), Lap: round4(lap), Clear: round4(clear), OK: clear >= -0.01})
		sealant := c.a.firstOf(TypeSealant)
		s := 0.0
		if sealant != nil && c.active(sealant) {
			s = sealant.param("size")
			lo, hi := -embed, -embed+s
			if embed == 0 {
				lo, hi = 0, s
			}
			SA := []V2{{lo, top}, {hi, top}}
			SB := []V2{{lo, top + s}, {hi, top + s}}
			if f.orient == "headwall" {
				c.add(sealant, ribbonPrism(sealant.ID+"#0", SA, SB, frame2{O: V3{}, U: V3{0, 1, 0}, V: V3{0, 0, 1}}, vmul(f.A, f.Wa)))
			} else {
				g2 := c.sheetInfo()
				fr := frame2{O: vmul(f.S, g2.tTop), U: f.A, V: f.N}
				c.add(sealant, ribbonPrism(sealant.ID+"#0", SA, SB, fr, vmul(f.S, g2.tBot-g2.tTop)))
			}
		}
		if embed > 0 {
			c.jg.grooveLo, c.jg.grooveHi = top-t, top+s
		}
	}
	if tw != nil && c.active(tw) && jg.ok && f.orient == "headwall" {
		t := tw.param("thickness")
		outerW := c.wytheThickness("masonry:outer")
		cav := c.cavityWidth()
		z := jg.upTop[1] + tw.param("aboveUpstand")
		yd := jg.flashT + 2 + t/2
		innerFace := -(outerW + cav)
		pts := []V2{{yd, z - tw.param("drip")}, {yd, z}, {innerFace + t/2, z}, {innerFace + t/2, z + tw.param("upturn")}}
		A, B := mitreRibbon(pts, t)
		c.add(tw, ribbonPrism(tw.ID+"#0", A, B, frame2{O: V3{}, U: V3{0, 1, 0}, V: V3{0, 0, 1}}, vmul(f.A, f.Wa)))
		c.jg.twZ, c.jg.twTh = z, t
		lap := jg.upTop[1] - (z - tw.param("drip"))
		c.facts.Laps = append(c.facts.Laps, LapFact{Kind: "drip-over-upstand", Over: tw.ID, Under: firstID(apron), Lap: round4(lap), Clear: round4(yd - t/2 - jg.flashT), OK: yd-t/2 >= jg.flashT-0.01})
	}
}

func firstID(cs ...*Component) string {
	for _, c := range cs {
		if c != nil {
			return c.ID
		}
	}
	return ""
}

func trans(c *Component) V3 {
	return V3{c.Transform.Translation[0], c.Transform.Translation[1], c.Transform.Translation[2]}
}

func (c *compiler) wytheThickness(role string) float64 {
	if w := c.a.byRole(role); w != nil && c.active(w) {
		return w.param("thickness")
	}
	return 0
}

// cavityWidth is the modelled cavity (0 unless the wall is a cavity wall
// with a cavity-space component).
func (c *compiler) cavityWidth() float64 {
	if c.a.Junction.WallCondition.Value != "cavity" {
		return 0
	}
	if cv := c.a.firstOf(TypeCavitySpace); cv != nil && c.active(cv) {
		return cv.param("width")
	}
	return 0
}

// wallParts builds the masonry, cavity, weeps and end dams.
func (c *compiler) wallParts() {
	f := c.f
	outer := c.a.byRole("masonry:outer")
	inner := c.a.byRole("masonry:inner")
	t1 := c.wytheThickness("masonry:outer")
	t2 := c.wytheThickness("masonry:inner")
	cav := c.cavityWidth()
	c.facts.CavityWidth = cav
	// vertical extent: from below the rafters to the configured height above
	deck := c.a.firstOf(TypeDecking)
	raf := c.a.firstOf(TypeRafterArray)
	low := 0.0
	if deck != nil {
		low = c.layerW[deck.ID][0]
	}
	if raf != nil && c.active(raf) {
		low -= raf.param("depth")
	}
	zBot := f.zAt(0, low) - c.a.aparam("wallDepthBelow")
	if f.orient == "sidewall" {
		zBot = f.zAt(f.Lh, low) - c.a.aparam("wallDepthBelow")
	}
	zTop := c.a.aparam("wallHeightAbove")
	need := 0.0
	if c.jg.ok {
		need = c.jg.upTop[1]
		if f.orient == "sidewall" {
			need = f.zAt(0, c.jg.upTop[1])
		}
		if c.jg.counterTop != 0 {
			ct := c.jg.counterTop
			if f.orient == "sidewall" {
				ct = f.zAt(0, ct)
			}
			need = math.Max(need, ct+30)
		}
		if c.jg.twZ != 0 {
			if tw := c.a.firstOf(TypeThroughWall); tw != nil {
				need = math.Max(need, c.jg.twZ+tw.param("upturn")+30)
			}
		}
	}
	c.facts.WallTopNeeded, c.facts.WallTop = round4(need+25), zTop
	if need+25 > zTop {
		c.errs = append(c.errs, fmt.Sprintf("the detail's wall extent (%.0f mm above the datum) is below the junction parts (needs %.0f mm); raise wallHeightAbove", zTop, need+25))
		return
	}
	W := c.a.aparam("widthAlongWall")
	if outer != nil && c.active(outer) {
		c.outerWythe(outer, t1, zBot, zTop, W)
	}
	if inner != nil && c.active(inner) {
		y1 := -(t1 + cav)
		c.add(inner, boxSolid(inner.ID+"#0", V3{0, y1 - t2, zBot}, V3{W, y1, zTop}))
	}
	if cv := c.a.firstOf(TypeCavitySpace); cv != nil && c.active(cv) {
		if cav > 0 {
			c.add(cv, boxSolid(cv.ID+"#0", V3{0, -(t1 + cav), zBot}, V3{W, -t1, zTop}))
		} else {
			c.facts.Inapplicable = append(c.facts.Inapplicable, cv.ID)
		}
	}
	if ed := c.a.firstOf(TypeEndDams); ed != nil && c.active(ed) && c.jg.twZ != 0 && cav > 0 {
		th, h := ed.param("thickness"), ed.param("height")
		z0 := c.jg.twZ + c.jg.twTh/2
		c.add(ed, boxSolid(ed.ID+"#0", V3{0, -(t1 + cav), z0}, V3{th, -t1, z0 + h}))
		c.add(ed, boxSolid(ed.ID+"#1", V3{W - th, -(t1 + cav), z0}, V3{W, -t1, z0 + h}))
	}
}

// outerWythe splits the outer wythe around a reglet groove or a through-wall
// bed joint and its weep course, so no solid overlaps a flashing.
func (c *compiler) outerWythe(w *Component, t1, zBot, zTop, W float64) {
	f := c.f
	weeps := c.a.firstOf(TypeWeepSet)
	switch {
	case c.jg.twZ != 0 && f.orient == "headwall":
		z0 := c.jg.twZ - c.jg.twTh/2
		z1 := c.jg.twZ + c.jg.twTh/2
		c.add(w, boxSolid(w.ID+"#below", V3{0, -t1, zBot}, V3{W, 0, z0}))
		if weeps != nil && c.active(weeps) {
			h, ww := weeps.param("height"), weeps.param("width")
			xs := c.arrayPositions(weeps.param("spacing")/2, weeps.param("spacing"), ww, W)
			x := 0.0
			for k, xc := range xs {
				c.add(w, boxSolid(fmt.Sprintf("%s#course%d", w.ID, k), V3{x, -t1, z1}, V3{xc - ww/2, 0, z1 + h}))
				c.add(weeps, boxSolid(fmt.Sprintf("%s#%d", weeps.ID, k), V3{xc - ww/2, -t1, z1}, V3{xc + ww/2, 0, z1 + h}))
				x = xc + ww/2
			}
			c.add(w, boxSolid(fmt.Sprintf("%s#course%d", w.ID, len(xs)), V3{x, -t1, z1}, V3{W, 0, z1 + h}))
			c.add(w, boxSolid(w.ID+"#above", V3{0, -t1, z1 + h}, V3{W, 0, zTop}))
		} else {
			c.add(w, boxSolid(w.ID+"#above", V3{0, -t1, z1}, V3{W, 0, zTop}))
		}
	case c.jg.embed > 0 && f.orient == "headwall":
		g0, g1, e := c.jg.grooveLo, c.jg.grooveHi, c.jg.embed
		c.add(w, boxSolid(w.ID+"#below", V3{0, -t1, zBot}, V3{W, 0, g0}))
		c.add(w, boxSolid(w.ID+"#behind", V3{0, -t1, g0}, V3{W, -e, g1}))
		c.add(w, boxSolid(w.ID+"#above", V3{0, -t1, g1}, V3{W, 0, zTop}))
	case c.jg.embed > 0 && f.orient == "sidewall":
		// a raking groove: pieces bounded by lines parallel to the slope
		g0, g1, e := c.jg.grooveLo, c.jg.grooveHi, c.jg.embed
		zl := func(x, wv float64) float64 { return f.zAt(x, wv) }
		fr := frame2{O: V3{}, U: V3{1, 0, 0}, V: V3{0, 0, 1}}
		piece := func(key string, lo func(float64) float64, hi func(float64) float64, y0, y1 float64) {
			A := []V2{{0, lo(0)}, {W, lo(W)}}
			B := []V2{{0, hi(0)}, {W, hi(W)}}
			s := ribbonPrism(key, A, B, fr, V3{0, y1 - y0, 0})
			c.add(w, translateSolid(s, V3{0, y0, 0}))
		}
		bot := func(float64) float64 { return zBot }
		top := func(float64) float64 { return zTop }
		lo := func(x float64) float64 { return zl(x, g0) }
		hi := func(x float64) float64 { return zl(x, g1) }
		piece(w.ID+"#below", bot, lo, -t1, 0)
		piece(w.ID+"#behind", lo, hi, -t1, -e)
		piece(w.ID+"#above", hi, top, -t1, 0)
	default:
		c.add(w, boxSolid(w.ID+"#0", V3{0, -t1, zBot}, V3{W, 0, zTop}))
	}
}

// fasteners places fastener instances along their axes and records reach,
// embedment and the layers each axis passes through.
func (c *compiler) fasteners() {
	f := c.f
	g := c.sheetInfo()
	cosT := math.Cos(f.theta)
	battens := c.a.firstOf(TypeBattenArray)
	raf := c.a.firstOf(TypeRafterArray)
	var battenT []float64
	if battens != nil && c.active(battens) {
		battenT = c.arrayPositions(battens.param("offset"), battens.param("spacing"), battens.param("width"), f.Lh/cosT)
	}
	for _, fs := range c.a.byType(TypeFastenerSet) {
		if !c.active(fs) {
			continue
		}
		d, L, hd := fs.param("diameter"), fs.param("length"), fs.param("headDiameter")
		fact := FastenerFact{Component: fs.ID, Host: fs.HostID, Length: L, MinEmbedment: fs.param("minEmbedment"),
			DesignStack: fs.param("designStack"), CurrentStack: round4(c.facts.StackToRafter)}
		host, _ := c.a.component(fs.HostID)
		var placements [][2]float64 // (t, a)
		var wStart float64
		switch {
		case host != nil && host.Type == TypeBattenArray && g.ok:
			every := int(math.Max(1, fs.param("every")))
			var crests []float64
			for a := g.a0 + g.pitch/2; a <= g.a1-g.pitch/2+1e-9; a += g.pitch {
				crests = append(crests, a)
			}
			for _, t := range battenT {
				for k := 0; k < len(crests); k += every {
					placements = append(placements, [2]float64{t, crests[k]})
				}
			}
			wStart = g.crestTop
			fact.StackBased = false
			bw := c.layerW[host.ID]
			fact.Embedment = round4(bw[1] - (wStart - L))
			fact.ReachesHost = wStart-L < bw[1]
		case host != nil && host.Type == TypeRafterArray && battens != nil:
			for _, t := range battenT {
				for _, a := range c.arrayPositions(host.param("offset"), host.param("spacing"), host.param("width"), f.Wa) {
					placements = append(placements, [2]float64{t, a})
				}
			}
			wStart = c.layerW[battens.ID][1]
			fact.StackBased = true
			deck := c.a.firstOf(TypeDecking)
			rafTop := 0.0
			if deck != nil {
				rafTop = c.layerW[deck.ID][0]
			}
			fact.Embedment = round4(rafTop - (wStart - L))
			fact.ReachesHost = wStart-L < rafTop
		default:
			// no resolvable host: no geometry, an issue later
		}
		// layers the axis passes through, top-down
		tip := wStart - L
		for _, l := range c.a.layers() {
			r, ok := c.layerW[l.ID]
			if !ok || l.Type == TypeCorrugatedSheet {
				continue
			}
			if tip < r[1] && wStart > r[0] {
				fact.Passes = append(fact.Passes, l.ID)
			}
		}
		if raf != nil && fact.StackBased && fact.ReachesHost {
			fact.Passes = append(fact.Passes, raf.ID)
		}
		sort.Strings(fact.Passes)
		fact.Count = len(placements)
		for k, pl := range placements {
			base := f.at(pl[0], pl[1], wStart)
			down := vmul(f.N, -1)
			c.add(fs, cylinderSolid(fmt.Sprintf("%s#%d.shank", fs.ID, k), base, down, L, d/2))
			if fact.StackBased {
				// countersunk head inside the batten top
				c.add(fs, cylinderSolid(fmt.Sprintf("%s#%d.head", fs.ID, k), vadd(base, vmul(f.N, -4)), f.N, 4, hd/2))
			} else {
				c.add(fs, cylinderSolid(fmt.Sprintf("%s#%d.head", fs.ID, k), base, f.N, 3, hd/2))
			}
			if k < 4 {
				c.ov = append(c.ov, IROverlay{Kind: "attachment", Network: "attachment", Label: fs.Name + " → " + hostName(host),
					Source: fs.ID, Outlet: fs.HostID, Points: [][3]float64{r3(vadd(base, vmul(f.N, 3))), r3(vadd(base, vmul(down, L)))}, Refs: []string{fs.ID, fs.HostID}})
			}
		}
		c.facts.Fasteners = append(c.facts.Fasteners, fact)
	}
}

func hostName(h *Component) string {
	if h == nil {
		return "no host"
	}
	return h.Name
}

func r3(v V3) [3]float64 { return [3]float64{round4(v[0]), round4(v[1]), round4(v[2])} }

// overlays: the roof water path and the masonry drainage network are kept
// separate, each with its own source and outlet; their intentional interface
// is labelled.
func (c *compiler) overlays() {
	f := c.f
	g := c.sheetInfo()
	if g.ok {
		for k := 0; k < 3; k++ {
			a := g.a0 + float64(2*k)*g.pitch*3
			if a > g.a1 {
				break
			}
			w := g.bottomAt(a) + g.th
			c.ov = append(c.ov, IROverlay{Kind: "roof-water", Network: "roof", Label: "Roof surface water → eave",
				Source: g.comp.ID, Outlet: "eave", Points: [][3]float64{r3(f.at(g.tTop+20, a, w+1)), r3(f.at(g.tBot-5, a, w+1))}, Refs: []string{g.comp.ID}})
		}
	}
	t1 := c.wytheThickness("masonry:outer")
	cav := c.cavityWidth()
	W := c.a.aparam("widthAlongWall")
	switch {
	case c.a.Junction.WallCondition.Value == "cavity" && cav > 0 && c.jg.twZ != 0:
		tw := c.a.firstOf(TypeThroughWall)
		x := W / 2
		if weeps := c.a.firstOf(TypeWeepSet); weeps != nil {
			if xs := c.arrayPositions(weeps.param("spacing")/2, weeps.param("spacing"), weeps.param("width"), W); len(xs) > 0 {
				x = xs[len(xs)/2]
			}
		}
		yc := -(t1 + cav/2)
		zt := c.jg.twZ + c.jg.twTh/2 + 2
		c.ov = append(c.ov, IROverlay{Kind: "masonry-water", Network: "masonry", Label: "Cavity drainage → tray → weep → exterior (separate from the roof)",
			Source: "masonry cavity", Outlet: "weep", Points: [][3]float64{r3(V3{x, yc, c.facts.WallTop - 40}), r3(V3{x, yc, zt}), r3(V3{x, -t1 / 2, zt}), r3(V3{x, 5, zt})},
			Refs: []string{c.a.firstOf(TypeCavitySpace).ID, tw.ID}})
		c.ov = append(c.ov, IROverlay{Kind: "masonry-water", Network: "interface", Label: "Intentional interface: tray drip discharges over the apron upstand onto the roof",
			Source: tw.ID, Outlet: "roof", Points: [][3]float64{r3(V3{x, 5, zt}), r3(V3{x, 5, c.jg.upTop[1] - 10})}, Refs: []string{tw.ID}})
	case c.a.Junction.WallCondition.Value == "solid-bonded" && c.jg.ok:
		c.ov = append(c.ov, IROverlay{Kind: "surface-water", Network: "masonry", Label: "Solid wall: wall-face run-off over the counterflashing onto the apron (no cavity network)",
			Source: "wall face", Outlet: "roof", Points: [][3]float64{r3(V3{W / 2, 1, c.facts.WallTop - 40}), r3(V3{W / 2, 1, c.jg.counterTop + 15}), r3(V3{W / 2, c.jg.flashT + 6, c.jg.counterBot - 2})}, Refs: []string{}})
	}
}

func (c *compiler) finish() (*GeometryIR, error) {
	ir := &GeometryIR{SchemaVersion: SchemaVersion, Compiler: CompilerVersion, Units: "mm", Convention: CoordinateConvention,
		AssemblyID: c.a.ID, Orientation: c.a.Junction.Orientation, Strategy: c.a.Junction.Strategy, WallCondition: c.a.Junction.WallCondition.Value,
		ChordErrorMM: round4(c.chord), Labels: c.labels, Notice: NonApprovalNotice, Overlays: c.ov, Facts: c.facts}
	if ir.Overlays == nil {
		ir.Overlays = []IROverlay{}
	}
	mn := V3{math.Inf(1), math.Inf(1), math.Inf(1)}
	mx := V3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, comp := range c.a.Components {
		p, ok := c.parts[comp.ID]
		if !ok {
			continue
		}
		for _, s := range p.Solids {
			ir.Triangles += len(s.Indices) / 3
			for i := 0; i+2 < len(s.Positions); i += 3 {
				for k := 0; k < 3; k++ {
					mn[k] = math.Min(mn[k], s.Positions[i+k])
					mx[k] = math.Max(mx[k], s.Positions[i+k])
				}
			}
		}
		ir.Parts = append(ir.Parts, *p)
	}
	if ir.Triangles > MaxTriangles {
		return nil, &CompileError{[]string{fmt.Sprintf("%d triangles exceed the %d budget", ir.Triangles, MaxTriangles)}}
	}
	if len(ir.Parts) > 0 {
		ir.Bounds = [6]float64{round4(mn[0]), round4(mn[1]), round4(mn[2]), round4(mx[0]), round4(mx[1]), round4(mx[2])}
	}
	c.disconnected(ir)
	if ir.Facts.Fasteners == nil {
		ir.Facts.Fasteners = []FastenerFact{}
	}
	if ir.Facts.LayerGaps == nil {
		ir.Facts.LayerGaps = []LayerGap{}
	}
	if ir.Facts.Laps == nil {
		ir.Facts.Laps = []LapFact{}
	}
	if ir.Facts.Disconnected == nil {
		ir.Facts.Disconnected = []string{}
	}
	if ir.Facts.Inapplicable == nil {
		ir.Facts.Inapplicable = []string{}
	}
	ir.Facts.StackAboveDeck = round4(ir.Facts.StackAboveDeck)
	ir.Facts.StackToRafter = round4(ir.Facts.StackToRafter)
	b, err := Canonical(ir)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	ir.Hash = hex.EncodeToString(sum[:])
	return ir, nil
}

// disconnected: a non-void part whose bounds touch no other part's bounds
// (within 0.05 mm) floats free of the assembly.
func (c *compiler) disconnected(ir *GeometryIR) {
	type box struct{ mn, mx V3 }
	boxes := map[string]box{}
	for _, p := range ir.Parts {
		b := box{V3{math.Inf(1), math.Inf(1), math.Inf(1)}, V3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}}
		for _, s := range p.Solids {
			for i := 0; i+2 < len(s.Positions); i += 3 {
				for k := 0; k < 3; k++ {
					b.mn[k] = math.Min(b.mn[k], s.Positions[i+k])
					b.mx[k] = math.Max(b.mx[k], s.Positions[i+k])
				}
			}
		}
		boxes[p.Component] = b
	}
	const tol = 0.05
	for _, p := range ir.Parts {
		if p.Void || len(p.Solids) == 0 {
			continue
		}
		a := boxes[p.Component]
		touch := false
		for _, q := range ir.Parts {
			if q.Component == p.Component || len(q.Solids) == 0 {
				continue
			}
			b := boxes[q.Component]
			ok := true
			for k := 0; k < 3; k++ {
				if a.mn[k] > b.mx[k]+tol || b.mn[k] > a.mx[k]+tol {
					ok = false
				}
			}
			if ok {
				touch = true
				break
			}
		}
		if !touch {
			ir.Facts.Disconnected = append(ir.Facts.Disconnected, p.Component)
		}
	}
}
