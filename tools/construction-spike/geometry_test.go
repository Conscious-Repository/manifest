package constructionspike

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/spike-scene.json")

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// Two independent compilations produce identical GLB bytes and mesh hash.
func TestSpikeDeterministicGLB(t *testing.T) {
	a, b := SpikeScene().GLB(), SpikeScene().GLB()
	if !bytes.Equal(a, b) {
		t.Fatal("GLB bytes differ between two compilations of the same scene")
	}
	if SpikeScene().MeshJSON().Hash != SpikeScene().MeshJSON().Hash {
		t.Fatal("mesh hash differs between compilations")
	}
	sum := sha256.Sum256(a)
	t.Logf("glb bytes=%d sha256=%s", len(a), hex.EncodeToString(sum[:]))
}

// GLB header/chunks are well formed, every part is a named node, and the
// accessor bounds of the calibration cube are exactly 1 m on every glTF axis.
func TestSpikeGLBStructureAndCalibrationCube(t *testing.T) {
	glb := SpikeScene().GLB()
	var head [5]uint32
	if err := binary.Read(bytes.NewReader(glb[:20]), binary.LittleEndian, &head); err != nil {
		t.Fatal(err)
	}
	if head[0] != 0x46546C67 || head[1] != 2 || int(head[2]) != len(glb) || head[4] != 0x4E4F534A {
		t.Fatalf("bad GLB header %x", head)
	}
	js := glb[20 : 20+head[3]]
	var doc struct {
		Asset struct {
			Version string            `json:"version"`
			Extras  map[string]string `json:"extras"`
		} `json:"asset"`
		Nodes []struct {
			Name   string            `json:"name"`
			Mesh   int               `json:"mesh"`
			Extras map[string]string `json:"extras"`
		} `json:"nodes"`
		Accessors []struct {
			Min []float64 `json:"min"`
			Max []float64 `json:"max"`
		} `json:"accessors"`
		Buffers []struct {
			ByteLength int    `json:"byteLength"`
			URI        string `json:"uri"`
		} `json:"buffers"`
	}
	if err := json.Unmarshal(js, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Asset.Version != "2.0" || doc.Asset.Extras["toWorld"] == "" {
		t.Fatalf("asset %+v", doc.Asset)
	}
	if len(doc.Buffers) != 1 || doc.Buffers[0].URI != "" {
		t.Fatalf("buffers must be embedded, no external URI: %+v", doc.Buffers)
	}
	binStart := 20 + int(head[3])
	var binHead [2]uint32
	binary.Read(bytes.NewReader(glb[binStart:binStart+8]), binary.LittleEndian, &binHead)
	if binHead[1] != 0x004E4942 || int(binHead[0]) != doc.Buffers[0].ByteLength {
		t.Fatalf("bin chunk %x vs buffer %d", binHead, doc.Buffers[0].ByteLength)
	}
	want := []string{"corrugated-panel", "bent-flashing", "outer-wythe", "inner-wythe", "calibration-cube"}
	for i, n := range doc.Nodes {
		if n.Name != want[i] || n.Extras["part"] != want[i] {
			t.Fatalf("node %d = %+v, want %s", i, n, want[i])
		}
	}
	cube := doc.Accessors[3*4] // POSITION of the 5th part
	// world x 1000..2000, y 0..1000, z 0..1000 → glTF x 1..2, y 0..1, z -1..0
	wantMin, wantMax := []float64{1, 0, -1}, []float64{2, 1, 0}
	for i := 0; i < 3; i++ {
		if !near(cube.Min[i], wantMin[i], 1e-6) || !near(cube.Max[i], wantMax[i], 1e-6) {
			t.Fatalf("cube bounds min=%v max=%v", cube.Min, cube.Max)
		}
	}
}

// The glTF basis map is right-handed and invertible: +X stays +X, +Z (up)
// becomes +Y (up), +Y (outward) becomes -Z; a round trip returns the point.
func TestSpikeBasisMap(t *testing.T) {
	cases := map[V3][3]float32{{1000, 0, 0}: {1, 0, 0}, {0, 1000, 0}: {0, 0, -1}, {0, 0, 1000}: {0, 1, 0}}
	for in, want := range cases {
		if got := ToGLTF(in); got != want {
			t.Fatalf("ToGLTF(%v) = %v, want %v", in, got, want)
		}
		back := FromGLTF(ToGLTF(in))
		for i := range back {
			if !near(back[i], in[i], 1e-3) {
				t.Fatalf("round trip %v → %v", in, back)
			}
		}
	}
	// handedness: X×Y=Z in the world must stay X×Y=Z in glTF
	g := func(v V3) V3 { q := ToGLTF(mul(v, 1000)); return V3{float64(q[0]), float64(q[1]), float64(q[2])} }
	z := cross(g(V3{1, 0, 0}), g(V3{0, 1, 0}))
	if gz := g(V3{0, 0, 1}); z != gz {
		t.Fatalf("basis map is not right-handed: X×Y=%v but Z maps to %v", z, gz)
	}
}

// The corrugated panel's slope runs DOWN as Y increases, and its tessellation
// chord error is stated and small.
func TestSpikeSlopeDirectionAndChordError(t *testing.T) {
	panel, chord := CorrugatedPanel("p", 600, 76, 18, 0.5, 900, 10, 16)
	// every profile vertex is swept by the same vector: 900 mm down the
	// slope, i.e. +Y outward and -Z down
	m := len(panel.Positions) / 2
	wantY, wantZ := 900*math.Cos(10*math.Pi/180), -900*math.Sin(10*math.Pi/180)
	for i := 0; i < m; i++ {
		d := sub(panel.Positions[m+i], panel.Positions[i])
		if !near(d[0], 0, 1e-9) || !near(d[1], wantY, 1e-9) || !near(d[2], wantZ, 1e-9) {
			t.Fatalf("vertex %d swept by %v, want (0, %.3f, %.3f)", i, d, wantY, wantZ)
		}
	}
	if chord <= 0 || chord > 0.3 {
		t.Fatalf("chord error %.4f mm", chord)
	}
	t.Logf("corrugation chord error %.4f mm at 16 segments per 76 mm wave", chord)
}

// An axis-aligned cut through the calibration cube is exactly 1000 × 1000.
func TestSpikeSectionCubeExact(t *testing.T) {
	sc := Scene{Solids: []Solid{Box("cube", V3{0, 0, 0}, V3{1000, 1000, 1000})}}
	loops := Section(sc, Plane{Origin: V3{500, 0, 0}, Normal: V3{1, 0, 0}, Up: V3{0, 0, 1}})
	if len(loops) != 1 || len(loops[0].Points) != 4 {
		t.Fatalf("loops %+v", loops)
	}
	if a := loops[0].Area(); !near(a, 1e6, 1e-6) {
		t.Fatalf("area %.6f, want 1e6 (CCW, solid on the left)", a)
	}
}

// A cut exactly along a shared face (coplanar case) reports the boundary
// once — for the solid below, by the positive-side rule — and never twice.
func TestSpikeSectionCoplanarFace(t *testing.T) {
	sc := Scene{Solids: []Solid{
		Box("lower", V3{0, 0, 0}, V3{100, 100, 50}),
		Box("upper", V3{0, 0, 50}, V3{100, 100, 100}),
	}}
	loops := Section(sc, Plane{Origin: V3{0, 0, 50}, Normal: V3{0, 0, 1}, Up: V3{0, 1, 0}})
	if len(loops) != 1 || loops[0].Part != "lower" || !near(math.Abs(loops[0].Area()), 1e4, 1e-6) {
		t.Fatalf("coplanar cut loops %+v", loops)
	}
}

// An arbitrary oblique plane through the wythes and the flashing gives closed,
// non-degenerate loops; through the box wythes the area matches the analytic
// value for an oblique slice of a prism.
func TestSpikeObliqueSection(t *testing.T) {
	sc := SpikeScene()
	n := unit(V3{0.35, 0.2, 1})
	p := Plane{Origin: V3{300, -50, 100}, Normal: n, Up: V3{0, 1, 0}}
	loops := Section(sc, p)
	byPart := map[string][]Loop{}
	for _, l := range loops {
		if len(l.Points) < 3 {
			t.Fatalf("degenerate loop %+v", l)
		}
		if l.Area() <= 0 {
			t.Fatalf("loop for %s is not CCW: area %.3f", l.Part, l.Area())
		}
		byPart[l.Part] = append(byPart[l.Part], l)
	}
	for _, part := range []string{"outer-wythe", "inner-wythe", "bent-flashing"} {
		if len(byPart[part]) == 0 {
			t.Fatalf("no section loop for %s; got %v", part, keys(byPart))
		}
	}
	// a plane z = c - ax - by cuts the 600 x 100 footprint of each wythe;
	// the slice area is footprint / |n_z| when the plane stays inside the
	// wythe's height everywhere over the footprint
	want := 600 * 100 / n[2]
	if got := byPart["outer-wythe"][0].Area(); !near(got, want, 1e-6*want) {
		t.Fatalf("outer wythe oblique area %.6f, want %.6f", got, want)
	}
}

// The PDF is vector (path operators, no image XObject), carries the scale
// statement and a dimension label, has a valid xref, and is deterministic.
// At 1:5, 100 mm of model prints as 20 mm = 56.693 pt.
func TestSpikeVectorPDF(t *testing.T) {
	loops := Section(SpikeScene(), Plane{Origin: V3{300, 0, 0}, Normal: V3{1, 0, 0}, Up: V3{0, 0, 1}})
	a := PDF(loops, 5, V2{0, -50}, V2{-100, -50}, "100 (illustrative)")
	b := PDF(loops, 5, V2{0, -50}, V2{-100, -50}, "100 (illustrative)")
	if !bytes.Equal(a, b) {
		t.Fatal("PDF not deterministic")
	}
	s := string(a)
	for _, want := range []string{"%PDF-1.7", "/BaseFont /Helvetica", " m\n", " l\n", "(100 \\(illustrative\\)) Tj", "Scale 1:5 on A4", "%%EOF"} {
		if !strings.Contains(s, want) {
			t.Fatalf("PDF lacks %q", want)
		}
	}
	if strings.Contains(s, "/Image") || strings.Contains(s, "/XObject") {
		t.Fatal("PDF must not contain raster images")
	}
	// the dimension endpoints are 100 model mm apart → 20 paper mm at 1:5
	re := regexp.MustCompile(`0\.18 w ([0-9.]+) ([0-9.]+) m ([0-9.]+) ([0-9.]+) l S`)
	m := re.FindStringSubmatch(s)
	if m == nil {
		t.Fatal("no dimension path")
	}
	x0, x1 := atof(m[1]), atof(m[3])
	if got, want := math.Abs(x1-x0), 100*72/25.4/5; !near(got, want, 0.002) {
		t.Fatalf("100 mm at 1:5 printed as %.3f pt, want %.3f", got, want)
	}
	// xref offsets point at "N 0 obj"
	xref := strings.Index(s, "xref\n")
	for i, line := range strings.Split(s[xref:], "\n")[3:8] {
		off, err := strconv.Atoi(strings.Fields(line)[0])
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(s[off:], itoa(i+1)+" 0 obj") {
			t.Fatalf("xref entry %d → %q", i+1, s[off:off+10])
		}
	}
	// text cannot escape its literal
	if got := pdfEscape(`a) Tj (b\ é`); got != `a\) Tj \(b\\ ?` {
		t.Fatalf("pdfEscape = %q", got)
	}
}

// The committed browser fixture (viewer.cjs input) is exactly what the
// compiler produces today; -update rewrites it.
func TestSpikeSceneFixtureCurrent(t *testing.T) {
	b, _ := json.Marshal(SpikeScene().MeshJSON())
	path := filepath.Join("testdata", "spike-scene.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	have, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -run TestSpikeSceneFixtureCurrent -update)", err)
	}
	if !bytes.Equal(have, b) {
		t.Fatal("testdata/spike-scene.json is stale; rerun with -update")
	}
}

func keys(m map[string][]Loop) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
func atof(s string) float64 {
	var f float64
	json.Unmarshal([]byte(s), &f)
	return f
}
func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
