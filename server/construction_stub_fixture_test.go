package server

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manifest/construction"
)

var updateConstructionStub = flag.Bool("update-construction-stub", false, "rewrite testdata/construction-stub-fixture.json")

// The recorded stub that `node server/testdata/construction-workbench.cjs`
// (no backend) renders: the real template compiled by the real compiler with
// fixed ids and a fixed clock, shaped like the private API's problem view.
// The test regenerates it and fails if the committed copy drifted.
func TestConstructionStubFixtureCurrent(t *testing.T) {
	n := 0
	ids := func(kind string) string { n++; return fmt.Sprintf("%s-%032x", kind, n) }
	pid := fmt.Sprintf("cp-%032x", 0x5ab)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	stamp := now.Format(time.RFC3339Nano)
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	req := &construction.CreateRequest{Title: "Corrugated roof to masonry wall (recorded stub)", Narrative: "SYNTHETIC FIXTURE for UI-only checks. Not a real site."}
	p := construction.NewProblem(pid, sub, req, nil, now)
	a := construction.RoofMasonryTemplate(pid, ids)
	cat := construction.NewCatalog(pid)
	for _, env := range []*construction.Envelope{&p.Envelope, &a.Envelope, &cat.Envelope} {
		env.RevisionNumber, env.CreatedAt, env.Actor = 1, stamp, construction.OwnerActor()
	}
	p.Alternatives, p.ActiveAssembly = []string{a.ID}, a.ID
	_, atok, err := construction.TokenOf(a)
	if err != nil {
		t.Fatal(err)
	}
	rep, ir, err := construction.EvaluateReport(a, cat, nil, p, atok, nil, construction.OwnerActor(), now)
	if err != nil {
		t.Fatal(err)
	}
	a.ModelHash = ir.Hash
	for i := range rep.Issues {
		rep.Issues[i].ID = fmt.Sprintf("iss-%032x", i+1)
	}
	rep.Envelope = construction.Envelope{SchemaVersion: 1, Kind: construction.DocValidation, ID: a.ID, RevisionNumber: 1, CreatedAt: stamp, Actor: construction.OwnerActor()}
	_, ptok, _ := construction.TokenOf(p)
	_, atok, _ = construction.TokenOf(a)
	_, ctok, _ := construction.TokenOf(cat)
	view := map[string]any{
		"problem":    p,
		"revisions":  map[string]string{"problem": ptok, "assembly:" + a.ID: atok, "catalog": ctok},
		"generation": 1, "updatedAt": stamp, "readOnly": false,
		"context":    map[string]any{"subject": map[string]any{"kind": "property", "id": sub.ID, "status": "resolved", "title": "1 Fixture Way"}, "budget": nil, "scope": nil, "tasks": []any{}},
		"assemblies": map[string]any{a.ID: a}, "validation": map[string]any{a.ID: rep},
		"decisions": map[string]any{}, "runs": map[string]any{}, "views": map[string]any{},
		"catalog": cat, "evidence": nil, "derived": nil, "notice": construction.NonApprovalNotice,
	}
	fixture := map[string]any{"slug": sub.ID, "problemId": pid, "assemblyId": a.ID, "title": p.Title,
		"apronId": a.Components[8].ID, "view": view,
		"geometry": map[string]any{"assemblyRevision": atok, "modelHash": ir.Hash, "geometry": ir, "generatorChanged": false}}
	b, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "construction-stub-fixture.json")
	if *updateConstructionStub {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	have, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test ./server -run TestConstructionStubFixtureCurrent -update-construction-stub)", err)
	}
	if !bytes.Equal(have, b) {
		t.Fatal("testdata/construction-stub-fixture.json is stale; rerun with -update-construction-stub")
	}
	if a.Components[8].Type != construction.TypeApronFlashing {
		t.Fatal("fixture apron index moved")
	}
}

// The direct node run (no backend) of the workbench fixture exercises the
// real JS/CSS against the recorded stub: layout, renderer and picking only.
func TestConstructionWorkbenchStubBrowser(t *testing.T) {
	runFixture(t, "construction-workbench.cjs", true, false)
}
