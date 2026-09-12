package spirits

import (
	"manifest/mdfm"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetirementExactIdentities(t *testing.T) {
	for _, p := range [][2]string{{"concierge", "briefing"}, {"ea-coordinator", "waiting-on"}, {"sage", "skill-cast"}} {
		if RetirementReason("excalibur", p[0], p[1]) == "" {
			t.Fatal("missing retirement")
		}
		if RetirementReason("other", p[0], p[1]) != "" || RetirementReason("excalibur", p[0], p[1]+"-other") != "" {
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
	if n != 3 {
		t.Fatalf("retired metadata count=%d", n)
	}
}
