package construction

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The same typed command from the owner and from an agent reduces through
// the same path to the same geometry; the receipts tell them apart
// truthfully. Stale revisions conflict (two browsers undoing), free text and
// scripts are not commands, removed ids are never reused.
func TestConstructionCommandHumanAgentEquivalence(t *testing.T) {
	s, st, asm := templateProblem(t)
	ins := st.Assemblies[asm].firstOf(TypeInsulation).ID
	op := map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"}
	r0 := st.Revision("assembly:" + asm)
	h0 := st.Assemblies[asm].ModelHash
	st = mustExec(t, s, st, asm, op)
	h1 := st.Assemblies[asm].ModelHash
	st = mustExec(t, s, st, asm, map[string]any{"op": "RestoreRevision", "revision": r0})
	if st.Assemblies[asm].ModelHash != h0 {
		t.Fatal("undo restores the identical geometry")
	}
	agent := AgentActor("alfred", "cap-equivalence", "")
	st, rc, err := exec(t, s, st, asm, agent, op)
	if err != nil {
		t.Fatal(err)
	}
	if st.Assemblies[asm].ModelHash != h1 || h1 == h0 {
		t.Fatalf("the agent's identical command gives the owner's geometry: %s vs %s", st.Assemblies[asm].ModelHash[:12], h1[:12])
	}
	if rc.Actor.Kind != ActorAgent || rc.Actor.Capability != "cap-equivalence" || rc.Actor.Principal != "agent:alfred" {
		t.Fatalf("the agent receipt names the agent and its capability: %+v", rc.Actor)
	}
	hist, err := s.AssemblyHistory(fixtureProperty, st.Problem.ID, asm, 3)
	if err != nil || len(hist) != 3 {
		t.Fatalf("history %v %d", err, len(hist))
	}
	if hist[0].Actor.Kind != ActorAgent || hist[1].Actor.Kind != ActorOwner || hist[2].Actor.Kind != ActorOwner ||
		hist[0].Operations[0] != "SetDimension" || hist[1].Operations[0] != "RestoreRevision" || hist[0].ModelHash != hist[2].ModelHash {
		t.Fatalf("history tells owner and agent apart: %+v", hist)
	}
	// two browsers loaded the same revision; the second undo conflicts
	loaded := st
	st = mustExec(t, s, st, asm, map[string]any{"op": "RestoreRevision", "revision": r0})
	_, _, err = exec(t, s, loaded, asm, OwnerActor(), map[string]any{"op": "RestoreRevision", "revision": r0})
	var de *Error
	if !errors.As(err, &de) || de.Status != 409 || de.Current["assembly:"+asm] != st.Revision("assembly:"+asm) {
		t.Fatalf("a stale undo conflicts and names the current revision: %v", err)
	}
	// free text, scripts and unknown fields are not commands
	for _, raw := range []string{
		`{"schemaVersion":1,"requestId":"free-text-01","problemId":"` + st.Problem.ID + `","operations":["please make the insulation thicker"]}`,
		`{"schemaVersion":1,"requestId":"script-op-01","problemId":"` + st.Problem.ID + `","operations":[{"op":"RunScript","code":"rm -rf /"}]}`,
		`{"schemaVersion":1,"requestId":"extra-fld-01","problemId":"` + st.Problem.ID + `","assemblyId":"` + asm + `","expectedAssemblyRevision":"` + st.Revision("assembly:"+asm) + `","operations":[{"op":"SetDimension","componentId":"` + ins + `","dimension":"thickness","value":120,"unit":"mm","script":"x"}]}`,
	} {
		if _, err := ParseCommand([]byte(raw)); StatusOf(err) != 422 {
			t.Fatalf("not a command (%s): %v", raw[:60], err)
		}
	}
	// a removed id is tombstoned and never reused
	seal := st.Assemblies[asm].firstOf(TypeSealant)
	st = mustExec(t, s, st, asm, map[string]any{"op": "RemoveComponent", "componentId": seal.ID, "dependents": "detach"})
	comp := *seal
	raw, _ := json.Marshal(comp)
	var cm map[string]any
	_ = json.Unmarshal(raw, &cm)
	if _, _, err := exec(t, s, st, asm, OwnerActor(), map[string]any{"op": "AddComponent", "component": cm}); err == nil || !strings.Contains(err.Error(), "never reused") {
		t.Fatalf("a tombstoned id is refused: %v", err)
	}
}

