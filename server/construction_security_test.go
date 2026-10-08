package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"manifest/construction"
)

// The §7 negative cases through the real routes: the access boundary,
// path encodings, IDOR and raw-hash reads, cross-project references,
// command unknowns, non-finite numbers, out-of-range dimensions, oversized
// bodies and operation counts, degenerate sections, URL sources never
// fetched (private/metadata targets included), no network client in the
// construction code, and export closure: one problem's bundle carries
// nothing of another's.
func TestConstructionSecurityNegativeCases(t *testing.T) {
	f := constructionFixture(t)
	vA, a, asmA := f.createTemplate(t, fixtureBase, "create-sec-0001")
	_, b, _ := f.createTemplate(t, fixtureBase, "create-sec-0002")
	vA = f.upload(t, fixtureBase, a, viewRev(vA, "problem"), "upload-sec-01", "note.txt", []byte("SYNTHETIC private note for problem A"), "document")
	inA := viewProblem(vA)["inputs"].([]any)[0].(map[string]any)
	expect := func(name string, r cResp, code int) {
		t.Helper()
		if r.Code != code {
			t.Fatalf("%s: want %d, got %d %s", name, code, r.Code, r.Body)
		}
	}
	// --- the access boundary ---
	expect("untrusted host", f.do(t, "GET", fixtureBase+"/problems", nil, func(r *http.Request) { r.Host = "evil.example" }), 403)
	expect("cross-site read", f.do(t, "GET", fixtureBase+"/problems", nil, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }), 403)
	expect("cross-origin write", f.do(t, "POST", fixtureBase+"/problems", map[string]any{}, func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }), 403)
	expect("missing nonce", f.do(t, "POST", fixtureBase+"/problems", map[string]any{}, func(r *http.Request) { r.Header.Del("X-Construction-Nonce") }), 403)
	expect("no-origin write", f.do(t, "POST", fixtureBase+"/problems", map[string]any{}, func(r *http.Request) { r.Header.Del("Origin") }), 403)
	expect("recovery export cross-site", f.do(t, "GET", fixtureBase+"/problems/"+a+"/export", nil, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }), 403)
	// --- path encodings and ids ---
	for _, p := range []string{
		"/api/properties/..%2f..%2fetc/construction/problems",
		"/api/properties/fixture-ooda-house/construction/problems/cp-..%2f..%2fhead",
		"/api/properties/fixture-ooda-house/construction/problems/" + a + "/assemblies/..%2fx",
		"/api/properties/FIXTURE-OODA-HOUSE/construction/problems/" + a,
	} {
		r := f.do(t, "GET", p, nil)
		if r.Code != 404 && r.Code != 400 && r.Code != 403 {
			t.Fatalf("path %s answered %d %s", p, r.Code, r.Body)
		}
	}
	// --- IDOR and raw-hash reads ---
	expect("input of A through B", f.do(t, "GET", fixtureBase+"/problems/"+b+"/artifacts/"+inA["artifactId"].(string)+"?revision="+inA["revision"].(string), nil), 404)
	expect("input of A through another property", f.do(t, "GET", "/api/properties/fixture-second-house/construction/problems/"+a+"/artifacts/"+inA["artifactId"].(string)+"?revision="+inA["revision"].(string), nil), 404)
	expect("input of A through Home", f.do(t, "GET", "/api/home/construction/problems/"+a, nil), 404)
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	stA, _ := f.srv.construction.store.Load(sub, a)
	docRef := stA.Head.Docs["problem"]
	expect("raw document revision of A through B", f.do(t, "GET", fixtureBase+"/problems/"+b+"/artifacts/"+docRef.ArtifactID+"?revision="+docRef.Revision, nil), 404)
	expect("generic artifact route blind", f.do(t, "GET", "/api/artifacts/"+docRef.ArtifactID, nil), 404)
	// --- cross-project references ---
	stB, _ := f.srv.construction.store.Load(sub, b)
	asmB := stB.Problem.Alternatives[0]
	cur := f.do(t, "GET", fixtureBase+"/problems/"+a, nil).json(t)
	expect("assembly of B in A's command", f.do(t, "POST", fixtureBase+"/problems/"+a+"/commands", map[string]any{"schemaVersion": 1, "requestId": cReqID("x"), "problemId": a,
		"assemblyId": asmB, "expectedAssemblyRevision": viewRev(f.do(t, "GET", fixtureBase+"/problems/"+b, nil).json(t), "assembly:"+asmB),
		"operations": []map[string]any{{"op": "SetPitch", "value": 12, "unit": "deg"}}}), 404)
	expect("problem id mismatch", f.do(t, "POST", fixtureBase+"/problems/"+a+"/commands", map[string]any{"schemaVersion": 1, "requestId": cReqID("x"), "problemId": b,
		"operations": []map[string]any{{"op": "SetLifecycle", "lifecycle": "archived"}}, "expectedProblemRevision": viewRev(cur, "problem")}), 422)
	expect("evidence of nowhere", f.asmCommand(t, a, asmA, map[string]any{"op": "LinkEvidence", "target": asmA, "evidenceId": construction.NewID("evd"), "relation": "supports"}), 404)
	// --- command unknowns, non-finite, ranges, sizes ---
	raw := func(body string) cResp { return f.do(t, "POST", fixtureBase+"/problems/"+a+"/commands", []byte(body)) }
	expect("unknown op", raw(`{"schemaVersion":1,"requestId":"sec-unknown-1","problemId":"`+a+`","operations":[{"op":"DropTables"}]}`), 422)
	expect("NaN literal", raw(`{"schemaVersion":1,"requestId":"sec-nan-0001","problemId":"`+a+`","assemblyId":"`+asmA+`","expectedAssemblyRevision":"`+viewRev(cur, "assembly:"+asmA)+`","operations":[{"op":"SetPitch","value":NaN,"unit":"deg"}]}`), 422)
	expect("overflowing number", raw(`{"schemaVersion":1,"requestId":"sec-inf-0001","problemId":"`+a+`","assemblyId":"`+asmA+`","expectedAssemblyRevision":"`+viewRev(cur, "assembly:"+asmA)+`","operations":[{"op":"SetPitch","value":1e999,"unit":"deg"}]}`), 422)
	ins := componentID(t, vA, asmA, construction.TypeInsulation)
	expect("negative thickness", f.asmCommand(t, a, asmA, map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": -5, "unit": "mm"}), 422)
	expect("excess thickness", f.asmCommand(t, a, asmA, map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 5, "unit": "m"}), 422)
	many := make([]map[string]any, construction.MaxOpsPerRequest+1)
	for i := range many {
		many[i] = map[string]any{"op": "SetPitch", "value": 10, "unit": "deg"}
	}
	expect("too many operations", f.asmCommand(t, a, asmA, many...), 422)
	expect("body too large", f.do(t, "POST", fixtureBase+"/problems/"+a+"/commands", bytes.Repeat([]byte(" "), construction.MaxCommandBytes+10)), 413)
	expect("degenerate section", f.do(t, "GET", fixtureBase+"/problems/"+a+"/assemblies/"+asmA+"/section?nx=0&ny=0&nz=0&ux=0&uy=0&uz=1", nil), 422)
	expect("section NaN parameter", f.do(t, "GET", fixtureBase+"/problems/"+a+"/assemblies/"+asmA+"/section?nx=NaN&ny=0&nz=1&ux=1&uy=0&uz=0", nil), 422)
	// --- URL sources are metadata only (private/metadata targets included) ---
	var hits atomic.Int32
	ext := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", 302)
	}))
	defer ext.Close()
	for _, u := range []string{ext.URL + "/redirects-to-metadata", "http://169.254.169.254/latest/meta-data/", "http://127.0.0.1:1/private", "http://[::1]/x"} {
		r := f.do(t, "POST", fixtureBase+"/problems/"+a+"/sources", map[string]any{"schemaVersion": 1, "requestId": cReqID("src"), "problemId": a,
			"operations": []map[string]any{{"op": "AddSource", "id": construction.NewID("src"), "title": "URL metadata " + u, "class": "secondary", "url": u}}})
		expect("url source "+u, r, 200)
	}
	if hits.Load() != 0 {
		t.Fatal("no source URL is ever fetched")
	}
	expect("credentialed url", f.do(t, "POST", fixtureBase+"/problems/"+a+"/sources", map[string]any{"schemaVersion": 1, "requestId": cReqID("src"), "problemId": a,
		"operations": []map[string]any{{"op": "AddSource", "id": construction.NewID("src"), "title": "x", "class": "secondary", "url": "https://user:pw@host/x"}}}), 422)
	for _, dir := range []string{filepath.Join("..", "construction"), "."} {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || (dir == "." && !strings.HasPrefix(name, "construction")) {
				continue
			}
			src, _ := os.ReadFile(filepath.Join(dir, name))
			for _, banned := range []string{"http.Get(", "http.Post(", "http.Client{", "http.DefaultClient", "net.Dial", "exec.Command("} {
				if strings.Contains(string(src), banned) {
					t.Fatalf("%s/%s uses %s: construction code never fetches or executes", dir, name, banned)
				}
			}
		}
	}
	// --- export closure: A's bundle carries nothing that only B owns ---
	ex := f.do(t, "GET", fixtureBase+"/problems/"+a+"/export", nil)
	expect("export A", ex, 200)
	membersB := map[string]bool{}
	for _, d := range stB.Head.Docs {
		membersB[d.Revision] = true
	}
	_, files, err := construction.ReadBundle(bytes.NewReader(ex.Body), int64(len(ex.Body)))
	if err != nil {
		t.Fatal(err)
	}
	for p := range files {
		if h := strings.TrimPrefix(p, "artifacts/blobs/"); h != p && membersB[h] && !sharedRevision(stA, h) {
			t.Fatalf("A's bundle leaks B's document %s", h[:12])
		}
	}
	var man construction.BundleManifest
	_ = json.Unmarshal(files["manifest.json"], &man)
	if man.ProblemID != a {
		t.Fatal("the bundle is A's")
	}
	f.assertSourcesUntouched(t)
}

// sharedRevision: identical bytes (e.g. the generic catalog seeded alike)
// can legitimately be members of both problems.
func sharedRevision(st *construction.State, h string) bool {
	for _, d := range st.Head.Docs {
		if d.Revision == h {
			return true
		}
	}
	return false
}
