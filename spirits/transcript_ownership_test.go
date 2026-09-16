package spirits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/connectorhandoff"
	"manifest/transcriptsync"
)

func TestTranscriptOwnershipEvidence(t *testing.T) {
	for _, source := range []string{"granola", "pocket"} {
		for _, tc := range []struct {
			name, damage, state, health             string
			enabled, schedule, protected, successor bool
		}{
			{"verified-label", "verified", "successor-enabled", "last-success", true, false, true, true},
			{"active", "", "successor-enabled", "last-success", true, false, true, true},
			{"legacy-schedule-enabled", "", "successor-enabled", "last-success", true, true, true, true},
			{"disabled", "", "fence-protected", "last-success", false, false, true, false},
			{"missing-fence", "fence", "blocked", "", true, false, false, false},
			{"missing-handoff", "handoff", "blocked", "", true, false, true, false},
			{"contradictory-phase", "phase", "blocked", "", true, false, true, false},
			{"missing-state", "state", "blocked", "", true, false, true, false},
			{"bad-checkpoint", "checkpoint", "blocked", "", true, false, true, false},
			{"bad-account", "account", "blocked", "", true, false, true, false},
			{"bad-fence-binding", "binding", "blocked", "", true, false, true, false},
			{"corrupt-fence-history", "history", "blocked", "", true, false, false, false},
			{"no-service", "service", "blocked", "", true, false, true, false},
			{"wrong-root", "root", "blocked", "", true, false, true, false},
			{"wrong-guard-dataDir", "guard", "blocked", "", true, false, true, false},
			{"wrong-state-dataDir", "data", "blocked", "", true, false, true, false},
			{"unguarded-service", "unguarded", "blocked", "", true, false, true, false},
			{"bad-account-binding", "account-binding", "blocked", "", true, false, true, false},
			{"stale-handoff", "stale", "blocked", "", true, false, true, false},
			{"unobserved", "unobserved", "fence-protected", "unknown", true, false, true, true},
			{"failed", "failed", "blocked", "error", true, false, true, true},
			{"attempt-without-success", "attempt", "fence-protected", "unknown", true, false, true, true},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
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
				hash := strings.Repeat("a", 64)
				record := connectorhandoff.Record{Version: 1, Source: source, Phase: connectorhandoff.Enabled, Owner: "manifest", RollbackOwner: "excalibur", CheckpointHash: hash, ApprovalHash: hash, Evidence: map[string]string{"dispatch-exclusion": hash, "account-binding": approvals.EvidenceHash("fixture"), "source-reconciliation": hash}}
				f := connectorhandoff.RecordFence{Version: 1, Revision: 1, Duty: "ea-coordinator/" + source + "-sync", PreviousOwner: "excalibur", Owner: "manifest", Action: "transfer", Evidence: hash, At: "2026-09-16T00:00:00Z"}
				now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
				st := transcriptsync.State{Version: 1, Account: "fixture", Watermark: now, ImportedFrom: hash, Items: map[string]transcriptsync.Outcome{}, LastAttempt: now, LastSuccess: now}
				cfg := transcriptsync.Config{LegacyRoot: root, Granola: transcriptsync.SourceConfig{Account: "fixture", Enabled: tc.enabled}, Pocket: transcriptsync.SourceConfig{Account: "fixture", Enabled: tc.enabled}}
				switch tc.damage {
				case "verified":
					record.Phase = connectorhandoff.Verified
					for _, k := range []string{"successor-run", "restart-no-change", "approval-vaultwriter"} {
						record.Evidence[k] = hash
					}
				case "phase":
					record.Phase = connectorhandoff.Paused
					record.Owner = "excalibur"
				case "checkpoint":
					st.ImportedFrom = strings.Repeat("b", 64)
				case "account":
					st.Account = "other"
				case "account-binding":
					record.Evidence["account-binding"] = approvals.EvidenceHash("other")
				case "stale":
					record.Evidence["dispatch-exclusion"] = strings.Repeat("b", 64)
				case "binding":
					f.Evidence = strings.Repeat("b", 64)
				case "root":
					cfg.LegacyRoot = t.TempDir()
				case "unobserved":
					st.LastAttempt = time.Time{}
					st.LastSuccess = time.Time{}
				case "failed":
					st.Error = "fixture upstream failure"
				case "attempt":
					st.LastAttempt = now.Add(time.Minute)
				}
				fenceDir := filepath.Join(root, "vessel/state/dispatch-fence", f.Duty)
				if tc.damage != "fence" {
					put(filepath.Join(fenceDir, "00000000000000000001.json"), f)
				}
				if tc.damage == "history" {
					put(filepath.Join(fenceDir, "00000000000000000003.json"), f)
				}
				if tc.damage != "handoff" {
					put(filepath.Join(data, "connector-handoff", source+".json"), record)
				}
				if tc.damage != "state" {
					put(filepath.Join(data, "transcript-sync", source, "state.json"), st)
				}
				store := NewStore(root).WithHarnessName("excalibur").WithConnectorHandoffs(data)
				if tc.damage != "service" {
					stateData, guardData := data, data
					if tc.damage == "guard" {
						guardData = t.TempDir()
					}
					if tc.damage == "unguarded" {
						guardData = ""
					}
					if tc.damage == "data" {
						stateData = t.TempDir()
						put(filepath.Join(stateData, "transcript-sync", source, "state.json"), st)
					}
					store.WithTranscriptSync(transcriptsync.New(stateData, cfg, nil, nil).WithHandoffGuard(guardData))
				}
				row := RitualRow{Spirit: "ea-coordinator", Ritual: source + "-sync", Valid: true, LegacyEnabled: tc.schedule}
				store.projectOwnership(&row)
				if row.MigrationState != tc.state || row.SuccessorHealth != tc.health || row.FenceProtected != tc.protected || row.SuccessorEnabled != tc.successor || row.LegacyEnabled != tc.schedule || row.LegacyActionable {
					t.Fatalf("%+v", row)
				}
				if tc.protected && row.ConfiguredOwner != "manifest" {
					t.Fatal("duty fence owner lost", row)
				}
				if tc.health != "" && (row.SuccessorLastAttempt != st.LastAttempt || row.SuccessorLastSuccess != st.LastSuccess) {
					t.Fatal("lost health timestamps", row)
				}
				// Read-side validation must not create operational lock files.
				if _, err := os.Stat(filepath.Join(fenceDir, "lock")); !os.IsNotExist(err) {
					t.Fatal("projection created fence lock", err)
				}
				if tc.protected {
					for _, guarded := range []*Store{store, NewStore(root).WithHarnessName("excalibur")} {
						if guarded.connectorDispatchGuard(row.Spirit, row.Ritual) == nil {
							t.Fatal("legacy dispatch allowed through duty fence")
						}
					}
				}
			})
		}
	}
}
