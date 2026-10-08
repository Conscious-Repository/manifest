package construction

// One vector drawing IR for every 2D output (§5): a section laid out on a
// real paper size at a stated scale, in paper millimetres (origin top-left,
// y down). SVG and PDF are two serialisations of the same Drawing, so they
// cannot disagree; neither is ever a raster or a screenshot. Nothing is
// fitted to the page: a window that does not fit at the requested scale is
// refused, never silently shrunk.

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// DrawingGenerator names this drawing pipeline in derived records.
const DrawingGenerator = "construction-drawing/1"

// Paper sizes (landscape, mm).
var papers = map[string][2]float64{"A4": {297, 210}, "A3": {420, 297}, "A2": {594, 420}}

// Scales the drawing accepts (1:N).
var drawingScales = map[float64]bool{1: true, 2: true, 5: true, 10: true, 20: true, 25: true, 50: true}

// DrawingOptions select paper, scale and the section window to draw.
type DrawingOptions struct {
	Paper  string     `json:"paper"`  // A4 | A3 | A2 (landscape)
	Scale  float64    `json:"scale"`  // N for 1:N
	Window [4]float64 `json:"window"` // section mm: xmin, ymin, xmax, ymax ("" = auto)
	Title  string     `json:"title"`
}

// Drawing is the shared vector IR.
type Drawing struct {
	Paper      string            `json:"paper"`
	Width      float64           `json:"widthMm"`
	Height     float64           `json:"heightMm"`
	Scale      float64           `json:"scale"`
	Window     [4]float64        `json:"window"`
	Frame      [4]float64        `json:"frame"` // drawing area on paper (x, y, w, h)
	Fills      []DrawFill        `json:"fills"`
	Lines      []DrawLine        `json:"lines"`
	Texts      []DrawText        `json:"texts"`
	SourceHash string            `json:"sourceHash"`
	Meta       map[string]string `json:"meta"`
}

// DrawFill is a closed region (one component's loops) with a hatch.
type DrawFill struct {
	Component string         `json:"component"`
	Hatch     string         `json:"hatch"` // solid-dark | masonry | insulation | timber | membrane | void | metal | foam | fastener
	Loops     [][][2]float64 `json:"loops"`
}

type DrawLine struct {
	A, B  [2]float64
	Width float64
	Dash  bool
	Kind  string // outline | dim | tick | frame | scale | leader
}

type DrawText struct {
	At     [2]float64
	Size   float64 // mm (cap height ≈ 0.7 size)
	Text   string
	Anchor string // start | middle | end
	Bold   bool
}

func hatchFor(p SectionPart) string {
	switch {
	case p.Type == TypeCavitySpace || p.Type == TypeWeepSet:
		return "void"
	case p.Family == "masonry" || p.Family == "mortar":
		return "masonry"
	case p.Family == "insulation":
		return "insulation"
	case p.Family == "timber" || p.Family == "sheathing":
		return "timber"
	case p.Family == "membrane" || p.Family == "underlayment" || p.Family == "sealant":
		return "membrane"
	case p.Family == "closure":
		return "foam"
	case p.Family == "fastener":
		return "fastener"
	case p.Family == "sheet-flashing" || p.Family == "corrugated-metal-roof" || p.Family == "standing-seam-roof":
		return "metal"
	}
	return "solid-dark"
}

// DrawingMeta is the title-block content every drawing must carry.
type DrawingMeta struct {
	Title            string
	AssemblyName     string
	AssemblyID       string
	AssemblyRevision string
	GeometryHash     string
	Compiler         string
	Plane            SectionPlane
	Assumptions      []string
	IssueSummary     string
	TopIssues        []string
	Provenance       string
}

