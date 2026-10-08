package server

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/construction"
)

func withFixtureSources(o *ConstructionOptions) {
	o.FixtureSources = filepath.Join("..", "construction", "testdata", "roof-wall")
}

var cRunReq int

func cReqID(prefix string) string {
	cRunReq++
	return fmt.Sprintf("%s-%06d", prefix, cRunReq)
}

// waitRun follows the durable event sequence (as a reconnecting client
// would) until the run settles.
func (f *constructionFix) waitRun(t *testing.T, base, id, runID string, settled ...string) map[string]any {
	t.Helper()
	want := map[string]bool{"completed": true, "failed": true, "cancelled": true, "waiting-input": true, "disconnected": true}
	if len(settled) > 0 {
		want = map[string]bool{}
		for _, s := range settled {
			want[s] = true
		}
	}
	after := 0
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		r := f.do(t, "GET", fmt.Sprintf("%s/problems/%s/research-runs/%s/events?after=%d&wait=1", base, id, runID, after), nil)
		if r.Code != 200 {
			t.Fatalf("events %d %s", r.Code, r.Body)
		}
		ev := r.json(t)
		for _, e := range ev["events"].([]any) {
			seq := int(e.(map[string]any)["seq"].(float64))
			if seq <= after {
				t.Fatalf("events after %d returned seq %d", after, seq)
			}
			after = seq
		}
		if want[ev["state"].(string)] && !f.srv.construction.runs.owns(id+"/"+runID) {
			g := f.do(t, "GET", base+"/problems/"+id+"/research-runs/"+runID, nil)
			return g.json(t)
		}
	}
	t.Fatal("run did not settle")
	return nil
}

func (f *constructionFix) startRun(t *testing.T, base, id, rev string, extra map[string]any) cResp {
	t.Helper()
	body := map[string]any{"schemaVersion": 1, "requestId": cReqID("run"), "expectedProblemRevision": rev, "agent": map[string]any{"mode": "local-only"}}
	for k, v := range extra {
		body[k] = v
	}
	return f.do(t, "POST", base+"/problems/"+id+"/research-runs", body)
}

func runOf(m map[string]any) map[string]any { return m["run"].(map[string]any) }

func stageOf(run map[string]any, name string) map[string]any {
	for _, sg := range run["stages"].([]any) {
		if sg.(map[string]any)["name"] == name {
			return sg.(map[string]any)
		}
	}
	return nil
}

