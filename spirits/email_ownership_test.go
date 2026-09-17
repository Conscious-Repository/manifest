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
	"manifest/personalemail"
)

func TestEmailOwnershipEvidence(t *testing.T) {
	for _, tc := range []struct {
		name              string
		enabled, schedule bool
		damage            string
		owner, state      string
		protected         bool
	}{
		{"disabled-worker", false, false, "", "manifest", "fence-protected", true},
		{"active-successor", true, false, "", "manifest", "successor-enabled", true},
		{"enabled-legacy-schedule", true, true, "", "manifest", "successor-enabled", true},
		{"missing-fence", true, false, "missing-fence", "excalibur", "blocked", false},
		{"contradictory-owner", true, false, "rollback", "excalibur", "blocked", false},
		{"wrong-hash", true, false, "hash", "excalibur", "blocked", true},
		{"missing-activation", true, false, "activation", "excalibur", "blocked", true},
		{"stale-label", true, false, "label", "excalibur", "blocked", true},
		{"corrupt-history", true, false, "history", "excalibur", "blocked", false},
		{"config-drift", true, false, "config", "excalibur", "blocked", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, data := t.TempDir(), t.TempDir()
			put := func(path string, value any) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			token := filepath.Join(data, "token")
			t.Setenv("GMAIL_TOKEN", token)
			t.Setenv("EMAIL_ACCOUNTS_FILE", filepath.Join(data, "settings"))
			t.Setenv("GMAIL_ACCOUNTS_DIR", filepath.Join(data, "accounts"))
			put(token, map[string]any{"email": "fixture@example.com", "token": map[string]string{}})
			put(filepath.Join(data, "settings"), map[string]any{"accounts": map[string]any{"fixture@example.com": map[string]any{"sync": true, "extract": false, "workspace": ""}}})
			c := personalemail.Config{Account: "fixture@example.com", LegacyRoot: root, DataDir: data, Index: filepath.Join(data, "index.db"), BackfillDays: 30}
			put(filepath.Join(root, "vessel/state/email/state.json"), map[string]any{"watermark": "2026-09-01T00:00:00Z", "threads": map[string]any{}})
			svc := personalemail.New(c, nil, approvals.NewStore(filepath.Join(root, "artifacts")))
			plan, err := svc.Prepare()
			if err != nil {
				t.Fatal(err)
			}
			fenceDir := filepath.Join(root, "vessel/state/dispatch-fence/ea-coordinator/email-sync")
			fencePath := filepath.Join(fenceDir, "00000000000000000001.json")
			f := connectorhandoff.RecordFence{Version: 1, Revision: 1, Duty: "ea-coordinator/email-sync", PreviousOwner: "excalibur", Owner: "manifest", Action: "transfer", Evidence: plan.Hash(), At: time.Now().UTC().Format(time.RFC3339Nano)}
			put(fencePath, f)
			if _, err := svc.Apply(plan.Hash(), 0); err != nil {
				t.Fatal(err)
			}
			c.Enabled = tc.enabled
			switch tc.damage {
			case "missing-fence":
				if err := os.Remove(fencePath); err != nil {
					t.Fatal(err)
				}
			case "rollback":
				f.Revision = 2
				f.PreviousOwner = "manifest"
				f.Owner = "excalibur"
				f.Action = "rollback"
				put(filepath.Join(fenceDir, "00000000000000000002.json"), f)
			case "hash":
				f.Evidence = strings.Repeat("a", 64)
				put(fencePath, f)
			case "activation":
				if err := os.Remove(filepath.Join(data, "personal-email/active.json")); err != nil {
					t.Fatal(err)
				}
			case "label":
				put(filepath.Join(data, "connector-handoff/email.json"), connectorhandoff.Record{Version: 1, Source: "email", Phase: connectorhandoff.NotReady, Owner: "excalibur", RollbackOwner: "excalibur"})
			case "history":
				put(filepath.Join(fenceDir, "00000000000000000003.json"), f)
			case "config":
				c.BackfillDays++
			}
			put(filepath.Join(data, "personal-email-worker.json"), c)
			store := NewStore(root).WithHarnessName("excalibur").WithConnectorHandoffs(data)
			row := RitualRow{Spirit: "ea-coordinator", Ritual: "email-sync", Valid: true, LegacyEnabled: tc.schedule, Retired: true, RetirementReason: "old engine retired", PausedReason: "old schedule paused"}
			store.projectOwnership(&row)
			if row.ConfiguredOwner != tc.owner || row.MigrationState != tc.state || row.LegacyActionable || row.FenceProtected != tc.protected || row.SuccessorEnabled != (tc.state == "successor-enabled") || row.LegacyEnabled != tc.schedule {
				t.Fatalf("%+v", row)
			}
			if row.SuccessorEnabled && (!row.Enabled || row.Retired || row.CapabilityPaused || row.PausedReason != "" || row.RetirementReason != "") {
				t.Fatalf("successor inherited predecessor retirement: %+v", row)
			}
			if tc.protected {
				for _, guarded := range []*Store{store, NewStore(root).WithHarnessName("excalibur")} {
					if guarded.connectorDispatchGuard(row.Spirit, row.Ritual) == nil || guarded.SpoolRunNow(row.Spirit, row.Ritual, "fixture", "") == nil {
						t.Fatal("legacy dispatch allowed through fence")
					}
					result, allowed, err := guarded.WriteFile("spirits/ea-coordinator/rituals/email-sync.md", "---\nenabled: true\n---\n")
					if err != nil || !allowed || result.OK {
						t.Fatal("legacy resume allowed through fence", result, err)
					}
				}
			}
			if tc.state != "blocked" && !strings.Contains(row.MigrationDetail, "not liveness, semantic parity or final decommission") {
				t.Fatal(row.MigrationDetail)
			}
		})
	}
}
