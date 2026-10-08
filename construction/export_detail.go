package construction

// The detail package (§9.1): a deliberate, redacted sharing artifact for ONE
// assembly revision — canonical assembly JSON, its validation report, an
// evidence/decision summary, a material/product schedule, the section as SVG
// and vector PDF, the model as GLB, a README with units, coordinates,
// assumptions, scale and the non-approval notice, and a manifest of every
// file's bytes and SHA-256. Every derivative is generated from the same
// revision and carries the same geometry hash. Property addresses/contacts,
// the private narrative, owner inputs, native conversations and other
// alternatives are omitted by default and the omissions are listed.

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// PackageGenerator names the detail-package exporter.
const PackageGenerator = "construction-detail-package/1"

// PackageOptions select the section and drawing of a package.
type PackageOptions struct {
	Plane   *SectionPlane  `json:"section,omitempty"`
	Drawing DrawingOptions `json:"drawing"`
}

// PackageFile is one manifest row.
type PackageFile struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// PackageManifest describes a detail package.
type PackageManifest struct {
	SchemaVersion    int               `json:"schemaVersion"`
	Kind             string            `json:"kind"`
	Generator        string            `json:"generator"`
	ProblemID        string            `json:"problemId"`
	AssemblyID       string            `json:"assemblyId"`
	AssemblyRevision string            `json:"assemblyRevision"`
	GeometryHash     string            `json:"geometryHash"`
	Compiler         string            `json:"compiler"`
	Units            string            `json:"units"`
	Convention       string            `json:"convention"`
	Drawing          map[string]any    `json:"drawing"`
	Audience         string            `json:"audience"`
	Omitted          []string          `json:"omitted"`
	Notice           string            `json:"notice"`
	Files            []PackageFile     `json:"files"`
	Versions         map[string]string `json:"versions"`
}

// DrawingFor builds the section drawing of one assembly revision.
func DrawingFor(st *State, a *Assembly, asmToken string, rep *ValidationReport, plane *SectionPlane, opt DrawingOptions) (*Drawing, *SectionResult, *GeometryIR, error) {
	ir, err := Compile(a, st.Catalog)
	if err != nil {
		return nil, nil, nil, err
	}
	p := StandardSection(a, defaultSectionOffset(a))
	if plane != nil {
		p = *plane
	}
	sec, err := Section(ir, st.Catalog, p)
	if err != nil {
		return nil, nil, nil, err
	}
	StandardChains(sec, a)
	meta := DrawingMeta{Title: a.Name, AssemblyName: a.Name, AssemblyID: a.ID, AssemblyRevision: asmToken, GeometryHash: ir.Hash,
		Compiler: ir.Compiler, Plane: p, Provenance: "dimensions and strategies are labelled assumptions until sourced; see evidence-summary.json"}
	for _, as := range a.Assumptions {
		if as.Status != "withdrawn" {
			meta.Assumptions = append(meta.Assumptions, as.Text)
		}
	}
	if rep != nil {
		meta.IssueSummary = fmt.Sprintf("%d critical-unresolved, %d advisory (%s)", rep.Counts.Critical, rep.Counts.Advisory, rep.RuleSet)
		for _, is := range rep.Issues {
			if is.Severity == SevCritical && is.Status != "resolved" && len(meta.TopIssues) < 4 {
				meta.TopIssues = append(meta.TopIssues, shortName(is.RuleKey)+": "+firstSentence(is.Message))
			}
		}
	} else {
		meta.IssueSummary = "no validation report"
	}
	d, err := LayoutDrawing(sec, meta, opt)
	if err != nil {
		return nil, nil, nil, err
	}
	return d, sec, ir, nil
}

func firstSentence(s string) string {
	if i := strings.IndexAny(s, ".;:"); i > 0 && i < 90 {
		return s[:i]
	}
	if len(s) > 90 {
		return s[:89] + "…"
	}
	return s
}

// defaultSectionOffset puts the standard section through a rafter.
func defaultSectionOffset(a *Assembly) float64 {
	if r := a.firstOf(TypeRafterArray); r != nil && r.Applicability != "inapplicable" {
		return r.param("offset")
	}
	return a.aparam("widthAlongWall") / 2
}

