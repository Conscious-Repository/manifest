package construction

// True plane sections (§5 Sections). An arbitrary plane {originMm, normal,
// up} cuts the compiled solids — never a picture. Each closed mesh is sliced
// triangle by triangle; intersection points are keyed by the shared mesh edge
// so neighbours agree bit-for-bit and loops close exactly; segments are
// oriented from vertex classification and winding alone, with a vertex on the
// plane counted on its positive side (symbolic perturbation), so a cut along
// a face or through a vertex reports each boundary once. Loops of material
// come out counter-clockwise in the section basis (X2 = up×n, Y2 = n×X2).
// Curved parts carry the compiler's stated chord error.

import (
	"fmt"
	"math"
	"sort"
)

// planeTolerance is the section plane-coincidence tolerance (mm).
const planeTolerance = 1e-3

// SectionResult is one plane's cut through one geometry revision.
type SectionResult struct {
	AssemblyID   string            `json:"assemblyId"`
	GeometryHash string            `json:"geometryHash"`
	Plane        SectionPlane      `json:"plane"`
	BasisX       [3]float64        `json:"basisX"`
	BasisY       [3]float64        `json:"basisY"`
	Parts        []SectionPart     `json:"parts"`
	Bounds       [4]float64        `json:"bounds"` // xmin, ymin, xmax, ymax (section mm)
	ChordErrorMM float64           `json:"chordErrorMm"`
	Dimensions   []DimensionChain  `json:"dimensions"`
	Labels       map[string]string `json:"labels"`
}

type SectionPart struct {
	Component string         `json:"component"`
	Type      string         `json:"type"`
	Role      string         `json:"role"`
	Name      string         `json:"name"`
	Material  string         `json:"material"`
	Family    string         `json:"family"`
	Void      bool           `json:"void"`
	Loops     [][][2]float64 `json:"loops"`
	Area      float64        `json:"areaMm2"`
}

// DimensionChain is a measured line through the section: each interval is a
// true section-coordinate distance through one part.
type DimensionChain struct {
	Name      string        `json:"name"`
	From      [2]float64    `json:"from"`
	To        [2]float64    `json:"to"`
	Intervals []DimInterval `json:"intervals"`
}

type DimInterval struct {
	Component string     `json:"component"`
	Name      string     `json:"name"`
	A         [2]float64 `json:"a"`
	B         [2]float64 `json:"b"`
	Length    float64    `json:"lengthMm"`
	Label     string     `json:"label"` // "", illustrative, unresolved
}

func planeBasis(p SectionPlane) (V3, V3, V3, error) {
	n := V3(p.Normal)
	if vlen(n) < 1e-12 {
		return V3{}, V3{}, V3{}, Invalid("section normal must be non-zero")
	}
	n = vunit(n)
	x := vcross(V3(p.Up), n)
	if vlen(x) < 1e-9 {
		return V3{}, V3{}, V3{}, Invalid("section up must not be parallel to the normal")
	}
	x = vunit(x)
	y := vcross(n, x)
	return n, x, y, nil
}

// Section cuts a compiled assembly with a plane.
func Section(ir *GeometryIR, cat *Catalog, plane SectionPlane) (*SectionResult, error) {
	n, bx, by, err := planeBasis(plane)
	if err != nil {
		return nil, err
	}
	if !finite(plane.Origin[:]...) {
		return nil, Invalid("section origin must be finite")
	}
	o := V3(plane.Origin)
	res := &SectionResult{AssemblyID: ir.AssemblyID, GeometryHash: ir.Hash, Plane: plane, BasisX: [3]float64(bx), BasisY: [3]float64(by),
		ChordErrorMM: ir.ChordErrorMM, Labels: ir.Labels, Parts: []SectionPart{}, Dimensions: []DimensionChain{}}
	mn := [2]float64{math.Inf(1), math.Inf(1)}
	mx := [2]float64{math.Inf(-1), math.Inf(-1)}
	for _, p := range ir.Parts {
		sp := SectionPart{Component: p.Component, Type: p.Type, Role: p.Role, Name: p.Name, Material: p.Material, Void: p.Void, Family: familyOf(cat, p.Material)}
		for _, s := range p.Solids {
			for _, loop := range sliceSolid(s, o, n, bx, by) {
				a := signedArea2(v2s(loop))
				if math.Abs(a) < 1e-6 {
					continue // tangent contact: a degenerate sliver, not material
				}
				sp.Loops = append(sp.Loops, loop)
				sp.Area += a
				for _, q := range loop {
					mn[0], mn[1] = math.Min(mn[0], q[0]), math.Min(mn[1], q[1])
					mx[0], mx[1] = math.Max(mx[0], q[0]), math.Max(mx[1], q[1])
				}
			}
		}
		if len(sp.Loops) > 0 {
			sp.Area = round4(sp.Area)
			res.Parts = append(res.Parts, sp)
		}
	}
	if len(res.Parts) > 0 {
		res.Bounds = [4]float64{round4(mn[0]), round4(mn[1]), round4(mx[0]), round4(mx[1])}
	}
	return res, nil
}

