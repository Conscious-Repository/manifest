package vaultwriter

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/record"
)

// An audit that cannot open its log must not lose the owner's write — but it
// must not vanish either: the failure is logged loudly and surfaced on the
// writer (count + last error) so a landed write with no trace is visible.
func TestAuditFailureIsSurfaced(t *testing.T) {
	vault := t.TempDir()
	// the audit dir does not exist and cannot be created by OpenFile, so every
	// audit append fails
	missing := filepath.Join(t.TempDir(), "nope", "deeper")
	w := New(vault).WithZoneRoots("system", "extrinsic").WithAudit(missing).Grant(
		Capability{Name: "goals", Zone: record.ZoneKnowledge, Pattern: "goals*", Actor: ActorUserAction},
	)
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(prev)

	if n, err := w.AuditFailure(); n != 0 || err != nil {
		t.Fatalf("fresh writer reports failures: %d %v", n, err)
	}
	if err := w.WriteCap("goals", "goals.md", []byte("# Goals\n")); err != nil {
		t.Fatalf("the owner's write must still land: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(vault, "goals.md")); string(got) != "# Goals\n" {
		t.Fatalf("write lost: %q", got)
	}
	n, err := w.AuditFailure()
	if n != 1 || err == nil {
		t.Fatalf("audit failure not surfaced: n=%d err=%v", n, err)
	}
	if !strings.Contains(err.Error(), "write-audit: goals.md") {
		t.Fatalf("audit error names neither the lane nor the path: %v", err)
	}
	if !strings.Contains(logged.String(), "WRITE LANDED WITHOUT AUDIT TRACE") {
		t.Fatalf("audit failure not logged: %q", logged.String())
	}
	// a rename through the same lane counts too
	if err := w.RenameCap("goals", "goals.md", "goals 2026q3.md"); err != nil {
		t.Fatal(err)
	}
	if n, _ := w.AuditFailure(); n != 2 {
		t.Fatalf("rename audit failure not counted: %d", n)
	}
}

// The healthy path reports nothing: a writer whose audit log opens keeps a
// zero failure count after writing.
func TestAuditHealthyReportsNoFailure(t *testing.T) {
	vault, data := t.TempDir(), t.TempDir()
	w := New(vault).WithAudit(data).Grant(
		Capability{Name: "goals", Zone: record.ZoneKnowledge, Pattern: "goals*", Actor: ActorUserAction},
	)
	if err := w.WriteCap("goals", "goals.md", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if n, err := w.AuditFailure(); n != 0 || err != nil {
		t.Fatalf("healthy audit reported a failure: %d %v", n, err)
	}
}
