package approvals

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConnectorInventoryReadOnlyAndConflicts(t *testing.T) {
	empty := t.TempDir()
	if _, err := ReadConnectorInventory(empty); err == nil {
		t.Fatal("missing history accepted")
	}
	entries, _ := os.ReadDir(empty)
	if len(entries) != 0 {
		t.Fatal("dry-run created inbox")
	}
	root := t.TempDir()
	s := NewStore(root)
	p, _, err := s.ProposeTranscript("granola", "source-one", Proposal{ID: "old123", Type: TypeCreateVaultNote, Action: "Create", ApplyPath: "2026-09-10 fixture.md", Proposed: "---\ngranola-id: source-one\n---\nPrivate body"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := ReadConnectorInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 1 || a.Items[0].ID != p.ID {
		t.Fatal(a)
	}
	if err := s.Reject(p.ID, "no"); err != nil {
		t.Fatal(err)
	}
	b, err := ReadConnectorInventory(root)
	if err != nil || b.Hash == a.Hash || b.Items[0].Status != "rejected" {
		t.Fatal("decision not hashed", err)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, "rejected", p.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(s.dir, "pending", p.ID+".md"), raw, 0600)
	if _, err := ReadConnectorInventory(root); err == nil {
		t.Fatal("conflicting canonical decisions accepted")
	}
	if _, _, err := s.ProposeTranscript("granola", "source-one", p); err == nil {
		t.Fatal("first-match lookup hid duplicate decision")
	}
	os.Remove(filepath.Join(s.dir, "pending", p.ID+".md"))
	old, err := s.parse(filepath.Join(s.dir, "rejected", p.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	old.ID = "different-id"
	os.WriteFile(filepath.Join(s.dir, "pending", old.ID+".md"), []byte(serialize(old)), 0600)
	if _, err := ReadConnectorInventory(root); err == nil {
		t.Fatal("two proposal IDs for one source accepted")
	}
}
