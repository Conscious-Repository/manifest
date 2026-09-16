package spirits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/connectorhandoff"
)

func TestPersistentConnectorOwnershipSurvivesDisabledFlag(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	store := NewStore(root).WithHarnessName("excalibur").WithConnectorHandoffs(data)
	path := filepath.Join(data, "connector-handoff", "granola.json")
	os.MkdirAll(filepath.Dir(path), 0700)
	r := connectorhandoff.Record{Version: 1, Source: "granola", Phase: connectorhandoff.Verified, Owner: "manifest", RollbackOwner: "excalibur", CheckpointHash: strings.Repeat("a", 64), ApprovalHash: strings.Repeat("b", 64), Evidence: map[string]string{}}
	for _, k := range []string{"dispatch-exclusion", "account-binding", "source-reconciliation", "successor-run", "restart-no-change", "approval-vaultwriter"} {
		r.Evidence[k] = strings.Repeat("c", 64)
	}
	b, _ := json.Marshal(r)
	os.WriteFile(path, b, 0600)
	if err := store.SpoolRunNow("ea-coordinator", "granola-sync", "", ""); err == nil {
		t.Fatal("verified duty dual-dispatched without config flag")
	}
	row := RitualRow{Spirit: "ea-coordinator", Ritual: "granola-sync", Valid: true}
	store.projectOwnership(&row)
	if row.MigrationState != "verified" || row.LegacyActionable || row.ConfiguredOwner != "manifest" {
		t.Fatal(row)
	}
	result, allowed, err := store.WriteFile("spirits/ea-coordinator/rituals/granola-sync.md", "---\nenabled: true\n---\n")
	if err != nil || !allowed || result.OK {
		t.Fatal("legacy re-enable accepted", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, "vessel", "spool")); !os.IsNotExist(err) {
		t.Fatal("refusal created spool")
	}
	os.WriteFile(path, []byte("bad-json"), 0600)
	if store.SpoolRunNow("ea-coordinator", "granola-sync", "", "") == nil {
		t.Fatal("corrupt evidence permitted fallback")
	}
	// The independent live Pocket duty remains available to the legacy dispatcher.
	if err := store.connectorDispatchGuard("ea-coordinator", "pocket-sync"); err != nil {
		t.Fatal(err)
	}
}
