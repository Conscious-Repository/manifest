package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"manifest/agentchat"
	"manifest/construction"
	"manifest/hermes"
)

// nativeFixture: the construction fixture plus a real agent-chat store and
// the real hermes Runner pointed at the protocol stub. Native construction
// steps are allowed with unverified checks — isolated fixture only.
func nativeFixture(t *testing.T, opts ...func(*ConstructionOptions)) (*constructionFix, string) {
	t.Helper()
	all := append([]func(*ConstructionOptions){withFixtureSources, func(o *ConstructionOptions) {
		o.AgentToolsets, o.AllowUnverifiedNative = "construction-none", true
	}}, opts...)
	f := constructionFixture(t, all...)
	src, err := os.ReadFile(filepath.Join("testdata", "construction-hermes-stub.py"))
	if err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(t.TempDir(), "hermes")
	if err := os.WriteFile(stub, src, 0o755); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	t.Setenv("CX_STUB_OUT", out)
	f.srv.UseHermes(hermes.NewRunner(hermes.Config{Enabled: true, Bin: stub}), "web,memory")
	var hosts HostsInfo
	hosts.Hermes.Enabled, hosts.Hermes.Bin = true, stub
	f.srv.UseHosts(hosts)
	f.srv.UseAgentChat(agentchat.New(filepath.Join(t.TempDir(), "artifacts", "chats")))
	return f, out
}

type stubReceived struct {
	Declared     string `json:"declared"`
	Computed     string `json:"computed"`
	Toolsets     string `json:"toolsets"`
	Model        string `json:"model"`
	Kind         string `json:"kind"`
	ChatPreamble bool   `json:"chatPreamble"`
}

func stubCalls(t *testing.T, dir string) []stubReceived {
	t.Helper()
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var out []stubReceived
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		var r stubReceived
		_ = json.Unmarshal(b, &r)
		out = append(out, r)
	}
	return out
}

func nativeRun(t *testing.T, f *constructionFix, id string, agent map[string]any) string {
	t.Helper()
	cur := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	r := f.startRun(t, fixtureBase, id, viewRev(cur, "problem"), map[string]any{"agent": agent})
	if r.Code != 200 {
		t.Fatalf("native run %d %s", r.Code, r.Body)
	}
	return runOf(r.json(t))["id"].(string)
}

func extractAttempt(run map[string]any) map[string]any {
	atts := stageOf(run, "extract")["attempts"].([]any)
	return atts[len(atts)-1].(map[string]any)
}

