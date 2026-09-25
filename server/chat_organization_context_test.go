package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/artifacts"
)

func TestOrganizationContextExactProfileAndClassification(t *testing.T) {
	s, root := personContextFixture(t)
	path := filepath.Join(root, "acme.md")
	raw := "---\naliases: [Acme Labs]\n---\nEXACT_ORGANIZATION_PROFILE\nLinked [[alice]].\n"
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.index.ReindexPaths([]string{"acme.md"}); err != nil {
		t.Fatal(err)
	}
	if err := s.contacts.MarkOrg("acme"); err != nil {
		t.Fatal(err)
	}
	if err := s.contacts.MarkOrg("unlinked org"); err != nil {
		t.Fatal(err)
	}
	code, result := artifactsDo(t, s, "GET", "/api/chat/records?kind=organization&q=labs", "")
	if code != 200 || len(result["records"].([]any)) != 1 {
		t.Fatal(code, result)
	}
	listed, _ := json.Marshal(result)
	if strings.Contains(string(listed), "EXACT_ORGANIZATION_PROFILE") {
		t.Fatal("search disclosed body")
	}
	preview := contextPreview(t, s, "organization", "acme")
	body := preview["content"].(string)
	if !strings.Contains(body, raw) || !strings.Contains(body, "explicitly marked by the owner") || strings.Contains(body, "KNOWLEDGE_SPECIALIZATION") || strings.Contains(body, "EXCLUDED_FUNDRAISING") {
		t.Fatal(body)
	}
	if _, _, err := s.chatContextRecordPreview("organization", "alice"); err == nil {
		t.Fatal("person inferred as organization")
	}
	if _, b, err := s.chatContextRecordPreview("organization", "unlinked org"); err != nil || !strings.Contains(string(b), "No profile note") {
		t.Fatal(string(b), err)
	}
	code, ref := contextRetain(t, s, "organization", "acme", preview["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	a, _ := s.artifactReg.Get(ref["id"].(string))
	kind, id, route := contextSnapshotSource(a)
	if kind != "organization" || id != "acme" || route != "#/contacts/acme" || a.Provenance.Task != "" {
		t.Fatal(kind, id, route)
	}
	if links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]; len(links) != 1 {
		t.Fatal(links)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(raw, "EXACT_ORGANIZATION_PROFILE", "CHANGED_PROFILE")), 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := contextRetain(t, s, "organization", "acme", preview["revision"].(string)); code != 409 {
		t.Fatal("stale source accepted", code)
	}
	refs := []artifactContextRef{{ID: a.ID, Revision: a.Head}}
	context, err := s.selectedArtifactContext(true, "", "", refs, nil)
	if err != nil || !strings.Contains(context, "EXACT_ORGANIZATION_PROFILE") || !strings.Contains(context, `source-organization="acme"`) {
		t.Fatal(context, err)
	}
	native, se, sends := terminalReceiptFixture(t, false, func(text string) {
		if !strings.Contains(text, "EXACT_ORGANIZATION_PROFILE") || strings.Contains(text, "CHANGED_PROFILE") {
			t.Error("wrong organization version delivered")
		}
	})
	native.artifactReg = s.artifactReg
	payload, _ := json.Marshal(map[string]any{"text": "Review this organization", "requestId": "receipt-input-001", "explicitArtifacts": true, "artifacts": refs})
	for i := 0; i < 2; i++ {
		if w := receiptInput(native, se.ID, string(payload)); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if sends.Load() != 1 {
		t.Fatal("duplicate delivery", sends.Load())
	}
	shared, sharedSE, _, sharedSends := sharedInputFixture(t, false)
	shared.artifactReg = s.artifactReg
	if w := receiptInput(shared, sharedSE.ID, string(payload)); w.Code == 200 || sharedSends.Load() != 0 {
		t.Fatal("organization leaked to shared input", w.Code)
	}
	if _, err := s.selectedArtifactContext(false, "", "", refs, nil); err == nil {
		t.Fatal("implicit private access")
	}
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/chat/records?kind=organization", nil))
	if w.Code < 400 {
		t.Fatal("public organizations", w.Code)
	}
	if err := os.MkdirAll(filepath.Join(root, "duplicate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "duplicate/acme.md"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.index.ReindexPaths([]string{"duplicate/acme.md"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.chatContextRecordPreview("organization", "acme"); err == nil {
		t.Fatal("ambiguous profile accepted")
	}
}
