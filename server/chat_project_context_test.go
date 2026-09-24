package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/artifacts"
	"manifest/record"
	"manifest/vaultwriter"
)

func projectContextFixture(t *testing.T, s *Server) string {
	t.Helper()
	root := t.TempDir()
	s.UseChatState(t.TempDir())
	value := json.RawMessage(`{"groups":{"project/one":"Same project","project/two":"Same project"},"members":{"agent:alfred/member-one":"project/one","terminal:codex/member-two":"project/one","agent:alfred/private-other":"project/two"},"contexts":{"project/one":{"instructions":"EXACT_PROJECT_INSTRUCTIONS\nhttps://example.test/reference"},"project/two":{"instructions":"OTHER_PROJECT_SECRET"}},"folders":{"folder:local:/selected/work":"project/one","folder:local:/private/other":"project/two"},"priorities":{"agent:alfred/member-one":2,"agent:alfred/private-other":3}}`)
	if _, err := s.chatState.Write("inbox", "workstreams", 0, value); err != nil {
		t.Fatal(err)
	}
	s.UseVault(vaultwriter.New(root).Grant(vaultwriter.Capability{Name: "chat-projects", Zone: record.ZoneSystem, Pattern: "system/workbench/projects.md", Actor: vaultwriter.ActorUserAction}))
	if err := s.UseChatProjects("system/workbench"); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, s.chatProjectsPath)
}
func TestProjectRecordContextIdentityRetentionAndPrivacy(t *testing.T) {
	s := New(nil, nil, nil)
	explicitArtifactFixture(t, s)
	path := projectContextFixture(t, s)
	before, _ := os.ReadFile(path)
	code, result := artifactsDo(t, s, "GET", "/api/chat/records?kind=project&q=same", "")
	if code != 200 {
		t.Fatal(code, result)
	}
	rows := result["records"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["id"] != "project/one" || rows[1].(map[string]any)["id"] != "project/two" {
		t.Fatal(rows)
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "EXACT_PROJECT") {
		t.Fatal("search leaked instructions")
	}
	preview := contextPreview(t, s, "project", "project/one")
	content := preview["content"].(string)
	for _, want := range []string{"EXACT_PROJECT_INSTRUCTIONS\nhttps://example.test/reference", "agent:alfred/member-one [priority: 2]", "terminal:codex/member-two", "folder:local:/selected/work", "contents excluded"} {
		if !strings.Contains(content, want) {
			t.Fatal(want, content)
		}
	}
	for _, excluded := range []string{"OTHER_PROJECT_SECRET", "private-other", "/private/other"} {
		if strings.Contains(content, excluded) {
			t.Fatal("other project leaked", content)
		}
	}
	for i := 0; i < 10; i++ {
		if contextPreview(t, s, "project", "project/one")["revision"] != preview["revision"] {
			t.Fatal("unstable ordering")
		}
	}
	count := len(s.artifactReg.List(artifacts.Filter{}))
	if count != 1 {
		t.Fatal("browse retained snapshots", count)
	}
	code, ref := contextRetain(t, s, "project", "project/one", preview["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	a, _ := s.artifactReg.Get(ref["id"].(string))
	kind, id, route := contextSnapshotSource(a)
	if kind != "project" || id != "project/one" || route != "#/chat/project/project%2Fone" || a.Provenance.Task != "" {
		t.Fatal(kind, id, route, a)
	}
	if links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]; len(links) != 1 || links[0].Route != route {
		t.Fatal(links)
	}
	if code, retry := contextRetain(t, s, "project", "project/one", preview["revision"].(string)); code != 200 || retry["id"] != ref["id"] {
		t.Fatal(code, retry)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("selection modified project")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(before), "EXACT_PROJECT_INSTRUCTIONS", "CHANGED_PROJECT", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := contextRetain(t, s, "project", "project/one", preview["revision"].(string)); code != 409 {
		t.Fatal("stale selection accepted", code)
	}
	selected := []artifactContextRef{{ID: a.ID, Revision: a.Head}}
	delivered, err := s.selectedArtifactContext(true, "", "", selected, nil)
	if err != nil || !strings.Contains(delivered, "EXACT_PROJECT_INSTRUCTIONS") || !strings.Contains(delivered, `source-project="project/one"`) {
		t.Fatal(delivered, err)
	}
	if _, err = s.selectedArtifactContext(false, "", "", selected, nil); err == nil {
		t.Fatal("implicit access")
	}
	portal, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/chat/records?kind=project", "/api/chat/records/preview?kind=project&id=project%2Fone"} {
		w := httptest.NewRecorder()
		portal.ServeHTTP(w, httptest.NewRequest("GET", endpoint, nil))
		if w.Code == 200 {
			t.Fatal("portal exposes projects")
		}
	}
	if err := os.WriteFile(path, []byte("broken source"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.chatContextRecordPreview("project", "project/one"); err == nil {
		t.Fatal("invalid source became empty snapshot")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.chatProjectRecords(); err == nil {
		t.Fatal("missing source became empty list")
	}
	if links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]; len(links) != 0 {
		t.Fatal("missing source linked", links)
	}
}
func TestProjectRecordContextNativeDelivery(t *testing.T) {
	s, se, sends := terminalReceiptFixture(t, false, func(text string) {
		if !strings.Contains(text, "EXACT_PROJECT_INSTRUCTIONS") || strings.Contains(text, "OTHER_PROJECT_SECRET") {
			t.Error("wrong project context")
		}
	})
	explicitArtifactFixture(t, s)
	path := projectContextFixture(t, s)
	preview := contextPreview(t, s, "project", "project/one")
	code, ref := contextRetain(t, s, "project", "project/one", preview["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "EXACT_PROJECT_INSTRUCTIONS", "NEW_PROJECT_HEAD", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"text": "Use this project reference", "requestId": "receipt-input-001", "explicitArtifacts": true, "artifacts": []artifactContextRef{{ID: ref["id"].(string), Revision: ref["revision"].(string)}}}
	body, _ := json.Marshal(payload)
	for i := 0; i < 2; i++ {
		if w := receiptInput(s, se.ID, string(body)); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if sends.Load() != 1 {
		t.Fatal(sends.Load())
	}
	shared, sharedSE, _, sharedSends := sharedInputFixture(t, false)
	shared.artifactReg = s.artifactReg
	if w := receiptInput(shared, sharedSE.ID, string(body)); w.Code == 200 || sharedSends.Load() != 0 {
		t.Fatal("project context leaked to shared input", w.Code)
	}
	receipt, err := s.terminal.readInputReceipt(se.ID, "receipt-input-001")
	if err != nil || receipt.Task != "" || len(receipt.Artifacts) != 1 {
		t.Fatal(receipt, err)
	}
}
