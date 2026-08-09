package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/approvals"
)

type fakeSink struct{ got [][]string }

func (f *fakeSink) Notify(paths []string) { f.got = append(f.got, paths) }

// TestConfirmNudgesExtractionSink: confirming a transcript proposal hands
// the WRITTEN path (post-retitle, lowercased, under log/) to the aion sink
// — the instant extraction trigger. The sink itself re-checks categories,
// so the nudge fires for every create-vault-note.
func TestConfirmNudgesExtractionSink(t *testing.T) {
	vault := t.TempDir()
	harness := filepath.Join(vault, "excalibur")
	if err := os.MkdirAll(harness, 0o755); err != nil {
		t.Fatal(err)
	}
	store := approvals.NewStore(filepath.Join(harness, "artifacts")).WithVaultRoot(vault)
	content := "---\ncategories:\n  - sync\ngranola-id: not_x\n---\n\n## Transcript\n\n**B:** hi\n"
	body := "New transcript.\n\n````proposed\n" + content + "\n````"
	md := "---\ntype: create-vault-note\nid: abcdefabcdef\naction: Create vault note: 2026-08-07 RJ Sync.md\nagent: ea-coordinator\ncreated: 2026-08-07T08:00:00Z\napply-path: 2026-08-07 RJ Sync.md\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(harness, "artifacts", "approvals", "pending", "abcdefabcdef.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	sink := &fakeSink{}
	srv := &Server{}
	srv.UseApprovals(store)
	srv.UseAionSink(sink)

	req := httptest.NewRequest("POST", "/api/spirits/approvals/abcdefabcdef/confirm",
		strings.NewReader(`{"editCategories":true,"categories":["sync","aion"],"title":"rj weekly"}`))
	req.SetPathValue("id", "abcdefabcdef")
	rec := httptest.NewRecorder()
	srv.handleSpiritsApprovalConfirm(rec, req)
	if rec.Code != 200 {
		t.Fatalf("confirm: %d %s", rec.Code, rec.Body.String())
	}
	// the note landed with the edited categories AND retitled filename
	got, err := os.ReadFile(filepath.Join(vault, "log", "2026-08-07 rj weekly.md"))
	if err != nil {
		t.Fatalf("note not written: %v", err)
	}
	if !strings.Contains(string(got), "categories: [sync, aion]") {
		t.Fatalf("categories not applied:\n%s", got)
	}
	// the sink was nudged with the post-retitle written path
	if len(sink.got) != 1 || len(sink.got[0]) != 1 || sink.got[0][0] != "log/2026-08-07 rj weekly.md" {
		t.Fatalf("sink nudge: %v", sink.got)
	}
}