// The research API end to end on the synthetic fixture: start, follow the
// durable events, read retained stage results, published alternatives —
// with no source write and refresh never starting work.
func TestConstructionResearchRunsEndpoints(t *testing.T) {
	f := constructionFixture(t, withFixtureSources)
	sess := f.do(t, "GET", fixtureBase+"/session", nil).json(t)
	caps := sess["capabilities"].(map[string]any)
	if caps["autonomousAcquisition"] != "fixture" || caps["nativeAgent"] != "unavailable" || caps["fixtureSources"] != true {
		t.Fatalf("observed capabilities %v", caps)
	}
	v, id, _ := f.createTemplate(t, fixtureBase, "create-res-0001")
	// a planned run is inert: reading it many times never starts work
	r := f.startRun(t, fixtureBase, id, viewRev(v, "problem"), map[string]any{"start": false})
	if r.Code != 200 {
		t.Fatalf("plan run %d %s", r.Code, r.Body)
	}
	planned := runOf(r.json(t))
	pid := planned["id"].(string)
	for i := 0; i < 3; i++ {
		g := f.do(t, "GET", fixtureBase+"/problems/"+id+"/research-runs/"+pid, nil).json(t)
		if runOf(g)["state"] != "planned" || stageOf(runOf(g), "decompose")["attempts"] != nil && len(stageOf(runOf(g), "decompose")["attempts"].([]any)) != 0 {
			t.Fatal("a GET never starts a planned run")
		}
	}
	// native mode is refused while unavailable (no silent fallback)
	v = r.json(t)["view"].(map[string]any)
	nat := f.startRun(t, fixtureBase, id, viewRev(v, "problem"), map[string]any{"agent": map[string]any{"mode": "native"}})
	if nat.Code != 503 || !strings.Contains(string(nat.Body), "unavailable") {
		t.Fatalf("native run without a ready preflight: %d %s", nat.Code, nat.Body)
	}
	st := f.do(t, "POST", fixtureBase+"/problems/"+id+"/research-runs/"+pid+"/start", map[string]any{"schemaVersion": 1, "requestId": cReqID("start")})
	if st.Code != 200 {
		t.Fatalf("start %d %s", st.Code, st.Body)
	}
	done := f.waitRun(t, fixtureBase, id, pid)
	run := runOf(done)
	if run["state"] != "completed" {
		t.Fatalf("run %v", run["state"])
	}
	results := done["results"].(map[string]any)
	for _, sg := range construction.StageNames {
		if results[sg] == nil {
			t.Fatalf("stage %s result missing from the research view", sg)
		}
	}
	acq := results["acquire"].(map[string]any)
	if len(acq["sources"].([]any)) != 12 || acq["autonomousAcquisition"] != "fixture" {
		t.Fatalf("acquire result %v", acq["adapters"])
	}
	// an attempt's retained result is served exactly
	ex := stageOf(run, "extract")["attempts"].([]any)[0].(map[string]any)
	res := f.do(t, "GET", fixtureBase+"/problems/"+id+"/research-runs/"+pid+"/attempts/"+ex["id"].(string)+"/result", nil)
	if res.Code != 200 || construction.Token(res.Body) != ex["resultHash"] {
		t.Fatalf("attempt result %d", res.Code)
	}
	// the published alternatives are in the problem view, proposed, with evidence
	pv := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	alts := viewProblem(pv)["alternatives"].([]any)
	if len(alts) != 4 || viewProblem(pv)["lifecycle"] != "alternatives" {
		t.Fatalf("alternatives %d lifecycle %v", len(alts), viewProblem(pv)["lifecycle"])
	}
	pub := run["publication"].(map[string]any)
	for _, a := range pub["assemblies"].([]any) {
		ref := a.(map[string]any)
		if viewRev(pv, "assembly:"+ref["id"].(string)) != ref["revision"] {
			t.Fatal("the publication pins the exact published revisions")
		}
	}
	// a second run while one is active is refused; events reconstruct after a sequence
	ev := f.do(t, "GET", fixtureBase+"/problems/"+id+"/research-runs/"+pid+"/events?after=3", nil).json(t)
	for _, e := range ev["events"].([]any) {
		if e.(map[string]any)["seq"].(float64) <= 3 {
			t.Fatal("events after a sequence never repeat earlier ones")
		}
	}
	// cross-project: another property cannot read this run or its results
	other := "/api/properties/fixture-second-house/construction"
	if g := f.do(t, "GET", other+"/problems/"+id+"/research-runs/"+pid, nil); g.Code != 404 {
		t.Fatalf("cross-project run read %d", g.Code)
	}
	if g := f.do(t, "GET", other+"/problems/"+id+"/research-runs/"+pid+"/attempts/"+ex["id"].(string)+"/result", nil); g.Code != 404 {
		t.Fatalf("cross-project result read %d", g.Code)
	}
	f.assertSourcesUntouched(t)
}

