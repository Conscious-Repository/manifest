package vaultwriter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/record"
)

func TestWriteCapabilities(t *testing.T) {
	vault, data := t.TempDir(), t.TempDir()
	w := New(vault).WithZoneRoots("system", "extrinsic").WithAudit(data).Grant(
		Capability{Name: "goals", Zone: record.ZoneKnowledge, Pattern: "goals*", Actor: ActorUserAction},
		Capability{Name: "daily", Zone: record.ZoneKnowledge, Pattern: "????-??-??.md", Actor: ActorUserAction},
		Capability{Name: "re", Zone: record.ZoneSystem, Pattern: "system/realestate/**", Actor: ActorUserAction},
	)

	// happy path: basename pattern, nested dir for daily, subtree for system
	if err := w.WriteCap("goals", "goals.md", []byte("# Goals\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteCap("daily", "Daily/2026-07-28.md", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteCap("re", "system/realestate/deals/a.md", []byte("y")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(vault, "goals.md")); err != nil {
		t.Fatal("goals.md not written")
	}

	// violations error loudly
	cases := []struct{ cap, rel string }{
		{"nope", "goals.md"},                     // undeclared capability
		{"goals", "to do.md"},                    // outside the pattern
		{"goals", "system/goals.md"},             // wrong zone
		{"re", "system/excalibur/x.md"},          // engine-owned always refused
		{"daily", "Daily/2026-07-28 meeting.md"}, // dated-prefix ≠ daily note
	}
	for _, c := range cases {
		if err := w.WriteCap(c.cap, c.rel, []byte("z")); err == nil {
			t.Fatalf("expected violation for %s/%s", c.cap, c.rel)
		}
	}

	// BindAbs refuses paths outside the vault
	if err := w.BindAbs("goals")(filepath.Join(t.TempDir(), "goals.md"), []byte("z")); err == nil {
		t.Fatal("expected outside-vault violation")
	}

	// audit: one line per successful write, with capability + actor
	raw, err := os.ReadFile(filepath.Join(data, "write-audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 {
		t.Fatalf("audit lines: %d\n%s", len(lines), raw)
	}
	if !strings.Contains(lines[0], "goals.md\tgoals\tuser-action\t+8") {
		t.Fatalf("audit line: %q", lines[0])
	}
}
