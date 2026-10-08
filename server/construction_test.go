package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"manifest/artifacts"
	"manifest/construction"
	"manifest/goals"
	"manifest/ledger"
	"manifest/realestate"
	"manifest/record"
	"manifest/tasks"
	mvault "manifest/vault"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

const cHost = "127.0.0.1:7781"

// cPeer is the loopback TCP peer the fixture's requests come from: the routes
// answer only this machine (construction_auth.go).
const cPeer = "127.0.0.1:54321"

// constructionFixture is a hermetic server: a temp vault holding two synthetic
// properties and a synthetic shared Home task, a counting writer on every
// source store, a temp ledger and a private construction root outside the
// vault. Nothing here touches a real vault, provider or network.
type constructionFix struct {
	srv     *Server
	vault   string
	root    string
	writes  *atomic.Int64
	ledger  *ledger.Store
	nonce   string
	vaultFP map[string]string
}

func constructionFixture(t *testing.T, opts ...func(*ConstructionOptions)) *constructionFix {
	t.Helper()
	vault := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(vault, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prop, err := os.ReadFile(filepath.Join("..", "construction", "testdata", "roof-wall", "property.md"))
	if err != nil {
		t.Fatal(err)
	}
	write("system/realestate/properties/fixture-ooda-house.md", string(prop))
	write("system/realestate/properties/fixture-second-house.md", strings.Join([]string{"---", "categories: [property]",
		"address: 2 Fixture Way, Testville, ZZ 00000", "status: construction", "control: owned", "---", "",
		"# Fixture second house (synthetic)", "", "## rocks", "- [ ] Roof [work:: roof]", ""}, "\n"))
	write("to do.md", strings.Join([]string{"# To Do", "", "## Inbox", "- [ ] loose thing [added:: 2026-08-01]", "",
		"## Home", "- [ ] Synthetic back addition roof (fixture) [todo:: home/synthetic-back-addition] [added:: 2026-09-01]", ""}, "\n"))
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	writes := &atomic.Int64{}
	vw := vaultwriter.New(vault).WithZoneRoots("system", "extrinsic").Grant(
		vaultwriter.Capability{Name: "todos", Zone: record.ZoneKnowledge, Pattern: "to do*", Actor: vaultwriter.ActorUserAction},
		vaultwriter.Capability{Name: "realestate", Zone: record.ZoneSystem, Pattern: "system/realestate/**", Actor: vaultwriter.ActorUserAction},
	)
	todoWrite := vw.BindAbs("todos")
	write("goals.md", "# Goals\n\n## Home\n")
	gidx, err := mvault.NewIndex(mvault.Config{Root: vault, GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{index: ix}
	srv.goals = goals.NewStore(gidx, vault, "goals.md", func(string, []byte) error { writes.Add(1); return fmt.Errorf("fixture: goals are read-only") })
	srv.UseVault(vw)
	srv.UseTasks(tasks.NewStore(vault, "to do.md", func(p string, b []byte) error { writes.Add(1); return todoWrite(p, b) }))
	srv.realestate = realestate.New(ix)
	led, err := ledger.New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	srv.UseLedger(led)
	// the global chat/artifact registry exists too, so tests can prove the
	// generic routes are blind to construction content
	pool, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg, err := artifacts.NewRegistry(pool)
	if err != nil {
		t.Fatal(err)
	}
	srv.UseArtifacts(pool)
	srv.UseArtifactRegistry(reg)
	root := filepath.Join(t.TempDir(), "data", "construction")
	o := ConstructionOptions{Forbidden: []string{vault}}
	for _, m := range opts {
		m(&o)
	}
	if err := srv.UseConstruction(root, o); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.stopAllConstructionRuns(); srv.WaitConstructionRuns(); srv.construction.store.Close() })
	f := &constructionFix{srv: srv, vault: vault, root: root, writes: writes, ledger: led}
	f.vaultFP = f.fingerprint(t)
	return f
}

// fingerprint hashes every file in the vault (source immutability evidence).
func (f *constructionFix) fingerprint(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(f.vault, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		out[p] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// assertSourcesUntouched: zero writes through any source writer and every
// vault file byte-identical to the start of the test.
func (f *constructionFix) assertSourcesUntouched(t *testing.T) {
	t.Helper()
	if n := f.writes.Load(); n != 0 {
		t.Fatalf("source writer called %d times", n)
	}
	now := f.fingerprint(t)
	if len(now) != len(f.vaultFP) {
		t.Fatalf("vault file set changed: %d → %d", len(f.vaultFP), len(now))
	}
	for p, h := range f.vaultFP {
		if now[p] != h {
			t.Fatalf("vault file changed: %s", p)
		}
	}
}

type cResp struct {
	Code int
	Body []byte
	Hdr  http.Header
}

func (r cResp) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("decode %d %s: %v", r.Code, r.Body, err)
	}
	return m
}

// do issues a same-origin request from a loopback peer to the loopback host;
// mutations carry the session nonce.
func (f *constructionFix) do(t *testing.T, method, path string, body any, mutate ...func(*http.Request)) cResp {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "http://"+cHost+path, rd)
	req.Host = cHost
	req.RemoteAddr = cPeer
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if method != http.MethodGet {
		req.Header.Set("Origin", "http://"+cHost)
		req.Header.Set("Content-Type", "application/json")
		if f.nonce == "" {
			f.nonce = f.srv.construction.nonce
		}
		req.Header.Set("X-Construction-Nonce", f.nonce)
	}
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	return cResp{Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

const fixtureBase = "/api/properties/fixture-ooda-house/construction"

func (f *constructionFix) create(t *testing.T, base, requestID, title string, scope map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"schemaVersion": 1, "requestId": requestID, "title": title,
		"narrative": "SYNTHETIC FIXTURE — not a real site."}
	if scope != nil {
		body["scope"] = scope
	}
	r := f.do(t, "POST", base+"/problems", body)
	if r.Code != 200 {
		t.Fatalf("create %d %s", r.Code, r.Body)
	}
	return r.json(t)["view"].(map[string]any)
}

func viewProblem(v map[string]any) map[string]any { return v["problem"].(map[string]any) }
func viewRev(v map[string]any, key string) string {
	return v["revisions"].(map[string]any)[key].(string)
}

// Open Construction from the exact property → create → reload the exact
// durable record from a fresh server on the same root, with the budget,
// scope and tasks projected read-only and no source write.
func TestConstructionProblemCreateReloadAndContext(t *testing.T) {
	f := constructionFixture(t)
	s := f.do(t, "GET", fixtureBase+"/session", nil)
	if s.Code != 200 {
		t.Fatalf("session %d %s", s.Code, s.Body)
	}
	sess := s.json(t)
	if sess["nonce"] == "" || sess["principal"] != "owner" || sess["subject"].(map[string]any)["status"] != "resolved" {
		t.Fatalf("session %v", sess)
	}
	v := f.create(t, fixtureBase, "create-roof-0001", "Corrugated roof to masonry wall", map[string]any{"workId": "roof"})
	p := viewProblem(v)
	id := p["id"].(string)
	if !construction.ValidID("cp", id) || p["subjectRef"].(map[string]any)["id"] != "fixture-ooda-house" {
		t.Fatalf("problem %v", p)
	}
	ctx := v["context"].(map[string]any)
	scope := ctx["scope"].(map[string]any)
	if scope["status"] != "resolved" || scope["workId"] != "roof" || scope["changed"] != false {
		t.Fatalf("scope %v", scope)
	}
	if len(ctx["tasks"].([]any)) != 2 {
		t.Fatalf("scope tasks %v", ctx["tasks"])
	}
	if ctx["budget"] == nil {
		t.Fatal("budget projection missing")
	}
	// steward assignment is inert: no chat, no run
	if p["steward"].(map[string]any)["agent"] != "alfred" || len(p["conversations"].([]any)) != 0 || p["latestRun"] != nil {
		t.Fatalf("assignment must not create a conversation or run: %v", p)
	}
	// a second server on the same root (restart)
	f.srv.construction.store.Close()
	srv2 := &Server{index: f.srv.index, realestate: f.srv.realestate, tasksStore: f.srv.tasksStore}
	if err := srv2.UseConstruction(f.root, ConstructionOptions{Forbidden: []string{f.vault}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv2.construction.store.Close() })
	f.srv, f.nonce = srv2, ""
	r := f.do(t, "GET", fixtureBase+"/problems/"+id, nil)
	if r.Code != 200 {
		t.Fatalf("reload %d %s", r.Code, r.Body)
	}
	again := r.json(t)
	if viewProblem(again)["id"] != id || viewRev(again, "problem") != viewRev(v, "problem") {
		t.Fatal("reload did not return the exact durable record")
	}
	if r.Hdr.Get("ETag") == "" {
		t.Fatal("GET must carry the revision ETag")
	}
	list := f.do(t, "GET", fixtureBase+"/problems", nil).json(t)
	if n := len(list["problems"].([]any)); n != 1 {
		t.Fatalf("list %d", n)
	}
	f.assertSourcesUntouched(t)
	// the ledger projection landed (activity), without being commit authority
	day, _ := f.ledger.Day(f.ledger.Today())
	found := false
	for _, e := range day {
		found = found || (e.Source == "construction" && e.Object.ID == id)
	}
	if !found {
		t.Fatal("no construction ledger projection")
	}
}

// The same domain binds to the shared Home: a Home problem links a shared
// Home task by exact id and never writes the Home records.
func TestConstructionProblemHomeSubject(t *testing.T) {
	f := constructionFixture(t)
	v := f.create(t, "/api/home/construction", "create-home-0001", "Synthetic back addition — roof to masonry (fixture)",
		map[string]any{"taskId": "home/synthetic-back-addition"})
	p := viewProblem(v)
	if p["subjectRef"].(map[string]any)["kind"] != "home" || p["propertyRef"] != nil {
		t.Fatalf("home problem %v", p)
	}
	scope := v["context"].(map[string]any)["scope"].(map[string]any)
	if scope["status"] != "resolved" || scope["taskId"] != "home/synthetic-back-addition" || !strings.Contains(scope["text"].(string), "Synthetic back addition") {
		t.Fatalf("home scope %v", scope)
	}
	r := f.do(t, "POST", "/api/home/construction/problems", map[string]any{"schemaVersion": 1, "requestId": "create-home-0002", "title": "x",
		"scope": map[string]any{"taskId": "home/renamed-task"}})
	if r.Code != 422 {
		t.Fatalf("unresolved Home task must be refused: %d %s", r.Code, r.Body)
	}
	r = f.do(t, "POST", "/api/home/construction/problems", map[string]any{"schemaVersion": 1, "requestId": "create-home-0003", "title": "x",
		"scope": map[string]any{"workId": "roof"}})
	if r.Code != 422 {
		t.Fatalf("a Home problem cannot scope a property work node: %d", r.Code)
	}
	// a Home problem is invisible from a property and vice versa
	id := p["id"].(string)
	if r := f.do(t, "GET", fixtureBase+"/problems/"+id, nil); r.Code != 404 {
		t.Fatalf("home problem through a property URL: %d", r.Code)
	}
	f.assertSourcesUntouched(t)
}

// Loopback, Origin, Fetch-Metadata, nonce and principal checks refuse before
// any state is read or written.
func TestConstructionBoundaryGuards(t *testing.T) {
	f := constructionFixture(t)
	v := f.create(t, fixtureBase, "create-guard-01", "Guarded", nil)
	id := viewProblem(v)["id"].(string)
	cmd := map[string]any{"schemaVersion": 1, "requestId": "cmd-guard-001", "problemId": id, "expectedProblemRevision": viewRev(v, "problem"),
		"operations": []any{map[string]any{"op": "SetProblemText", "title": "changed"}}}
	cases := []struct {
		name   string
		method string
		mutate func(*http.Request)
		want   int
	}{
		{"non-loopback host (DNS rebinding)", "GET", func(r *http.Request) { r.Host = "attacker.example:7781" }, 403},
		{"remote peer", "GET", func(r *http.Request) { r.RemoteAddr = "100.101.102.103:41234" }, 403},
		{"cross-site fetch", "GET", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"foreign origin", "GET", func(r *http.Request) { r.Header.Set("Origin", "https://attacker.example") }, 403},
		{"missing nonce", "POST", func(r *http.Request) { r.Header.Del("X-Construction-Nonce") }, 403},
		{"wrong nonce", "POST", func(r *http.Request) { r.Header.Set("X-Construction-Nonce", strings.Repeat("0", 64)) }, 403},
		{"mutation without Origin", "POST", func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"top-level navigation mutation", "POST", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "none") }, 403},
		{"caller-asserted owner header ignored", "POST", func(r *http.Request) {
			r.Header.Del("X-Construction-Nonce")
			r.Header.Set("X-Construction-Principal", "owner")
		}, 403},
	}
	head := func() string {
		b, _ := os.ReadFile(filepath.Join(f.root, "projects", construction.ProjectKey(construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}), "problems", id, "head.json"))
		return string(b)
	}
	before := head()
	for _, c := range cases {
		path := fixtureBase + "/problems/" + id
		var body any
		if c.method == "POST" {
			path += "/commands"
			body = cmd
		}
		if r := f.do(t, c.method, path, body, c.mutate); r.Code != c.want {
			t.Fatalf("%s: %d %s", c.name, r.Code, r.Body)
		}
	}
	if head() != before {
		t.Fatal("a refused request changed the head")
	}
	// a tailnet Host is refused even from a loopback peer (no configured host
	// admits anything); a loopback GET without Fetch-Metadata passes
	if r := f.do(t, "GET", fixtureBase+"/session", nil, func(r *http.Request) { r.Host = "metis.example.ts.net" }); r.Code != 403 {
		t.Fatalf("tailnet host: %d", r.Code)
	}
	if r := f.do(t, "GET", fixtureBase+"/session", nil, func(r *http.Request) { r.Header.Del("Sec-Fetch-Site") }); r.Code != 200 {
		t.Fatalf("loopback tool GET: %d", r.Code)
	}
	// an injected resolver can deny (unauthenticated) or present an agent,
	// which browser routes refuse
	f.srv.construction.principal = func(constructionPeer) (construction.Actor, error) {
		return construction.Actor{}, construction.Forbidden("no trusted owner session")
	}
	if r := f.do(t, "GET", fixtureBase+"/problems", nil); r.Code != 403 {
		t.Fatalf("unauthenticated principal: %d", r.Code)
	}
	f.srv.construction.principal = func(constructionPeer) (construction.Actor, error) {
		return construction.AgentActor("alfred", "cap", ""), nil
	}
	if r := f.do(t, "POST", fixtureBase+"/problems/"+id+"/commands", cmd); r.Code != 403 {
		t.Fatalf("agent principal on a browser route: %d", r.Code)
	}
	f.srv.construction.principal = nil
	// disabled feature answers 503 and never panics
	off := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://"+cHost+fixtureBase+"/session", nil)
	req.Host = cHost
	off.Handler().ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Fatalf("disabled construction: %d", rec.Code)
	}
	if head() != before {
		t.Fatal("head changed")
	}
	f.assertSourcesUntouched(t)
}