// One real native delivery path with the fake runner: the extract step is
// accepted, claimed and finished in the agent-chat store; the runner gets
// the exact retained packet (re-hashed by the stub) and only the bounded
// construction toolset, not the chat preamble; requested and observed model
// are kept apart; the passages it proposes are verified like any other.
func TestConstructionNativeResearchDelivery(t *testing.T) {
	f, out := nativeFixture(t)
	sess := f.do(t, "GET", fixtureBase+"/session", nil).json(t)
	if sess["capabilities"].(map[string]any)["nativeAgent"] != "available" {
		t.Fatalf("capabilities %v", sess["capabilities"])
	}
	pf := f.do(t, "GET", fixtureBase+"/preflight", nil).json(t)["preflight"].(map[string]any)
	checks := map[string]string{}
	for _, c := range pf["checks"].([]any) {
		cm := c.(map[string]any)
		checks[cm["name"].(string)] = cm["status"].(string)
	}
	if pf["ready"] != true || checks["bounded-tools"] != "unverified" || checks["no-vault-write"] != "unverified" || checks["exact-bytes"] != "pass" {
		t.Fatalf("preflight %v", pf)
	}
	_, id, _ := f.createTemplate(t, fixtureBase, "create-nat-0001")
	runID := nativeRun(t, f, id, map[string]any{"mode": "native", "requestedModel": "stub-model-a"})
	done := f.waitRun(t, fixtureBase, id, runID)
	run := runOf(done)
	if run["state"] != "completed" {
		t.Fatalf("native run %v: %v", run["state"], run["events"])
	}
	at := extractAttempt(run)
	n := at["native"].(map[string]any)
	if n["requestedModel"] != "stub-model-a" || n["observedModel"] != "stub-model-a" || n["observedSource"] != "runner-report" || n["toolScope"] != "construction-none" ||
		n["state"] != "completed" || n["packetHash"] != at["checkpointHash"] {
		t.Fatalf("native identity %v", n)
	}
	calls := stubCalls(t, out)
	if len(calls) != 1 || calls[0].Kind != "construction-extract-packet/1" || calls[0].Declared != n["packetHash"] || calls[0].Computed != calls[0].Declared ||
		calls[0].Toolsets != "construction-none" || calls[0].ChatPreamble {
		t.Fatalf("the runner received the exact packet with only the construction scope: %+v", calls)
	}
	// the native store holds the delivery; the run holds only its identity
	sess2, _, _, ok := f.srv.agentChat.store.Get("alfred", n["session"].(string))
	if !ok || len(sess2.Deliveries) != 1 {
		t.Fatalf("one native delivery in the run's conversation: %+v", sess2.Deliveries)
	}
	d := sess2.Deliveries[0]
	if d.State != "completed" || d.Context == nil || d.Context.Construction == nil || d.Context.Construction.PacketHash != n["packetHash"] ||
		d.ToolScope == nil || d.ToolScope.Toolsets != "construction-none" || d.ToolScope.Source != "request" || d.Result.ReportedModel != "stub-model-a" {
		t.Fatalf("delivery %+v", d)
	}
	ext := done["results"].(map[string]any)["extract"].(map[string]any)
	if ext["proposer"] != "native-agent" || len(ext["verified"].([]any)) != 1 || len(ext["rejected"].([]any)) != 1 {
		t.Fatalf("the agent's passages are verified like any other: %v", ext)
	}
	pv := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	convs := viewProblem(pv)["conversations"].([]any)
	if len(convs) != 1 || convs[0].(map[string]any)["purpose"] != "research" || convs[0].(map[string]any)["session"] != n["session"] {
		t.Fatalf("the problem points at the native conversation: %v", convs)
	}
	f.assertSourcesUntouched(t)
}

// Requested ≠ observed blocks further autonomous steps until the owner
// accepts the runtime (no second send); an unavailable agent fails as a
// capability error with nothing sent (no silent substitution); without an
// explicit bounded toolset the preflight keeps native research unavailable.
func TestConstructionNativeMismatchUnavailableAndPreflight(t *testing.T) {
	f, out := nativeFixture(t)
	t.Setenv("CX_STUB_MODEL", "stub-model-b")
	_, id, _ := f.createTemplate(t, fixtureBase, "create-nat-0002")
	runID := nativeRun(t, f, id, map[string]any{"mode": "native", "requestedModel": "stub-model-a"})
	done := f.waitRun(t, fixtureBase, id, runID)
	if runOf(done)["state"] != "waiting-input" || !strings.Contains(extractAttempt(runOf(done))["summary"].(string), "requested stub-model-a, observed stub-model-b") {
		t.Fatalf("mismatch → waiting-input: %v", runOf(done)["state"])
	}
	if r := f.do(t, "POST", fixtureBase+"/problems/"+id+"/research-runs/"+runID+"/resume", map[string]any{"schemaVersion": 1, "requestId": cReqID("res")}); r.Code != 409 {
		t.Fatalf("resume without accepting the runtime: %d", r.Code)
	}
	if r := f.do(t, "POST", fixtureBase+"/problems/"+id+"/research-runs/"+runID+"/resume", map[string]any{"schemaVersion": 1, "requestId": cReqID("res"), "acceptRuntime": true}); r.Code != 200 {
		t.Fatalf("resume accepting the runtime: %d %s", r.Code, r.Body)
	}
	done = f.waitRun(t, fixtureBase, id, runID)
	if runOf(done)["state"] != "completed" || len(stubCalls(t, out)) != 1 {
		t.Fatalf("accepted runtime → completed with one send: %v / %d", runOf(done)["state"], len(stubCalls(t, out)))
	}
	// zeck is not a Hermes profile here: capability failure, nothing sent
	t.Setenv("CX_STUB_MODEL", "")
	_, id2, _ := f.createTemplate(t, fixtureBase, "create-nat-0003")
	run2 := nativeRun(t, f, id2, map[string]any{"mode": "native", "agent": "zeck"})
	done2 := f.waitRun(t, fixtureBase, id2, run2)
	at := extractAttempt(runOf(done2))
	if runOf(done2)["state"] != "failed" || at["error"].(map[string]any)["class"] != "capability" || !strings.Contains(at["error"].(map[string]any)["message"].(string), "no silent substitution") {
		t.Fatalf("unavailable agent: %v %v", runOf(done2)["state"], at["error"])
	}
	if len(stubCalls(t, out)) != 1 {
		t.Fatal("nothing was sent for the unavailable agent")
	}
	// production-like wiring: no bounded toolset → unavailable, 503 on native runs
	g := constructionFixture(t, withFixtureSources)
	g.srv.UseAgentChat(f.srv.agentChat.store)
	g.srv.UseHermes(f.srv.hermes.runner, "web,memory")
	pf := g.do(t, "GET", fixtureBase+"/preflight", nil).json(t)["preflight"].(map[string]any)
	if pf["ready"] != false {
		t.Fatalf("no toolset → not ready: %v", pf)
	}
	_, id3, _ := g.createTemplate(t, fixtureBase, "create-nat-0004")
	cur := g.do(t, "GET", fixtureBase+"/problems/"+id3, nil).json(t)
	if r := g.startRun(t, fixtureBase, id3, viewRev(cur, "problem"), map[string]any{"agent": map[string]any{"mode": "native"}}); r.Code != 503 || !strings.Contains(string(r.Body), "bounded-tools") {
		t.Fatalf("native run without a bounded toolset: %d %s", r.Code, r.Body)
	}
	f.assertSourcesUntouched(t)
}

