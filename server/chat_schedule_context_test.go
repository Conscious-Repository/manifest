package server

import (
	"encoding/json"
	"manifest/artifacts"
	"manifest/daily"
	"manifest/vault"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScheduleContextExactSlotDelivery(t *testing.T) {
	s, se, sends := terminalReceiptFixture(t, false, func(text string) {
		if !strings.Contains(text, "FROZEN_SLOT") || strings.Contains(text, "OTHER_SLOT") || strings.Contains(text, "PRIVATE_JOURNAL") {
			t.Error("wrong schedule context")
		}
	})
	explicitArtifactFixture(t, s)
	root := t.TempDir()
	ix, err := vault.NewIndex(vault.Config{Root: root, NewDailyDir: ""})
	if err != nil {
		t.Fatal(err)
	}
	s.svc = daily.NewService(daily.Config{VaultPath: root}, ix)
	path := filepath.Join(root, "2026-08-17.md")
	raw := "PRIVATE_JOURNAL\n<!-- manifest:start -->\n## Schedule\n| 9A | FROZEN_SLOT | x |\n| 10A | OTHER_SLOT | |\n<!-- manifest:end -->\n"
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	code, out := artifactsDo(t, s, "GET", "/api/chat/records?kind=schedule&q=2026-08-17", "")
	if code != 200 || len(out["records"].([]any)) != 2 {
		t.Fatal(code, out)
	}
	id := "2026-08-17/9A"
	preview := contextPreview(t, s, "schedule", id)
	content := preview["content"].(string)
	if !strings.Contains(content, "FROZEN_SLOT") || strings.Contains(content, "OTHER_SLOT") || strings.Contains(content, "PRIVATE_JOURNAL") {
		t.Fatal(content)
	}
	code, ref := contextRetain(t, s, "schedule", id, preview["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	a, _ := s.artifactReg.Get(ref["id"].(string))
	kind, source, route := contextSnapshotSource(a)
	if kind != "schedule" || source != id || route != "#/day/2026-08-17" || a.Provenance.Task != "" {
		t.Fatal(kind, source, route)
	}
	if links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]; len(links) != 1 || links[0].Route != route {
		t.Fatal("historical source link lost", links)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(raw, "FROZEN_SLOT", "NEW_SLOT", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := contextRetain(t, s, "schedule", id, preview["revision"].(string)); code != 409 {
		t.Fatal("stale slot accepted", code)
	}
	payload, _ := json.Marshal(map[string]any{"text": "Review this schedule slot", "requestId": "receipt-input-001", "explicitArtifacts": true, "artifacts": []artifactContextRef{{ID: a.ID, Revision: a.Head}}})
	for i := 0; i < 2; i++ {
		if w := receiptInput(s, se.ID, string(payload)); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if sends.Load() != 1 {
		t.Fatal("duplicate delivery", sends.Load())
	}
	shared, sharedSE, _, sharedSends := sharedInputFixture(t, false)
	shared.artifactReg = s.artifactReg
	if w := receiptInput(shared, sharedSE.ID, string(payload)); w.Code == 200 || sharedSends.Load() != 0 {
		t.Fatal("shared private schedule", w.Code)
	}
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/chat/records?kind=schedule", nil))
	if w.Code < 400 {
		t.Fatal("public schedule", w.Code)
	}
}
