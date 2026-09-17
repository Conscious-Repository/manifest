package spirits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/excaliburretire"
)

func TestOwnershipProjectionAndControls(t *testing.T) {
	for _, tc := range []struct {
		name, harness, spirit, ritual, owner, enabled, state string
		actionable                                           bool
	}{
		{"connector", "excalibur", "ea-coordinator", "email-sync", "", "true", "blocked", false},
		{"disabled-successor", "excalibur", "ea-coordinator", "pocket-sync", "", "true", "legacy-retiring", true},
		{"dual-dispatch", "excalibur", "extractor", "aion", "manifest", "true", "pause-pending", false},
		{"flag-is-not-proof", "excalibur", "extractor", "aion", "manifest", "false", "pause-pending", false},
		{"retired", "excalibur", "sage", "skill-cast", "", "false", "retired", false},
		{"retirement-drift", "excalibur", "sage", "skill-cast", "", "true", "retirement-conflict", false},
		{"other-harness", "team", "extractor", "aion", "", "true", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			rel := "spirits/" + tc.spirit + "/rituals/" + tc.ritual + ".md"
			file := filepath.Join(root, rel)
			if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
				t.Fatal(err)
			}
			body := "---\nritual: " + tc.ritual + "\nenabled: " + tc.enabled + "\n---\nhistory\n"
			if err := os.WriteFile(file, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			st := NewStore(root).WithHarnessName(tc.harness).WithDutyOwners(map[string]string{tc.spirit + "/" + tc.ritual: tc.owner})
			rows := st.Rituals(time.Now())
			if len(rows) != 1 {
				t.Fatal(rows)
			}
			r := rows[0]
			if r.Harness != tc.harness || r.Path != rel || r.MigrationState != tc.state || r.LegacyActionable != tc.actionable || r.LegacyEnabled != (tc.enabled == "true") {
				t.Fatalf("projection: %+v", r)
			}
			if tc.owner != "" {
				if len(st.Spirits()[tc.spirit]) != 0 || len(st.Castables(time.Now())) != 0 {
					t.Fatal("legacy picker advertises successor duty")
				}
				if err := st.SpoolRunNow(tc.spirit, tc.ritual, "test", ""); err == nil {
					t.Fatal("legacy spool accepted")
				}
				res, allowed, err := st.WriteFile(rel, strings.Replace(body, "enabled: false", "enabled: true", 1))
				if err != nil || !allowed || res.OK {
					t.Fatalf("resume accepted: %+v %v", res, err)
				}
				after, _ := os.ReadFile(file)
				if string(after) != body {
					t.Fatal("refused edit changed file")
				}
			}
		})
	}
}

func TestExtractorFenceRefusesLegacyWithoutEnableFlag(t *testing.T) {
	for _, ritual := range []string{"aion", "real-estate", "ooda-email"} {
		root := t.TempDir()
		path := filepath.Join(root, "vessel/state/dispatch-fence/extractor", ritual)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		body := `{"version":1,"revision":1,"duty":"extractor/` + ritual + `","previous_owner":"excalibur","owner":"manifest","action":"transfer","evidence_sha256":"` + strings.Repeat("a", 64) + `","at":"2026-09-16T00:00:00Z"}`
		if err := os.WriteFile(filepath.Join(path, "00000000000000000001.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		st := NewStore(root).WithHarnessName("excalibur")
		if err := st.SpoolRunNow("extractor", ritual, "probe", ""); err == nil {
			t.Fatal("legacy dispatch accepted")
		}
		row := RitualRow{Spirit: "extractor", Ritual: ritual, Valid: true, LegacyEnabled: true}
		st.projectOwnership(&row)
		if row.ConfiguredOwner != "manifest" || row.LegacyActionable || row.MigrationState != "successor-disabled" {
			t.Fatal(row)
		}
		data := t.TempDir()
		dir := filepath.Join(data, "excalibur-decommission")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		plan := excaliburretire.Plan{Version: 1}
		receipt, _ := json.Marshal(excaliburretire.Receipt{Version: 1, PlanHash: plan.Hash(), Engine: "retired; unavailable"})
		for name, body := range map[string][]byte{plan.Hash() + ".json": plan.Bytes(), "retired.json": receipt} {
			if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
		st.WithConnectorHandoffs(data)
		st.WithDutyOwners(map[string]string{"extractor/" + ritual: "manifest"})
		row = RitualRow{Spirit: "extractor", Ritual: ritual, Valid: true, LegacyEnabled: false, Retired: true, PausedReason: "legacy retired"}
		st.projectOwnership(&row)
		if !row.EngineRetired || row.LegacyEnabled || !row.Enabled || !row.SuccessorEnabled || !row.FenceProtected || row.CapabilityPaused || row.Retired || row.PausedReason != "" || row.LegacyActionable || row.Provider != "lab-sparks" || row.Model != "deepseek-v4.1-flash" {
			t.Fatal(row)
		}
		// Exercise the complete board projection with disabled legacy files.
		duties := [][2]string{{"extractor", ritual}, {"concierge", "briefing"}, {"ea-coordinator", "waiting-on"}, {"sage", "skill-cast"}, {"warden", "audit"}, {"extractor", "re-intake"}}
		for _, duty := range duties {
			file := filepath.Join(root, "spirits", duty[0], "rituals", duty[1]+".md")
			if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("---\nritual: "+duty[1]+"\nenabled: false\n---\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		rows := st.Rituals(time.Now())
		if len(rows) != len(duties) {
			t.Fatal(rows)
		}
		for _, got := range rows {
			migrated := got.Spirit == "extractor" && got.Ritual == ritual
			if got.Enabled != migrated || got.Retired == migrated || got.SuccessorEnabled != migrated || got.LegacyActionable || !got.EngineRetired {
				t.Fatalf("board projection: %+v", got)
			}
			if migrated && (got.Cadence != "" || got.NextFire != "" || got.PausedReason != "" || got.RetirementReason != "") {
				t.Fatalf("on-demand successor inherited legacy state: %+v", got)
			}
		}
	}
}