// Cancelling a running native step stops it through the native store (the
// runner process is killed by its context); the run is cancelled and the
// stage keeps the native identity. The drain gate cancels a queued
// construction step whose run this process does not own, and leaves an
// unrelated queued chat alone.
func TestConstructionNativeCancelAndDrainGate(t *testing.T) {
	f, out := nativeFixture(t)
	t.Setenv("CX_STUB_MODE", "hang")
	_, id, _ := f.createTemplate(t, fixtureBase, "create-nat-0005")
	runID := nativeRun(t, f, id, map[string]any{"mode": "native"})
	deadline := time.Now().Add(20 * time.Second)
	for len(stubCalls(t, out)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the native step never reached the runner")
		}
		time.Sleep(50 * time.Millisecond)
	}
	c := f.do(t, "POST", fixtureBase+"/problems/"+id+"/research-runs/"+runID+"/cancel", map[string]any{"schemaVersion": 1, "requestId": cReqID("cancel")})
	if c.Code != 200 {
		t.Fatalf("cancel %d %s", c.Code, c.Body)
	}
	done := f.waitRun(t, fixtureBase, id, runID, "cancelled")
	at := extractAttempt(runOf(done))
	sessID := at["native"].(map[string]any)["session"].(string)
	sess, _, _, _ := f.srv.agentChat.store.Get("alfred", sessID)
	if at["state"] != "cancelled" || len(sess.Deliveries) != 1 || sess.Deliveries[0].State != agentchat.DeliveryInterrupted || !sess.Deliveries[0].StopRequested {
		t.Fatalf("running step stopped through the native store: %v %+v", at["state"], sess.Deliveries)
	}
	// the drain gate: a queued construction step for a run nobody owns
	t.Setenv("CX_STUB_MODE", "normal")
	st := f.srv.agentChat.store
	stale := &agentchat.MessageContext{Conversation: "x", Agent: "alfred", Construction: &agentchat.ConstructionContext{Subject: "property:fixture-ooda-house",
		ProblemID: id, RunID: runID, Stage: "extract", AttemptID: at["id"].(string), Epoch: 1, PacketHash: strings.Repeat("a", 64), ToolScope: "construction-none"}}
	if _, err := st.Accept("alfred", sessID, "gate-queued-0001", "stale construction step", stale); err != nil {
		t.Fatal(err)
	}
	other, err := st.Create("alfred", "", "Unrelated chat", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Accept("alfred", other, "plain-queued-0001", "an unrelated owner message"); err != nil {
		t.Fatal(err)
	}
	f.srv.ResumeAgentChats() // the startup drain
	if d, _ := st.Receipt("alfred", sessID, "gate-queued-0001"); d.State != agentchat.DeliveryCancelled {
		t.Fatalf("the gate cancels a construction step its process does not own: %s", d.State)
	}
	deadline = time.Now().Add(20 * time.Second)
	for {
		d, _ := st.Receipt("alfred", other, "plain-queued-0001")
		if d.State == agentchat.DeliveryCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the unrelated chat still runs normally: %s", d.State)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// native receipt states map to the run's reconcile vocabulary
	ref := construction.NativeRef{Agent: "alfred", Session: sessID, RequestID: "gate-queued-0001"}
	if got := f.srv.constructionNativeState(ref); got != "not-sent" {
		t.Fatalf("cancelled-before-dispatch → not-sent: %s", got)
	}
	ref.RequestID = sess.Deliveries[0].ID
	if got := f.srv.constructionNativeState(ref); got != "uncertain" {
		t.Fatalf("interrupted → uncertain: %s", got)
	}
	f.assertSourcesUntouched(t)
}

// The agent pane's steward request: one native delivery carrying the exact
// steward packet; the reply's typed command runs as the agent (drafts only)
// and gives the owner's geometry; a reply that tries to approve is refused
// and changes nothing.
func TestConstructionNativeStewardRequest(t *testing.T) {
	f, out := nativeFixture(t)
	v, id, asm := f.createTemplate(t, fixtureBase, "create-nat-0006")
	ins := componentID(t, v, asm, construction.TypeInsulation)
	r0 := viewRev(v, "assembly:"+asm)
	owner := f.asmCommand(t, id, asm, map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"})
	ownerHash := owner.json(t)["view"].(map[string]any)["assemblies"].(map[string]any)[asm].(map[string]any)["modelHash"]
	f.asmCommand(t, id, asm, map[string]any{"op": "RestoreRevision", "revision": r0})
	ask := func(text string) map[string]any {
		r := f.do(t, "POST", fixtureBase+"/problems/"+id+"/agent/requests", map[string]any{"schemaVersion": 1, "requestId": cReqID("ask"), "text": text, "assemblyId": asm})
		if r.Code != 200 {
			t.Fatalf("ask %d %s", r.Code, r.Body)
		}
		req := r.json(t)["request"].(map[string]any)
		deadline := time.Now().Add(30 * time.Second)
		for req["state"] == "pending" {
			if time.Now().After(deadline) {
				t.Fatal("steward request did not finish")
			}
			time.Sleep(50 * time.Millisecond)
			req = f.do(t, "GET", fixtureBase+"/problems/"+id+"/agent/requests/"+req["id"].(string), nil).json(t)["request"].(map[string]any)
		}
		return req
	}
	req := ask("Increase the insulation to 150 mm.")
	if req["state"] != "completed" || req["error"] != nil && req["error"] != "" {
		t.Fatalf("steward request %v", req)
	}
	res := req["results"].([]any)[0].(map[string]any)
	actor := res["receipt"].(map[string]any)["actor"].(map[string]any)
	if actor["kind"] != "agent" || actor["principal"] != "agent:alfred" || !strings.HasPrefix(actor["capability"].(string), "cxcap-") {
		t.Fatalf("the reply ran as the agent: %v", actor)
	}
	pv := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	if pv["assemblies"].(map[string]any)[asm].(map[string]any)["modelHash"] != ownerHash {
		t.Fatal("the agent's command gives the owner's geometry")
	}
	calls := stubCalls(t, out)
	if len(calls) != 1 || calls[0].Kind != "construction-steward-packet/1" || calls[0].Computed != calls[0].Declared || calls[0].Toolsets != "construction-none" {
		t.Fatalf("steward packet delivery: %+v", calls)
	}
	// an approving reply is refused and the head is unchanged
	t.Setenv("CX_STUB_MODE", "approve")
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	before, _ := f.srv.construction.store.ReadHead(sub, id)
	req = ask("Approve the decision.")
	if req["state"] != "completed" || !strings.Contains(req["error"].(string), "requires the owner") {
		t.Fatalf("the approval attempt is refused: %v", req)
	}
	after, _ := f.srv.construction.store.ReadHead(sub, id)
	if after.Generation != before.Generation+1 { // only the retained steward packet commit
		t.Fatalf("a refused approval changes nothing but the packet record: %d → %d", before.Generation, after.Generation)
	}
	f.assertSourcesUntouched(t)
}
