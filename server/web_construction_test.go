package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"manifest/construction"
)

// constructionBrowser runs a construction browser fixture against a REAL
// backend: the Go server (embedded assets, real store, real auth guard) on an
// httptest loopback listener. In-memory API stubs cannot prove persistence or
// the access boundary, so the journey checks the store afterwards.
func constructionBrowser(t *testing.T, script string, f *constructionFix, extra map[string]any) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node unavailable: the construction browser gate cannot be skipped")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Fatal("playwright unavailable (set NODE_PATH): the construction browser gate cannot be skipped")
	}
	ts := httptest.NewServer(f.srv.Handler())
	defer ts.Close()
	cfg := map[string]any{"url": ts.URL, "shots": os.Getenv("CONSTRUCTION_SHOTS")}
	for k, v := range extra {
		cfg[k] = v
	}
	raw, _ := json.Marshal(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, node, "testdata/"+script, string(raw)).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}
	return string(out)
}

// The P1 journey in a browser against the real backend: open Construction
// from the exact property, create a problem, add a fact and a synthetic photo,
// reload and find the same durable record; create the Home pilot problem from
// TASKS; at phone width the pane switch works. Then the store is checked.
func TestConstructionWorkbenchBrowser(t *testing.T) {
	f := constructionFixture(t)
	out := constructionBrowser(t, "construction-workbench.cjs", f, nil)
	t.Log(strings.TrimSpace(out))
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	list, err := f.srv.construction.store.List(sub)
	if err != nil || len(list) != 2 {
		t.Fatalf("property problems after the browser journey (main + race fixture): %+v %v", list, err)
	}
	var mainID string
	for _, row := range list {
		if row.Title == "Corrugated roof to masonry wall" && row.Inputs == 1 {
			mainID = row.ID
		}
	}
	st, err := f.srv.construction.store.Load(sub, mainID)
	if err != nil || len(st.Problem.Existing) != 1 || st.Problem.Inputs[0].Role != "photo" {
		t.Fatalf("durable problem: %+v %v", st.Problem, err)
	}
	asm := st.Problem.Alternatives[0]
	ins := st.Assemblies[asm].Components[3]
	if ins.Type != construction.TypeInsulation || *ins.Shape.Params["thickness"].Value != 180 {
		t.Fatalf("the committed handle value must be durable: %+v", ins.Shape.Params["thickness"])
	}
	v := st.Views[st.Problem.LatestView]
	if v == nil || v.Mode != "realistic" || v.Section == nil || !v.Section.Enabled || len(v.Selection) != 1 {
		t.Fatalf("the working view persisted: %+v", v)
	}
	hist, err := f.srv.construction.store.History(sub, mainID, 0)
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]int{}
	viewOnly := 0
	for _, rc := range hist {
		for _, op := range rc.Operations {
			ops[op.Op]++
		}
		if rc.ViewOnly {
			viewOnly++
		}
	}
	if ops["SetDimension"] < 2 || ops["RestoreRevision"] != 2 || viewOnly == 0 {
		t.Fatalf("receipts for edit, undo, redo, handle and view saves: %v (view-only %d)", ops, viewOnly)
	}
	home, err := f.srv.construction.store.List(construction.SubjectRef{Kind: "home", ID: "home"})
	if err != nil || len(home) != 1 || home[0].Title != "761 N Euclid — Back Addition" {
		t.Fatalf("home pilot problem: %+v %v", home, err)
	}
	f.assertSourcesUntouched(t)
}

// Sections and exports through the UI against the real backend; then the
// retained derivatives are checked in the store.
func TestConstructionSectionBrowser(t *testing.T) {
	f := constructionFixture(t)
	out := constructionBrowser(t, "construction-sections.cjs", f, nil)
	t.Log(strings.TrimSpace(out))
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	list, err := f.srv.construction.store.List(sub)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	st, err := f.srv.construction.store.Load(sub, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	formats := map[string]int{}
	for _, rec := range st.Derived.Artifacts {
		formats[rec.Format]++
		b, err := f.srv.construction.store.Content(sub, st.Problem.ID, rec.ArtifactID, rec.Revision)
		if err != nil || construction.Token(b) != rec.Revision {
			t.Fatalf("derived %s not retrievable exactly: %v", rec.Name, err)
		}
	}
	if formats["svg"] != 1 || formats["pdf"] != 1 || formats["package"] != 1 || formats["glb"] != 1 {
		t.Fatalf("derived formats %v", formats)
	}
	f.assertSourcesUntouched(t)
}
