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
	legacyMap := store.LegacyIDMap()
	if got := legacyMap[ItemID(KindTask, "A completely new title")]; got != before {
		t.Fatalf("LegacyIDMap after title edit = %q, want %q", got, before)
	}
}

// The 2026-08-24 "item not found": a line appended WITHOUT an [id::] token
// (the approval lane's pre-stamped legacy hash defeated the empty-only mint
// guard) reads as the legacy hash, while the live projection serves a
// transient aion-bl/<slug> for it. Clicking done sent the transient id; Find
// missed; the item was visible everywhere and editable nowhere.
func TestResolveAcceptsTheTransientProjectionID(t *testing.T) {
	// a token-less line, exactly as the bug left them in the vault
	raw := "# AION — backlog\n\n## Tasks\n" +
		"- [ ] Add task-editing capability directly in the AION portal [kind:: task] [owner:: BA] [captured:: 2026-08-24] [status:: open]\n"
	doc := ParseBacklog(raw)
	it, err := doc.Resolve("aion-bl/add-task-editing-capability-directly-in-the-aion-portal")
	if err != nil {
		t.Fatalf("transient id must resolve: %v", err)
	}
	if it.Text != "Add task-editing capability directly in the AION portal" {
		t.Fatalf("resolved the wrong item: %q", it.Text)
	}
	// the resolve STAMPS the id — the next save persists the token and the
	// line never needs the fallback again
	if !it.IDPersisted || it.ID != "aion-bl/add-task-editing-capability-directly-in-the-aion-portal" {
		t.Fatalf("resolve must self-heal the identity: %+v", it.ID)
	}
	if !strings.Contains(SerializeBacklog(doc), "[id:: aion-bl/add-task-editing-capability-directly-in-the-aion-portal]") {
		t.Fatal("the healed id does not serialize")
	}
	// an exact persisted id still wins over the fallback
	if _, err := doc.Resolve("aion-bl/add-task-editing-capability-directly-in-the-aion-portal"); err != nil {
		t.Fatalf("second resolve (now exact): %v", err)
	}
	// junk refuses
	if _, err := doc.Resolve("aion-bl/no-such-item"); err == nil {
		t.Fatal("unknown id must refuse")
	}
}

// Two token-less lines sharing a title share a transient id — guessing between
// them is how the wrong task gets checked done, so Resolve refuses.
func TestResolveRefusesAmbiguousTransientIDs(t *testing.T) {
	raw := "# AION — backlog\n\n## Tasks\n" +
		"- [ ] Call the inspector [kind:: task] [status:: open]\n" +
		"- [ ] Call the inspector [kind:: task] [status:: open]\n"
	doc := ParseBacklog(raw)
	_, err := doc.Resolve("aion-bl/call-the-inspector")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguity must refuse loudly, got %v", err)
	}
}

// The mint guard itself: the approval lane arrives with a legacy hash already
// stamped, and AppendItem must mint a persisted portal id anyway.
func TestAppendMintsOverALegacyHash(t *testing.T) {
	doc := ParseBacklog("# AION — backlog\n\n## Tasks\n")
	it := &BacklogItem{Kind: KindTask, Text: "Wire the new panel", Status: StatusOpen}
	it.ID = ItemID(KindTask, it.Text) // what BacklogItemFromPayload does
	doc.AppendItem(it)
	if !it.IDPersisted || !strings.HasPrefix(it.ID, "aion-bl/") {
		t.Fatalf("legacy pre-stamp defeated the mint: %q persisted=%v", it.ID, it.IDPersisted)
	}
	if !strings.Contains(SerializeBacklog(doc), "[id:: aion-bl/wire-the-new-panel]") {
		t.Fatal("minted id does not serialize")
	}
}