func familyOf(cat *Catalog, materialID string) string {
	if cat != nil {
		for _, m := range cat.Materials {
			if m.ID == materialID {
				return m.Family
			}
		}
	}
	return ""
}

func v2s(loop [][2]float64) []V2 {
	out := make([]V2, len(loop))
	for i, q := range loop {
		out[i] = V2(q)
	}
	return out
}

// sliceSolid intersects one closed mesh with the plane.
func sliceSolid(s IRSolid, o, n, bx, by V3) [][][2]float64 {
	nv := len(s.Positions) / 3
	pos := func(i int) V3 { return V3{s.Positions[3*i], s.Positions[3*i+1], s.Positions[3*i+2]} }
	d := make([]float64, nv)
	anyPos, anyNeg := false, false
	for i := 0; i < nv; i++ {
		d[i] = vdot(vsub(pos(i), o), n)
		// a vertex within 0.001 mm of the plane IS on it: IR positions are
		// stored to 0.0001 mm, and that rounding must not split a coplanar
		// face (still 10× below the 0.01 mm geometric target). On-plane
		// counts as the positive side.
		if math.Abs(d[i]) < planeTolerance {
			d[i] = 0
		}
		if d[i] >= 0 {
			anyPos = true
		} else {
			anyNeg = true
		}
	}
	if !anyPos || !anyNeg {
		return nil
	}
	type ek struct{ a, b int }
	key := func(a, b int) ek {
		if a > b {
			a, b = b, a
		}
		return ek{a, b}
	}
	point := map[ek][2]float64{}
	edgePoint := func(a, b int) ek {
		k := key(a, b)
		if _, ok := point[k]; !ok {
			t := d[k.a] / (d[k.a] - d[k.b])
			p := vlerp(pos(k.a), pos(k.b), t)
			q := vsub(p, o)
			point[k] = [2]float64{round4(vdot(q, bx)), round4(vdot(q, by))}
		}
		return k
	}
	next := map[ek]ek{}
	for t := 0; t+2 < len(s.Indices); t += 3 {
		tri := [3]int{s.Indices[t], s.Indices[t+1], s.Indices[t+2]}
		var from, to ek
		found := 0
		for e := 0; e < 3; e++ {
			a, b := tri[e], tri[(e+1)%3]
			pa, pb := d[a] >= 0, d[b] >= 0
			switch {
			case pa && !pb:
				from = edgePoint(a, b)
				found++
			case !pa && pb:
				to = edgePoint(a, b)
				found++
			}
		}
		if found == 2 {
			next[from] = to
		}
	}
	starts := make([]ek, 0, len(next))
	for k := range next {
		starts = append(starts, k)
	}
	sort.Slice(starts, func(i, j int) bool {
		if starts[i].a != starts[j].a {
			return starts[i].a < starts[j].a
		}
		return starts[i].b < starts[j].b
	})
	used := map[ek]bool{}
	var loops [][][2]float64
	for _, st := range starts {
		if used[st] {
			continue
		}
		var pts [][2]float64
		k := st
		for !used[k] {
			used[k] = true
			pts = append(pts, point[k])
			nk, ok := next[k]
			if !ok {
				break
			}
			k = nk
		}
		if pts = cleanLoop(pts); len(pts) >= 3 {
			loops = append(loops, pts)
		}
	}
	return loops
}

