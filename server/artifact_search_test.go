package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"manifest/agentchat"
	"manifest/artifacts"
)

func TestArtifactSearchCurrentTextAndScope(t *testing.T) {
	s, _, _ := artifactFixture(t)
	s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}
	se := termSession{ID: "aaaabbbbccccdddd", Kind: "codex"}
	s.terminal.upsert(se)
	put := func(ref string, content []byte, scope string) artifacts.Artifact {
		t.Helper()
		v, err := s.artifactReg.Put(artifacts.Put{Title: "Identical title", Ref: ref, Content: content, Provenance: artifacts.Provenance{Session: scope}})
		if err != nil {
			t.Fatal(err)
		}
		return v.Artifact
	}
	own := put("report.md", []byte("old-only phrase"), s.runtimeArtifactScope(se))
	if _, err := s.artifactReg.Put(artifacts.Put{ID: own.ID, Content: []byte("Current needle finding")}); err != nil {
		t.Fatal(err)
	}
	put("other.md", []byte("Current needle private-other"), "unrelated")
	put("image.bin", []byte{0, 1, 2}, "unrelated")
	put("large.txt", []byte(strings.Repeat("x", (1<<20)+1)), "unrelated")
	code, out := artifactsDo(t, s, "GET", "/api/artifacts?q=current+needle", "")
	if code != 200 || out["count"] != float64(2) || out["contentSkipped"] != float64(2) {
		t.Fatal(code, out)
	}
	encoded, _ := json.Marshal(out)
	if strings.Contains(string(encoded), "Current needle finding") {
		t.Fatal("search exposes raw content", out)
	}
	code, out = artifactsDo(t, s, "GET", "/api/artifacts?q=old-only", "")
	if code != 200 || out["count"] != float64(0) {
		t.Fatal("historical bytes matched current-head search", code, out)
	}
	query := "/api/artifacts?q=needle&conversation_backend=terminal&conversation_agent=codex&conversation_id=" + se.ID
	code, out = artifactsDo(t, s, "GET", query, "")
	if code != 200 || out["count"] != float64(1) || out["artifacts"].([]any)[0].(map[string]any)["id"] != own.ID {
		t.Fatal("scope leaked", code, out)
	}
	for _, q := range []string{"report.md", own.ID, "IMAGE.BIN"} {
		code, out = artifactsDo(t, s, "GET", "/api/artifacts?q="+url.QueryEscape(q), "")
		if code != 200 || out["count"] != float64(1) {
			t.Fatal("metadata search", q, code, out)
		}
	}
	code, _ = artifactsDo(t, s, "GET", "/api/artifacts?q="+strings.Repeat("a", 257), "")
	if code != 400 {
		t.Fatal("unbounded query", code)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/artifacts?q=needle&sources=1", nil))
	if rr.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(rr.Header())
	}
}

func TestArtifactSearchSourcesPreserveExecutionAndConversation(t *testing.T) {
	s, _, _ := artifactFixture(t)
	agentServer, store, _ := agentChatFixture(t, echoStub)
	s.agentChat = agentServer.agentChat
	agentID, err := store.Create("alfred", "", "Identical title", "")
	if err != nil {
		t.Fatal(err)
	}
	agent, _, _, _ := store.Get("alfred", agentID)
	s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}
	root := termSession{ID: "aaaabbbbccccdddd", Kind: "codex", Name: "Identical title"}
	s.terminal.upsert(root)
	child := termSession{ID: "eeeeffffaaaabbbb", Kind: "claude", Origin: &agentchat.Origin{Backend: "terminal", Agent: "codex", ID: root.ID, Mode: "continue"}}
	s.terminal.upsert(child)
	put := func(ref string, p artifacts.Provenance) artifacts.Artifact {
		t.Helper()
		v, err := s.artifactReg.Put(artifacts.Put{Title: "Identical title", Ref: ref, Content: []byte("output"), Provenance: p})
		if err != nil {
			t.Fatal(err)
		}
		return v.Artifact
	}
	output := put("changes.diff", artifacts.Provenance{Source: "runtime-changes", Session: s.runtimeArtifactScope(child), Run: child.ID})
	planned := put("plan.md", artifacts.Provenance{Source: "chat", Session: sessionConversation(agent).Key})
	missing := put("missing.md", artifacts.Provenance{Source: "chat", Session: "legacy-unknown-id"})
	wrong := put("wrong.md", artifacts.Provenance{Source: "runtime-changes", Session: sessionConversation(agent).Key, Run: child.ID})
	links := s.artifactSourceLinks([]artifacts.Artifact{output, planned, missing, wrong})
	if len(links[output.ID]) != 2 || links[output.ID][0].Route != terminalConversation(root).Route || links[output.ID][1].Route != terminalConversation(child).Route {
		t.Fatal(links)
	}
	if len(links[planned.ID]) != 1 || links[planned.ID][0].Route != sessionConversation(agent).Route {
		t.Fatal(links)
	}
	if len(links[missing.ID]) != 0 || len(links[wrong.ID]) != 1 {
		t.Fatal("invented source from legacy ID or mismatched execution", links)
	}
	code, out := artifactsDo(t, s, "GET", "/api/artifacts?q=changes.diff&sources=1", "")
	if code != 200 || len(out["artifacts"].([]any)[0].(map[string]any)["sources"].([]any)) != 2 {
		t.Fatal(code, out)
	}
	code, exact := artifactsDo(t, s, "GET", "/api/artifacts/get?id="+output.ID+"&sources=1&preview=1&rev="+output.Head, "")
	if code != 200 || len(exact["sources"].([]any)) != 2 {
		t.Fatal(code, exact)
	}
	// Deleting the recorded runtime removes its link, without relinking by title.
	s.terminal.save([]termSession{root})
	links = s.artifactSourceLinks([]artifacts.Artifact{output})
	if len(links[output.ID]) != 1 || links[output.ID][0].Kind != "conversation" {
		t.Fatal(links)
	}
}

func TestArtifactSearchRunLinkNamesExactHarness(t *testing.T) {
	s, _, _ := artifactFixture(t)
	h := s.eachHarness()[1]
	dir := filepath.Join(h.Spirits.Root(), "artifacts", "runs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "same-run.md"), []byte("---\nspirit: research\nrun: same-run\noutcome: done\n---\nReport"), 0600); err != nil {
		t.Fatal(err)
	}
	a := artifacts.Artifact{ID: "output", Harness: h.Name, Provenance: artifacts.Provenance{Source: "run", Run: "same-run"}}
	links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]
	want := "#/artifact/run-in/" + url.PathEscape(h.Name) + "/same-run"
	if len(links) != 1 || links[0].Route != want {
		t.Fatal(links, want)
	}
	code, out := artifactsDo(t, s, "GET", "/api/spirits/runs/same-run?harness="+url.QueryEscape(h.Name), "")
	if code != 200 || out["harness"] != h.Name || !strings.Contains(out["body"].(string), "Report") {
		t.Fatal(code, out)
	}
	for _, harness := range []string{s.eachHarness()[0].Name, "missing", ""} {
		code, _ = artifactsDo(t, s, "GET", "/api/spirits/runs/same-run?harness="+url.QueryEscape(harness), "")
		if code != 404 {
			t.Fatal("explicit harness fell back", harness, code)
		}
	}
	a.Harness = s.eachHarness()[0].Name
	if len(s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]) != 0 {
		t.Fatal("cross-harness run leak")
	}
}

func TestArtifactSourceBrowserRoutes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/artifact-sources.cjs").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}