// Construction routes exist only on the private handler: the team portal,
// the OODA deal-share wrapper and the bare web handler never route them.
func TestConstructionBoundaryNotOnPortalOrPublic(t *testing.T) {
	f := constructionFixture(t)
	portal, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	team := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	for name, h := range map[string]http.Handler{"portal": portal, "deal-share": f.srv.DealShareHandler(team), "web": WebHandler()} {
		for _, p := range []string{fixtureBase + "/session", fixtureBase + "/problems", "/api/home/construction/problems"} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "http://"+cHost+p, nil)
			req.Host = cHost
			h.ServeHTTP(rec, req)
			if rec.Code == 200 && strings.Contains(rec.Body.String(), "nonce") {
				t.Fatalf("%s handler exposed %s", name, p)
			}
			if strings.Contains(rec.Body.String(), f.srv.construction.nonce) {
				t.Fatalf("%s handler leaked the nonce", name)
			}
		}
	}
}

// Cross-project ids and artifact hashes resolve to 404, never another
// project's record; a body cannot switch the URL's problem.
func TestConstructionBoundaryCrossProjectHTTP(t *testing.T) {
	f := constructionFixture(t)
	a := f.create(t, fixtureBase, "create-xa-0001", "Corrugated roof to masonry wall", nil)
	b := f.create(t, "/api/properties/fixture-second-house/construction", "create-xb-0001", "Corrugated roof to masonry wall", nil)
	aid, bid := viewProblem(a)["id"].(string), viewProblem(b)["id"].(string)
	up := f.upload(t, fixtureBase, aid, viewRev(a, "problem"), "upload-xa-01", "synthetic.png", syntheticPNG(), "photo")
	in := viewProblem(up)["inputs"].([]any)[0].(map[string]any)
	art, rev := in["artifactId"].(string), in["revision"].(string)
	other := "/api/properties/fixture-second-house/construction"
	for _, p := range []string{other + "/problems/" + aid, other + "/problems/" + bid + "/artifacts/" + art + "?revision=" + rev,
		other + "/problems/" + aid + "/artifacts/" + art + "?revision=" + rev, "/api/home/construction/problems/" + aid,
		"/api/properties/no-such-house/construction/problems/" + aid} {
		if r := f.do(t, "GET", p, nil); r.Code != 404 {
			t.Fatalf("%s: %d %s", p, r.Code, r.Body)
		}
	}
	cmd := map[string]any{"schemaVersion": 1, "requestId": "cmd-switch-1", "problemId": aid, "expectedProblemRevision": viewRev(a, "problem"),
		"operations": []any{map[string]any{"op": "SetProblemText", "title": "hijack"}}}
	if r := f.do(t, "POST", other+"/problems/"+bid+"/commands", cmd); r.Code != 422 {
		t.Fatalf("body problemId switching the URL problem: %d", r.Code)
	}
	if r := f.do(t, "POST", other+"/problems/"+aid+"/commands", cmd); r.Code != 404 {
		t.Fatalf("A's problem through B's property: %d %s", r.Code, r.Body)
	}
	// a missing property cannot bind a new problem
	r := f.do(t, "POST", "/api/properties/no-such-house/construction/problems", map[string]any{"schemaVersion": 1, "requestId": "create-ghost-1", "title": "ghost"})
	if r.Code != 404 {
		t.Fatalf("problem for a missing property: %d", r.Code)
	}
	f.assertSourcesUntouched(t)
}

