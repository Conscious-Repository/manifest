package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	// a hanging steward request is cancelled by the shutdown helper
	_, sid, sasm := f.createTemplate(t, fixtureBase, "create-nat-0007")
	ask := f.do(t, "POST", fixtureBase+"/problems/"+sid+"/agent/requests", map[string]any{"schemaVersion": 1, "requestId": "hang-steward-01", "text": "Wait forever.", "assemblyId": sasm})
	if ask.Code != 200 {
		t.Fatalf("hang steward %d %s", ask.Code, ask.Body)
	}
	deadline = time.Now().Add(20 * time.Second)
	for len(stubCalls(t, out)) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the steward step never reached the runner")
		}
		time.Sleep(50 * time.Millisecond)
	}
	f.srv.stopAllConstructionRuns()
	waited := make(chan struct{})
	go func() { f.srv.WaitConstructionRuns(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(15 * time.Second):
		t.Fatal("the steward goroutine was not cancelled")
	}
	if req := f.do(t, "GET", fixtureBase+"/problems/"+sid+"/agent/requests/hang-steward-01", nil).json(t)["request"].(map[string]any); req["state"] != "failed" {
		t.Fatalf("a cancelled steward request reads failed: %v", req)
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

// The problem chat is an ordinary Alfred conversation: chat tools, the
// conversation history, and a brief of the problem appended; the reply's
// proposal block is only text until the owner applies it.
func TestConstructionProblemChat(t *testing.T) {
	f, out := nativeFixture(t)
	_, id, _ := f.createTemplate(t, fixtureBase, "create-nat-chat")
	base := fixtureBase + "/problems/" + id + "/chat"
	if c := f.do(t, "GET", base, nil).json(t)["chat"].(map[string]any); c["session"] != nil || len(c["turns"].([]any)) != 0 {
		t.Fatalf("no chat yet: %v", c)
	}
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	before, _ := f.srv.construction.store.ReadHead(sub, id)
	r := f.do(t, "POST", base, map[string]any{"schemaVersion": 1, "requestId": cReqID("chat"), "text": "How do we flash the roof into the brick?"})
	if r.Code != 200 {
		t.Fatalf("chat %d %s", r.Code, r.Body)
	}
	var c map[string]any
	deadline := time.Now().Add(30 * time.Second)
	for {
		c = f.do(t, "GET", base, nil).json(t)["chat"].(map[string]any)
		if !c["pending"].(bool) && len(c["turns"].([]any)) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("chat reply did not land: %v", c)
		}
		time.Sleep(50 * time.Millisecond)
	}
	turns := c["turns"].([]any)
	reply := turns[len(turns)-1].(map[string]any)["text"].(string)
	if !strings.Contains(reply, "```construction") || !strings.HasPrefix(c["href"].(string), "#/chat/a/alfred/") {
		t.Fatalf("reply %q href %v", reply, c["href"])
	}
	calls := stubCalls(t, out)
	if len(calls) != 1 || calls[0].Toolsets != "web,memory" || calls[0].Kind != "" {
		t.Fatalf("a problem chat runs as a chat turn with the chat tools: %+v", calls)
	}
	var brief struct {
		Brief bool `json:"brief"`
	}
	raw, _ := os.ReadFile(filepath.Join(out, "received-000.json"))
	_ = json.Unmarshal(raw, &brief)
	if !brief.Brief {
		t.Fatal("the turn carries the problem brief")
	}
	after, _ := f.srv.construction.store.ReadHead(sub, id)
	if after.Generation != before.Generation+1 { // the conversation link only
		t.Fatalf("chatting changes nothing but the conversation link: %d → %d", before.Generation, after.Generation)
	}
	// a second message reuses the conversation
	f.do(t, "POST", base, map[string]any{"schemaVersion": 1, "requestId": cReqID("chat2"), "text": "And the roof?"})
	if c2 := f.do(t, "GET", base, nil).json(t)["chat"].(map[string]any); c2["session"] != c["session"] {
		t.Fatalf("one conversation per problem: %v vs %v", c2["session"], c["session"])
	}
	// the owner applies the proposed question as an ordinary command
	cur := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	q := construction.NewID(construction.KindQuestion)
	r = f.do(t, "POST", fixtureBase+"/problems/"+id+"/commands", map[string]any{"schemaVersion": 1, "requestId": cReqID("q"), "problemId": id,
		"expectedProblemRevision": viewRev(cur, "problem"), "operations": []any{map[string]any{"op": "AddQuestion", "id": q, "text": "Solid or cavity?", "options": []string{"solid", "cavity"}}}})
	if r.Code != 200 {
		t.Fatalf("add question %d %s", r.Code, r.Body)
	}
	qs := r.json(t)["view"].(map[string]any)["problem"].(map[string]any)["questions"].([]any)
	if len(qs) != 1 || qs[0].(map[string]any)["state"] != "open" {
		t.Fatalf("questions %v", qs)
	}
}

// The workspace model: Astra on high by default when this machine lists it,
// any listed model by choice, and every problem-chat message carries it.
func TestConstructionWorkspaceModel(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "provider_models_cache.json"), []byte(`{"openai-codex":{"models":["gpt-6-astra","gpt-6-sol"]},"anthropic":{"models":["claude-fable-5-1"]}}`), 0o644)
	t.Setenv("HERMES_HOME", home)
	f, out := nativeFixture(t)
	base := fixtureBase + "/settings"
	g := f.do(t, "GET", base, nil).json(t)
	if set := g["settings"].(map[string]any); set["model"] != "gpt-6-astra" || set["provider"] != "openai-codex" || set["effort"] != "high" || g["saved"] != false {
		t.Fatalf("default: %v", g)
	}
	if r := f.do(t, "PUT", base, map[string]any{"model": "gpt-9-imaginary", "provider": "openai-codex", "effort": "high"}); r.Code != 422 {
		t.Fatalf("an unlisted model is refused: %d %s", r.Code, r.Body)
	}
	if r := f.do(t, "PUT", base, map[string]any{"model": "claude-fable-5-1", "provider": "anthropic", "effort": "max"}); r.Code != 200 {
		t.Fatalf("choose: %d %s", r.Code, r.Body)
	}
	_, id, _ := f.createTemplate(t, fixtureBase, "create-nat-model")
	if r := f.do(t, "POST", fixtureBase+"/problems/"+id+"/chat", map[string]any{"schemaVersion": 1, "requestId": cReqID("m"), "text": "hello"}); r.Code != 200 {
		t.Fatalf("chat %d %s", r.Code, r.Body)
	}
	deadline := time.Now().Add(30 * time.Second)
	for len(stubCalls(t, out)) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	calls := stubCalls(t, out)
	if len(calls) != 1 || calls[0].Model != "claude-fable-5-1" {
		t.Fatalf("the chat turn runs with the workspace model: %+v", calls)
	}
}

