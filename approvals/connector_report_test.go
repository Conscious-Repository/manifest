package approvals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconciliationEvidence(t *testing.T) {
	root := t.TempDir()
	NewStore(root)
	write := func(id, status, typ, ritual, fields string) {
		t.Helper()
		raw := "---\nid: " + id + "\ntype: " + typ + "\nritual: " + ritual + "\napply-path: secret@example.test\n---\nprivate message TOKEN_SECRET\n```proposed\n---\n" + fields + "---\nprivate body\n```\n"
		if err := os.WriteFile(filepath.Join(root, "approvals", status, id+".md"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("private-one", "pending", TypeCreateVaultNote, "email-sync", "gmail-thread-id: sensitive-identity\n")
	write("private-two", "rejected", TypeCreateVaultNote, "email-sync", "gmail-thread-id: sensitive-identity\n")
	write("private-three", "approved", TypeAppendVaultNote, "email-sync", "gmail-thread-id: sensitive-identity\n")
	write("private-four", "pending", TypeCreateVaultNote, "granola-sync", "")
	write("private-five", "pending", TypeCreateVaultNote, "pocket-sync", "pocket-id: secret-first\npocket_id: secret-second\n")
	write("private-six", "pending", TypeCreateVaultNote, "pocket-sync", "pocket-id: secret-first\npocket-id: secret-first\n")
	r := InspectConnectorInventory(root)
	a, _ := json.Marshal(r)
	b, _ := json.Marshal(InspectConnectorInventory(root))
	if string(a) != string(b) {
		t.Fatal("nondeterministic")
	}
	for _, secret := range []string{"private-", "sensitive-identity", "secret@example.test", "TOKEN_SECRET", "private body", "secret-first", "secret-second", root} {
		if strings.Contains(string(a), secret) {
			t.Fatalf("leaked %q", secret)
		}
	}
	for _, class := range []string{"duplicate-create-identity", "create-append-lineage", "rejected-duplicates", "missing-source-identity", "malformed-conflicting-aliases"} {
		if !strings.Contains(string(a), class) {
			t.Fatal("missing", class)
		}
	}
	if r.Files != 6 {
		t.Fatal(r.Files)
	}
	if _, err := ReadConnectorInventory(root); err == nil {
		t.Fatal("strict gate weakened")
	}
}

func TestReportHashMatchesStrictInventory(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	_, _, err := s.ProposeTranscript("granola", "source-one", Proposal{ID: "fixture", Type: TypeCreateVaultNote, Action: "Create", ApplyPath: "2026-09-10 fixture.md", Proposed: "---\ngranola-id: source-one\n---\nbody"})
	if err != nil {
		t.Fatal(err)
	}
	strict, err := ReadConnectorInventory(root)
	if err != nil {
		t.Fatal(err)
	}
	r := InspectConnectorInventory(root)
	if r.Hash != strict.Hash || len(r.Issues) != 0 {
		t.Fatal(r)
	}
}
