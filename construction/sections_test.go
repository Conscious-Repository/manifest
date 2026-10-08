package construction

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func partArea(sec *SectionResult, comp string) (float64, int) {
	for _, p := range sec.Parts {
		if p.Component == comp {
			return p.Area, len(p.Loops)
		}
	}
	return 0, 0
}

func chainLengths(ch DimensionChain) []float64 {
	var out []float64
	for _, iv := range ch.Intervals {
		out = append(out, iv.Length)
	}
	return out
}

// An orthogonal cut across the wall through a rafter: every layer's area is
// the analytic parallelogram D·t/cosθ, the masonry box is exact, loops are
// counter-clockwise, and the dimension chain reads the true stack.
func TestConstructionSectionOrthogonalAcrossWall(t *testing.T) {
	a, cat := fixtureAssembly()
	ir, err := Compile(a, cat)
	if err != nil {
		t.Fatal(err)
	}
	sec, err := Section(ir, cat, StandardSection(a, 300))
	if err != nil {
		t.Fatal(err)
	}
	StandardChains(sec, a)
	cosT := math.Cos(10 * math.Pi / 180)
	for typ, th := range map[string]float64{TypeInsulation: 100, TypeDecking: 25, TypeControlLayer: 1, TypeUnderlayment: 2} {
		got, loops := partArea(sec, a.firstOf(typ).ID)
		if want := 1800 * th / cosT; loops != 1 || !approx(got, want, 1e-3*want+0.01) {
			t.Fatalf("%s area %.4f (loops %d), want %.4f", typ, got, loops, want)
		}
	}
	if got, _ := partArea(sec, a.firstOf(TypeRafterArray).ID); !approx(got, 1800*200/cosT, 0.5) {
		t.Fatalf("rafter area %.3f", got)
	}
	zBot := -225/cosT - 450
	if got, _ := partArea(sec, a.byRole("masonry:outer").ID); !approx(got, 100*(700-zBot), 0.05) {
		t.Fatalf("outer wythe area %.3f want %.3f", got, 100*(700-zBot))
	}
	for _, p := range sec.Parts {
		if p.Area <= 0 {
			t.Fatalf("%s loop orientation: area %.4f", p.Type, p.Area)
		}
	}
	if len(sec.Dimensions) != 2 {
		t.Fatalf("chains %d", len(sec.Dimensions))
	}
	stack := chainLengths(sec.Dimensions[0])
	want := []float64{200, 25, 1, 100, 2, 38, 0.5}
	if len(stack) != len(want) {
		t.Fatalf("stack chain %v, want %v", stack, want)
	}
	for i := range want {
		if !approx(stack[i], want[i], 0.002) {
			t.Fatalf("stack chain %v, want %v", stack, want)
		}
	}
	for _, iv := range sec.Dimensions[0].Intervals {
		if iv.Label != "illustrative" {
			t.Fatalf("every fixture dimension is labelled illustrative: %+v", iv)
		}
	}
	if wall := chainLengths(sec.Dimensions[1]); len(wall) != 2 || !approx(wall[0], 100, 0.002) || !approx(wall[1], 100, 0.002) {
		t.Fatalf("wall chain %v (two adjacent wythes, no assumed cavity)", wall)
	}
	// 150 mm insulation: the same plane reads 150 (geometry follows the revision)
	ins := a.firstOf(TypeInsulation)
	v := 150.0
	q := ins.Shape.Params["thickness"]
	q.Value = &v
	ins.Shape.Params["thickness"] = q
	ir2, _ := Compile(a, cat)
	sec2, _ := Section(ir2, cat, StandardSection(a, 300))
	StandardChains(sec2, a)
	if got := chainLengths(sec2.Dimensions[0])[3]; !approx(got, 150, 0.002) {
		t.Fatalf("insulation in the chain after the edit: %v", got)
	}
}

