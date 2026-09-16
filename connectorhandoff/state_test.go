package connectorhandoff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"manifest/approvals"
)

func TestEmailCheckpointPreservesLedgerAndRefusesAmbiguity(t *testing.T) {
	raw := []byte(`{"watermark":"2026-09-10T00:00:00Z","threads":{"thread1":{"status":"proposed","proposal_id":"old-id","last_msg_id":"message1","last_internal_ms":123,"filename":"2026-09-10 call.md"}}}`)
	inv := approvals.ConnectorInventory{Hash: strings.Repeat("a", 64), Items: []approvals.ConnectorApproval{{ID: "old-id", Source: "gmail-thread", SourceID: "thread1", Type: approvals.TypeCreateVaultNote, Status: "pending"}}}
	cp, err := PrepareEmail(raw, "fixture@example.test", inv)
	if err != nil {
		t.Fatal(err)
	}
	th := cp.Threads["thread1"]
	if th.Status != "proposed" || th.ProposalID != "old-id" || th.LastMsgID != "message1" || th.LastInternalMS != 123 || th.Filename != "2026-09-10 call.md" || len(cp.Uncertain) != 0 {
		t.Fatal("ledger changed")
	}
	inv.Items[0].Status = "approved"
	cp, err = PrepareEmail(raw, "fixture@example.test", inv)
	if err != nil || len(cp.Uncertain) != 1 || cp.Threads["thread1"].Status != "proposed" {
		t.Fatal("uncertain effect silently advanced", err)
	}
	for _, bad := range []string{
		`{"watermark":"2026-09-10T00:00:00Z","threads":{"x":{"status":"muted"},"x":{"status":"synced"}}}`,
		`{"watermark":"2026-09-10T00:00:00Z","threads":{"x":{"status":"muted","status":"synced"}}}`,
		`{"watermark":"bad","threads":{}}`,
		`{"watermark":"2026-09-10T00:00:00Z","threads":{"x":null}}`,
		string(raw) + ` {}`,
	} {
		if _, err := PrepareEmail([]byte(bad), "fixture", inv); err == nil {
			t.Fatal("ambiguous state accepted")
		}
	}
	inv.Items[0].SourceID = "another-thread"
	if _, err := PrepareEmail(raw, "fixture", inv); err == nil {
		t.Fatal("conflicting approval accepted")
	}
}
func TestCheckpointAtomicNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	wins := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, e := WriteCheckpoint(dir, "email", map[string]string{"version": "fixture"})
			if e == nil {
				wins <- h
			}
		}()
	}
	wg.Wait()
	close(wins)
	if len(wins) != 1 {
		t.Fatal("atomic no-replace failed", len(wins))
	}
	h := <-wins
	path := filepath.Join(dir, "connector-handoff", "checkpoints", "email", h+".json")
	b, err := os.ReadFile(path)
	if err != nil || !json.Valid(b) {
		t.Fatal("partial checkpoint", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("checkpoint permissions")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("staging files leaked")
	}
}
func TestLegacyGuardAndVerificationEvidence(t *testing.T) {
	dir := t.TempDir()
	if err := LegacyAllowed(dir, "granola"); err != nil {
		t.Fatal("live legacy duty blocked without migration", err)
	}
	path := filepath.Join(dir, "connector-handoff", "granola.json")
	os.MkdirAll(filepath.Dir(path), 0700)
	r := Record{Version: 1, Source: "granola", Phase: Verified, Owner: "manifest", RollbackOwner: "excalibur", CheckpointHash: strings.Repeat("a", 64), ApprovalHash: strings.Repeat("b", 64)}
	if r.Validate() == nil {
		t.Fatal("verified inferred without evidence")
	}
	r.Evidence = map[string]string{}
	for _, k := range []string{"dispatch-exclusion", "account-binding", "source-reconciliation", "successor-run", "restart-no-change", "approval-vaultwriter"} {
		r.Evidence[k] = strings.Repeat("c", 64)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	os.WriteFile(path, b, 0600)
	if LegacyAllowed(dir, "granola") == nil {
		t.Fatal("verified successor fell back to legacy")
	}
	os.WriteFile(path, []byte("broken"), 0600)
	if LegacyAllowed(dir, "granola") == nil {
		t.Fatal("corrupt evidence fell back to legacy")
	}
}
func TestPauseDoesNotEstablishDispatchExclusion(t *testing.T) {
	root := t.TempDir()
	ritual := filepath.Join(root, "spirits", "ea-coordinator", "rituals", "pocket-sync.md")
	os.MkdirAll(filepath.Dir(ritual), 0700)
	os.MkdirAll(filepath.Join(root, "vessel", "spool"), 0700)
	os.MkdirAll(filepath.Join(root, "artifacts", "runs"), 0700)
	os.WriteFile(ritual, []byte("---\nenabled: true\n---\n"), 0600)
	if CheckLegacyPause(root, "pocket") == nil {
		t.Fatal("enabled schedule accepted")
	}
	os.WriteFile(ritual, []byte("---\nenabled: false\npaused_reason: fixture\n---\n"), 0600)
	if err := CheckLegacyPause(root, "pocket"); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(root, "vessel", "spool", "unknown.claim")
	os.WriteFile(spool, []byte("unreadable claim"), 0600)
	if CheckLegacyPause(root, "pocket") == nil {
		t.Fatal("uncertain claim accepted")
	}
	os.Remove(spool)
	os.WriteFile(filepath.Join(root, "artifacts", "runs", "fixture.md"), []byte("---\noutcome: running\n---\n"), 0600)
	if CheckLegacyPause(root, "pocket") == nil {
		t.Fatal("running work accepted")
	}
}
