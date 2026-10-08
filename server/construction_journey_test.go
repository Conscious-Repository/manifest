package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/construction"
)

// The integrated §4 journey on the real backend (P9): the 761 pilot is
// created from the Home context with the Home task scope; a synthetic
// stand-in drawing is retained (the historical PDF is never read); the
// roof-to-masonry junction is represented and edited (insulation, visible
// timber); 3D, a true section and the detail package are exported; research
// is cancelled mid-run and resumed; alternatives are compared; the agent
// proposes a decision, its approval is refused, the owner accepts it; the
// agent edits a draft through the native steward path; the private recovery
// bundle is exported; the original construction root is removed and the
// bundle restored into an empty root, where ids, revisions, history,
// evidence, views and exports come back exactly and geometry regenerates.
func TestConstructionJourneyHomePilotExportRestore(t *testing.T) {
	f, _ := nativeFixture(t)
	f.srv.construction.runner.Adapters[1].(*construction.FixtureAdapter).Delay = 1200 * time.Millisecond
	const home = "/api/home/construction"
	homeSub := construction.SubjectRef{Kind: "home", ID: "home"}
	// 1. the pilot, from Home, scoped to the shared Home task
	r := f.do(t, "POST", home+"/problems", map[string]any{"schemaVersion": 1, "requestId": "journey-create-1", "title": "761 N Euclid — Back Addition",
		"narrative": "SYNTHETIC journey fixture: a back addition's corrugated roof meets old brick. No real site data.", "template": construction.TemplateRoofMasonry,
		"scope": map[string]any{"taskId": "home/synthetic-back-addition"}})
	if r.Code != 200 {
		t.Fatalf("create %d %s", r.Code, r.Body)
	}
	v := r.json(t)["view"].(map[string]any)
	id := viewProblem(v)["id"].(string)
	asm := viewProblem(v)["alternatives"].([]any)[0].(string)
	if viewProblem(v)["subjectRef"].(map[string]any)["kind"] != "home" || v["context"].(map[string]any)["scope"].(map[string]any)["status"] != "resolved" {
		t.Fatalf("pilot bound to Home with its task: %v", v["context"])
	}
	// 2. a synthetic stand-in for the historical drawing (never the real PDF)
	pdf := []byte("%PDF-1.4\n% SYNTHETIC stand-in for the historical 761 drawing; not the real document\n%%EOF\n")
	v = f.upload(t, home, id, viewRev(v, "problem"), "journey-upload-1", "historical-drawing-standin.pdf", pdf, "drawing")
	cmd := func(path string, body map[string]any) map[string]any {
		t.Helper()
		body["schemaVersion"], body["problemId"] = 1, id
		if body["requestId"] == nil {
			body["requestId"] = cReqID("j")
		}
		res := f.do(t, "POST", home+"/problems/"+id+path, body)
		if res.Code != 200 {
			t.Fatalf("%s %d %s", path, res.Code, res.Body)
		}
		return res.json(t)["view"].(map[string]any)
	}
	asmOps := func(ops ...map[string]any) map[string]any {
		cur := f.do(t, "GET", home+"/problems/"+id, nil).json(t)
		return cmd("/commands", map[string]any{"assemblyId": asm, "expectedAssemblyRevision": viewRev(cur, "assembly:"+asm), "operations": ops})
	}
	// 3. the junction, insulation and visible timber
	ins := componentID(t, v, asm, construction.TypeInsulation)
	deck := componentID(t, v, asm, construction.TypeDecking)
	v = asmOps(map[string]any{"op": "SetJunctionStrategy", "orientation": "headwall", "strategy": "apron-surface-counterflashing", "newComponentIds": map[string]string{"apron": construction.NewID("cmp")}},
		map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 120, "unit": "mm"},
		map[string]any{"op": "SetAppearance", "componentId": deck, "appearance": "timber-rafter"})
	// 4. geometry, a true section, the package
	if g := f.do(t, "GET", home+"/problems/"+id+"/assemblies/"+asm+"/geometry", nil); g.Code != 200 {
		t.Fatalf("geometry %d", g.Code)
	}
	exportRec := map[string]any{}
	for _, format := range []string{"svg", "pdf", "glb", "package"} {
		rev := viewRev(f.do(t, "GET", home+"/problems/"+id, nil).json(t), "assembly:"+asm)
		e := f.do(t, "POST", home+"/problems/"+id+"/assemblies/"+asm+"/exports", map[string]any{"schemaVersion": 1, "requestId": "journey-export-" + format, "revision": rev, "format": format})
		if e.Code != 200 {
			t.Fatalf("export %s %d %s", format, e.Code, e.Body)
		}
		exportRec[format] = e.json(t)["record"]
		rec := exportRec[format].(map[string]any)
		t.Logf("export %s: %s sha256=%s bytes=%v assemblyRevision=%s geometryHash=%s", format, rec["name"], rec["revision"], rec["size"], rec["assemblyRevision"], rec["geometryHash"])
	}
	// 5. research: cancel mid-run, then resume
	cur := f.do(t, "GET", home+"/problems/"+id, nil).json(t)
	rr := f.do(t, "POST", home+"/problems/"+id+"/research-runs", map[string]any{"schemaVersion": 1, "requestId": "journey-run-1", "expectedProblemRevision": viewRev(cur, "problem"),
		"agent": map[string]any{"mode": "local-only"}, "baseAssembly": asm})
	if rr.Code != 200 {
		t.Fatalf("run %d %s", rr.Code, rr.Body)
	}
	runID := runOf(rr.json(t))["id"].(string)
	deadline := time.Now().Add(15 * time.Second)
	for {
		g := f.do(t, "GET", home+"/problems/"+id+"/research-runs/"+runID, nil).json(t)
		if stageOf(runOf(g), "acquire")["state"] == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("acquire never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if c := f.do(t, "POST", home+"/problems/"+id+"/research-runs/"+runID+"/cancel", map[string]any{"schemaVersion": 1, "requestId": "journey-cancel-1"}); c.Code != 200 {
		t.Fatalf("cancel %d", c.Code)
	}
	f.waitRunAt(t, home, id, runID, "cancelled")
	if c := f.do(t, "POST", home+"/problems/"+id+"/research-runs/"+runID+"/resume", map[string]any{"schemaVersion": 1, "requestId": "journey-resume-1"}); c.Code != 200 {
		t.Fatalf("resume %d %s", c.Code, c.Body)
	}
	done := f.waitRunAt(t, home, id, runID, "completed")
	pub := runOf(done)["publication"].(map[string]any)["assemblies"].([]any)
	if len(pub) != 3 {
		t.Fatalf("published %d", len(pub))
	}
	research := pub[0].(map[string]any)["id"].(string)
	// 6. compare the owner's draft with a research alternative
	cmp := f.do(t, "GET", home+"/problems/"+id+"/compare?a="+asm+"&b="+research, nil).json(t)["comparison"].(map[string]any)
	if cmp["same"] == true {
		t.Fatal("the research alternative differs from the draft")
	}
	// 7. the agent proposes; its approval is refused; the owner accepts
	capa, err := f.srv.grantConstructionAgent(homeSub, id, "alfred", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	dec := construction.NewID("dec")
	prop, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": "journey-agent-propose", "problemId": id,
		"operations": []map[string]any{{"op": "ProposeDecision", "decisionId": dec, "assemblyId": research, "title": "Proceed with the surface-counterflashing research alternative",
			"proposal": "Use it as the working detail for review; every open issue stays open."}}})
	if _, _, err := f.srv.constructionAgentCommand(capa.ID, prop); err != nil {
		t.Fatal(err)
	}
	st, _ := f.srv.construction.store.Load(homeSub, id)
	appr, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": "journey-agent-approve", "problemId": id,
		"operations": []map[string]any{{"op": "ApproveDecision", "decisionId": dec, "expectedDecisionRevision": st.Revision("decision:" + dec)}}})
	if _, _, err := f.srv.constructionAgentCommand(capa.ID, appr); construction.StatusOf(err) != 403 {
		t.Fatalf("agent approval: %v", err)
	}
	cmd("/decisions", map[string]any{"operations": []map[string]any{{"op": "ApproveDecision", "decisionId": dec, "expectedDecisionRevision": st.Revision("decision:" + dec)}}})
	// 8. the agent edits the draft through the native steward path
	ask := f.do(t, "POST", home+"/problems/"+id+"/agent/requests", map[string]any{"schemaVersion": 1, "requestId": "journey-ask-1", "text": "Increase the insulation to 150 mm.", "assemblyId": asm})
	if ask.Code != 200 {
		t.Fatalf("ask %d %s", ask.Code, ask.Body)
	}
	deadline = time.Now().Add(20 * time.Second)
	for {
		req := f.do(t, "GET", home+"/problems/"+id+"/agent/requests/journey-ask-1", nil).json(t)["request"].(map[string]any)
		if req["state"] == "completed" {
			break
		}
		if req["state"] == "failed" || time.Now().After(deadline) {
			t.Fatalf("steward request %v", req)
		}
		time.Sleep(50 * time.Millisecond)
	}
	f.srv.WaitConstructionRuns()
	// 9. the private recovery bundle (complete: native copies included)
	before := f.do(t, "GET", home+"/problems/"+id, nil).json(t)
	histBefore := f.do(t, "GET", home+"/problems/"+id+"/history", nil).json(t)["history"].([]any)
	ex := f.do(t, "GET", home+"/problems/"+id+"/export", nil)
	if ex.Code != 200 || ex.Hdr.Get("Content-Type") != "application/zip" || ex.Hdr.Get("X-Bundle-Complete") != "true" {
		t.Fatalf("recovery export %d %v", ex.Code, ex.Hdr)
	}
	man, files, err := construction.ReadBundle(bytes.NewReader(ex.Body), int64(len(ex.Body)))
	if err != nil {
		t.Fatal(err)
	}
	natives := 0
	for p := range files {
		if strings.HasPrefix(p, "native/alfred/") {
			natives++
		}
	}
	if natives != 1 || !man.Complete {
		t.Fatalf("the steward conversation is included: %d native, complete=%v", natives, man.Complete)
	}
	// 10. remove the original root; restore into an empty, isolated root
	f.srv.construction.store.Close()
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(privateTestDir(t), "restored-construction")
	rep, err := construction.Restore(bytes.NewReader(ex.Body), int64(len(ex.Body)), target, construction.RestoreOptions{Forbidden: []string{f.vault}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.srv.UseConstruction(target, ConstructionOptions{Forbidden: []string{f.vault}}); err != nil {
		t.Fatal(err)
	}
	f.nonce = ""
	after := f.do(t, "GET", home+"/problems/"+id, nil).json(t)
	for _, k := range []string{"generation"} {
		if fmt.Sprint(after[k]) != fmt.Sprint(before[k]) {
			t.Fatalf("%s %v vs %v", k, after[k], before[k])
		}
	}
	rb, _ := json.Marshal(before["revisions"])
	ra, _ := json.Marshal(after["revisions"])
	if !bytes.Equal(rb, ra) {
		t.Fatal("every document revision comes back exactly")
	}
	for _, part := range []string{"problem", "assemblies", "evidence", "decisions", "views", "catalog", "derived", "runs"} {
		pb, _ := json.Marshal(before[part])
		pa, _ := json.Marshal(after[part])
		if !bytes.Equal(pb, pa) {
			t.Fatalf("%s differs after restore", part)
		}
	}
	histAfter := f.do(t, "GET", home+"/problems/"+id+"/history", nil).json(t)["history"].([]any)
	if len(histAfter) != len(histBefore) || rep.Receipts != len(histBefore) {
		t.Fatalf("history %d vs %d", len(histAfter), len(histBefore))
	}
	// the selected revision still opens and its geometry regenerates; a
	// re-export of the same revision gives the same geometry hash
	sel := viewProblem(after)["selectedAssembly"].(map[string]any)
	if g := f.do(t, "GET", home+"/problems/"+id+"/assemblies/"+sel["id"].(string)+"/geometry?revision="+sel["revision"].(string), nil); g.Code != 200 {
		t.Fatalf("selected revision geometry after restore %d", g.Code)
	}
	old := exportRec["svg"].(map[string]any)
	re := f.do(t, "POST", home+"/problems/"+id+"/assemblies/"+asm+"/exports", map[string]any{"schemaVersion": 1, "requestId": "journey-reexport-svg", "revision": old["assemblyRevision"], "format": "svg"})
	if re.Code != 200 || re.json(t)["record"].(map[string]any)["geometryHash"] != old["geometryHash"] {
		t.Fatalf("re-export after restore: %d %s", re.Code, re.Body)
	}
	if dl := f.do(t, "GET", home+"/problems/"+id+"/artifacts/"+old["artifactId"].(string)+"?revision="+old["revision"].(string), nil); dl.Code != 200 || construction.Token(dl.Body) != old["revision"] {
		t.Fatalf("the original export downloads byte-exact after restore: %d", dl.Code)
	}
	f.assertSourcesUntouched(t)
}

// waitRunAt is waitRun for any subject base.
func (f *constructionFix) waitRunAt(t *testing.T, base, id, runID string, settled ...string) map[string]any {
	t.Helper()
	return f.waitRun(t, base, id, runID, settled...)
}

// privateTestDir is a 0700 directory to restore beneath: a restore refuses a
// parent that other accounts could write, and t.TempDir's numbered
// directories follow the umask.
func privateTestDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}
