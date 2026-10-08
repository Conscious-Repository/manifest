package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"manifest/construction"
)

// constructionBrowser runs a construction browser fixture against a REAL
// backend: the Go server (embedded assets, real store, real auth guard) on an
// httptest loopback listener. In-memory API stubs cannot prove persistence or
// the access boundary, so the journey checks the store afterwards.
//
// A handler panic fails the test even when the page's own assertions pass:
// net/http recovers it, logs it to the server's error log (captured here) and
// drops the connection, which a page can easily survive unnoticed.
func constructionBrowser(t *testing.T, script string, f *constructionFix, extra map[string]any) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	ts := httptest.NewUnstartedServer(f.srv.Handler())
	serverLog := &lockedBuffer{}
	ts.Config.ErrorLog = log.New(serverLog, "", 0)
	ts.Start()
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
		t.Fatalf("%s: %v\n%s\nserver log:\n%s", script, err, out, serverLog.String())
	}
	if l := serverLog.String(); strings.Contains(l, "panic") {
		t.Fatalf("%s: the backend panicked while the page ran:\n%s", script, l)
	}
	return string(out)
}

// lockedBuffer collects the test server's error log, written from handler
// goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
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

// Research runs and evidence through the workbench against the real backend
// with the synthetic fixture adapter (slowed so progress and cancel are
// observable); then the durable runs and evidence are checked in the store.
func TestConstructionResearchBrowser(t *testing.T) {
	f := constructionFixture(t, withFixtureSources)
	f.srv.construction.runner.Adapters[1].(*construction.FixtureAdapter).Delay = 1500 * time.Millisecond
	out := constructionBrowser(t, "construction-research.cjs", f, nil)
	t.Log(strings.TrimSpace(out))
	f.srv.WaitConstructionRuns()
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	list, err := f.srv.construction.store.List(sub)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	st, err := f.srv.construction.store.Load(sub, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]int{}
	for _, r := range st.Runs {
		states[r.State]++
	}
	if states["completed"] != 2 || len(st.Runs) != 2 {
		t.Fatalf("two durable completed runs (one cancelled then resumed): %v", states)
	}
	verified := 0
	for _, e := range st.Evidence.Evidence {
		if e.Verification == "verified" {
			verified++
		}
	}
	if verified < 12 || len(st.Problem.Alternatives) != 7 {
		t.Fatalf("verified passages %d, alternatives %d", verified, len(st.Problem.Alternatives))
	}
	if len(st.Catalog.Products) != 1 || st.Catalog.Products[0].Lifecycle != "stale" {
		t.Fatalf("the UI-added product is durable and stale: %+v", st.Catalog.Products)
	}
	pinned := 0
	for _, a := range st.Assemblies {
		for _, c := range a.Components {
			if c.Product != nil && c.Product.ID == st.Catalog.Products[0].ID && *c.Shape.Params["thickness"].Value == 120 {
				pinned++
			}
		}
	}
	// the substituted alternative plus the three alternatives the second
	// research run derived from it (variants copy the pin, never follow head)
	if pinned != 4 {
		t.Fatalf("the applied substitution is durable (1 + 3 derived): %d", pinned)
	}
	f.assertSourcesUntouched(t)
}