// LayoutDrawing places a section on paper at scale and adds outlines,
// hatches, dimension chains, labels, a scale bar and the title block.
func LayoutDrawing(sec *SectionResult, meta DrawingMeta, opt DrawingOptions) (*Drawing, error) {
	if opt.Paper == "" {
		opt.Paper = "A4"
	}
	ps, ok := papers[opt.Paper]
	if !ok {
		return nil, Invalid("paper must be A4, A3 or A2")
	}
	if !drawingScales[opt.Scale] {
		return nil, Invalid("scale must be one of 1, 2, 5, 10, 20, 25, 50 (1:N)")
	}
	const margin, titleH = 10.0, 44.0
	frame := [4]float64{margin, margin, ps[0] - 2*margin, ps[1] - 2*margin - titleH}
	win := opt.Window
	if win == [4]float64{} {
		win = autoWindow(sec, frame, opt.Scale)
	}
	if !finite(win[:]...) || win[2] <= win[0] || win[3] <= win[1] {
		return nil, Invalid("section window must be a non-empty rectangle")
	}
	w := (win[2] - win[0]) / opt.Scale
	h := (win[3] - win[1]) / opt.Scale
	if w > frame[2]+1e-9 || h > frame[3]+1e-9 {
		return nil, Invalid(fmt.Sprintf("the window (%.0f × %.0f mm) does not fit %s at 1:%g (%.0f × %.0f mm available); choose a smaller window, a smaller scale or larger paper — nothing is fitted to the page",
			win[2]-win[0], win[3]-win[1], opt.Paper, opt.Scale, frame[2]*opt.Scale, frame[3]*opt.Scale))
	}
	// centre the window in the frame; model y up → paper y down
	ox := frame[0] + (frame[2]-w)/2
	oy := frame[1] + (frame[3]-h)/2
	toPaper := func(q [2]float64) [2]float64 {
		return [2]float64{round4(ox + (q[0]-win[0])/opt.Scale), round4(oy + (win[3]-q[1])/opt.Scale)}
	}
	d := &Drawing{Paper: opt.Paper, Width: ps[0], Height: ps[1], Scale: opt.Scale, Window: win, Frame: frame, SourceHash: sec.GeometryHash,
		Meta: map[string]string{"assemblyId": meta.AssemblyID, "assemblyRevision": meta.AssemblyRevision, "geometryHash": meta.GeometryHash, "generator": DrawingGenerator}}
	inWin := func(loop [][2]float64) bool {
		for _, q := range loop {
			if q[0] >= win[0] && q[0] <= win[2] && q[1] >= win[1] && q[1] <= win[3] {
				return true
			}
		}
		return false
	}
	clip := func(loop [][2]float64) [][2]float64 { return clipPolygon(loop, win) }
	parts := append([]SectionPart{}, sec.Parts...)
	sort.SliceStable(parts, func(i, j int) bool { return hatchOrder(hatchFor(parts[i])) < hatchOrder(hatchFor(parts[j])) })
	for _, p := range parts {
		f := DrawFill{Component: p.Component, Hatch: hatchFor(p)}
		for _, loop := range p.Loops {
			if !inWin(loop) && !windowInside(loop, win) {
				continue
			}
			c := clip(loop)
			if len(c) < 3 {
				continue
			}
			pl := make([][2]float64, len(c))
			for i, q := range c {
				pl[i] = toPaper(q)
			}
			f.Loops = append(f.Loops, pl)
		}
		if len(f.Loops) > 0 {
			d.Fills = append(d.Fills, f)
		}
	}
	// labels: one per sizeable part, at its largest loop's centroid — unless
	// a dimension chain already names it (avoids stacked duplicate labels)
	chained := map[string]bool{}
	for _, ch := range sec.Dimensions {
		for _, iv := range ch.Intervals {
			chained[iv.Component] = true
		}
	}
	for _, p := range parts {
		if (p.Void && p.Type != TypeCavitySpace) || chained[p.Component] {
			continue
		}
		best, area := -1, 0.0
		for i, loop := range p.Loops {
			if a := math.Abs(signedArea2(v2s(loop))); a > area {
				best, area = i, a
			}
		}
		if best < 0 || area/(opt.Scale*opt.Scale) < 4 { // under 4 mm² of paper: dimensioned instead
			continue
		}
		c := centroid(p.Loops[best])
		if c[0] < win[0] || c[0] > win[2] || c[1] < win[1] || c[1] > win[3] {
			continue
		}
		at := toPaper(c)
		d.Texts = append(d.Texts, DrawText{At: at, Size: 2.2, Text: shortName(p.Name), Anchor: "middle"})
	}
	// dimension chains (true section lengths), offset to the side of the line
	for ci, ch := range sec.Dimensions {
		for k, iv := range ch.Intervals {
			if !(inWin([][2]float64{iv.A}) && inWin([][2]float64{iv.B})) {
				continue
			}
			a, b := toPaper(iv.A), toPaper(iv.B)
			d.Lines = append(d.Lines, DrawLine{A: a, B: b, Width: 0.18, Kind: "dim"})
			// ticks perpendicular to the chain
			dx, dy := b[0]-a[0], b[1]-a[1]
			l := math.Hypot(dx, dy)
			if l < 1e-9 {
				continue
			}
			nx, ny := -dy/l*1.2, dx/l*1.2
			for _, p := range [][2]float64{a, b} {
				d.Lines = append(d.Lines, DrawLine{A: [2]float64{round4(p[0] - nx), round4(p[1] - ny)}, B: [2]float64{round4(p[0] + nx), round4(p[1] + ny)}, Width: 0.18, Kind: "tick"})
			}
			txt := trimFloat(iv.Length)
			switch iv.Label {
			case "illustrative":
				txt += " illus."
			case "unresolved":
				txt += " UNRESOLVED"
			}
			// alternate sides so neighbouring labels never stack
			side := 3.0 + float64(ci)
			if k%2 == 1 {
				side = -side - 1.5
			}
			tx := (a[0]+b[0])/2 + (-dy/l)*side
			ty := (a[1]+b[1])/2 + (dx/l)*side
			anchor := "start"
			if k%2 == 1 && math.Abs(dx) > math.Abs(dy) {
				anchor = "middle"
			}
			d.Texts = append(d.Texts, DrawText{At: [2]float64{round4(tx + 1.5), round4(ty)}, Size: 1.8, Text: txt + " " + shortName(iv.Name), Anchor: anchor})
		}
	}
	// frame + title block
	fx, fy, fw, fh := frame[0], frame[1], frame[2], frame[3]
	rect := func(x, y, w, h float64, kind string) {
		d.Lines = append(d.Lines, DrawLine{A: [2]float64{x, y}, B: [2]float64{x + w, y}, Width: 0.35, Kind: kind},
			DrawLine{A: [2]float64{x + w, y}, B: [2]float64{x + w, y + h}, Width: 0.35, Kind: kind},
			DrawLine{A: [2]float64{x + w, y + h}, B: [2]float64{x, y + h}, Width: 0.35, Kind: kind},
			DrawLine{A: [2]float64{x, y + h}, B: [2]float64{x, y}, Width: 0.35, Kind: kind})
	}
	rect(fx, fy, fw, fh, "frame")
	tb := fy + fh + 3
	rect(fx, tb, fw, titleH-3, "frame")
	line := func(i int, size float64, s string, bold bool) {
		d.Texts = append(d.Texts, DrawText{At: [2]float64{fx + 3, round4(tb + 4.5 + float64(i)*4.2)}, Size: size, Text: s, Bold: bold})
	}
	title := strings.TrimSpace(opt.Title)
	if title == "" {
		title = meta.Title
	}
	line(0, 3.0, title+" — section", true)
	line(1, 2.0, fmt.Sprintf("%s · assembly %s rev %s · geometry %s · %s", meta.AssemblyName, short(meta.AssemblyID, 12), short(meta.AssemblyRevision, 12), short(meta.GeometryHash, 12), meta.Compiler), false)
	line(2, 2.0, fmt.Sprintf("Scale 1:%g on %s landscape (%g × %g mm). Units: mm. Plane origin %v normal %v up %v (%s).", opt.Scale, opt.Paper, ps[0], ps[1], fmtVec(meta.Plane.Origin), fmtVec(meta.Plane.Normal), fmtVec(meta.Plane.Up), CoordinateConvention), false)
	line(3, 2.0, "Issues: "+meta.IssueSummary, false)
	if len(meta.TopIssues) > 0 {
		line(4, 1.8, "Critical: "+strings.Join(meta.TopIssues, " | "), false)
	}
	line(5, 1.8, "Assumptions: "+strings.Join(meta.Assumptions, " | "), false)
	line(6, 1.8, "Provenance: "+meta.Provenance+" Labels: 'illus.' = illustrative test-only assumption; UNRESOLVED = unknown, drawn with a placeholder.", false)
	line(7, 2.2, NonApprovalNotice, true)
	// scale bar: 100 model mm
	bar := 100.0 / opt.Scale
	bx, by := fx+fw-bar-8, fy+fh-6
	d.Lines = append(d.Lines, DrawLine{A: [2]float64{round4(bx), round4(by)}, B: [2]float64{round4(bx + bar), round4(by)}, Width: 0.5, Kind: "scale"},
		DrawLine{A: [2]float64{round4(bx), round4(by - 1.5)}, B: [2]float64{round4(bx), round4(by + 1.5)}, Width: 0.3, Kind: "scale"},
		DrawLine{A: [2]float64{round4(bx + bar), round4(by - 1.5)}, B: [2]float64{round4(bx + bar), round4(by + 1.5)}, Width: 0.3, Kind: "scale"})
	d.Texts = append(d.Texts, DrawText{At: [2]float64{round4(bx + bar/2), round4(by - 2.5)}, Size: 1.8, Text: fmt.Sprintf("100 mm at 1:%g", opt.Scale), Anchor: "middle"})
	return d, nil
}

