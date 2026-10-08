package construction

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

var updateGoldens = flag.Bool("update", false, "rewrite construction testdata goldens")

// fixtureIDs mints fixed, valid ids so geometry goldens are stable.
func fixtureIDs() IDSource {
	n := 0
	return func(kind string) string { n++; return fmt.Sprintf("%s-%032x", kind, n) }
}

func fixtureAssembly() (*Assembly, *Catalog) {
	pid := fmt.Sprintf("cp-%032x", 0xc0ffee)
	return RoofMasonryTemplate(pid, fixtureIDs()), newCatalog(pid)
}

func solidOf(t *testing.T, ir *GeometryIR, comp string) IRSolid {
	t.Helper()
	for _, p := range ir.Parts {
		if p.Component == comp && len(p.Solids) > 0 {
			return p.Solids[0]
		}
	}
	t.Fatalf("no solid for %s", comp)
	return IRSolid{}
}

func pt(s IRSolid, i int) V3 { return V3{s.Positions[3*i], s.Positions[3*i+1], s.Positions[3*i+2]} }

// Every solid of the template — sheets, flashings, closures, wythes, cylinders
// — is a closed 2-manifold with consistent outward winding: each edge is used
// once in each direction and the signed volume is positive.
func TestConstructionGeometryWatertightAndOutward(t *testing.T) {
	a, cat := fixtureAssembly()
	ir, err := Compile(a, cat)
	if err != nil {
		t.Fatal(err)
	}
	solids := 0
	for _, p := range ir.Parts {
		for _, s := range p.Solids {
			solids++
			edges := map[[2]int]int{}
			vol := 0.0
			for i := 0; i+2 < len(s.Indices); i += 3 {
				tri := [3]int{s.Indices[i], s.Indices[i+1], s.Indices[i+2]}
				for e := 0; e < 3; e++ {
					edges[[2]int{tri[e], tri[(e+1)%3]}]++
				}
				a, b, c := pt(s, tri[0]), pt(s, tri[1]), pt(s, tri[2])
				vol += vdot(a, vcross(b, c)) / 6
			}
			for e, n := range edges {
				if n != 1 || edges[[2]int{e[1], e[0]}] != 1 {
					t.Fatalf("%s %s: edge %v used %d/%d times — not closed or inconsistently wound", p.Type, s.Key, e, n, edges[[2]int{e[1], e[0]}])
				}
			}
			if vol <= 0 {
				t.Fatalf("%s %s: signed volume %.4f — inward winding", p.Type, s.Key, vol)
			}
		}
	}
	if solids < 100 {
		t.Fatalf("only %d solids", solids)
	}
}

// Analytic checks: the roof slopes down as +Y (headwall), layers have their
// perpendicular thickness, arrays have the expected member counts, and the
// detail's bounds follow the parameters.
func TestConstructionGeometryAnalytic(t *testing.T) {
	a, cat := fixtureAssembly()
	ir, err := Compile(a, cat)
	if err != nil {
		t.Fatal(err)
	}
	th := 10 * math.Pi / 180
	n := V3{0, math.Sin(th), math.Cos(th)}
	ins := solidOf(t, ir, a.firstOf(TypeInsulation).ID)
	ws := map[float64]bool{}
	for i := 0; i < len(ins.Positions)/3; i++ {
		ws[math.Round(vdot(pt(ins, i), n)*1000)/1000] = true
	}
	if len(ws) != 2 || !ws[1] || !ws[101] {
		t.Fatalf("insulation normal offsets %v (want 1 and 101: on the 1 mm control layer, 100 mm thick)", ws)
	}
	// top surface at the wall vs at the eave: drop = D·tanθ
	zAt := func(s IRSolid, y float64) float64 {
		best := math.Inf(-1)
		for i := 0; i < len(s.Positions)/3; i++ {
			if p := pt(s, i); math.Abs(p[1]-y) < 1e-6 {
				best = math.Max(best, p[2])
			}
		}
		return best
	}
	if drop := zAt(ins, 0) - zAt(ins, 1800); math.Abs(drop-1800*math.Tan(th)) > 0.01 {
		t.Fatalf("roof drops %.4f mm over 1800 mm, want %.4f (slope down as +Y)", drop, 1800*math.Tan(th))
	}
	count := func(id string) int {
		for _, p := range ir.Parts {
			if p.Component == id {
				return len(p.Solids)
			}
		}
		return 0
	}
	if count(a.firstOf(TypeRafterArray).ID) != 4 || count(a.firstOf(TypeBattenArray).ID) != 3 {
		t.Fatalf("rafters %d (want 4 at 600 c/c in 2400), battens %d (want 3)", count(a.firstOf(TypeRafterArray).ID), count(a.firstOf(TypeBattenArray).ID))
	}
	if ir.Bounds[0] != 0 || ir.Bounds[3] != 2400 || ir.Bounds[4] < 1800 || ir.Bounds[5] != 700 || ir.Bounds[1] != -200 {
		t.Fatalf("bounds %v", ir.Bounds)
	}
	if ir.ChordErrorMM <= 0 || ir.ChordErrorMM > 0.2 {
		t.Fatalf("stated chord error %v", ir.ChordErrorMM)
	}
	for _, o := range ir.Overlays {
		if o.Kind == "roof-water" && !(o.Points[len(o.Points)-1][2] < o.Points[0][2] && o.Points[len(o.Points)-1][1] > o.Points[0][1]) {
			t.Fatalf("roof water must run down and away from the wall: %v", o.Points)
		}
		if o.Kind == "attachment" && o.Points[1][2] >= o.Points[0][2] {
			t.Fatalf("fastener axis runs head → tip downward: %v", o.Points)
		}
	}
	if ir.Notice != NonApprovalNotice {
		t.Fatal("the IR carries the non-approval notice")
	}
}

