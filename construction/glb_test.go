package construction

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

// The 1000 mm calibration cube survives the glTF map exactly: 1 m on every
// axis, X kept, Z (up) → Y, Y (outward) → −Z, and the inverse round-trips.
func TestConstructionGeometryBasisCalibration(t *testing.T) {
	cube := boxSolid("cube", V3{1000, 0, 0}, V3{2000, 1000, 1000})
	ir := &GeometryIR{Convention: CoordinateConvention, Parts: []IRPart{{Component: "cmp-" + fmt.Sprintf("%032x", 1), Appearance: Appearance{Color: "#cccccc", Opacity: 1}}}}
	is := IRSolid{Key: "cube"}
	for _, p := range cube.Pos {
		is.Positions = append(is.Positions, p[0], p[1], p[2])
	}
	for _, tr := range cube.Tris {
		is.Indices = append(is.Indices, tr[0], tr[1], tr[2])
	}
	ir.Parts[0].Solids = []IRSolid{is}
	glb, err := GLB(ir, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Accessors []struct {
			Min []float64 `json:"min"`
			Max []float64 `json:"max"`
		} `json:"accessors"`
	}
	jsLen := int(uint32(glb[12]) | uint32(glb[13])<<8 | uint32(glb[14])<<16 | uint32(glb[15])<<24)
	if err := json.Unmarshal(bytes.TrimRight(glb[20:20+jsLen], " "), &doc); err != nil {
		t.Fatal(err)
	}
	mn, mx := doc.Accessors[0].Min, doc.Accessors[0].Max
	want := [2][3]float64{{1, 0, -1}, {2, 1, 0}}
	for i := 0; i < 3; i++ {
		if math.Abs(mn[i]-want[0][i]) > 1e-6 || math.Abs(mx[i]-want[1][i]) > 1e-6 {
			t.Fatalf("cube bounds in glTF %v..%v", mn, mx)
		}
	}
	for _, p := range []V3{{1000, 0, 0}, {0, 1000, 0}, {0, 0, 1000}, {123.4, -56.7, 890.1}} {
		back := FromGLTF(ToGLTF(p))
		for i := 0; i < 3; i++ {
			if math.Abs(back[i]-p[i]) > 1e-3 {
				t.Fatalf("round trip %v → %v", p, back)
			}
		}
	}
	x, y, z := ToGLTF(V3{1, 0, 0}), ToGLTF(V3{0, 1, 0}), ToGLTF(V3{0, 0, 1})
	g := func(q [3]float32) V3 { return V3{float64(q[0]), float64(q[1]), float64(q[2])} }
	if c := vcross(g(x), g(y)); vlen(vsub(vunit(c), vunit(g(z)))) > 1e-9 {
		t.Fatal("the glTF map must stay right-handed")
	}
}

// The GLB of the template: well-formed, embedded buffer only, one node per
// drawn component with its semantic id, triangle count equal to the IR, and
// byte-identical across exports.
func TestConstructionGLBStructure(t *testing.T) {
	a, cat := fixtureAssembly()
	ir, err := Compile(a, cat)
	if err != nil {
		t.Fatal(err)
	}
	meta := map[string]string{"assemblyRevision": "rev"}
	g1, err := GLB(ir, meta)
	if err != nil {
		t.Fatal(err)
	}
	g2, _ := GLB(ir, meta)
	if !bytes.Equal(g1, g2) {
		t.Fatal("GLB not deterministic")
	}
	sum, err := ParseGLB(g1)
	if err != nil {
		t.Fatal(err)
	}
	if sum.ExternalURIs != 0 || sum.Triangles != ir.Triangles || sum.Extras["geometryHash"] != ir.Hash || sum.Extras["notice"] != NonApprovalNotice {
		t.Fatalf("glb summary %+v", sum)
	}
	want := map[string]bool{}
	for _, p := range ir.Parts {
		if len(p.Solids) > 0 {
			want[p.Component] = true
		}
	}
	if len(sum.ComponentIDs) != len(want) {
		t.Fatalf("%d nodes for %d drawn parts", len(sum.ComponentIDs), len(want))
	}
	for i, id := range sum.ComponentIDs {
		if !want[id] || sum.Nodes[i] != id {
			t.Fatalf("node %d %s not a drawn component id", i, id)
		}
	}
	// corrupt GLBs are refused
	bad := append([]byte{}, g1...)
	bad[8]++
	if _, err := ParseGLB(bad); err == nil {
		t.Fatal("bad total length accepted")
	}
	if _, err := ParseGLB(g1[:40]); err == nil {
		t.Fatal("truncated GLB accepted")
	}
}