// DetailPackage builds the redacted ZIP for one assembly revision.
func DetailPackage(st *State, a *Assembly, asmToken string, rep *ValidationReport, opt PackageOptions) ([]byte, *PackageManifest, error) {
	if opt.Drawing.Paper == "" {
		opt.Drawing.Paper = "A3"
	}
	if opt.Drawing.Scale == 0 {
		opt.Drawing.Scale = 5
	}
	d, sec, ir, err := DrawingFor(st, a, asmToken, rep, opt.Plane, opt.Drawing)
	if err != nil {
		return nil, nil, err
	}
	title := a.Name + " — section"
	files := map[string][]byte{}
	asmJSON, err := Canonical(a)
	if err != nil {
		return nil, nil, err
	}
	files["assembly.json"] = asmJSON
	if rep != nil {
		b, _ := Canonical(rep)
		files["validation.json"] = b
	}
	files["section.svg"] = d.SVG(title)
	if err := CheckSVG(files["section.svg"]); err != nil {
		return nil, nil, err
	}
	files["section.pdf"] = d.PDF(title)
	glb, err := GLB(ir, map[string]string{"assemblyId": a.ID, "assemblyRevision": asmToken, "assemblyName": a.Name})
	if err != nil {
		return nil, nil, err
	}
	files["model.glb"] = glb
	secJSON, _ := Canonical(sec)
	files["section.json"] = secJSON
	files["evidence-summary.json"] = evidenceSummary(st, a, asmToken)
	files["bom.csv"] = bomCSV(st, a)
	files["README.md"] = []byte(packageReadme(a, asmToken, ir, d, rep))
	man := &PackageManifest{SchemaVersion: SchemaVersion, Kind: "construction.detail-package", Generator: PackageGenerator,
		ProblemID: a.ProblemID, AssemblyID: a.ID, AssemblyRevision: asmToken, GeometryHash: ir.Hash, Compiler: ir.Compiler,
		Units: "mm (GLB in metres, Y up: (x, z, -y)/1000)", Convention: CoordinateConvention,
		Drawing:  map[string]any{"paper": d.Paper, "scale": "1:" + trimFloat(d.Scale), "window": d.Window, "plane": sec.Plane, "pdfPointsPerModelMm": pdfPerMM / d.Scale},
		Audience: "deliberate sharing (redacted)",
		Omitted: []string{"property address, owner contacts and the subject record", "the problem's private narrative and owner facts",
			"owner inputs (photos, drawings, documents)", "native agent conversations and receipts", "credentials", "other alternatives and research-run internals"},
		Notice:   NonApprovalNotice,
		Versions: map[string]string{"schema": fmt.Sprint(SchemaVersion), "compiler": CompilerVersion, "rules": RuleSet, "glb": GLBGenerator, "drawing": DrawingGenerator, "package": PackageGenerator},
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		man.Files = append(man.Files, PackageFile{Path: p, Bytes: len(files[p]), SHA256: Token(files[p])})
	}
	mb, _ := json.MarshalIndent(man, "", "  ")
	files["manifest.json"] = append(mb, '\n')
	paths = append(paths, "manifest.json")
	sort.Strings(paths)
	z, err := deterministicZip(paths, files)
	if err != nil {
		return nil, nil, err
	}
	return z, man, nil
}