func TestConstructionDecisionsOwnerOnlyAndStale(t *testing.T) {
	s, st, base := templateProblem(t)
	variant := NewID(KindAssembly)
	st = mustExec(t, s, st, base, map[string]any{"op": "CreateVariant", "newAssemblyId": variant, "name": "Alternative B (synthetic)"})
	agent := AgentActor("alfred", "cap-decide", "")
	dec := NewID(KindDecision)
	st, _, err := exec(t, s, st, "", agent, map[string]any{"op": "ProposeDecision", "decisionId": dec, "assemblyId": variant,
		"title": "Proceed with alternative B for review", "proposal": "Use alternative B as the working detail, conditional on the open issues."})
	if err != nil {
		t.Fatalf("an agent may propose: %v", err)
	}
	d := st.Decisions[dec]
	if d.Status != DecisionProposed || d.ProposedBy.Kind != ActorAgent || d.Assembly.Revision != st.Revision("assembly:"+variant) || len(d.UnresolvedIssues) == 0 {
		t.Fatalf("the proposal binds the exact revision and keeps its unresolved issues: %+v", d)
	}
	// an agent approval is forbidden and changes nothing
	before := headBytes(t, s, fixtureProperty, st.Problem.ID)
	_, _, err = exec(t, s, st, "", agent, map[string]any{"op": "ApproveDecision", "decisionId": dec, "expectedDecisionRevision": st.Revision("decision:" + dec)})
	if StatusOf(err) != 403 {
		t.Fatalf("agent approval → 403: %v", err)
	}
	if string(before) != string(headBytes(t, s, fixtureProperty, st.Problem.ID)) {
		t.Fatal("a refused approval leaves the head untouched")
	}
	if _, _, err := exec(t, s, st, "", agent, map[string]any{"op": "SetLifecycle", "lifecycle": "archived"}); StatusOf(err) != 403 {
		t.Fatalf("agents cannot change problem lifecycle: %v", err)
	}
	// the owner accepts the exact decision revision they reviewed
	if _, _, err := exec(t, s, st, "", OwnerActor(), map[string]any{"op": "ApproveDecision", "decisionId": dec, "expectedDecisionRevision": strings.Repeat("0", 64)}); StatusOf(err) != 409 {
		t.Fatalf("approval of a different decision revision conflicts: %v", err)
	}
	st = mustExec(t, s, st, "", map[string]any{"op": "ApproveDecision", "decisionId": dec, "expectedDecisionRevision": st.Revision("decision:" + dec), "note": "accepted for the project only"})
	d = st.Decisions[dec]
	if d.Status != DecisionAccepted || d.ReviewedBy.Kind != ActorOwner || st.Problem.Lifecycle != LifecycleOwnerSelected ||
		st.Problem.SelectedAssembly == nil || *st.Problem.SelectedAssembly != d.Assembly {
		t.Fatalf("accepted for the project, selection pinned: %+v %+v", d, st.Problem.SelectedAssembly)
	}
	if v := DecisionStates(st)[dec]; v.Stale {
		t.Fatal("fresh acceptance is not stale")
	}
	// a significant edit stales the acceptance; the selection does not follow head
	ins := st.Assemblies[variant].firstOf(TypeInsulation).ID
	st = mustExec(t, s, st, variant, map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 140, "unit": "mm"})
	if v := DecisionStates(st)[dec]; !v.Stale || !strings.Contains(v.Reason, "changed after") {
		t.Fatalf("the edit stales the acceptance: %+v", v)
	}
	if st.Problem.SelectedAssembly.Revision != d.Assembly.Revision {
		t.Fatal("the selected revision never follows the head")
	}
	// a proposal bound to an old revision cannot be accepted after the assembly moved
	old := NewID(KindDecision)
	st = mustExec(t, s, st, "", map[string]any{"op": "ProposeDecision", "decisionId": old, "assemblyId": variant, "assemblyRevision": d.Assembly.Revision,
		"title": "Re-proposal on the earlier revision", "proposal": "The earlier revision."})
	if _, _, err := exec(t, s, st, "", OwnerActor(), map[string]any{"op": "ApproveDecision", "decisionId": old, "expectedDecisionRevision": st.Revision("decision:" + old)}); StatusOf(err) != 409 {
		t.Fatalf("approving a decision on a moved assembly conflicts: %v", err)
	}
	// a fresh proposal on the current revision supersedes the earlier acceptance
	cur := NewID(KindDecision)
	st = mustExec(t, s, st, "", map[string]any{"op": "ProposeDecision", "decisionId": cur, "assemblyId": variant, "title": "Current revision", "proposal": "Current."})
	st = mustExec(t, s, st, "", map[string]any{"op": "ApproveDecision", "decisionId": cur, "expectedDecisionRevision": st.Revision("decision:" + cur)})
	if st.Decisions[dec].Status != DecisionSuperseded || st.Decisions[cur].Status != DecisionAccepted {
		t.Fatal("a later acceptance supersedes the earlier one")
	}
	// rejection is owner-only too
	rej := NewID(KindDecision)
	st = mustExec(t, s, st, "", map[string]any{"op": "ProposeDecision", "decisionId": rej, "assemblyId": base, "title": "Base", "proposal": "Base."})
	if _, _, err := exec(t, s, st, "", agent, map[string]any{"op": "RejectDecision", "decisionId": rej, "expectedDecisionRevision": st.Revision("decision:" + rej)}); StatusOf(err) != 403 {
		t.Fatalf("agent rejection → 403: %v", err)
	}
	st = mustExec(t, s, st, "", map[string]any{"op": "RejectDecision", "decisionId": rej, "expectedDecisionRevision": st.Revision("decision:" + rej)})
	if st.Decisions[rej].Status != DecisionRejected {
		t.Fatal("rejected")
	}
}

