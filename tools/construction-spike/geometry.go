// Package constructionspike is the P0 proof for Construction Intelligence.
//
// It answers four go/no-go questions with the smallest code that can be
// wrong in the same ways the product could be: can a deterministic, pure-Go
// compiler turn controlled primitives (a corrugated panel, a bent flashing,
// two masonry wythes and a 1000 mm calibration cube) into
//
//  1. byte-identical GLB on every run, with semantic part names;
//  2. an oblique plane section made of closed loops, computed from the solid
//     boundaries rather than a picture;
//  3. a vector PDF page at a stated paper scale with a dimension on it; and
//  4. data a browser (pinned three.js, viewer.cjs) can draw and pick by part?
//
// It is a bounded proof artifact, not the product: construction/ owns the
// product geometry. Units are millimetres in a right-handed world, +X along
// the wall, +Y outward from the wall, +Z up. glTF output maps (x, z, -y)/1000.
package constructionspike

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type V2 [2]float64
type V3 [3]float64

func add(a, b V3) V3         { return V3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func sub(a, b V3) V3         { return V3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func mul(a V3, s float64) V3 { return V3{a[0] * s, a[1] * s, a[2] * s} }
func dot(a, b V3) float64    { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func cross(a, b V3) V3 {
	return V3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func length(a V3) float64        { return math.Sqrt(dot(a, a)) }
func unit(a V3) V3               { return mul(a, 1/length(a)) }
func lerp(a, b V3, t float64) V3 { return add(a, mul(sub(b, a), t)) }

// Solid is a closed, consistently wound triangle mesh with shared vertices
// (the section algorithm keys intersection points by shared edges).
type Solid struct {
	Part      string
	Positions []V3
	Tris      [][3]int
}

// Frame places a 2D profile: point (p0, p1) sits at Origin + p0*U + p1*V.
type Frame struct{ Origin, U, V V3 }

func (f Frame) at(p V2) V3 { return add(f.Origin, add(mul(f.U, p[0]), mul(f.V, p[1]))) }

// Ribbon is a profile given as two rails of equal length; the polygon is
// A in order followed by B reversed. Thin offset curves (sheets, flashings)
// and rectangles are ribbons, so caps triangulate as quad strips without a
// general polygon triangulator.
type Ribbon struct{ A, B []V2 }

func signedArea(poly []V2) float64 {
	s := 0.0
	for i := range poly {
		j := (i + 1) % len(poly)
		s += poly[i][0]*poly[j][1] - poly[j][0]*poly[i][1]
	}
	return s / 2
}

// Extrude builds a prism from a ribbon profile swept along e.
func Extrude(part string, r Ribbon, f Frame, e V3) Solid {
	n := len(r.A)
	if n < 2 || len(r.B) != n {
		panic("ribbon rails must have equal length ≥ 2")
	}
	poly := append(append([]V2{}, r.A...), reversed(r.B)...)
	ccw := signedArea(poly) > 0
	up := dot(e, cross(f.U, f.V)) > 0
	flip := ccw != up // wind every face outward
	m := len(poly)
	s := Solid{Part: part}
	for _, p := range poly {
		s.Positions = append(s.Positions, f.at(p))
	}
	for _, p := range poly {
		s.Positions = append(s.Positions, add(f.at(p), e))
	}
	tri := func(a, b, c int) {
		if flip {
			b, c = c, b
		}
		s.Tris = append(s.Tris, [3]int{a, b, c})
	}
	// caps: quad strip between rail A (index i) and rail B (index m-1-i)
	for i := 0; i+1 < n; i++ {
		a0, a1 := i, i+1
		b0, b1 := m-1-i, m-2-i
		tri(b0, a1, a0) // bottom cap faces against the sweep
		tri(b0, b1, a1)
		tri(m+a0, m+a1, m+b0) // top cap faces with it
		tri(m+a1, m+b1, m+b0)
	}
	for i := 0; i < m; i++ {
		j := (i + 1) % m
		tri(i, j, m+j)
		tri(i, m+j, m+i)
	}
	return s
}

func reversed(p []V2) []V2 {
	out := make([]V2, len(p))
	for i := range p {
		out[len(p)-1-i] = p[i]
	}
	return out
}

// Box is an axis-aligned prism (a two-point ribbon swept along Z).
func Box(part string, min, max V3) Solid {
	r := Ribbon{A: []V2{{min[0], min[1]}, {max[0], min[1]}}, B: []V2{{min[0], max[1]}, {max[0], max[1]}}}
	return Extrude(part, r, Frame{Origin: V3{0, 0, min[2]}, U: V3{1, 0, 0}, V: V3{0, 1, 0}}, V3{0, 0, max[2] - min[2]})
}

// CorrugatedPanel: sinusoidal sheet across X (pitch, depth), thickness t,
// running down a slope of pitchDeg away from the wall (+Y), length along
// the slope. Returns the solid and the chord error of the tessellation.
func CorrugatedPanel(part string, width, pitch, depth, t, length, pitchDeg float64, segsPerWave int) (Solid, float64) {
	steps := int(math.Ceil(width / pitch * float64(segsPerWave)))
	var a, b []V2
	for i := 0; i <= steps; i++ {
		x := width * float64(i) / float64(steps)
		w := depth / 2 * (1 - math.Cos(2*math.Pi*x/pitch))
		a = append(a, V2{x, w})
		b = append(b, V2{x, w + t})
	}
	th := pitchDeg * math.Pi / 180
	s := V3{0, math.Cos(th), -math.Sin(th)} // downslope
	n := V3{0, math.Sin(th), math.Cos(th)}  // roof normal
	solid := Extrude(part, Ribbon{A: a, B: b}, Frame{Origin: V3{}, U: V3{1, 0, 0}, V: n}, mul(s, length))
	// a sinusoid's largest chord error is at the crest/trough, where its
	// curvature is depth/2 * (2π/pitch)²; sagitta of a chord h is h²κ/8
	h := pitch / float64(segsPerWave)
	k := depth / 2 * math.Pow(2*math.Pi/pitch, 2)
	return solid, h * h * k / 8
}

// BentFlashing: an L flashing in the Y-Z plane — an upstand against the wall
// face (y = 0) and a leg running down the slope — with inner bend radius r and
// thickness t, swept along X for width. bendZ is the height of the bend.
func BentFlashing(part string, width, upstand, leg, t, r, pitchDeg, bendZ float64, arcSegs int) Solid {
	th := pitchDeg * math.Pi / 180
	// centreline: down the wall (direction -Z), turning through 90°+θ into
	// the downslope direction (cosθ, -sinθ) in (y, z)
	rc := r + t/2
	// the arc's centre sits rc out from the wall face, rc above the bend
	cy, cz := t/2+rc, bendZ+rc
	start := math.Pi // pointing from centre to the wall-side tangent point
	end := 1.5*math.Pi + th
	var a, b []V2
	addPt := func(y, z, ny, nz float64) {
		a = append(a, V2{y - ny*t/2, z - nz*t/2})
		b = append(b, V2{y + ny*t/2, z + nz*t/2})
	}
	// upstand top to arc start
	addPt(t/2, cz+upstand, 1, 0)
	for i := 0; i <= arcSegs; i++ {
		ang := start + (end-start)*float64(i)/float64(arcSegs)
		y, z := cy+rc*math.Cos(ang), cz+rc*math.Sin(ang)
		// inward normal (toward the centre) is the "+" side of the ribbon
		addPt(y, z, -math.Cos(ang), -math.Sin(ang))
	}
	ey, ez := cy+rc*math.Cos(end), cz+rc*math.Sin(end)
	ny, nz := -math.Cos(end), -math.Sin(end)
	addPt(ey+leg*math.Cos(th), ez-leg*math.Sin(th), ny, nz)
	return Extrude(part, Ribbon{A: a, B: b}, Frame{Origin: V3{}, U: V3{0, 1, 0}, V: V3{0, 0, 1}}, V3{width, 0, 0})
}

// Scene is the spike's fixture: what viewer.cjs draws and the tests measure.
type Scene struct {
	Solids     []Solid
	ChordError float64
}

func SpikeScene() Scene {
	panel, chord := CorrugatedPanel("corrugated-panel", 600, 76, 18, 0.5, 900, 10, 16)
	// lift the panel so its troughs sit on z = 0 at the wall line
	flashing := BentFlashing("bent-flashing", 600, 150, 150, 0.6, 3, 10, 18.5, 6)
	sc := Scene{ChordError: chord, Solids: []Solid{
		panel,
		flashing,
		Box("outer-wythe", V3{0, -100, -300}, V3{600, 0, 400}),
		Box("inner-wythe", V3{0, -210, -300}, V3{600, -110, 400}),
		Box("calibration-cube", V3{1000, 0, 0}, V3{2000, 1000, 1000}),
	}}
	return sc
}

// ---- determinism helpers ----------------------------------------------------

func canonicalJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// MeshJSON is the browser payload: millimetres, Z-up, per part.
type MeshJSON struct {
	Units      string     `json:"units"`
	Convention string     `json:"convention"`
	ChordError float64    `json:"chordErrorMm"`
	Parts      []PartJSON `json:"parts"`
	Hash       string     `json:"hash"`
}
type PartJSON struct {
	Part      string    `json:"part"`
	Positions []float64 `json:"positions"`
	Indices   []int     `json:"indices"`
}

func round6(x float64) float64 {
	r := math.Round(x*1e6) / 1e6
	if r == 0 {
		return 0 // no -0 in canonical output
	}
	return r
}

func (sc Scene) MeshJSON() MeshJSON {
	m := MeshJSON{Units: "mm", Convention: "x-along-wall,y-outward,z-up", ChordError: round6(sc.ChordError)}
	for _, s := range sc.Solids {
		p := PartJSON{Part: s.Part}
		for _, v := range s.Positions {
			p.Positions = append(p.Positions, round6(v[0]), round6(v[1]), round6(v[2]))
		}
		for _, t := range s.Tris {
			p.Indices = append(p.Indices, t[0], t[1], t[2])
		}
		m.Parts = append(m.Parts, p)
	}
	sum := sha256.Sum256(canonicalJSON(m))
	m.Hash = hex.EncodeToString(sum[:])
	return m
}

// ---- GLB ---------------------------------------------------------------------

// ToGLTF maps the world (mm, Z-up) to glTF (m, Y-up): (x, z, -y)/1000.
func ToGLTF(p V3) [3]float32 {
	return [3]float32{float32(p[0] / 1000), float32(p[2] / 1000), float32(-p[1] / 1000)}
}

// FromGLTF is the stored inverse.
func FromGLTF(q [3]float32) V3 {
	return V3{float64(q[0]) * 1000, -float64(q[2]) * 1000, float64(q[1]) * 1000}
}

// GLB writes flat-shaded meshes, one node per part with the part id in the
// node name and extras. Byte-identical for identical input.
func (sc Scene) GLB() []byte {
	type acc struct {
		BufferView    int       `json:"bufferView"`
		ComponentType int       `json:"componentType"`
		Count         int       `json:"count"`
		Type          string    `json:"type"`
		Min           []float32 `json:"min,omitempty"`
		Max           []float32 `json:"max,omitempty"`
	}
	type view struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
		Target     int `json:"target,omitempty"`
	}
	var bin bytes.Buffer
	var accessors []acc
	var views []view
	var meshes, nodes []map[string]any
	pushView := func(data []byte, target int) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		views = append(views, view{Buffer: 0, ByteOffset: bin.Len(), ByteLength: len(data), Target: target})
		bin.Write(data)
		return len(views) - 1
	}
	for i, s := range sc.Solids {
		var pos, nor bytes.Buffer
		var idx bytes.Buffer
		mn := [3]float32{float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1))}
		mx := [3]float32{float32(math.Inf(-1)), float32(math.Inf(-1)), float32(math.Inf(-1))}
		k := uint32(0)
		for _, t := range s.Tris {
			a, b, c := s.Positions[t[0]], s.Positions[t[1]], s.Positions[t[2]]
			nrm := cross(sub(b, a), sub(c, a))
			if l := length(nrm); l > 0 {
				nrm = mul(nrm, 1/l)
			}
			g := ToGLTF(nrm) // a direction maps like a point, then renormalise
			gn := V3{float64(g[0]), float64(g[1]), float64(g[2])}
			if l := length(gn); l > 0 {
				gn = mul(gn, 1/l)
			}
			for _, p := range []V3{a, b, c} {
				q := ToGLTF(p)
				for j := 0; j < 3; j++ {
					mn[j] = float32(math.Min(float64(mn[j]), float64(q[j])))
					mx[j] = float32(math.Max(float64(mx[j]), float64(q[j])))
				}
				binary.Write(&pos, binary.LittleEndian, q)
				binary.Write(&nor, binary.LittleEndian, [3]float32{float32(gn[0]), float32(gn[1]), float32(gn[2])})
				binary.Write(&idx, binary.LittleEndian, k)
				k++
			}
		}
		pv := pushView(pos.Bytes(), 34962)
		accessors = append(accessors, acc{BufferView: pv, ComponentType: 5126, Count: int(k), Type: "VEC3", Min: mn[:], Max: mx[:]})
		nv := pushView(nor.Bytes(), 34962)
		accessors = append(accessors, acc{BufferView: nv, ComponentType: 5126, Count: int(k), Type: "VEC3"})
		iv := pushView(idx.Bytes(), 34963)
		accessors = append(accessors, acc{BufferView: iv, ComponentType: 5125, Count: int(k), Type: "SCALAR"})
		meshes = append(meshes, map[string]any{"name": s.Part, "primitives": []map[string]any{{
			"attributes": map[string]int{"POSITION": 3 * i, "NORMAL": 3*i + 1}, "indices": 3*i + 2, "material": 0}}})
		nodes = append(nodes, map[string]any{"name": s.Part, "mesh": i, "extras": map[string]string{"part": s.Part}})
	}
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	sceneNodes := make([]int, len(nodes))
	for i := range nodes {
		sceneNodes[i] = i
	}
	doc := map[string]any{
		"asset": map[string]any{"version": "2.0", "generator": "manifest-construction-spike/0",
			"extras": map[string]string{"units": "m", "fromWorld": "(x, z, -y)/1000", "toWorld": "x=X*1000, y=-Z*1000, z=Y*1000"}},
		"scene": 0, "scenes": []map[string]any{{"nodes": sceneNodes}},
		"nodes": nodes, "meshes": meshes,
		"materials":   []map[string]any{{"name": "technical", "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float64{0.7, 0.7, 0.7, 1}, "metallicFactor": 0, "roughnessFactor": 1}}},
		"accessors":   accessors,
		"bufferViews": views,
		"buffers":     []map[string]int{{"byteLength": bin.Len()}},
	}
	js := canonicalJSON(doc)
	for len(js)%4 != 0 {
		js = append(js, ' ')
	}
	var out bytes.Buffer
	total := 12 + 8 + len(js) + 8 + bin.Len()
	binary.Write(&out, binary.LittleEndian, []uint32{0x46546C67, 2, uint32(total)})
	binary.Write(&out, binary.LittleEndian, []uint32{uint32(len(js)), 0x4E4F534A})
	out.Write(js)
	binary.Write(&out, binary.LittleEndian, []uint32{uint32(bin.Len()), 0x004E4942})
	out.Write(bin.Bytes())
	return out.Bytes()
}

// ---- plane sections ------------------------------------------------------------

// Plane is a cutting plane through Origin with unit Normal; Up fixes the 2D
// section basis (X2 = Up×Normal, Y2 = Normal×X2).
type Plane struct{ Origin, Normal, Up V3 }

func (p Plane) basis() (V3, V3) {
	n := unit(p.Normal)
	x := unit(cross(p.Up, n))
	y := cross(n, x)
	return x, y
}

// Loop is one closed section boundary in plane coordinates (mm).
type Loop struct {
	Part   string
	Points []V2
}

// Area is the loop's signed area (mm²).
func (l Loop) Area() float64 { return signedArea(l.Points) }

// Section intersects every solid with the plane. A vertex exactly on the
// plane counts as being on its positive side (symbolic perturbation), so a
// cut along a face or through a vertex yields each loop exactly once and
// never a double edge. Intersection points are keyed by the shared mesh edge
// so neighbouring triangles agree bit-for-bit and loops close exactly.
func Section(sc Scene, p Plane) []Loop {
	n := unit(p.Normal)
	bx, by := p.basis()
	var loops []Loop
	for _, s := range sc.Solids {
		d := make([]float64, len(s.Positions))
		for i, v := range s.Positions {
			d[i] = dot(sub(v, p.Origin), n)
		}
		pos := func(i int) bool { return d[i] >= 0 }
		type ek struct{ a, b int }
		key := func(a, b int) ek {
			if a > b {
				a, b = b, a
			}
			return ek{a, b}
		}
		point := map[ek]V3{}
		edgePoint := func(a, b int) ek {
			k := key(a, b)
			if _, ok := point[k]; !ok {
				t := d[k.a] / (d[k.a] - d[k.b])
				point[k] = lerp(s.Positions[k.a], s.Positions[k.b], t)
			}
			return k
		}
		next := map[ek]ek{}
		for _, t := range s.Tris {
			// Walk the triangle in winding order. The segment runs from the
			// edge where the walk leaves the positive side to the edge where
			// it re-enters it. That orientation depends only on vertex
			// classification and winding — never on segment length — so a
			// zero-length segment through a vertex still chains correctly,
			// and loops of the kept (negative-side) material come out
			// counter-clockwise seen from +n.
			var from, to ek
			found := 0
			for e := 0; e < 3; e++ {
				a, b := t[e], t[(e+1)%3]
				switch {
				case pos(a) && !pos(b):
					from = edgePoint(a, b)
					found++
				case !pos(a) && pos(b):
					to = edgePoint(a, b)
					found++
				}
			}
			if found == 2 {
				next[from] = to
			}
		}
		// chain segments into loops deterministically (smallest key first)
		var starts []ek
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
		for _, st := range starts {
			if used[st] {
				continue
			}
			var pts []V2
			k := st
			for !used[k] {
				used[k] = true
				q := sub(point[k], p.Origin)
				pts = append(pts, V2{dot(q, bx), dot(q, by)})
				nk, ok := next[k]
				if !ok {
					break
				}
				k = nk
			}
			loops = append(loops, Loop{Part: s.Part, Points: dedupe(pts)})
		}
	}
	return loops
}

func dedupe(p []V2) []V2 {
	var out []V2
	for _, q := range p {
		if len(out) > 0 && math.Abs(out[len(out)-1][0]-q[0]) < 1e-9 && math.Abs(out[len(out)-1][1]-q[1]) < 1e-9 {
			continue
		}
		out = append(out, q)
	}
	if len(out) > 1 && math.Abs(out[0][0]-out[len(out)-1][0]) < 1e-9 && math.Abs(out[0][1]-out[len(out)-1][1]) < 1e-9 {
		out = out[:len(out)-1]
	}
	// drop points that lie on the straight line through their neighbours
	// (a box face's triangulation diagonal crosses the plane mid-edge)
	for changed := true; changed && len(out) > 3; {
		changed = false
		for i := 0; i < len(out) && len(out) > 3; i++ {
			a, b, c := out[(i+len(out)-1)%len(out)], out[i], out[(i+1)%len(out)]
			cr := (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0])
			scale := math.Hypot(c[0]-a[0], c[1]-a[1])
			if math.Abs(cr) <= 1e-9*math.Max(1, scale) {
				out = append(out[:i], out[i+1:]...)
				changed = true
				i--
			}
		}
	}
	return out
}

// ---- vector PDF ------------------------------------------------------------------

// PDF draws section loops at 1:scale on an A4 landscape page with one
// dimension line between two model points. No raster, no timestamps: the
// file ID is the SHA-256 of the content stream.
func PDF(loops []Loop, scale float64, dimA, dimB V2, label string) []byte {
	const pageW, pageH = 297.0, 210.0 // mm
	const margin = 15.0
	pt := func(mm float64) float64 { return mm * 72 / 25.4 }
	// model mm → paper mm: divide by scale, then offset to the page origin
	ox, oy := margin+120.0, margin+90.0
	px := func(p V2) (float64, float64) { return pt(ox + p[0]/scale), pt(oy + p[1]/scale) }
	var c strings.Builder
	c.WriteString("0.25 w 0 0 0 RG\n")
	for _, l := range loops {
		for i, q := range l.Points {
			x, y := px(q)
			op := "l"
			if i == 0 {
				op = "m"
			}
			fmt.Fprintf(&c, "%.3f %.3f %s\n", x, y, op)
		}
		c.WriteString("h S\n")
	}
	ax, ay := px(dimA)
	bx, by := px(dimB)
	fmt.Fprintf(&c, "0.18 w %.3f %.3f m %.3f %.3f l S\n", ax, ay, bx, by)
	fmt.Fprintf(&c, "BT /F1 8 Tf %.3f %.3f Td (%s) Tj ET\n", (ax+bx)/2, (ay+by)/2+4, pdfEscape(label))
	fmt.Fprintf(&c, "BT /F1 7 Tf %.3f %.3f Td (%s) Tj ET\n", pt(margin), pt(margin), pdfEscape(fmt.Sprintf("Scale 1:%g on A4 (297 x 210 mm). Illustrative proof geometry.", scale)))
	content := c.String()
	sum := sha256.Sum256([]byte(content))
	id := hex.EncodeToString(sum[:16])
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.3f %.3f] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", pt(pageW), pt(pageH)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /ID [<%s> <%s>] >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, id, id, xref)
	return out.Bytes()
}

// pdfEscape keeps text printable ASCII and escapes the string delimiters, so
// no input can end the literal or inject an operator.
func pdfEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r >= 32 && r < 127:
			b.WriteRune(r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}
