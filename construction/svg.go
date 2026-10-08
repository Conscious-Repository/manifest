package construction

// SVG serialisation of a Drawing: physical units (width/height in mm and a
// viewBox in paper millimetres, so 1 user unit = 1 mm of paper), hatches as
// patterns, semantic component ids on every region. Only an allowlist of
// elements is ever written; CheckSVG refuses scripts, event handlers,
// foreignObject, entities and external references in any SVG we serve.

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func f3(x float64) string {
	s := strconv.FormatFloat(round3(x), 'f', -1, 64)
	if s == "-0" {
		return "0"
	}
	return s
}

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		default:
			if r < 0x20 && r != '\t' && r != '\n' {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

var svgFills = map[string]string{
	"void":       `fill="#ffffff" fill-opacity="0" stroke-dasharray="1 0.7"`,
	"masonry":    `fill="url(#h-masonry)"`,
	"insulation": `fill="url(#h-insulation)"`,
	"timber":     `fill="url(#h-timber)"`,
	"membrane":   `fill="#8c8c8c"`,
	"foam":       `fill="url(#h-foam)"`,
	"metal":      `fill="#3a3a3a"`,
	"fastener":   `fill="#111111"`,
	"solid-dark": `fill="#262626"`,
}

// SVG renders the drawing.
func (d *Drawing) SVG(title string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%smm" height="%smm" viewBox="0 0 %s %s" font-family="Helvetica, Arial, sans-serif" data-scale="1:%s" data-geometry-hash="%s">`+"\n",
		f3(d.Width), f3(d.Height), f3(d.Width), f3(d.Height), f3(d.Scale), xmlEscape(d.SourceHash))
	fmt.Fprintf(&b, "<title>%s</title>\n<desc>%s Scale 1:%s on %s; 1 user unit = 1 mm of paper.</desc>\n", xmlEscape(title), xmlEscape(NonApprovalNotice), f3(d.Scale), d.Paper)
	b.WriteString(`<defs>
<pattern id="h-masonry" patternUnits="userSpaceOnUse" width="1.6" height="1.6" patternTransform="rotate(45)"><rect width="1.6" height="1.6" fill="#f1e3dc"/><line x1="0" y1="0" x2="0" y2="1.6" stroke="#7a3a2a" stroke-width="0.25"/></pattern>
<pattern id="h-insulation" patternUnits="userSpaceOnUse" width="2.4" height="2.4"><rect width="2.4" height="2.4" fill="#fbf3cf"/><path d="M0 2.4 L1.2 0 L2.4 2.4" fill="none" stroke="#8a6d00" stroke-width="0.18"/></pattern>
<pattern id="h-timber" patternUnits="userSpaceOnUse" width="3" height="1.2"><rect width="3" height="1.2" fill="#f2e2c9"/><line x1="0" y1="0.6" x2="3" y2="0.6" stroke="#8a5a2a" stroke-width="0.15"/></pattern>
<pattern id="h-foam" patternUnits="userSpaceOnUse" width="1.2" height="1.2"><rect width="1.2" height="1.2" fill="#d9d9d9"/><circle cx="0.6" cy="0.6" r="0.18" fill="#555555"/></pattern>
</defs>
<rect id="paper" x="0" y="0" width="` + f3(d.Width) + `" height="` + f3(d.Height) + `" fill="#ffffff"/>
<g id="section" stroke="#111111" stroke-width="0.25" stroke-linejoin="round">
`)
	for _, f := range d.Fills {
		var path strings.Builder
		for _, loop := range f.Loops {
			for i, q := range loop {
				if i == 0 {
					path.WriteString("M")
				} else {
					path.WriteString(" L")
				}
				path.WriteString(f3(q[0]) + " " + f3(q[1]))
			}
			path.WriteString(" Z ")
		}
		fill := svgFills[f.Hatch]
		if fill == "" {
			fill = svgFills["solid-dark"]
		}
		fmt.Fprintf(&b, `<path d="%s" %s fill-rule="evenodd" data-component="%s" data-hatch="%s"/>`+"\n", strings.TrimSpace(path.String()), fill, xmlEscape(f.Component), f.Hatch)
	}
	b.WriteString("</g>\n<g id=\"annotation\" stroke=\"#111111\" fill=\"none\">\n")
	for _, l := range d.Lines {
		dash := ""
		if l.Dash {
			dash = ` stroke-dasharray="1 1"`
		}
		fmt.Fprintf(&b, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke-width="%s"%s data-kind="%s"/>`+"\n", f3(l.A[0]), f3(l.A[1]), f3(l.B[0]), f3(l.B[1]), f3(l.Width), dash, l.Kind)
	}
	b.WriteString("</g>\n<g id=\"text\" fill=\"#111111\">\n")
	for _, t := range d.Texts {
		anchor := t.Anchor
		if anchor == "" {
			anchor = "start"
		}
		weight := ""
		if t.Bold {
			weight = ` font-weight="bold"`
		}
		fmt.Fprintf(&b, `<text x="%s" y="%s" font-size="%s" text-anchor="%s"%s>%s</text>`+"\n", f3(t.At[0]), f3(t.At[1]), f3(t.Size), anchor, weight, xmlEscape(t.Text))
	}
	b.WriteString("</g>\n</svg>\n")
	return b.Bytes()
}

var (
	svgElementRE = regexp.MustCompile(`<\s*([a-zA-Z][a-zA-Z0-9:-]*)`)
	svgEventRE   = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	svgHrefRE    = regexp.MustCompile(`(?i)(xlink:)?href\s*=\s*["']([^"']*)["']`)
)

var svgAllowed = map[string]bool{"svg": true, "title": true, "desc": true, "defs": true, "pattern": true, "rect": true, "line": true,
	"path": true, "circle": true, "g": true, "text": true}

// CheckSVG refuses active or external content in an SVG.
func CheckSVG(b []byte) error {
	s := string(b)
	low := strings.ToLower(s)
	for _, bad := range []string{"<script", "foreignobject", "<!entity", "<!doctype", "javascript:", "<iframe", "<object", "<embed", "<use", "<image"} {
		if strings.Contains(low, bad) {
			return Invalid("svg contains forbidden content: " + bad)
		}
	}
	if svgEventRE.MatchString(s) {
		return Invalid("svg contains an event handler attribute")
	}
	for _, m := range svgHrefRE.FindAllStringSubmatch(s, -1) {
		if !strings.HasPrefix(m[2], "#") {
			return Invalid("svg references an external resource")
		}
	}
	for _, m := range svgElementRE.FindAllStringSubmatch(s, -1) {
		if !svgAllowed[strings.ToLower(m[1])] {
			return Invalid("svg element " + m[1] + " is not allowed")
		}
	}
	return nil
}
