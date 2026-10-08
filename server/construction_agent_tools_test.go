package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"manifest/construction"
)

// "Increase insulation" from a fake structured agent tool and from the
// owner's direct control reduce through the identical command path to the
// identical geometry; the receipts differ truthfully. The agent cannot
// approve (403, head unchanged), cannot switch subject or problem, cannot
// write sources or the catalog, and a revoked or expired capability is
// refused.
func TestConstructionAgentToolEquivalenceAndBounds(t *testing.T) {
	f := constructionFixture(t)
	v, id, asm := f.createTemplate(t, fixtureBase, "create-agt-0001")
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	ins := componentID(t, v, asm, construction.TypeInsulation)
	r0 := viewRev(v, "assembly:"+asm)
	// owner direct control
	r := f.asmCommand(t, id, asm, map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"})
	if r.Code != 200 {
		t.Fatalf("owner edit %d %s", r.Code, r.Body)
	}
	ownerHash := r.json(t)["view"].(map[string]any)["assemblies"].(map[string]any)[asm].(map[string]any)["modelHash"].(string)
	f.asmCommand(t, id, asm, map[string]any{"op": "RestoreRevision", "revision": r0})
	// the agent's structured reply carrying the same typed command
	capa, err := f.srv.grantConstructionAgent(sub, id, "alfred", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cur := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	cmd := map[string]any{"schemaVersion": 1, "requestId": "agent-increase-01", "problemId": id, "assemblyId": asm, "expectedAssemblyRevision": viewRev(cur, "assembly:"+asm),
		"operations": []map[string]any{{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"}}}
	rawCmd, _ := json.Marshal(cmd)
	reply, err := parseConstructionAgentReply("Increasing the insulation as asked.\n" + `{"summary":"increase insulation to 150 mm","commands":[` + string(rawCmd) + `]}`)
	if err != nil {
		t.Fatal(err)
	}
	res := f.srv.applyConstructionAgentReply(capa.ID, reply)
	if len(res) != 1 || res[0].Status != 200 || res[0].Receipt.Actor.Kind != "agent" || res[0].Receipt.Actor.Capability != capa.ID {
		t.Fatalf("agent result %+v", res)
	}
	st, err := f.srv.construction.store.Load(sub, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Assemblies[asm].ModelHash != ownerHash {
		t.Fatal("the agent's typed command gives exactly the owner's geometry")
	}
	// a lost reply replayed: one receipt, no second edit
	res2 := f.srv.applyConstructionAgentReply(capa.ID, reply)
	if res2[0].Status != 200 || res2[0].Receipt.ID != res[0].Receipt.ID {
		t.Fatalf("an identical replay returns the original receipt: %+v", res2)
	}
	// approval is owner-only: 403 and the head is byte-identical
	dec := construction.NewID("dec")
	prop, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": "agent-propose-01", "problemId": id,
		"operations": []map[string]any{{"op": "ProposeDecision", "decisionId": dec, "assemblyId": asm, "title": "Agent proposal", "proposal": "Use 150 mm."}}})
	if _, _, err := f.srv.constructionAgentCommand(capa.ID, prop); err != nil {
		t.Fatalf("the agent may propose: %v", err)
	}
	st, _ = f.srv.construction.store.Load(sub, id)
	before, _ := json.Marshal(st.Head)
	appr, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": "agent-approve-01", "problemId": id,
		"operations": []map[string]any{{"op": "ApproveDecision", "decisionId": dec, "expectedDecisionRevision": st.Revision("decision:" + dec)}}})
	if _, _, err := f.srv.constructionAgentCommand(capa.ID, appr); construction.StatusOf(err) != 403 {
		t.Fatalf("agent approval → 403: %v", err)
	}
	st2, _ := f.srv.construction.store.Load(sub, id)
	after, _ := json.Marshal(st2.Head)
	if string(before) != string(after) || st2.Decisions[dec].Status != "proposed" {
		t.Fatal("a refused agent approval changes nothing")
	}
	// bound to its problem: no other problem, no subject switch
	v2 := f.create(t, fixtureBase, "create-agt-0002", "Other problem", nil)
	other, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": "agent-other-01", "problemId": viewProblem(v2)["id"],
		"operations": []map[string]any{{"op": "SetLifecycle", "lifecycle": "archived"}}, "expectedProblemRevision": viewRev(v2, "problem")})
	if _, _, err := f.srv.constructionAgentCommand(capa.ID, other); construction.StatusOf(err) != 403 {
		t.Fatalf("a command for another problem is refused: %v", err)
	}
	// owner-only families
	for _, op := range []map[string]any{
		{"op": "AddSource", "id": construction.NewID("src"), "title": "agent source", "class": "secondary"},
		{"op": "SetSteward", "agent": "zeck"},
		{"op": "AddProduct", "id": construction.NewID("prd"), "manufacturer": "x", "family": "insulation", "model": "y", "geography": "z", "checkedAt": "2026-10-01", "documents": []string{}},
	} {
		b, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": construction.NewID("op")[:20], "problemId": id, "operations": []map[string]any{op},
			"expectedProblemRevision": st2.Revision("problem")})
		if _, _, err := f.srv.constructionAgentCommand(capa.ID, b); construction.StatusOf(err) != 403 {
			t.Fatalf("%s by an agent → 403: %v", op["op"], err)
		}
	}
	// free text is not a reply
	if _, err := parseConstructionAgentReply("I increased the insulation."); err == nil {
		t.Fatal("free text is refused")
	}
	if _, err := parseConstructionAgentReply(`{"summary":"x","commands":[],"shell":"rm -rf /"}`); err == nil {
		t.Fatal("extra fields are refused")
	}
	// revoked and expired capabilities
	f.srv.revokeConstructionAgent(capa.ID)
	if _, _, err := f.srv.constructionAgentCommand(capa.ID, rawCmd); construction.StatusOf(err) != 403 {
		t.Fatalf("a revoked capability is refused: %v", err)
	}
	short, _ := f.srv.grantConstructionAgent(sub, id, "alfred", "", -time.Second)
	if _, _, err := f.srv.constructionAgentCommand(short.ID, rawCmd); construction.StatusOf(err) != 403 {
		t.Fatalf("an expired capability is refused: %v", err)
	}
	// no HTTP route accepts a capability: the id is not a path or header anywhere
	srcs, _ := os.ReadFile("construction_agent_tools.go")
	if strings.Contains(string(srcs), "mux.HandleFunc") {
		t.Fatal("the agent tool must stay in-process")
	}
	f.assertSourcesUntouched(t)
}
