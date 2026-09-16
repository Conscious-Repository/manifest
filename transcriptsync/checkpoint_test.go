package transcriptsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/approvals"
)

func TestReconcileCheckpointPreservesDecisionsWithoutWrites(t *testing.T) {
	for _, status := range []string{"pending", "rejected", "approved", "uncertain", "duplicate-note"} {
		t.Run(status, func(t *testing.T) {
			fixture, _ := fixtureService(t, "granola", granolaFixture)
			root, data := t.TempDir(), t.TempDir()
			wm := filepath.Join(root, "vessel", "state", "granola", "watermark")
			os.MkdirAll(filepath.Dir(wm), 0700)
			os.WriteFile(wm, []byte("2026-09-10T00:00:00Z\n"), 0600)
			ap := approvals.NewStore(filepath.Join(root, "artifacts"))
			p, _, err := ap.ProposeTranscript("granola", "legacy-source", approvals.Proposal{ID: "old-card", Type: approvals.TypeCreateVaultNote, Action: "Old", ApplyPath: "2026-09-10 old.md", Proposed: "---\ngranola-id: legacy-source\n---\nTranscript"})
			if err != nil {
				t.Fatal(err)
			}
			if status == "rejected" {
				if err := ap.Reject(p.ID, "reviewed"); err != nil {
					t.Fatal(err)
				}
			}
			if status == "approved" || status == "uncertain" {
				os.Rename(filepath.Join(root, "artifacts", "approvals", "pending", p.ID+".md"), filepath.Join(root, "artifacts", "approvals", "approved", p.ID+".md"))
			}
			if status == "approved" || status == "duplicate-note" {
				_, err = fixture.idx.db.Exec("INSERT INTO notes(path,granola_id) VALUES ('log/renamed.md','legacy-source')")
				if err != nil {
					t.Fatal(err)
				}
			}
			if status == "duplicate-note" {
				fixture.idx.db.Exec("INSERT INTO notes(path,granola_id) VALUES ('log/other.md','legacy-source')")
			}
			svc := New(data, Config{Granola: SourceConfig{Account: "fixture"}}, fixture.idx, ap)
			st, hash, err := svc.ReconcileCheckpoint("granola", root)
			if status == "uncertain" || status == "duplicate-note" {
				if err == nil {
					t.Fatal("uncertain effect accepted")
				}
				return
			}
			if err != nil || st.Items["legacy-source"].ProposalID != p.ID || st.Items["legacy-source"].Disposition != status || len(hash) != 64 {
				t.Fatal(st, hash, err)
			}
			entries, _ := os.ReadDir(data)
			if len(entries) != 0 {
				t.Fatal("checkpoint wrote active state")
			}
		})
	}
}
func TestLostApprovalCannotReplayOrAdvance(t *testing.T) {
	s, ap := fixtureService(t, "granola", granolaFixture)
	st, err := s.Poll(context.Background(), "granola")
	if err != nil {
		t.Fatal(err)
	}
	p := ap.List("pending")[0]
	// The fixture artifacts are beside transcript-sync, never a live inbox.
	if err := os.Remove(filepath.Join(filepath.Dir(s.dir), "artifacts", "approvals", "pending", p.ID+".md")); err != nil {
		t.Fatal(err)
	}
	after, err := s.Poll(context.Background(), "granola")
	if err == nil || !strings.Contains(err.Error(), "replay refused") || !after.Watermark.Equal(st.Watermark) || len(ap.List("pending")) != 0 {
		t.Fatal("uncertain outcome replayed", err)
	}
}
func TestProductionFlagCannotBypassHandoff(t *testing.T) {
	s, _ := fixtureService(t, "granola", granolaFixture)
	s.WithHandoffGuard(t.TempDir())
	if _, err := s.Poll(context.Background(), "granola"); err == nil || !strings.Contains(err.Error(), "handoff evidence required") {
		t.Fatal("flag bypassed migration", err)
	}
}