// cleanLoop drops repeated and collinear points (box diagonals crossing the
// plane mid-edge leave them).
func cleanLoop(p [][2]float64) [][2]float64 {
	var out [][2]float64
	for _, q := range p {
		if len(out) > 0 && math.Abs(out[len(out)-1][0]-q[0]) < 1e-6 && math.Abs(out[len(out)-1][1]-q[1]) < 1e-6 {
			continue
		}
		out = append(out, q)
	}
	if len(out) > 1 && math.Abs(out[0][0]-out[len(out)-1][0]) < 1e-6 && math.Abs(out[0][1]-out[len(out)-1][1]) < 1e-6 {
		out = out[:len(out)-1]
	}
	for changed := true; changed && len(out) > 3; {
		changed = false
		for i := 0; i < len(out) && len(out) > 3; i++ {
			a, b, c := out[(i+len(out)-1)%len(out)], out[i], out[(i+1)%len(out)]
			cr := (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0])
			if math.Abs(cr) <= 1e-7*math.Max(1, math.Hypot(c[0]-a[0], c[1]-a[1])) {
				out = append(out[:i], out[i+1:]...)
				changed = true
				i--
			}
		}
	}
	return out
}

// ---- dimension chains -------------------------------------------------------------

// Chain measures a straight line through the section: the parts it crosses,
// in order, with true lengths (section mm).
func (r *SectionResult) Chain(name string, from, to [2]float64, labelFor func(component string) string) DimensionChain {
	ch := DimensionChain{Name: name, From: from, To: to, Intervals: []DimInterval{}}
	dir := [2]float64{to[0] - from[0], to[1] - from[1]}
	L := math.Hypot(dir[0], dir[1])
	if L == 0 {
		return ch
	}
	type hit struct {
		t    float64
		part int
	}
	for pi, p := range r.Parts {
		// chains measure construction layers; fasteners crossing the line and
		// voids other than the cavity are not layer thicknesses
		if (p.Void && p.Type != TypeCavitySpace) || p.Type == TypeFastenerSet {
			continue
		}
		var ts []float64
		for _, loop := range p.Loops {
			for i := range loop {
				a, b := loop[i], loop[(i+1)%len(loop)]
				if t, ok := segIntersect(from, dir, a, b); ok {
					ts = append(ts, t)
				}
			}
		}
		sort.Float64s(ts)
		ts = dedupeFloats(ts, 1e-9)
		for i := 0; i+1 < len(ts); i += 2 {
			t0, t1 := math.Max(0, ts[i]), math.Min(1, ts[i+1])
			if (t1-t0)*L < 0.01 {
				continue // a touch, not a crossing
			}
			pa := [2]float64{round4(from[0] + dir[0]*t0), round4(from[1] + dir[1]*t0)}
			pb := [2]float64{round4(from[0] + dir[0]*t1), round4(from[1] + dir[1]*t1)}
			iv := DimInterval{Component: p.Component, Name: p.Name, A: pa, B: pb, Length: round3((t1 - t0) * L)}
			if labelFor != nil {
				iv.Label = labelFor(p.Component)
			}
			ch.Intervals = append(ch.Intervals, iv)
		}
		_ = pi
	}
	sort.Slice(ch.Intervals, func(i, j int) bool {
		di := math.Hypot(ch.Intervals[i].A[0]-from[0], ch.Intervals[i].A[1]-from[1])
		dj := math.Hypot(ch.Intervals[j].A[0]-from[0], ch.Intervals[j].A[1]-from[1])
		if di != dj {
			return di < dj
		}
		return ch.Intervals[i].Component < ch.Intervals[j].Component
	})
	return ch
}

func dedupeFloats(xs []float64, eps float64) []float64 {
	var out []float64
	for _, x := range xs {
		if len(out) > 0 && math.Abs(out[len(out)-1]-x) < eps {
			// a vertex on the line counts once; keep parity by dropping both
			// only when the polygon merely touches (handled by min/max clamp)
			continue
		}
		out = append(out, x)
	}
	return out
}