func hatchOrder(h string) int {
	return map[string]int{"void": 0, "masonry": 1, "timber": 2, "insulation": 3, "membrane": 4, "foam": 5, "metal": 6, "fastener": 7, "solid-dark": 8}[h]
}

func short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func shortName(s string) string {
	if i := strings.Index(s, " ("); i > 0 {
		s = s[:i]
	}
	if len(s) > 34 {
		s = s[:33] + "…"
	}
	return s
}

func trimFloat(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	return strings.TrimSuffix(s, ".0")
}

func fmtVec(v [3]float64) string {
	return fmt.Sprintf("(%s, %s, %s)", trimFloat(v[0]), trimFloat(v[1]), trimFloat(v[2]))
}

func centroid(loop [][2]float64) [2]float64 {
	a, cx, cy := 0.0, 0.0, 0.0
	for i := range loop {
		p, q := loop[i], loop[(i+1)%len(loop)]
		cr := p[0]*q[1] - q[0]*p[1]
		a += cr
		cx += (p[0] + q[0]) * cr
		cy += (p[1] + q[1]) * cr
	}
	if math.Abs(a) < 1e-12 {
		return loop[0]
	}
	return [2]float64{cx / (3 * a), cy / (3 * a)}
}

// autoWindow centres the largest window that fits the frame at scale on the
// junction (the section's wall-side region), never larger than the section.
func autoWindow(sec *SectionResult, frame [4]float64, scale float64) [4]float64 {
	b := sec.Bounds
	w := math.Min(frame[2]*scale, b[2]-b[0])
	h := math.Min(frame[3]*scale, b[3]-b[1])
	// the junction sits near section x = 0 (the wall face) and the deck datum
	cx := math.Max(b[0]+w/2, math.Min(b[2]-w/2, 0+w*0.15))
	cy := math.Max(b[1]+h/2, math.Min(b[3]-h/2, 120))
	return [4]float64{round4(cx - w/2), round4(cy - h/2), round4(cx + w/2), round4(cy + h/2)}
}

