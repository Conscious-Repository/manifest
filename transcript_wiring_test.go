package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/connectorhandoff"
	"manifest/server"
	"manifest/spirits"
	"manifest/transcriptsync"
)

func TestWorkerTranscriptOwnershipWiring(t *testing.T) {
	for _, damage := range []string{"", "disabled", "root", "data", "malformed", "account", "mode", "missing-state", "stale-handoff", "stale-fence"} {
		t.Run(damage, func(t *testing.T) {
			root, data := t.TempDir(), t.TempDir()
			put := func(path string, v any) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			c := transcriptsync.SourceConfig{Enabled: true, ContinuityOnly: true, Account: "fixture"}
			worker := struct {
				DataDir        string                `json:"dataDir"`
				TranscriptSync transcriptsync.Config `json:"transcriptSync"`
			}{data, transcriptsync.Config{LegacyRoot: root, Granola: c, Pocket: c}}
			switch damage {
			case "disabled":
				worker.TranscriptSync.Granola.Enabled = false
				worker.TranscriptSync.Pocket.Enabled = false
			case "root":
				worker.TranscriptSync.LegacyRoot = t.TempDir()
			case "data":
				worker.DataDir = t.TempDir()
			case "account":
				worker.TranscriptSync.Granola.Account = "other"
				worker.TranscriptSync.Pocket.Account = "other"
			case "mode":
				worker.TranscriptSync.Granola.ContinuityOnly = false
			}
			put(filepath.Join(data, "transcript-worker.json"), worker)
			if damage == "malformed" {
				put(filepath.Join(data, "transcript-worker.json"), "invalid")
			}
			hash := strings.Repeat("a", 64)
			for _, source := range []string{"granola", "pocket"} {
				ritual := filepath.Join(root, "spirits/ea-coordinator/rituals", source+"-sync.md")
				if err := os.MkdirAll(filepath.Dir(ritual), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(ritual, []byte("---\nritual: "+source+"-sync\nenabled: false\n---\n"), 0600); err != nil {
					t.Fatal(err)
				}
				record := connectorhandoff.Record{Version: 1, Source: source, Phase: connectorhandoff.Enabled, Owner: "manifest", RollbackOwner: "excalibur", CheckpointHash: hash, ApprovalHash: hash, Evidence: map[string]string{"dispatch-exclusion": hash, "account-binding": approvals.EvidenceHash("fixture"), "source-reconciliation": hash}}
				if damage == "stale-handoff" {
					record.Evidence["dispatch-exclusion"] = strings.Repeat("b", 64)
				}
				put(filepath.Join(data, "connector-handoff", source+".json"), record)
				fence := connectorhandoff.RecordFence{Version: 1, Revision: 1, Duty: "ea-coordinator/" + source + "-sync", PreviousOwner: "excalibur", Owner: "manifest", Action: "transfer", Evidence: hash, At: "2026-09-16T00:00:00Z"}
				if damage == "stale-fence" {
					fence.Evidence = strings.Repeat("b", 64)
				}
				put(filepath.Join(root, "vessel/state/dispatch-fence", fence.Duty, "00000000000000000001.json"), fence)
				if damage != "missing-state" {
					put(filepath.Join(data, "transcript-sync", source, "state.json"), transcriptsync.State{Version: 1, Account: "fixture", Watermark: time.Now(), ImportedFrom: hash, Items: map[string]transcriptsync.Outcome{}})
				}
			}
			// Excalibur need not be primary. The app's empty config is deliberately
			// different from the standalone worker's, as in the deployment.
			store := spirits.NewStore(root).WithHarnessName("excalibur")
			local := transcriptsync.New(data, transcriptsync.Config{}, nil, nil).WithHandoffGuard(data)
			err := wireTranscriptOwnership(Config{DataDir: data}, []server.Harness{{Name: "other", Spirits: spirits.NewStore(t.TempDir())}, {Name: "excalibur", Spirits: store}}, local)
			wantErr := damage == "data" || damage == "malformed" || damage == "mode"
			if (err != nil) != wantErr {
				t.Fatalf("wiring error: %v", err)
			}
			rows := store.Rituals(time.Now())
			if len(rows) != 2 {
				t.Fatal(rows)
			}
			for _, row := range rows {
				wantState := "blocked"
				if damage == "" || damage == "disabled" {
					wantState = "fence-protected"
				}
				if row.MigrationState != wantState || row.SuccessorEnabled != (damage == "") || row.LegacyActionable || !row.FenceProtected {
					t.Fatalf("%+v", row)
				}
				if row.SuccessorHealth == "last-success" {
					t.Fatal("wiring asserted health")
				}
			}
			if local.Enabled("granola") || local.Enabled("pocket") {
				t.Fatal("worker config enabled local poller")
			}
		})
	}
}

func TestTranscriptOwnershipWithoutWorkerConfig(t *testing.T) {
	cfg := Config{DataDir: t.TempDir()}
	local := transcriptsync.New(cfg.DataDir, transcriptsync.Config{}, nil, nil).WithHandoffGuard(cfg.DataDir)
	got, err := transcriptOwnershipService(cfg, local)
	if err != nil || got != local {
		t.Fatalf("local fallback: %p %v", got, err)
	}
	got, err = transcriptOwnershipService(cfg, nil)
	if err != nil || got != nil {
		t.Fatalf("nil fallback: %p %v", got, err)
	}
}
