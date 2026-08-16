package vaultwriter

import (
	"testing"

	"manifest/record"
)

// The persona carve-out (persona plan Phase 1, owner decision 2026-08-16):
// system/agents/personas/ is app-writable under its capability; the rest of
// the engine-owned agents subtree stays sealed.
func TestPersonasCarveOut(t *testing.T) {
	vault, data := t.TempDir(), t.TempDir()
	w := New(vault).WithZoneRoots("system", "extrinsic").WithAudit(data).Grant(
		Capability{Name: "agent-personas", Zone: record.ZoneSystem,
			Pattern: "system/agents/**", Actor: ActorUserAction},
	)
	// seeding a persona record works
	if _, err := w.CreateRecord("system/agents/personas/brief.md", "---\nintent: brief\n---\nbody\n"); err != nil {
		t.Fatalf("persona seed must pass the guard: %v", err)
	}
	// capability writes to a persona record work
	if err := w.WriteCap("agent-personas", "system/agents/personas/info.md", []byte("x")); err != nil {
		t.Fatalf("persona write under capability: %v", err)
	}
	// the REST of the agents subtree stays engine-owned even under the capability
	if err := w.WriteCap("agent-personas", "system/agents/cornerstone.md", []byte("x")); err == nil {
		t.Fatal("non-persona agents path must stay engine-owned")
	}
	// and a write OUTSIDE the pattern under this capability fails loudly
	if err := w.WriteCap("agent-personas", "system/todo-plans/x.md", []byte("x")); err == nil {
		t.Fatal("outside-pattern write must be refused")
	}
	// raw-user edits (note view) may touch personas but not the sealed subtree
	if !w.CanUserWrite("system/agents/personas/brief.md") {
		t.Fatal("note view must be able to edit persona records")
	}
	if w.CanUserWrite("system/agents/secret.md") {
		t.Fatal("note view must not edit the sealed agents subtree")
	}
}
