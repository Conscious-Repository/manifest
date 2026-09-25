package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"manifest/artifacts"
)

func TestCandidateRecordContextExactVersionAndPrivacy(t *testing.T) {
	s, _, _, _ := testRecruitingServer(t)
	explicitArtifactFixture(t, s)
	if err := os.MkdirAll(s.recruiting.Path("candidates"), 0755); err != nil {
		t.Fatal(err)
	}
	path := s.recruiting.Path("candidates/context-candidate.md")
	raw := "---\nid: cand/context-candidate\nname: Same Candidate\nstage: sourced\nrole: role/mri-engineer\n---\n\n## notes\nEXACT_PRIVATE_CANDIDATE <script>literal</script>\n\n## evidence\n- [id:: ev1] [file:: unrelated-secret.md]\n"
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	other := strings.ReplaceAll(raw, "context-candidate", "other-candidate")
	other = strings.ReplaceAll(other, "EXACT_PRIVATE_CANDIDATE", "OTHER_CANDIDATE_SECRET")
	if err := os.WriteFile(s.recruiting.Path("candidates/other-candidate.md"), []byte(other), 0644); err != nil {
		t.Fatal(err)
	}
	code, result := artifactsDo(t, s, "GET", "/api/chat/records?kind=candidate&q=same", "")
	if code != 200 || len(result["records"].([]any)) != 2 {
		t.Fatal(code, result)
	}
	listed, _ := json.Marshal(result)
	if strings.Contains(string(listed), "PRIVATE_CANDIDATE") || strings.Contains(string(listed), "OTHER_CANDIDATE_SECRET") {
		t.Fatal("search leaked body")
	}
	preview := contextPreview(t, s, "candidate", "cand/context-candidate")
	content := preview["content"].(string)
	if !strings.Contains(content, raw) || strings.Contains(content, "OTHER_CANDIDATE_SECRET") {
		t.Fatal(content)
	}
	code, ref := contextRetain(t, s, "candidate", "cand/context-candidate", preview["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	a, _ := s.artifactReg.Get(ref["id"].(string))
	kind, id, route := contextSnapshotSource(a)
	if kind != "candidate" || id != "cand/context-candidate" || route != "#/aion/recruiting/candidate/cand%2Fcontext-candidate" || a.Provenance.Task != "" {
		t.Fatal(kind, id, route, a)
	}
	if links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]; len(links) != 1 || links[0].Route != route {
		t.Fatal(links)
	}
	if code, retry := contextRetain(t, s, "candidate", id, preview["revision"].(string)); code != 200 || retry["id"] != ref["id"] {
		t.Fatal(code, retry)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(raw, "EXACT_PRIVATE_CANDIDATE", "CHANGED_PRIVATE_CANDIDATE")), 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := contextRetain(t, s, "candidate", id, preview["revision"].(string)); code != 409 {
		t.Fatal("stale candidate accepted", code)
	}
	refs := []artifactContextRef{{ID: a.ID, Revision: a.Head}}
	delivered, err := s.selectedArtifactContext(true, "", "", refs, nil)
	if err != nil || !strings.Contains(delivered, "EXACT_PRIVATE_CANDIDATE") || strings.Contains(delivered, "CHANGED_PRIVATE") || !strings.Contains(delivered, `source-candidate="cand/context-candidate"`) {
		t.Fatal(delivered, err)
	}
	if _, err := s.selectedArtifactContext(false, "", "", refs, nil); err == nil {
		t.Fatal("implicit context access")
	}
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"/api/chat/records?kind=candidate", "/api/chat/records/preview?kind=candidate&id=cand%2Fcontext-candidate"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		if w.Code < 400 {
			t.Fatal("public candidate context", w.Code)
		}
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(raw, "id: cand/context-candidate", "id: cand/wrong")), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.chatContextRecordPreview("candidate", id); err == nil {
		t.Fatal("mismatched identity accepted")
	}

	if err := os.WriteFile(path, []byte(raw+strings.Repeat("x", 64001)), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.chatContextRecordPreview("candidate", id); err == nil {
		t.Fatal("oversized record accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir() + "/outside.md"
	if err := os.WriteFile(outside, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.chatContextRecordPreview("candidate", id); err == nil {
		t.Fatal("source symlink escaped recruiting root")
	}
}

func TestCandidateRecordContextNativeDelivery(t *testing.T) {
	s, se, sends := terminalReceiptFixture(t, false, func(text string) {
		if !strings.Contains(text, "FROZEN_CANDIDATE") || strings.Contains(text, "NEW_HEAD") {
			t.Error("wrong candidate version")
		}
	})
	recruitingServer, _, _, _ := testRecruitingServer(t)
	s.recruiting = recruitingServer.recruiting
	explicitArtifactFixture(t, s)
	if err := os.MkdirAll(s.recruiting.Path("candidates"), 0755); err != nil {
		t.Fatal(err)
	}
	path := s.recruiting.Path("candidates/context.md")
	raw := "---\nid: cand/context\nname: Candidate\nstage: sourced\n---\nFROZEN_CANDIDATE\n"
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	preview := contextPreview(t, s, "candidate", "cand/context")
	code, ref := contextRetain(t, s, "candidate", "cand/context", preview["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(raw, "FROZEN_CANDIDATE", "NEW_HEAD")), 0644); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"text": "Review this candidate", "requestId": "receipt-input-001", "explicitArtifacts": true, "artifacts": []artifactContextRef{{ID: ref["id"].(string), Revision: ref["revision"].(string)}}}
	body, _ := json.Marshal(payload)
	for i := 0; i < 2; i++ {
		if w := receiptInput(s, se.ID, string(body)); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if sends.Load() != 1 {
		t.Fatal("duplicate delivery", sends.Load())
	}
	shared, sharedSE, _, sharedSends := sharedInputFixture(t, false)
	shared.artifactReg = s.artifactReg
	if w := receiptInput(shared, sharedSE.ID, string(body)); w.Code == 200 || sharedSends.Load() != 0 {
		t.Fatal("private candidate reached shared input", w.Code)
	}
}
