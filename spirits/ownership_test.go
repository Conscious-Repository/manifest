package spirits

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOwnershipProjectionAndControls(t *testing.T) {
	for _, tc := range []struct {
		name, harness, spirit, ritual, owner, enabled, state string
		actionable                                           bool
	}{
		{"connector", "excalibur", "ea-coordinator", "email-sync", "", "true", "legacy-retiring", true},
		{"disabled-successor", "excalibur", "ea-coordinator", "pocket-sync", "", "true", "legacy-retiring", true},
		{"dual-dispatch", "excalibur", "extractor", "aion", "manifest", "true", "ownership-conflict", false},
		{"flag-is-not-proof", "excalibur", "extractor", "aion", "manifest", "false", "handoff-unverified", false},
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