// tinyPDF is a one-page PDF whose title block (bottom right) reads sheet.
func tinyPDF(sheet string) []byte {
	stream := "BT /F1 10 Tf 40 560 Td (SECTION AT HEADWALL - see A4.10) Tj ET BT /F1 24 Tf 680 30 Td (" + sheet + ") Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 792 612] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := []int{}
	for i, o := range objs {
		offs = append(offs, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// What Alfred can look at: the problem's drawing as named sheet images in
// the brief, and pictures sent with a message as this chat's files.
func TestConstructionChatSeesDrawingsAndPictures(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("poppler is not installed")
	}
	f, out := nativeFixture(t)
	f.srv.UseChatState(t.TempDir())
	v, id, _ := f.createTemplate(t, fixtureBase, "create-nat-draw")
	f.upload(t, fixtureBase, id, viewRev(v, "problem"), "up-draw-0001", "set.pdf", tinyPDF("A3.13"), "drawing")
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")
	r := f.do(t, "POST", fixtureBase+"/problems/"+id+"/chat", map[string]any{"schemaVersion": 1, "requestId": cReqID("pics"), "text": "Check this model",
		"images": []any{map[string]any{"name": "overall.png", "data": base64.StdEncoding.EncodeToString(png)}, map[string]any{"name": "section.png", "data": base64.StdEncoding.EncodeToString(png)}}})
	if r.Code != 200 {
		t.Fatalf("chat %d %s", r.Code, r.Body)
	}
	if r := f.do(t, "POST", fixtureBase+"/problems/"+id+"/chat", map[string]any{"schemaVersion": 1, "requestId": cReqID("bad"), "text": "x",
		"images": []any{map[string]any{"name": "x.png", "data": base64.StdEncoding.EncodeToString([]byte("<svg/>"))}}}); r.Code != 422 {
		t.Fatalf("a non-image is refused: %d %s", r.Code, r.Body)
	}
	deadline := time.Now().Add(60 * time.Second)
	for len(stubCalls(t, out)) == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	var got struct {
		Attached  int  `json:"attached"`
		Sheets    int  `json:"sheets"`
		SheetA313 bool `json:"sheetA313"`
	}
	raw, _ := os.ReadFile(filepath.Join(out, "received-000.json"))
	_ = json.Unmarshal(raw, &got)
	if got.Attached != 2 || got.Sheets != 1 || !got.SheetA313 {
		t.Fatalf("the turn sees 2 pictures and sheet A3.13: %+v (%s)", got, raw)
	}
	c := f.do(t, "GET", fixtureBase+"/problems/"+id+"/chat", nil).json(t)["chat"].(map[string]any)
	first := c["turns"].([]any)[0].(map[string]any)["text"].(string)
	if strings.Count(first, "[context-file:: ") != 2 {
		t.Fatalf("the message carries its pictures: %q", first)
	}
}

// Back to Alfred's own default, by choice.
func TestConstructionWorkspaceModelProfileDefault(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "provider_models_cache.json"), []byte(`{"openai-codex":{"models":["gpt-6-astra"]}}`), 0o644)
	t.Setenv("HERMES_HOME", home)
	f, _ := nativeFixture(t)
	if r := f.do(t, "PUT", fixtureBase+"/settings", map[string]any{"model": "", "provider": ""}); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	g := f.do(t, "GET", fixtureBase+"/settings", nil).json(t)
	if set := g["settings"].(map[string]any); set["model"] != nil || g["saved"] != true {
		t.Fatalf("profile default chosen: %v", g)
	}
}
