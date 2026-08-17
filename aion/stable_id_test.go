package aion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStableIDMigrationPreservesLegacyAndRepairsCollisions(t *testing.T) {
	vault := t.TempDir()
	root := filepath.Join(vault, "system", "aion")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "# AION — backlog\n\n## Tasks\n- [ ] Legacy title [kind:: task]\n- [ ] First [id:: aion-bl/fixed] [kind:: task]\n- [ ] Second [id:: aion-bl/fixed] [kind:: task]\n  - [ ] Child [kind:: task]\n\n## Decisions\n"
	if err := os.WriteFile(filepath.Join(root, "backlog.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	portal := t.TempDir()
	contract := filepath.Join(portal, "server", "web", "portal", "data")
	if err := os.MkdirAll(contract, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contract, "backlog.json"), []byte(`{"items":[{"id":"aion-bl/preserved","kind":"task","title":"Legacy title"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(vault, "system/aion", func(path string, data []byte) error { return os.WriteFile(path, data, 0o644) })
	n, collisions, err := store.EnsureStableIDsFromPortal(portal)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || collisions != 1 {
		t.Fatalf("migration = %d changed, %d collisions", n, collisions)
	}
	doc := store.LoadBacklog()
	all := doc.AllItems()
	want := []string{"aion-bl/preserved", "aion-bl/fixed", "aion-bl/second", "aion-bl/child"}
	for i, id := range want {
		if all[i].ID != id || !all[i].IDPersisted {
			t.Fatalf("item %d id = %q persisted=%v, want %q", i, all[i].ID, all[i].IDPersisted, id)
		}
	}
	if n2, _, err := store.EnsureStableIDsFromPortal(portal); err != nil || n2 != 0 {
		t.Fatalf("migration must be idempotent: n=%d err=%v", n2, err)
	}
	before := all[0].ID
	if err := store.UpdateItem(before, map[string]string{"title": "A completely new title"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := store.LoadBacklog().Find(before); got == nil || got.Text != "A completely new title" {
		t.Fatalf("title edit changed stable identity: %+v", got)
	}
	serialized := store.RawFile("backlog.md")
	if again := SerializeBacklog(ParseBacklog(serialized)); again != serialized {
		t.Fatal("migrated backlog is not a byte-stable round trip")
	}
	if strings.Count(serialized, "[id::") != 4 {
		t.Fatalf("not every nested item received an id:\n%s", serialized)
	}
}