// segIntersect intersects the ray from+dir·t (t∈[0,1]) with segment a–b.
func segIntersect(from, dir, a, b [2]float64) (float64, bool) {
	e := [2]float64{b[0] - a[0], b[1] - a[1]}
	den := dir[0]*e[1] - dir[1]*e[0]
	if math.Abs(den) < 1e-12 {
		return 0, false
	}
	w := [2]float64{a[0] - from[0], a[1] - from[1]}
	t := (w[0]*e[1] - w[1]*e[0]) / den
	u := (w[0]*dir[1] - w[1]*dir[0]) / den
	if u < 0 || u >= 1 || t < -1e-9 || t > 1+1e-9 {
		return 0, false
	}
	return t, true
}

// StandardSection is the across-wall section through the junction: the
// plane x = c (headwall) with Z up, or the across-slope section for a
// sidewall. offset places it along the wall (mm from the detail's start).
func StandardSection(a *Assembly, offset float64) SectionPlane {
	if a.Junction.Orientation == "sidewall" {
		return SectionPlane{Origin: [3]float64{offset, 0, 0}, Normal: [3]float64{1, 0, 0}, Up: [3]float64{0, 0, 1}, Enabled: true}
	}
	return SectionPlane{Origin: [3]float64{offset, 0, 0}, Normal: [3]float64{1, 0, 0}, Up: [3]float64{0, 0, 1}, Enabled: true}
}

// StandardChains adds the junction's dimension chains: a chain normal to the
// roof through the stack, and a horizontal chain through the masonry.
func StandardChains(r *SectionResult, a *Assembly) {
	label := func(component string) string {
		c, ok := a.Component(component)
		if !ok {
			return ""
		}
		for _, k := range []string{"thickness", "depth", "width"} {
			if q, ok := c.Shape.Params[k]; ok {
				return q.Label()
			}
		}
		return ""
	}
	th := a.aparam("pitch") * math.Pi / 180
	n3 := roofNormal(a)
	bx, by := V3(r.BasisX), V3(r.BasisY)
	// the roof normal projected into the section; usable when the plane
	// contains it (across-wall section of a headwall)
	n2 := [2]float64{vdot(n3, bx), vdot(n3, by)}
	if math.Hypot(n2[0], n2[1]) > 0.99 && a.Junction.Orientation != "sidewall" {
		// through the first batten (so the chain crosses every stack layer),
		// starting below the rafters and running up the roof normal
		h := 600.0 * math.Cos(th)
		if b := a.firstOf(TypeBattenArray); b != nil {
			h = b.param("offset") * math.Cos(th)
		}
		s3 := V3{0, math.Cos(th), -math.Sin(th)}
		start3 := vadd(vmul(s3, h/math.Cos(th)), vmul(n3, -400))
		rel := vsub(start3, V3(r.Plane.Origin))
		rel[0] = 0 // the plane's own coordinate along its normal drops out
		from := [2]float64{vdot(rel, bx), vdot(rel, by)}
		to := [2]float64{from[0] + n2[0]*900, from[1] + n2[1]*900}
		r.Dimensions = append(r.Dimensions, r.Chain("Roof stack (normal to slope)", from, to, label))
	}
	// horizontal chain through the masonry 150 mm below the deck datum
	dir := [2]float64{vdot(V3{0, 1, 0}, bx), vdot(V3{0, 1, 0}, by)}
	if math.Hypot(dir[0], dir[1]) > 0.99 {
		rel := vsub(V3{0, -600, -150}, V3(r.Plane.Origin))
		rel[0] = 0
		from := [2]float64{vdot(rel, bx), vdot(rel, by)}
		to := [2]float64{from[0] + dir[0]*600, from[1] + dir[1]*600} // ends at the wall face
		r.Dimensions = append(r.Dimensions, r.Chain("Wall (horizontal, 150 mm below datum)", from, to, label))
	}
}

// String summarises a section for logs.
func (r *SectionResult) String() string {
	return fmt.Sprintf("section of %s: %d parts, bounds %v", r.AssemblyID, len(r.Parts), r.Bounds)
}