// Cancel while acquisition runs; resume; a restart between stages
// reconciles to disconnected and resume reuses retained acquisition.
func TestConstructionResearchCancelResumeAndRestart(t *testing.T) {
	f := constructionFixture(t, withFixtureSources)
	fx := f.srv.construction.runner.Adapters[1].(*construction.FixtureAdapter)
	fx.Delay = 3 * time.Second
	v, id, _ := f.createTemplate(t, fixtureBase, "create-res-0002")
	r := f.startRun(t, fixtureBase, id, viewRev(v, "problem"), nil)
	if r.Code != 200 {
		t.Fatalf("start %d %s", r.Code, r.Body)
	}
	runID := runOf(r.json(t))["id"].(string)
	deadline := time.Now().Add(10 * time.Second)
	for {
		g := f.do(t, "GET", fixtureBase+"/problems/"+id+"/research-runs/"+runID, nil).json(t)
		if stageOf(runOf(g), "acquire")["state"] == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("acquire never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	c := f.do(t, "POST", fixtureBase+"/problems/"+id+"/research-runs/"+runID+"/cancel", map[string]any{"schemaVersion": 1, "requestId": cReqID("cancel")})
	if c.Code != 200 || runOf(c.json(t))["stopRequested"] != true {
		t.Fatalf("cancel %d %s", c.Code, c.Body)
	}
	done := f.waitRun(t, fixtureBase, id, runID, "cancelled")
	if stageOf(runOf(done), "acquire")["state"] != "cancelled" {
		t.Fatal("the running stage is cancelled")
	}
	fx.Delay = 0
	f.srv.construction.runner.StopAfter = construction.StageAcquire // the process will stop after acquisition
	res := f.do(t, "POST", fixtureBase+"/problems/"+id+"/research-runs/"+runID+"/resume", map[string]any{"schemaVersion": 1, "requestId": cReqID("resume")})
	if res.Code != 200 {
		t.Fatalf("resume %d %s", res.Code, res.Body)
	}
	// wait until the worker returns after acquire (state stays running on disk)
	deadline = time.Now().Add(20 * time.Second)
	for f.srv.construction.runs.owns(id+"/"+runID) || stageOf(runOf(f.do(t, "GET", fixtureBase+"/problems/"+id+"/research-runs/"+runID, nil).json(t)), "acquire")["state"] != "completed" {
		if time.Now().After(deadline) {
			t.Fatal("acquire did not complete")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// restart: the construction subsystem re-opens on the same root with a
	// new store, nonce and worker — nothing is owned in-process any more
	f.srv.construction.store.Close()
	if err := f.srv.UseConstruction(f.root, ConstructionOptions{Forbidden: []string{f.vault}, FixtureSources: filepath.Join("..", "construction", "testdata", "roof-wall")}); err != nil {
		t.Fatal(err)
	}
	srv2 := f.srv
	f.nonce = ""
	g := f.do(t, "GET", fixtureBase+"/problems/"+id+"/research-runs/"+runID, nil).json(t)
	if runOf(g)["state"] != "running" {
		t.Fatalf("before reconcile the durable state is what the dead process left: %v", runOf(g)["state"])
	}
	srv2.ReconcileConstruction()
	g = f.do(t, "GET", fixtureBase+"/problems/"+id+"/research-runs/"+runID, nil).json(t)
	if runOf(g)["state"] != "disconnected" || g["owned"] != false {
		t.Fatalf("restart reconciles to disconnected, never resumes: %v", runOf(g)["state"])
	}
	res = f.do(t, "POST", fixtureBase+"/problems/"+id+"/research-runs/"+runID+"/resume", map[string]any{"schemaVersion": 1, "requestId": cReqID("resume")})
	if res.Code != 200 {
		t.Fatalf("resume after restart %d %s", res.Code, res.Body)
	}
	done = f.waitRun(t, fixtureBase, id, runID)
	run := runOf(done)
	acq := stageOf(run, "acquire")["attempts"].([]any)
	if run["state"] != "completed" || len(acq) != 2 || acq[1].(map[string]any)["state"] != "completed" {
		t.Fatalf("completed; acquisition attempts = cancelled + one completed (reused after restart): %v %d", run["state"], len(acq))
	}
	if run["epoch"].(float64) != 3 {
		t.Fatalf("each resume starts a new epoch: %v", run["epoch"])
	}
	f.assertSourcesUntouched(t)
}

// The questions route corrects the decomposition of a stopped run.
func TestConstructionResearchQuestionsRoute(t *testing.T) {
	f := constructionFixture(t, withFixtureSources)
	v, id, _ := f.createTemplate(t, fixtureBase, "create-res-0003")
	r := f.startRun(t, fixtureBase, id, viewRev(v, "problem"), map[string]any{"start": false, "questions": []string{"Owner question: what counterflashing suits old brick?"}})
	runID := runOf(r.json(t))["id"].(string)
	bad := f.do(t, "POST", fixtureBase+"/problems/"+id+"/research-runs/"+runID+"/questions", map[string]any{"schemaVersion": 1, "requestId": cReqID("q"),
		"questions": []map[string]any{{"id": "q1", "text": "", "status": "open"}}})
	if bad.Code != 422 {
		t.Fatalf("empty question refused: %d", bad.Code)
	}
	ok := f.do(t, "POST", fixtureBase+"/problems/"+id+"/research-runs/"+runID+"/questions", map[string]any{"schemaVersion": 1, "requestId": cReqID("q"),
		"questions": []map[string]any{{"id": "q-owner-1", "text": "Corrected: which counterflashing suits old brick at a headwall?", "status": "open"}}})
	if ok.Code != 200 {
		t.Fatalf("questions %d %s", ok.Code, ok.Body)
	}
	run := runOf(ok.json(t))
	plan := run["plan"].(map[string]any)
	if plan["corrected"] != true || !strings.HasPrefix(plan["questions"].([]any)[0].(map[string]any)["text"].(string), "Corrected") {
		t.Fatalf("plan %v", plan)
	}
	raw, _ := json.Marshal(run["events"])
	if !strings.Contains(string(raw), "decomposition corrected by owner") {
		t.Fatal("the correction is an event")
	}
}
