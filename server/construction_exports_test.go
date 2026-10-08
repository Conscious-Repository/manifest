package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"manifest/construction"
)

// Exports are same-version derivatives: GLB, section SVG, vector PDF and the
// detail package all record the exact assembly revision and geometry hash,
// download byte-exact from the private membership-checked route, replay to
// the same record, and refuse a revision outside the assembly's history.
func TestConstructionExportsSameVersion(t *testing.T) {
	f := constructionFixture(t)
	v, id, asm := f.createTemplate(t, fixtureBase, "create-exp-0001")
	rev0 := viewRev(v, "assembly:"+asm)
	model0 := v["assemblies"].(map[string]any)[asm].(map[string]any)["modelHash"].(string)
	base := fixtureBase + "/problems/" + id + "/assemblies/" + asm
	export := func(requestID, format, rev string) cResp {
		return f.do(t, "POST", base+"/exports", map[string]any{"schemaVersion": 1, "requestId": requestID, "revision": rev, "format": format})
	}
	got := map[string][]byte{}
	for _, format := range []string{"glb", "svg", "pdf", "package"} {
		r := export("export-"+format+"-0001", format, rev0)
		if r.Code != 200 {
			t.Fatalf("%s export %d %s", format, r.Code, r.Body)
		}
		j := r.json(t)
		rec := j["record"].(map[string]any)
		if rec["assemblyRevision"] != rev0 || rec["geometryHash"] != model0 || rec["format"] != format {
			t.Fatalf("%s record %v", format, rec)
		}
		dl := f.do(t, "GET", strings.TrimPrefix(j["url"].(string), ""), nil)
		if dl.Code != 200 || construction.Token(dl.Body) != rec["revision"] || dl.Hdr.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(dl.Hdr.Get("Content-Disposition"), "attachment") {
			t.Fatalf("%s download %d %v", format, dl.Code, dl.Hdr)
		}
		got[format] = dl.Body
	}
	if err := construction.CheckSVG(got["svg"]); err != nil || !strings.Contains(string(got["svg"]), model0) {
		t.Fatalf("svg %v", err)
	}
	if !bytes.HasPrefix(got["pdf"], []byte("%PDF-1.7")) || bytes.Contains(got["pdf"], []byte("/Image")) {
		t.Fatal("pdf must be vector")
	}
	if sum, err := construction.ParseGLB(got["glb"]); err != nil || sum.Extras["geometryHash"] != model0 {
		t.Fatalf("glb %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(got["package"]), int64(len(got["package"])))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, zf := range zr.File {
		names[zf.Name] = true
	}
	if !names["manifest.json"] || !names["model.glb"] || !names["section.pdf"] {
		t.Fatalf("package entries %v", names)
	}
	// replay returns the same record; different content under the id is 409
	again := export("export-glb-0001", "glb", rev0).json(t)["record"].(map[string]any)
	if again["revision"] == nil || again["artifactId"] == nil {
		t.Fatal("replay record")
	}
	if r := export("export-glb-0001", "svg", rev0); r.Code != 409 {
		t.Fatalf("request id reuse: %d", r.Code)
	}
	if r := export("export-bad-rev1", "glb", construction.Token([]byte("not a revision"))); r.Code != 404 {
		t.Fatalf("foreign revision: %d", r.Code)
	}
	// after an edit, exporting the OLD revision pins the old geometry
	ins := componentID(t, v, asm, construction.TypeInsulation)
	c := f.do(t, "POST", base+"/commands", map[string]any{"schemaVersion": 1, "requestId": "cmd-exp-ins-1", "problemId": id, "assemblyId": asm,
		"expectedAssemblyRevision": rev0, "operations": []any{map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"}}})
	if c.Code != 200 {
		t.Fatalf("edit %d %s", c.Code, c.Body)
	}
	v2 := c.json(t)["view"].(map[string]any)
	rev1 := viewRev(v2, "assembly:"+asm)
	old := export("export-old-0001", "glb", rev0).json(t)["record"].(map[string]any)
	cur := export("export-cur-0001", "glb", rev1).json(t)["record"].(map[string]any)
	if old["geometryHash"] != model0 || cur["geometryHash"] == model0 || old["assemblyRevision"] != rev0 || cur["assemblyRevision"] != rev1 {
		t.Fatalf("exports must follow the requested revision: old %v cur %v", old, cur)
	}
	// the export commits changed no physical model (no assembly revision)
	after := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	if viewRev(after, "assembly:"+asm) != rev1 {
		t.Fatal("exports must not create assembly revisions")
	}
	// section preview: inert SVG and JSON, scoped to the project
	sv := f.do(t, "GET", base+"/section?revision="+rev1, nil)
	if sv.Code != 200 || sv.Hdr.Get("Content-Type") != "image/svg+xml" || !strings.Contains(sv.Hdr.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("section svg %d %v", sv.Code, sv.Hdr)
	}
	sj := f.do(t, "GET", base+"/section?format=json&nx=0&ny=1&nz=0&oy=600&ux=0&uy=0&uz=1", nil)
	var sec struct {
		Section construction.SectionResult `json:"section"`
	}
	if err := json.Unmarshal(sj.Body, &sec); err != nil || sj.Code != 200 || len(sec.Section.Parts) == 0 {
		t.Fatalf("section json %d %v", sj.Code, err)
	}
	if r := f.do(t, "GET", base+"/section?nx=abc", nil); r.Code != 422 {
		t.Fatalf("bad section parameter %d", r.Code)
	}
	if r := f.do(t, "GET", "/api/properties/fixture-second-house/construction/problems/"+id+"/assemblies/"+asm+"/section", nil); r.Code != 404 {
		t.Fatalf("cross-project section %d", r.Code)
	}
	f.assertSourcesUntouched(t)
}
