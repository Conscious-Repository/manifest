package construction

// Vector PDF serialisation of a Drawing (same IR as the SVG). Paper
// millimetres map to points once: pt = mm·72/25.4 (a model length L at 1:N is
// L·72/25.4/N pt). Hatches are clipped vector line sets, text uses the base-14
// Helvetica fonts with every string escaped to printable WinAnsi-safe ASCII,
// and there is no raster, no timestamp and no embedded font program; the
// file ID is the SHA-256 of the content stream, so equal drawings give
// byte-identical files.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

const pdfPerMM = 72 / 25.4

func pnum(x float64) string {
	s := fmt.Sprintf("%.3f", x)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

var pdfTranslit = map[rune]string{'—': "-", '–': "-", '×': "x", '°': " deg", '…': "...", '→': "->", '←': "<-", '’': "'", '‘': "'", '“': "\"", '”': "\"", '·': "-", '≥': ">=", '≤': "<=", '±': "+/-", '²': "2", '³': "3"}

// pdfText makes a literal string safe: printable ASCII only, delimiters escaped.
func pdfText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if t, ok := pdfTranslit[r]; ok {
			b.WriteString(t)
			continue
		}
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

var pdfHatch = map[string]struct {
	fill    float64 // gray fill (−1 none)
	angle   float64 // hatch angle in degrees (NaN none)
	spacing float64 // mm
	cross   bool
}{
	"void":       {-1, math.NaN(), 0, false},
	"masonry":    {0.93, 45, 1.6, false},
	"insulation": {0.97, 60, 2.4, true},
	"timber":     {0.92, 0, 1.2, false},
	"membrane":   {0.55, math.NaN(), 0, false},
	"foam":       {0.85, math.NaN(), 0, false},
	"metal":      {0.23, math.NaN(), 0, false},
	"fastener":   {0.07, math.NaN(), 0, false},
	"solid-dark": {0.15, math.NaN(), 0, false},
}

// PDF renders the drawing as a single-page vector PDF.
func (d *Drawing) PDF(title string) []byte {
	H := d.Height
	X := func(mm float64) float64 { return mm * pdfPerMM }
	Y := func(mm float64) float64 { return (H - mm) * pdfPerMM }
	var c strings.Builder
	pathOf := func(loops [][][2]float64) string {
		var p strings.Builder
		for _, loop := range loops {
			for i, q := range loop {
				op := "l"
				if i == 0 {
					op = "m"
				}
				fmt.Fprintf(&p, "%s %s %s\n", pnum(X(q[0])), pnum(Y(q[1])), op)
			}
			p.WriteString("h\n")
		}
		return p.String()
	}
	c.WriteString("1 J 1 j\n")
	for _, f := range d.Fills {
		h, ok := pdfHatch[f.Hatch]
		if !ok {
			h = pdfHatch["solid-dark"]
		}
		path := pathOf(f.Loops)
		if h.fill >= 0 {
			fmt.Fprintf(&c, "%s g\n%sf*\n", pnum(h.fill), path)
		}
		if !math.IsNaN(h.angle) {
			mn, mx := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
			for _, loop := range f.Loops {
				for _, q := range loop {
					mn[0], mn[1] = math.Min(mn[0], q[0]), math.Min(mn[1], q[1])
					mx[0], mx[1] = math.Max(mx[0], q[0]), math.Max(mx[1], q[1])
				}
			}
			fmt.Fprintf(&c, "q\n%sW* n\n0.12 w 0.35 G\n", path)
			for _, ang := range append([]float64{h.angle}, map[bool][]float64{true: {180 - h.angle}, false: nil}[h.cross]...) {
				hatchLines(&c, mn, mx, ang, h.spacing, X, Y)
			}
			c.WriteString("Q\n")
		}
		dash := ""
		if f.Hatch == "void" {
			dash = "[1 0.7] 0 d "
		}
		fmt.Fprintf(&c, "%s0.25 w 0.07 G\n%sS [] 0 d\n", dash, path)
	}
	for _, l := range d.Lines {
		dash := ""
		if l.Dash {
			dash = "[2.8 2.8] 0 d "
		}
		fmt.Fprintf(&c, "%s%s w 0 G %s %s m %s %s l S [] 0 d\n", dash, pnum(l.Width*pdfPerMM), pnum(X(l.A[0])), pnum(Y(l.A[1])), pnum(X(l.B[0])), pnum(Y(l.B[1])))
	}
	c.WriteString("0 g\n")
	for _, t := range d.Texts {
		size := t.Size * pdfPerMM
		txt := pdfText(t.Text)
		x := X(t.At[0])
		approx := 0.52 * size * float64(len(txt))
		switch t.Anchor {
		case "middle":
			x -= approx / 2
		case "end":
			x -= approx
		}
		font := "F1"
		if t.Bold {
			font = "F2"
		}
		fmt.Fprintf(&c, "BT /%s %s Tf %s %s Td (%s) Tj ET\n", font, pnum(size), pnum(x), pnum(Y(t.At[1])), txt)
	}
	content := c.String()
	sum := sha256.Sum256([]byte(content))
	id := hex.EncodeToString(sum[:16])
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /Font << /F1 4 0 R /F2 5 0 R >> >> /Contents 6 0 R >>", pnum(d.Width*pdfPerMM), pnum(d.Height*pdfPerMM)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		fmt.Sprintf("<< /Title (%s) /Producer (%s) /Subject (%s) >>", pdfText(title), DrawingGenerator, pdfText(NonApprovalNotice)),
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
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info 7 0 R /ID [<%s> <%s>] >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, id, id, xref)
	return out.Bytes()
}

// hatchLines writes parallel lines at angle (deg) and spacing (mm) across a
// paper-mm bounding box (the caller has set the clip).
func hatchLines(c *strings.Builder, mn, mx [2]float64, angle, spacing float64, X, Y func(float64) float64) {
	th := angle * math.Pi / 180
	dir := [2]float64{math.Cos(th), math.Sin(th)}
	nrm := [2]float64{-dir[1], dir[0]}
	corners := [][2]float64{{mn[0], mn[1]}, {mx[0], mn[1]}, {mn[0], mx[1]}, {mx[0], mx[1]}}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range corners {
		v := p[0]*nrm[0] + p[1]*nrm[1]
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	diag := math.Hypot(mx[0]-mn[0], mx[1]-mn[1])
	cx, cy := (mn[0]+mx[0])/2, (mn[1]+mx[1])/2
	start := math.Floor(lo/spacing) * spacing
	for k := 0; start+float64(k)*spacing <= hi && k < 4000; k++ {
		off := start + float64(k)*spacing
		// a point on the line: centre shifted along the normal to offset
		base := (cx*nrm[0] + cy*nrm[1])
		px, py := cx+nrm[0]*(off-base), cy+nrm[1]*(off-base)
		ax, ay := px-dir[0]*diag, py-dir[1]*diag
		bx, by := px+dir[0]*diag, py+dir[1]*diag
		fmt.Fprintf(c, "%s %s m %s %s l S\n", pnum(X(ax)), pnum(Y(ay)), pnum(X(bx)), pnum(Y(by)))
	}
}
