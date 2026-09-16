package spirits

import (
	"encoding/json"
	"manifest/excaliburretire"
	"manifest/mdfm"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetirementExactIdentities(t *testing.T) {
	for _, p := range [][2]string{{"warden", "audit"}, {"concierge", "briefing"}, {"ea-coordinator", "waiting-on"}, {"sage", "skill-cast"}, {"extractor", "re-intake"}} {
		if RetirementReason("excalibur", p[0], p[1]) == "" {
			t.Fatal("missing retirement")
		}
		if RetirementReason("other", p[0], p[1]) != "" || RetirementReason("excalibur", p[0]+"-other", p[1]) != "" || RetirementReason("excalibur", p[0], p[1]+"-other") != "" {
			t.Fatal("overbroad retirement")
		}
	}
	for _, r := range []string{"email-sync", "granola-sync", "pocket-sync"} {
		if RetirementReason("excalibur", "ea-coordinator", r) != "" {
			t.Fatal("connector retired")
		}
	}
}

// Explicit opt-in read-only check against the stopping-point harness metadata.
func TestPhase2HarnessMetadata(t *testing.T) {
	root := os.Getenv("EXCALIBUR_PHASE2_HARNESS")
	if root == "" {
		t.Skip("set EXCALIBUR_PHASE2_HARNESS for read-only deployment verification")
	}
	st := NewStore(root).WithHarnessName("excalibur")
	n := 0
	for _, row := range st.Rituals(time.Now()) {
		if !row.Retired {
			continue
		}
		n++
		b, err := os.ReadFile(filepath.Join(root, row.Path))
		if err != nil {
			t.Fatal(err)
		}
		fm, _ := mdfm.Split(string(b))
		if fm["enabled"] != "false" || fm["paused_reason"] == "" {
			t.Fatalf("not paused: %s", row.Path)
		}
	}
	if n != 5 {
		t.Fatalf("retired metadata count=%d", n)
	}
}

func TestMigratedDutyCannotUseLegacySpool(t *testing.T) {
	store := NewStore(t.TempDir()).WithDutyOwners(map[string]string{"extractor/aion": "manifest", "ea-coordinator/pocket-sync": "manifest"})
	for _, pair := range [][2]string{{"extractor", "aion"}, {"ea-coordinator", "pocket-sync"}} {
		if err := store.SpoolRunNow(pair[0], pair[1], "fixture", ""); err == nil {
			t.Fatal("legacy spool accepted migrated duty")
		}
	}
}

func TestEngineRetirementKeepsHistoryUnavailable(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	st := NewStore(root).WithHarnessName("excalibur").WithConnectorHandoffs(data)
	dir := filepath.Join(data, "excalibur-decommission")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := excaliburretire.Plan{Version: 1}
	b := p.Bytes()
	if err := os.WriteFile(filepath.Join(dir, p.Hash()+".json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	r, _ := json.Marshal(excaliburretire.Receipt{Version: 1, PlanHash: p.Hash(), Engine: "retired; unavailable"})
	if err := os.WriteFile(filepath.Join(dir, "retired.json"), r, 0600); err != nil {
		t.Fatal(err)
	}
	if !st.EngineRetired() {
		t.Fatal("retired state missing")
	}
	if alive, _ := st.EngineAlive(); alive {
		t.Fatal("retired engine available")
	}
	if st.SpoolChatMessage("sage", "session", "message", "test") == nil || st.SpoolRunNow("sage", "anything", "", "") == nil {
		t.Fatal("retired execution allowed")
	}
	if st.DeleteSpirit("extractor") == nil || st.DeleteChatSession("session") == nil || st.DeleteRitual("extractor", "aion") == nil {
		t.Fatal("retired history deletion allowed")
	}
	if _, e := os.Stat(filepath.Join(root, "vessel")); !os.IsNotExist(e) {
		t.Fatal("runtime state created")
	}
}