// Native agent steps through the workbench: the real agent-chat store and
// hermes Runner (protocol stub) behind the real backend; afterwards the
// agent's edit is in the store as an agent receipt.
func TestConstructionNativeBrowser(t *testing.T) {
	f, _ := nativeFixture(t)
	out := constructionBrowser(t, "construction-native.cjs", f, nil)
	t.Log(strings.TrimSpace(out))
	f.srv.WaitConstructionRuns()
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	list, err := f.srv.construction.store.List(sub)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	hist, err := f.srv.construction.store.History(sub, list[0].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	agentEdits := 0
	for _, rc := range hist {
		if rc.Actor.Kind == "agent" {
			for _, op := range rc.Operations {
				if op.Op == "SetDimension" {
					agentEdits++
				}
			}
		}
	}
	if agentEdits != 1 {
		t.Fatalf("one agent SetDimension receipt: %d", agentEdits)
	}
	f.assertSourcesUntouched(t)
}

// The integrated journey in a browser (P9): the Home pilot through edits,
// research cancel/resume, a decision, the detail package and the private
// recovery bundle; afterwards the downloaded bundle restores into an empty
// root and reopens the same problem.
func TestConstructionJourneyBrowser(t *testing.T) {
	f := constructionFixture(t, withFixtureSources)
	f.srv.construction.runner.Adapters[1].(*construction.FixtureAdapter).Delay = 1500 * time.Millisecond
	out := constructionBrowser(t, "construction-journey.cjs", f, nil)
	t.Log(strings.TrimSpace(out))
	var res struct {
		Shots string `json:"shots"`
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil || res.Shots == "" {
		t.Fatalf("journey output: %v", err)
	}
	shots := res.Shots
	f.srv.WaitConstructionRuns()
	home := construction.SubjectRef{Kind: "home", ID: "home"}
	list, err := f.srv.construction.store.List(home)
	if err != nil || len(list) != 1 || list[0].Title != "761 N Euclid — Back Addition" || list[0].Lifecycle != "owner-selected" {
		t.Fatalf("home pilot %+v %v", list, err)
	}
	raw, err := os.ReadFile(filepath.Join(shots, "journey-recovery.zip"))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(privateTestDir(t), "restored")
	rep, err := construction.Restore(bytes.NewReader(raw), int64(len(raw)), target, construction.RestoreOptions{Forbidden: []string{f.vault}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.srv.construction.store.Load(home, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ProblemID != list[0].ID || rep.Generation > st.Head.Generation || rep.Documents == 0 {
		t.Fatalf("the downloaded bundle restores the pilot: %+v", rep)
	}
	f.assertSourcesUntouched(t)
}

// A request from a public CDN/tunnel is refused, and the UI says so: every
// construction entry shows the refusal and nothing of the problem. The
// owner's tailnet path (tailscale serve headers) and a local context open the
// problem. The refused context changed nothing in the store.
func TestConstructionRemoteRefusalBrowser(t *testing.T) {
	f := constructionFixture(t)
	v, id, _ := f.createTemplate(t, fixtureBase, "create-remote-ui1")
	out := constructionBrowser(t, "construction-remote.cjs", f, map[string]any{"problemId": id, "title": viewProblem(v)["title"]})
	t.Log(strings.TrimSpace(out))
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	st, err := f.srv.construction.store.Load(sub, id)
	if err != nil || st.Head.Generation != int64(v["generation"].(float64)) {
		t.Fatalf("the refused browser context changed the problem: %v", err)
	}
	if home, err := f.srv.construction.store.List(construction.SubjectRef{Kind: "home", ID: "home"}); err != nil || len(home) != 0 {
		t.Fatalf("home problems %+v %v", home, err)
	}
	f.assertSourcesUntouched(t)
}

// Every REAL ESTATE tab folded under MORE on a phone opens from MORE, in both
// themes, with no page error, no dropped backend request and (checked by the
// harness) no handler panic. The workbench run's MORE step once reached
// SETTINGS, whose entities list dereferenced the absent import memory: 28
// panics behind passing assertions.
func TestConstructionMobileTabsMoreBrowser(t *testing.T) {
	f := constructionFixture(t)
	v, id, _ := f.createTemplate(t, fixtureBase, "create-tabs-0001")
	out := constructionBrowser(t, "construction-mobile-tabs.cjs", f, map[string]any{"problemId": id, "title": viewProblem(v)["title"]})
	t.Log(strings.TrimSpace(out))
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var res struct {
		Visited []string `json:"visited"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("script output: %v", err)
	}
	for _, at := range []string{"320 (default)", "390 (default)", "320 (jarvis)", "390 (jarvis)"} {
		seen := map[string]bool{}
		for _, v := range res.Visited {
			if name, ok := strings.CutPrefix(v, at+" "); ok {
				seen[name] = true
			}
		}
		for _, name := range []string{"CONTRACTORS", "MAP", "SETTINGS"} {
			if !seen[name] {
				t.Fatalf("%s: %s was not visited from MORE (visited %v)", at, name, res.Visited)
			}
		}
	}
	f.assertSourcesUntouched(t)
}