// Lost ACKs replay to the same commit; reuse with a different body is 409.
func TestConstructionReplayHTTP(t *testing.T) {
	f := constructionFixture(t)
	body := map[string]any{"schemaVersion": 1, "requestId": "create-rp-0001", "title": "Replay"}
	r1 := f.do(t, "POST", fixtureBase+"/problems", body)
	r2 := f.do(t, "POST", fixtureBase+"/problems", body)
	if r1.Code != 200 || r2.Code != 200 {
		t.Fatalf("%d %d", r1.Code, r2.Code)
	}
	v1, v2 := r1.json(t), r2.json(t)
	if viewProblem(v1["view"].(map[string]any))["id"] != viewProblem(v2["view"].(map[string]any))["id"] {
		t.Fatal("replayed create made a second problem")
	}
	if v1["receipt"].(map[string]any)["id"] != v2["receipt"].(map[string]any)["id"] {
		t.Fatal("replayed create returned a different receipt")
	}
	body["title"] = "Different"
	if r := f.do(t, "POST", fixtureBase+"/problems", body); r.Code != 409 {
		t.Fatalf("request id reuse: %d", r.Code)
	}
}

// Two concurrent editors with the same expected revision: one wins, the
// other gets 409 with the current revision, and nothing is merged silently.
func TestConstructionConcurrentHTTP(t *testing.T) {
	f := constructionFixture(t)
	v := f.create(t, fixtureBase, "create-cc-0001", "Concurrent", nil)
	id := viewProblem(v)["id"].(string)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = f.do(t, "POST", fixtureBase+"/problems/"+id+"/commands", map[string]any{"schemaVersion": 1, "requestId": fmt.Sprintf("cmd-cc-%04d", i),
				"problemId": id, "expectedProblemRevision": viewRev(v, "problem"),
				"operations": []any{map[string]any{"op": "SetProblemText", "narrative": fmt.Sprintf("editor %d", i)}}}).Code
		}(i)
	}
	wg.Wait()
	if !(codes[0] == 200 && codes[1] == 409 || codes[0] == 409 && codes[1] == 200) {
		t.Fatalf("codes %v", codes)
	}
}

func syntheticPNG() []byte {
	// a real PNG signature + IHDR so DetectContentType sees image/png
	return append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde"), []byte("synthetic fixture")...)
}

func (f *constructionFix) uploadRaw(t *testing.T, base, id, rev, requestID, name string, content []byte, role string) cResp {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("requestId", requestID)
	_ = mw.WriteField("expectedProblemRevision", rev)
	_ = mw.WriteField("role", role)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(content)
	mw.Close()
	return f.do(t, "POST", base+"/problems/"+id+"/inputs", buf.Bytes(), func(r *http.Request) { r.Header.Set("Content-Type", mw.FormDataContentType()) })
}

func (f *constructionFix) upload(t *testing.T, base, id, rev, requestID, name string, content []byte, role string) map[string]any {
	t.Helper()
	r := f.uploadRaw(t, base, id, rev, requestID, name, content, role)
	if r.Code != 200 {
		t.Fatalf("upload %s: %d %s", name, r.Code, r.Body)
	}
	return r.json(t)["view"].(map[string]any)
}