// A cavity variant shows the void between the wythes at its (placeholder)
// width, labelled unresolved; through-wall flashing and a weep appear when
// the plane passes through one.
func TestConstructionSectionCavityVoidAndWeep(t *testing.T) {
	s, st, id := templateProblem(t)
	st2 := mustExec(t, s, st, id, map[string]any{"op": "SetWallCondition", "value": "cavity", "provenance": "user-assumption", "newComponentIds": map[string]string{"cavity": NewID(KindComponent)}})
	weeps := NewID(KindComponent)
	st3 := mustExec(t, s, st2, id, map[string]any{"op": "SetJunctionStrategy", "orientation": "headwall", "strategy": "apron-through-wall-flashing",
		"newComponentIds": map[string]string{"through-wall": NewID(KindComponent), "weeps": weeps, "end-dams": NewID(KindComponent)}})
	a := st3.Assemblies[id]
	ir := compile(t, st3, id)
	sec, err := Section(ir, st3.Catalog, StandardSection(a, 300)) // weeps at 300 + k·600
	if err != nil {
		t.Fatal(err)
	}
	StandardChains(sec, a)
	wall := chainLengths(sec.Dimensions[1])
	if len(wall) != 3 || !approx(wall[0], 100, 0.002) || !approx(wall[1], 50, 0.002) || !approx(wall[2], 100, 0.002) {
		t.Fatalf("wall chain %v (inner wythe, cavity void, outer wythe)", wall)
	}
	if sec.Dimensions[1].Intervals[1].Label != "unresolved" {
		t.Fatalf("the cavity width is unknown and must read unresolved: %+v", sec.Dimensions[1].Intervals[1])
	}
	cav := a.firstOf(TypeCavitySpace).ID
	found := map[string]bool{}
	for _, p := range sec.Parts {
		found[p.Component] = true
		if p.Component == cav && !p.Void {
			t.Fatal("the cavity is a void region")
		}
	}
	if !found[cav] || !found[a.firstOf(TypeThroughWall).ID] || !found[weeps] {
		t.Fatal("cavity, tray and weep expected in a section through a weep")
	}
	// between weeps the outer wythe course is solid again
	sec2, _ := Section(ir, st3.Catalog, StandardSection(a, 600))
	for _, p := range sec2.Parts {
		if p.Component == weeps {
			t.Fatal("no weep between weeps")
		}
	}
}

