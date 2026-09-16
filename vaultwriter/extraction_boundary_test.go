package vaultwriter

import (
	"os"
	"path/filepath"
	"testing"

	"manifest/record"
)

// A lock held by Manifest cannot prevent an external editor from changing a
// dependency after validation. This deterministic interleaving demonstrates why
// a matching extraction preflight cannot authorize a later WriteCap.
func TestExtractionExternalEditorBoundary(t *testing.T) {
	vault := t.TempDir()
	source := filepath.Join(vault, "source.md")
	if err := os.WriteFile(source, []byte("reviewed"), 0600); err != nil {
		t.Fatal(err)
	}
	w := New(vault).WithAudit(t.TempDir()).Grant(Capability{Name: "extract", Zone: record.ZoneSystem, Pattern: "system/**", Actor: ActorApprovedProposal})
	checked := make(chan struct{})
	edited := make(chan struct{})
	go func() {
		<-checked
		err := os.WriteFile(source, []byte("owner edit"), 0600)
		if err != nil {
			t.Error(err)
		}
		close(edited)
	}()
	err := w.UpdateCap("extract", "system/one.md", func(before []byte) ([]byte, error) {
		b, e := os.ReadFile(source)
		if e != nil {
			return nil, e
		}
		if string(b) != "reviewed" {
			t.Error("invalid fixture")
		}
		close(checked)
		<-edited // external edit occurs while editMu is held
		return []byte("derived from reviewed source"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(vault, "system/one.md"))
	if err != nil || string(b) != "derived from reviewed source" {
		t.Fatalf("%s %v", b, err)
	}
	b, err = os.ReadFile(source)
	if err != nil || string(b) != "owner edit" {
		t.Fatalf("%s %v", b, err)
	}
	// The second file can fail after the first is visible. Per-file renames are
	// not a transaction; recovery must never roll back the subsequent owner edit.
	if err = w.WriteCap("undeclared", "system/two.md", []byte("second")); err == nil {
		t.Fatal("undeclared write allowed")
	}
	if _, err = os.Stat(filepath.Join(vault, "system/one.md")); err != nil {
		t.Fatal("first write unexpectedly atomic with second")
	}
	if _, err = os.Stat(filepath.Join(vault, "system/two.md")); !os.IsNotExist(err) {
		t.Fatal("second write landed")
	}
}