func windowInside(loop [][2]float64, win [4]float64) bool {
	// the window lies inside the loop (a part bigger than the window)
	c := [2]float64{(win[0] + win[2]) / 2, (win[1] + win[3]) / 2}
	inside := false
	for i, j := 0, len(loop)-1; i < len(loop); j, i = i, i+1 {
		a, b := loop[i], loop[j]
		if (a[1] > c[1]) != (b[1] > c[1]) && c[0] < (b[0]-a[0])*(c[1]-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
	}
	return inside
}

// clipPolygon clips a polygon to the window rectangle (Sutherland–Hodgman).
func clipPolygon(loop [][2]float64, win [4]float64) [][2]float64 {
	out := loop
	edges := []struct {
		inside func(p [2]float64) bool
		cut    func(a, b [2]float64) [2]float64
	}{
		{func(p [2]float64) bool { return p[0] >= win[0] }, func(a, b [2]float64) [2]float64 {
			t := (win[0] - a[0]) / (b[0] - a[0])
			return [2]float64{win[0], a[1] + t*(b[1]-a[1])}
		}},
		{func(p [2]float64) bool { return p[0] <= win[2] }, func(a, b [2]float64) [2]float64 {
			t := (win[2] - a[0]) / (b[0] - a[0])
			return [2]float64{win[2], a[1] + t*(b[1]-a[1])}
		}},
		{func(p [2]float64) bool { return p[1] >= win[1] }, func(a, b [2]float64) [2]float64 {
			t := (win[1] - a[1]) / (b[1] - a[1])
			return [2]float64{a[0] + t*(b[0]-a[0]), win[1]}
		}},
		{func(p [2]float64) bool { return p[1] <= win[3] }, func(a, b [2]float64) [2]float64 {
			t := (win[3] - a[1]) / (b[1] - a[1])
			return [2]float64{a[0] + t*(b[0]-a[0]), win[3]}
		}},
	}
	for _, e := range edges {
		if len(out) == 0 {
			return out
		}
		var next [][2]float64
		for i := range out {
			cur, prev := out[i], out[(i+len(out)-1)%len(out)]
			ci, pi := e.inside(cur), e.inside(prev)
			switch {
			case ci && pi:
				next = append(next, cur)
			case ci && !pi:
				next = append(next, e.cut(prev, cur), cur)
			case !ci && pi:
				next = append(next, e.cut(prev, cur))
			}
		}
		out = next
	}
	return out
}