// Bent flashing thickness and extent; corrugated crests and troughs in an
// along-wall cut; an oblique plane; coplanar and tangent planes.
func TestConstructionSectionFlashingCorrugationObliqueCoplanarTangent(t *testing.T) {
	a, cat := fixtureAssembly()
	ir, _ := Compile(a, cat)
	cosT, sinT := math.Cos(10*math.Pi/180), math.Sin(10*math.Pi/180)
	sec, _ := Section(ir, cat, StandardSection(a, 300))
	var apronLoop [][2]float64
	for _, p := range sec.Parts {
		if p.Type == TypeApronFlashing {
			apronLoop = p.Loops[0]
			if !approx(p.Area, 0.6*300, 0.6*300*0.03) {
				t.Fatalf("apron section area %.3f ≈ thickness × developed length", p.Area)
			}
		}
	}
	minY, maxY, maxZ := math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, q := range apronLoop {
		minY, maxY, maxZ = math.Min(minY, q[0]), math.Max(maxY, q[0]), math.Max(maxZ, q[1])
	}
	support := 141 + 18.5 + 3
	if !approx(minY, 0, 1e-6) || !approx(maxZ, support/cosT+150, 1e-3) || !approx(maxY, 150*cosT+0.6*sinT, 0.01) {
		t.Fatalf("apron extent y %.4f..%.4f z max %.4f", minY, maxY, maxZ)
	}
	// along-wall cut through the sheet: crests every 76 mm, height (18+0.5)/cosθ
	along := SectionPlane{Origin: [3]float64{0, 600, 0}, Normal: [3]float64{0, 1, 0}, Up: [3]float64{0, 0, 1}}
	sa, _ := Section(ir, cat, along)
	var sheet [][2]float64
	for _, p := range sa.Parts {
		if p.Type == TypeCorrugatedSheet {
			sheet = p.Loops[0]
		}
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, q := range sheet {
		lo, hi = math.Min(lo, q[1]), math.Max(hi, q[1])
	}
	if want := 18.5 / cosT; !approx(hi-lo, want, ir.ChordErrorMM+1e-3) {
		t.Fatalf("corrugation height in section %.4f, want %.4f", hi-lo, want)
	}
	crests := 0
	for i := range sheet {
		p, q, r := sheet[(i+len(sheet)-1)%len(sheet)], sheet[i], sheet[(i+1)%len(sheet)]
		if q[1] > p[1] && q[1] >= r[1] && q[1] > hi-(ir.ChordErrorMM+0.05) {
			crests++
		}
	}
	if crests < 30 || crests > 32 {
		t.Fatalf("crests in an along-wall cut: %d (2400/76 ≈ 31.6)", crests)
	}
	// oblique: a near-horizontal plane through the outer wythe's footprint
	n := vunit(V3{0.05, 0.03, 1})
	sob, _ := Section(ir, cat, SectionPlane{Origin: [3]float64{1200, -50, 0}, Normal: [3]float64(n), Up: [3]float64{0, 1, 0}})
	if got, _ := partArea(sob, a.byRole("masonry:outer").ID); !approx(got, 2400*100/n[2], 1e-6*240000) {
		t.Fatalf("oblique wythe area %.6f want %.6f", got, 2400*100/n[2])
	}
	for _, p := range sob.Parts {
		for _, l := range p.Loops {
			if len(l) < 3 {
				t.Fatal("degenerate loop")
			}
		}
	}
	// coplanar: the plane IS the insulation/underlayment interface → reported
	// once (by the layer below)
	N := roofNormal(a)
	cop, _ := Section(ir, cat, SectionPlane{Origin: [3]float64(vmul(N, 101)), Normal: [3]float64(N), Up: [3]float64{1, 0, 0}})
	insA, insL := partArea(cop, a.firstOf(TypeInsulation).ID)
	_, ulL := partArea(cop, a.firstOf(TypeUnderlayment).ID)
	if insL != 1 || ulL != 0 || !approx(math.Abs(insA), 2400*1800/cosT, 1) {
		t.Fatalf("coplanar interface: insulation loops %d area %.1f, underlayment loops %d", insL, insA, ulL)
	}
	// tangent: a horizontal plane at the wall top touches the wythes' top
	// faces exactly; a hair above touches nothing
	top, _ := Section(ir, cat, SectionPlane{Origin: [3]float64{0, 0, 700}, Normal: [3]float64{0, 0, 1}, Up: [3]float64{0, 1, 0}})
	if got, _ := partArea(top, a.byRole("masonry:outer").ID); !approx(got, 240000, 1e-6) {
		t.Fatalf("tangent at the wall top: %.3f", got)
	}
	above, _ := Section(ir, cat, SectionPlane{Origin: [3]float64{0, 0, 700.01}, Normal: [3]float64{0, 0, 1}, Up: [3]float64{0, 1, 0}})
	if len(above.Parts) != 0 {
		t.Fatalf("above everything: %d parts", len(above.Parts))
	}
	if _, err := Section(ir, cat, SectionPlane{Normal: [3]float64{0, 0, 1}, Up: [3]float64{0, 0, 2}}); err == nil {
		t.Fatal("up parallel to normal accepted")
	}
}

// The committed golden of the standard section's numbers (areas, chain).
func TestConstructionSectionGolden(t *testing.T) {
	a, cat := fixtureAssembly()
	ir, _ := Compile(a, cat)
	sec, _ := Section(ir, cat, StandardSection(a, 300))
	StandardChains(sec, a)
	type row struct {
		Component string  `json:"component"`
		Type      string  `json:"type"`
		Loops     int     `json:"loops"`
		Area      float64 `json:"areaMm2"`
	}
	g := struct {
		Plane  SectionPlane     `json:"plane"`
		Bounds [4]float64       `json:"bounds"`
		Parts  []row            `json:"parts"`
		Chains []DimensionChain `json:"chains"`
	}{Plane: sec.Plane, Bounds: sec.Bounds, Chains: sec.Dimensions}
	for _, p := range sec.Parts {
		g.Parts = append(g.Parts, row{p.Component, p.Type, len(p.Loops), round3(p.Area)})
	}
	b, _ := json.MarshalIndent(g, "", "  ")
	path := filepath.Join("testdata", "roof-wall", "sections.json")
	if *updateGoldens {
		os.WriteFile(path, append(b, '\n'), 0o644)
	}
	have, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(bytes.TrimSpace(have), bytes.TrimSpace(b)) {
		t.Fatalf("section golden changed or missing (%v); review and rerun with -update", err)
	}
}

// SVG and PDF are two serialisations of one drawing: physical units, true
// paper scale (100 mm at 1:5 = 20 mm = 56.693 pt), hatches, the notice, no
// raster, deterministic; hostile text is escaped; nothing is fitted to the
// page.
func TestConstructionSVGPDFScaleSafetyDeterminism(t *testing.T) {
	_, st, id := templateProblem(t)
	a := st.Assemblies[id]
	a.Name = `Draft <script>alert(1)</script> & "quotes" — (paren) \ test`
	d, sec, ir, err := DrawingFor(st, a, st.Revision("assembly:"+id), st.Validation[id], nil, DrawingOptions{Paper: "A3", Scale: 5})
	if err != nil {
		t.Fatal(err)
	}
	svg := d.SVG(a.Name)
	if err := CheckSVG(svg); err != nil {
		t.Fatalf("our own SVG fails the sanitizer: %v", err)
	}
	s := string(svg)
	for _, want := range []string{`width="420mm" height="297mm" viewBox="0 0 420 297"`, `data-scale="1:5"`, ir.Hash, "not approved for construction", `data-hatch="masonry"`, `data-hatch="insulation"`, "&lt;script&gt;"} {
		if !strings.Contains(s, want) {
			t.Fatalf("svg lacks %q", want)
		}
	}
	scaleBar := false
	for _, l := range d.Lines {
		if l.Kind == "scale" && approx(math.Hypot(l.B[0]-l.A[0], l.B[1]-l.A[1]), 20, 1e-6) {
			scaleBar = true
		}
	}
	if !scaleBar {
		t.Fatal("100 mm must print as 20 mm at 1:5")
	}
	for _, bad := range []string{`<svg><script>x</script></svg>`, `<svg onload="x"></svg>`, `<svg><foreignObject/></svg>`, `<svg><a href="https://x.example">x</a></svg>`, `<svg><image href="data:x"/></svg>`} {
		if CheckSVG([]byte(bad)) == nil {
			t.Fatalf("sanitizer accepted %s", bad)
		}
	}
	pdf := d.PDF(a.Name)
	ps := string(pdf)
	for _, want := range []string{"%PDF-1.7", "/MediaBox [0 0 1190.551 841.89]", "/BaseFont /Helvetica", "(Draft <script>alert\\(1\\)</script> & \"quotes\" - \\(paren\\) \\\\ test - section)", "not approved for construction"} {
		if !strings.Contains(ps, want) {
			t.Fatalf("pdf lacks %q", want)
		}
	}
	if strings.Contains(ps, "/Image") || strings.Contains(ps, "/XObject") || strings.Contains(ps, "CreationDate") {
		t.Fatal("pdf must be vector-only without timestamps")
	}
	// the scale bar in points: 20 mm × 72/25.4
	if !strings.Contains(ps, " 56.693 ") && !scaleInPDF(ps, 56.693) {
		t.Fatal("scale bar length in points")
	}
	if !bytes.Equal(pdf, d.PDF(a.Name)) || !bytes.Equal(svg, d.SVG(a.Name)) {
		t.Fatal("drawings must be deterministic")
	}
	if _, err := LayoutDrawing(sec, DrawingMeta{}, DrawingOptions{Paper: "A4", Scale: 5, Window: [4]float64{-500, -800, 4500, 900}}); err == nil || !strings.Contains(err.Error(), "does not fit") {
		t.Fatalf("an oversize window must be refused, not fitted: %v", err)
	}
	if _, err := LayoutDrawing(sec, DrawingMeta{}, DrawingOptions{Paper: "A4", Scale: 7}); err == nil {
		t.Fatal("unsupported scale accepted")
	}
}

// scaleInPDF finds a horizontal line of the given length in points.
func scaleInPDF(ps string, want float64) bool {
	for _, line := range strings.Split(ps, "\n") {
		var w, x0, y0, x1, y1 float64
		if n, _ := fmtSscan(line, &w, &x0, &y0, &x1, &y1); n == 5 && approx(y0, y1, 1e-6) && approx(math.Abs(x1-x0), want, 0.002) {
			return true
		}
	}
	return false
}
