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
	list, err := f.srv.construction.store.List(construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"})
	if err != nil || len(list) != 1 || list[0].Inputs != 1 {
		t.Fatalf("property problems after the browser journey: %+v %v", list, err)
	}
	st, err := f.srv.construction.store.Load(construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}, list[0].ID)
	if err != nil || len(st.Problem.Existing) != 1 || st.Problem.Inputs[0].Role != "photo" {
		t.Fatalf("durable problem: %+v %v", st.Problem, err)
	}
	home, err := f.srv.construction.store.List(construction.SubjectRef{Kind: "home", ID: "home"})
	if err != nil || len(home) != 1 || home[0].Title != "761 N Euclid — Back Addition" {
		t.Fatalf("home pilot problem: %+v %v", home, err)
	}
	f.assertSourcesUntouched(t)
}