// Determinism: two compilations are byte-identical, and the fixed-id fixture
// matches the committed golden (hash, counts, facts). -update rewrites it.
func TestConstructionGeometryDeterministicGolden(t *testing.T) {
	a, cat := fixtureAssembly()
	x, err := Compile(a, cat)
	if err != nil {
		t.Fatal(err)
	}
	y, _ := Compile(a, cat)
	bx, _ := Canonical(x)
	by, _ := Canonical(y)
	if !bytes.Equal(bx, by) || x.Hash != y.Hash {
		t.Fatal("compile output differs between runs")
	}
	type golden struct {
		Template      string           `json:"template"`
		Components    []map[string]any `json:"components"`
		Parameters    map[string]any   `json:"parameters"`
		Hash          string           `json:"geometryHash"`
		Triangles     int              `json:"triangles"`
		Bounds        [6]float64       `json:"bounds"`
		Facts         IRFacts          `json:"facts"`
		Insulation150 struct {
			Hash          string  `json:"geometryHash"`
			StackToRafter float64 `json:"stackToRafterMm"`
			Embedment     float64 `json:"structuralEmbedmentMm"`
		} `json:"insulation150"`
	}
	g := golden{Template: TemplateRoofMasonry, Hash: x.Hash, Triangles: x.Triangles, Bounds: x.Bounds, Facts: x.Facts, Parameters: map[string]any{}}
	for name, q := range a.Parameters {
		g.Parameters[name] = *q.Value
	}
	for _, c := range a.Components {
		params := map[string]float64{}
		for k, q := range c.Shape.Params {
			v, _ := q.Effective()
			params[k] = v
		}
		g.Components = append(g.Components, map[string]any{"id": c.ID, "type": c.Type, "role": c.Role, "params": params})
	}
	ins := a.firstOf(TypeInsulation)
	v := 150.0
	q := ins.Shape.Params["thickness"]
	q.Value = &v
	ins.Shape.Params["thickness"] = q
	z, err := Compile(a, cat)
	if err != nil {
		t.Fatal(err)
	}
	g.Insulation150.Hash, g.Insulation150.StackToRafter = z.Hash, z.Facts.StackToRafter
	for _, f := range z.Facts.Fasteners {
		if f.StackBased {
			g.Insulation150.Embedment = f.Embedment
		}
	}
	b, _ := json.MarshalIndent(g, "", "  ")
	path := filepath.Join("testdata", "roof-wall", "assemblies.json")
	if *updateGoldens {
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	have, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if !bytes.Equal(bytes.TrimSpace(have), bytes.TrimSpace(b)) {
		t.Fatalf("geometry golden changed (hash %s); review and rerun with -update", x.Hash)
	}
	if x.Hash == z.Hash || z.Facts.StackToRafter != 216 || g.Insulation150.Embedment != 14 {
		t.Fatal("150 mm insulation must change geometry and embedment")
	}
}
