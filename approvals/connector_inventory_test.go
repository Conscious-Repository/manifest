package approvals

import (
	"os"
	"path/filepath"
	"strings"
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

func TestConnectorInventorySourceScope(t *testing.T) {
	root := t.TempDir()
	NewStore(root)
	write := func(status, id, metadata, proposed string) {
		t.Helper()
		raw := "---\nid: " + id + "\ntype: create-vault-note\n" + metadata + "---\n```proposed\n---\n" + proposed + "---\nprivate body\n```\n"
		if err := os.WriteFile(filepath.Join(root, "approvals", status, id+".md"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("pending", "granola-card", "ritual: granola-sync\n", "granola-id: granola-one\n")
	write("pending", "pocket-card", "ritual: pocket-sync\n", "pocket_id: pocket-one\n")
	before, err := ReadConnectorInventoryForSource(root, "granola")
	if err != nil {
		t.Fatal(err)
	}
	write("pending", "gmail-card", "gmail-thread-id: thread-one\n", "")
	write("rejected", "gmail-card", "gmail-thread-id: thread-one\n", "")
	for _, source := range []string{"granola", "pocket"} {
		inv, err := ReadConnectorInventoryForSource(root, source)
		if err != nil || len(inv.Items) != 1 || inv.Items[0].Source != source {
			t.Fatalf("%s: %+v %v", source, inv, err)
		}
		if source == "granola" && inv.Hash != before.Hash {
			t.Fatal("unrelated history changed scoped hash")
		}
	}
	for _, source := range []string{"email", "gmail-thread", "invalid"} {
		if _, err := ReadConnectorInventoryForSource(root, source); err == nil {
			t.Fatalf("accepted conflict or invalid source: %s", source)
		}
	}
	if _, err := ReadConnectorInventory(root); err == nil {
		t.Fatal("global audit ignored Gmail conflict")
	}
	write("approved", "granola-card", "ritual: granola-sync\n", "granola-id: granola-one\n")
	if _, err := ReadConnectorInventoryForSource(root, "granola"); err == nil {
		t.Fatal("same-source conflicting decisions accepted")
	}
}

func TestConnectorInventorySourceAttributionFailsClosed(t *testing.T) {
	for _, tc := range []struct{ name, metadata, proposed string }{
		{"unattributed", "", ""},
		{"unknown-ritual", "ritual: unknown-sync\n", ""},
		{"delegate-conflicting-aliases", "ritual: delegate\n", "granola-id: one\ngranola_id: two\n"},
		{"delegate-empty-alias", "ritual: delegate\n", "granola-id: \n"},
		{"missing-id", "ritual: granola-sync\n", ""},
		{"mixed-identities", "", "granola-id: one\ngmail-thread-id: two\n"},
		{"mixed-ritual", "ritual: granola-sync\n", "gmail-thread-id: two\n"},
		{"conflicting-aliases", "", "granola-id: one\ngranola_id: two\n"},
		{"conflicting-locations", "granola-id: two\n", "granola-id: one\n"},
		{"repeated-alias", "", "granola-id: one\ngranola-id: two\n"},
		{"empty-alias", "", "granola-id: \n"},
		{"malformed-id", "", "granola-id: private/value\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			NewStore(root)
			raw := "---\nid: private-card\ntype: create-vault-note\n" + tc.metadata + "---\n```proposed\n---\n" + tc.proposed + "---\nprivate body\n```\n"
			if err := os.WriteFile(filepath.Join(root, "approvals", "pending", "private-card.md"), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadConnectorInventoryForSource(root, "granola"); err == nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("attribution accepted or leaked private evidence: %v", err)
			}
		})
	}
}

func TestConnectorInventoryKnownNonConnectorRituals(t *testing.T) {
	for _, source := range []string{"granola", "pocket", "email"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			NewStore(root)
			identity := source
			if source == "email" {
				identity = "gmail-thread"
			}
			write := func(status, id, typ, metadata, proposed string) {
				t.Helper()
				raw := "---\nid: " + id + "\ntype: " + typ + "\n" + metadata + "---\n```proposed\n---\n" + proposed + "---\nprivate body\n```\n"
				if err := os.WriteFile(filepath.Join(root, "approvals", status, id+".md"), []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("pending", "target", TypeCreateVaultNote, "ritual: "+source+"-sync\n", identity+"-id: one\n")
			before, err := ReadConnectorInventoryForSource(root, source)
			if err != nil || len(before.Items) != 1 {
				t.Fatalf("clean target: %+v %v", before, err)
			}
			for _, ritual := range []string{"delegate", "waiting-on"} {
				for _, typ := range []string{TypeCreateVaultNote, TypeAppendVaultNote} {
					write("rejected", ritual+"-"+typ, typ, "ritual: "+ritual+"\n", "")
				}
			}
			inv, err := ReadConnectorInventoryForSource(root, source)
			if err != nil || len(inv.Items) != 1 || inv.Items[0] != before.Items[0] || inv.Hash != before.Hash {
				t.Fatalf("non-connector history affected scope: %+v %v", inv, err)
			}
			global, err := ReadConnectorInventory(root)
			if err != nil || len(global.Items) != 3 {
				t.Fatalf("global audit lost unattributed creates: %+v %v", global, err)
			}
			for _, item := range global.Items {
				if item.ID != "target" && (item.Source != "" || item.SourceID != "" || item.Status != "rejected") {
					t.Fatalf("global audit changed unattributed create: %+v", item)
				}
			}
			for _, metadata := range []string{"", "ritual: unknown-sync\n", "ritual: " + source + "-sync\n"} {
				for _, typ := range []string{TypeCreateVaultNote, TypeAppendVaultNote} {
					write("rejected", "ambiguous", typ, metadata, "")
					if _, err := ReadConnectorInventoryForSource(root, source); err == nil {
						t.Fatalf("accepted missing identity: type=%s metadata=%q", typ, metadata)
					}
				}
			}
		})
	}
}
