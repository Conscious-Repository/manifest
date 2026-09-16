package transcriptsync

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/connectorhandoff"
)

func cutoverFixture(t *testing.T, source string) (*Service, string, string) {
	t.Helper()
	f, _ := fixtureService(t, source, func(w http.ResponseWriter, r *http.Request) {
		if source == "granola" {
			w.Write([]byte(`{"notes":[],"hasMore":false}`))
		} else {
			w.Write([]byte(`{"success":true,"data":{"recordings":[],"pagination":{"total_pages":1}}}`))
		}
	})
	root, data := t.TempDir(), t.TempDir()
	approvals.NewStore(filepath.Join(root, "artifacts"))
	path := filepath.Join(root, "vessel", "state", source, "watermark")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("2026-09-10T00:00:00Z\n"), 0600)
	cfg := Config{LegacyRoot: root, Granola: SourceConfig{Account: "fixture"}, Pocket: SourceConfig{Account: "fixture"}}
	svc := New(data, cfg, f.idx, nil)
	svc.granola = f.granola
	svc.pocket = f.pocket
	st, ah, err := svc.ReconcileCheckpoint(source, root)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := connectorhandoff.WriteCheckpoint(data, source, stagedTranscript{st, ah})
	if err != nil {
		t.Fatal(err)
	}
	return svc, root, hash
}
func transferFixture(t *testing.T, root, source, hash string, rev uint64, from, to string) {
	t.Helper()
	r := connectorhandoff.RecordFence{Version: 1, Revision: rev, Duty: "ea-coordinator/" + source + "-sync", PreviousOwner: from, Owner: to, Action: "transfer", Evidence: hash, At: time.Now().UTC().Format(time.RFC3339Nano)}
	if to == "excalibur" {
		r.Action = "rollback"
	}
	dir := filepath.Join(root, "vessel", "state", "dispatch-fence", r.Duty)
	os.MkdirAll(dir, 0700)
	b, _ := json.Marshal(r)
	// Fixture publisher only. Production uses the harness CLI, never this write.
	name := "00000000000000000001.json"
	if rev == 2 {
		name = "00000000000000000002.json"
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestCutoverRequiresMatchingFenceAndPreservesCheckpoint(t *testing.T) {
	for _, source := range []string{"granola", "pocket"} {
		t.Run(source, func(t *testing.T) {
			s, root, hash := cutoverFixture(t, source)
			p, err := s.PrepareCutover(source, root, hash)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.ApplyCutover(source, root, hash, p.Hash(), 0); err == nil {
				t.Fatal("activated without fence")
			}
			if _, err = os.Stat(s.statePath(source)); !os.IsNotExist(err) {
				t.Fatal("failed apply wrote cursor")
			}
			transferFixture(t, root, source, p.Hash(), 1, "excalibur", "manifest")
			if _, err = s.ApplyCutover(source, root, hash, p.Hash(), 0); err != nil {
				t.Fatal(err)
			}
			st, err := s.read(source)
			if err != nil || st.ImportedFrom != p.CheckpointHash || len(st.Items) != 0 {
				t.Fatal(st, err)
			}
			s.WithHandoffGuard(filepath.Dir(s.dir))
			release, err := s.enterSuccessor(source)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = connectorhandoff.AcquireFence(root, source); err == nil {
				t.Fatal("successor did not hold shared lock")
			}
			release()
			if _, err = s.ApplyCutover(source, root, hash, p.Hash(), 0); err == nil {
				t.Fatal("overwrote active cursor")
			}
			transferFixture(t, root, source, strings.Repeat("b", 64), 2, "manifest", "excalibur")
			if _, err = s.enterSuccessor(source); err == nil {
				t.Fatal("successor ignored rollback")
			}
		})
	}
}
func TestCutoverRefusesDrift(t *testing.T) {
	for _, mode := range []string{"account", "watermark", "approval", "stage", "receipt"} {
		t.Run(mode, func(t *testing.T) {
			s, root, hash := cutoverFixture(t, "granola")
			p, err := s.PrepareCutover("granola", root, hash)
			if err != nil {
				t.Fatal(err)
			}
			transferFixture(t, root, "granola", p.Hash(), 1, "excalibur", "manifest")
			switch mode {
			case "account":
				s.cfg.Granola.Account = "different"
			case "watermark":
				os.WriteFile(filepath.Join(root, "vessel/state/granola/watermark"), []byte("2026-09-11T00:00:00Z\n"), 0600)
			case "approval":
				ap := approvals.NewStore(filepath.Join(root, "artifacts"))
				_, _, err = ap.ProposeTranscript("granola", "new", approvals.Proposal{ID: "new", Action: "New", Type: approvals.TypeCreateVaultNote, ApplyPath: "2026-09-10 new.md", Proposed: "---\ngranola-id: new\n---\nbody"})
				if err != nil {
					t.Fatal(err)
				}
			case "stage":
				hash = strings.Repeat("a", 64)
			case "receipt":
				p.Account = "different"
			}
			if _, err = s.ApplyCutover("granola", root, hash, p.Hash(), 0); err == nil {
				t.Fatal("accepted drift")
			}
			if _, err = os.Stat(s.statePath("granola")); !os.IsNotExist(err) {
				t.Fatal("drift wrote cursor")
			}
		})
	}
}
func TestContinuityIsReadOnly(t *testing.T) {
	s, root, hash := cutoverFixture(t, "granola")
	p, err := s.PrepareCutover("granola", root, hash)
	if err != nil {
		t.Fatal(err)
	}
	transferFixture(t, root, "granola", p.Hash(), 1, "excalibur", "manifest")
	if _, err = s.ApplyCutover("granola", root, hash, p.Hash(), 0); err != nil {
		t.Fatal(err)
	}
	s.SetKey("granola", "fixture")
	before, _ := os.ReadFile(s.statePath("granola"))
	counts, err := s.WithHandoffGuard(filepath.Dir(s.dir)).VerifyContinuity(context.Background(), "granola")
	if err != nil || counts["listed"] != 0 {
		t.Fatal(counts, err)
	}
	after, _ := os.ReadFile(s.statePath("granola"))
	if string(before) != string(after) {
		t.Fatal("verification changed cursor")
	}
	inv, err := approvals.ReadConnectorInventoryForSource(filepath.Join(root, "artifacts"), "granola")
	if err != nil || len(inv.Items) != 0 {
		t.Fatal("verification wrote proposal", err)
	}
}

func TestReconciledUncertaintyIsTerminalOnlyWithUnchangedOwnerEvidence(t *testing.T) {
	for _, source := range []string{"granola", "pocket"} {
		t.Run(source, func(t *testing.T) {
			s, root, _ := cutoverFixture(t, source)
			id, card, path := "not_vJw8dIUwVUiWDT", "58719e5e1d11", "2026-06-25 austin.md"
			if source == "pocket" {
				id, card, path = "72886f85-9810-488e-a70a-b32ef2fd9dd6", "00fcb06ba967", "2026-08-25 raise process and communications.md"
			}
			raw := "---\nid: " + card + "\ntype: create-vault-note\napply-path: " + path + "\n---\n```proposed\n---\n" + source + "-id: " + id + "\n---\nbody\n```\n"
			os.WriteFile(filepath.Join(root, "artifacts/approvals/approved", card+".md"), []byte(raw), 0600)
			owner, inv, err := approvals.PreviewOwnerReconciliation(filepath.Join(root, "artifacts"), source)
			if err != nil {
				t.Fatal(err)
			}
			prior := Outcome{ProposalID: card, Disposition: approvals.ReconciledUncertain}
			if err = s.checkReconciledOutcome(source, id, &prior, inv, &owner); err != nil {
				t.Fatal(err)
			}
			if err = s.checkReconciledOutcome(source, id, &prior, inv, nil); err == nil {
				t.Fatal("missing receipt accepted")
			}
			prior.Replay = true
			if err = s.checkReconciledOutcome(source, id, &prior, inv, &owner); err == nil {
				t.Fatal("replay permitted")
			}
			prior.Replay = false
			if _, err = s.idx.db.Exec("INSERT INTO notes(path,"+source+"_id) VALUES (?,?)", path, id); err != nil {
				t.Fatal(err)
			}
			if err = s.checkReconciledOutcome(source, id, &prior, inv, &owner); err == nil {
				t.Fatal("new note accepted without review")
			}
		})
	}
}

func TestContinuitySchedulerPreservesCursorAndRefusesProposalPoll(t *testing.T) {
	s, root, hash := cutoverFixture(t, "granola")
	p, err := s.PrepareCutover("granola", root, hash)
	if err != nil {
		t.Fatal(err)
	}
	transferFixture(t, root, "granola", p.Hash(), 1, "excalibur", "manifest")
	if _, err = s.ApplyCutover("granola", root, hash, p.Hash(), 0); err != nil {
		t.Fatal(err)
	}
	s.SetKey("granola", "fixture")
	s.WithHandoffGuard(filepath.Dir(s.dir))
	s.cfg.Granola.Enabled = true
	s.cfg.Granola.ContinuityOnly = true
	before, _ := s.read("granola")
	if _, err = s.Poll(context.Background(), "granola"); err == nil {
		t.Fatal("continuity mode allowed proposal poll")
	}
	s.observe(context.Background(), "granola")
	after, err := s.read("granola")
	if err != nil || after.LastSuccess.IsZero() || !after.Watermark.Equal(before.Watermark) || after.ImportedFrom != before.ImportedFrom || len(after.Items) != len(before.Items) || after.Filed != 0 {
		t.Fatal(after, err)
	}
}
func TestGuardedPollRunsWithFenceAndRefusesChangedAccount(t *testing.T) {
	s, root, hash := cutoverFixture(t, "granola")
	p, err := s.PrepareCutover("granola", root, hash)
	if err != nil {
		t.Fatal(err)
	}
	transferFixture(t, root, "granola", p.Hash(), 1, "excalibur", "manifest")
	if _, err = s.ApplyCutover("granola", root, hash, p.Hash(), 0); err != nil {
		t.Fatal(err)
	}
	s.SetKey("granola", "fixture")
	s.WithHandoffGuard(filepath.Dir(s.dir))
	s.cfg.Granola.Enabled = true
	s.approvals = approvals.NewStore(filepath.Join(root, "artifacts"))
	if _, err = s.Poll(context.Background(), "granola"); err != nil {
		t.Fatal(err)
	}
	s.cfg.Granola.Account = "other"
	if _, err = s.Poll(context.Background(), "granola"); err == nil {
		t.Fatal("changed account accepted")
	}
}