func TestConstructionCompareRevisionsAndAlternatives(t *testing.T) {
	s, st, base := templateProblem(t)
	variant := NewID(KindAssembly)
	st = mustExec(t, s, st, base, map[string]any{"op": "CreateVariant", "newAssemblyId": variant, "name": "Alternative B (synthetic)"})
	same := CompareAssemblies(st.Assemblies[base], st.Assemblies[variant], st.Revision("assembly:"+base), st.Revision("assembly:"+variant), st.Validation[base], st.Validation[variant])
	if len(same.Components) != 0 || len(same.Parameters) != 0 || len(same.Junction) != 0 {
		t.Fatalf("a fresh variant differs only by identity: %+v", same)
	}
	ins := st.Assemblies[variant].firstOf(TypeInsulation).ID
	st = mustExec(t, s, st, variant,
		map[string]any{"op": "SetJunctionStrategy", "orientation": "headwall", "strategy": "apron-surface-counterflashing", "newComponentIds": map[string]string{"apron": NewID(KindComponent)}},
		map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"})
	c := CompareAssemblies(st.Assemblies[base], st.Assemblies[variant], st.Revision("assembly:"+base), st.Revision("assembly:"+variant), st.Validation[base], st.Validation[variant])
	if len(c.Junction) < 2 || c.Geometry.From == c.Geometry.To || c.Same {
		t.Fatalf("junction and geometry differ: %+v", c)
	}
	found := false
	for _, ch := range c.Components {
		if ch.ID == ins && ch.Change == "changed" && strings.Contains(strings.Join(ch.Details, ";"), "thickness 100") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the insulation change is matched by stable id: %+v", c.Components)
	}
	if len(c.Issues.OnlyA)+len(c.Issues.OnlyB) == 0 {
		t.Fatal("the issue sets differ between the alternatives")
	}
	// comparing two revisions of the same assembly
	hist, err := s.AssemblyHistory(fixtureProperty, st.Problem.ID, variant, 0)
	if err != nil || len(hist) != 2 {
		t.Fatalf("variant history %d %v", len(hist), err)
	}
	old, _, err := s.AssemblyAt(fixtureProperty, st.Problem.ID, variant, hist[1].Revision)
	if err != nil {
		t.Fatal(err)
	}
	rc := CompareAssemblies(old, st.Assemblies[variant], hist[1].Revision, hist[0].Revision, nil, nil)
	if len(rc.Components) == 0 || rc.A.Revision == rc.B.Revision {
		t.Fatalf("revision compare %+v", rc)
	}
}