// deterministicZip writes entries in path order with a fixed timestamp.
func deterministicZip(paths []string, files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range paths {
		h := &zip.FileHeader{Name: p, Method: zip.Deflate, Modified: stamp}
		h.SetMode(0o600)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(files[p]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func evidenceSummary(st *State, a *Assembly, asmToken string) []byte {
	type ev struct {
		ID, Source, Quote, Page, Figure, Verification string
		Relation, Target                              string
	}
	out := map[string]any{"assemblyId": a.ID, "assemblyRevision": asmToken, "evidence": []ev{}, "claims": []any{}, "decisions": []any{}, "sources": []any{},
		"note": "Evidence links and quotes are as retained; a URL alone is not verified evidence. Applicability and engineering suitability remain specialist questions."}
	var evs []ev
	used := map[string]bool{}
	if st.Evidence != nil {
		byID := map[string]Evidence{}
		for _, e := range st.Evidence.Evidence {
			byID[e.ID] = e
		}
		for _, l := range a.EvidenceLinks {
			e, ok := byID[l.EvidenceID]
			if !ok {
				continue
			}
			used[e.SourceID] = true
			evs = append(evs, ev{ID: e.ID, Source: e.SourceID, Quote: e.Quote, Page: fmt.Sprint(e.Page) + " (" + e.PageLabel + ")", Figure: e.Figure, Verification: e.Verification, Relation: l.Relation, Target: l.Target})
		}
		var srcs []any
		for _, s := range st.Evidence.Sources {
			if used[s.ID] {
				srcs = append(srcs, map[string]any{"id": s.ID, "title": s.Title, "publisher": s.Publisher, "class": s.Class, "url": s.URL, "documentRevision": s.DocumentRev, "contentHash": s.ContentHash, "fictional": s.Fictional, "retrievedAt": s.RetrievedAt})
			}
		}
		if srcs != nil {
			out["sources"] = srcs
		}
		var claims []any
		for _, c := range st.Evidence.Claims {
			for _, e := range evs {
				if contains(c.Supporting, e.ID) || contains(c.Contradicting, e.ID) {
					claims = append(claims, map[string]any{"id": c.ID, "statement": c.Statement, "provenance": c.Provenance, "verification": c.Verification, "supporting": c.Supporting, "contradicting": c.Contradicting})
					break
				}
			}
		}
		if claims != nil {
			out["claims"] = claims
		}
	}
	if evs != nil {
		out["evidence"] = evs
	}
	var decs []any
	ids := make([]string, 0, len(st.Decisions))
	for id := range st.Decisions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := st.Decisions[id]
		if d.Assembly.ID != a.ID {
			continue
		}
		decs = append(decs, map[string]any{"id": d.ID, "title": d.Title, "status": d.Status, "assemblyRevision": d.Assembly.Revision, "rationale": d.Rationale, "openQuestions": d.OpenQuestions, "unresolvedIssues": d.UnresolvedIssues,
			"note": "accepted-for-project is a human project decision, never approval for construction"})
	}
	if decs != nil {
		out["decisions"] = decs
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return append(b, '\n')
}

func bomCSV(st *State, a *Assembly) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"component_id", "name", "type", "role", "applicability", "material", "material_revision", "product", "key_dimensions_mm", "dimension_labels"})
	for _, c := range a.Components {
		mat, matRev, prod := "", "", "generic — unspecified"
		if c.Material != nil {
			if m, ok := st.Catalog.material(c.Material.ID, c.Material.Revision); ok {
				mat = m.Name
			}
			matRev = fmt.Sprint(c.Material.Revision)
		}
		if c.Product != nil {
			if p, ok := st.Catalog.product(c.Product.ID, c.Product.Revision); ok {
				prod = p.Manufacturer + " " + p.Model
				if p.Fictional {
					prod += " (FICTIONAL FIXTURE)"
				}
			}
		}
		keys := make([]string, 0, len(c.Shape.Params))
		for k := range c.Shape.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var dims, labels []string
		for _, k := range keys {
			q := c.Shape.Params[k]
			v, _ := q.Effective()
			dims = append(dims, k+"="+trimFloat(v))
			if l := q.Label(); l != "" {
				labels = append(labels, k+":"+l)
			}
		}
		_ = w.Write([]string{c.ID, c.Name, c.Type, c.Role, c.Applicability, mat, matRev, prod, strings.Join(dims, "; "), strings.Join(labels, "; ")})
	}
	w.Flush()
	return buf.Bytes()
}

func packageReadme(a *Assembly, asmToken string, ir *GeometryIR, d *Drawing, rep *ValidationReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — detail package\n\n", a.Name)
	fmt.Fprintf(&b, "**%s**\n\n", NonApprovalNotice)
	fmt.Fprintf(&b, "- Assembly `%s`, revision `%s`\n- Geometry hash `%s` (%s) — the same in `model.glb`, `section.svg`, `section.pdf` and `section.json`\n", a.ID, asmToken, ir.Hash, ir.Compiler)
	fmt.Fprintf(&b, "- Units: millimetres. World frame %s: +X along the wall, +Y outward from the wall, +Z up; origin at the top of the timber deck at the wall face. `model.glb` is in metres, Y up: (x, z, -y)/1000.\n", CoordinateConvention)
	fmt.Fprintf(&b, "- Drawing: %s landscape at 1:%s (100 mm of model = %s mm of paper). Not fitted to the page; print at 100%%.\n", d.Paper, trimFloat(d.Scale), trimFloat(100/d.Scale))
	fmt.Fprintf(&b, "- Junction: %s / %s; wall: %s (%s).\n\n", a.Junction.Orientation, a.Junction.Strategy, a.Junction.WallCondition.Value, a.Junction.WallCondition.Provenance)
	b.WriteString("## Assumptions\n\n")
	for _, as := range a.Assumptions {
		fmt.Fprintf(&b, "- [%s] %s (%s)\n", as.Status, as.Text, as.Provenance)
	}
	b.WriteString("\nDimensions marked *illus.* are illustrative test-only assumptions; *UNRESOLVED* are unknown and drawn with a placeholder.\n\n## Issues\n\n")
	if rep != nil {
		fmt.Fprintf(&b, "%d critical-unresolved, %d advisory, %d informational (%s). Geometric validity is not physical appropriateness, code compliance or permission to build.\n\n", rep.Counts.Critical, rep.Counts.Advisory, rep.Counts.Informational, rep.RuleSet)
		for _, is := range rep.Issues {
			if is.Severity == SevCritical && is.Status != "resolved" {
				fmt.Fprintf(&b, "- %s — %s\n", is.RuleKey, is.Message)
			}
		}
	}
	b.WriteString("\n## Files\n\nSee `manifest.json` for every file's size and SHA-256 and for what was omitted from this package.\n")
	return b.String()
}
