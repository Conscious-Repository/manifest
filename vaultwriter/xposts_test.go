package vaultwriter

import (
	"strings"
	"testing"
)

func TestMigrateXPosts(t *testing.T) {
	in := "\n# queue\n- alpha\n- thread\n\t- **1/** one\n\t- **2/** two\n\n# posted\n- done\n"
	out, changed := migrateXPosts(in)
	if !changed {
		t.Fatal("expected migration")
	}
	// the old queue bullets are now under # drafts, verbatim; a fresh empty # queue exists; # posted intact
	if !strings.Contains(out, "# drafts\n- alpha\n- thread\n\t- **1/** one") {
		t.Fatalf("drafts not carried verbatim:\n%s", out)
	}
	if !strings.Contains(out, "\n# queue\n") {
		t.Fatalf("no fresh queue section:\n%s", out)
	}
	if !strings.Contains(out, "# posted\n- done\n") {
		t.Fatalf("posted changed:\n%s", out)
	}
	// idempotent
	if _, changed := migrateXPosts(out); changed {
		t.Fatal("second migration should be a no-op")
	}
	// every original bullet survives
	for _, b := range []string{"- alpha", "**1/** one", "**2/** two", "- done"} {
		if !strings.Contains(out, b) {
			t.Fatalf("lost bullet %q", b)
		}
	}
}

func TestParseXPosts(t *testing.T) {
	in := "# drafts\n- d1\n- thread\n\t- **1/** a\n\n# queue\n- q1\n\n# posted\n- p1\n- p2\n"
	doc := ParseXPosts(in)
	if doc.NeedsMigration {
		t.Fatal("migrated file should not need migration")
	}
	if len(doc.Drafts) != 2 || doc.Drafts[0].Lead != "d1" {
		t.Fatalf("drafts=%+v", doc.Drafts)
	}
	// the thread block groups its indented children
	if !strings.Contains(doc.Drafts[1].Text, "**1/** a") {
		t.Fatalf("thread block not grouped: %q", doc.Drafts[1].Text)
	}
	if len(doc.Queue) != 1 || len(doc.Posted) != 2 {
		t.Fatalf("queue=%d posted=%d", len(doc.Queue), len(doc.Posted))
	}

	old := "# queue\n- scratch\n# posted\n"
	if !ParseXPosts(old).NeedsMigration {
		t.Fatal("old-shape file should flag migration")
	}
}

func TestReplaceDeleteAddBullet(t *testing.T) {
	in := "# drafts\n- alpha\n- beta\n\n# queue\n\n# posted\n"
	// replace
	out, ok := replaceBullet(in, "- beta", "- beta edited")
	if !ok || !strings.Contains(out, "- beta edited") || strings.Contains(out, "- beta\n") {
		t.Fatalf("replace failed: %q", out)
	}
	// stale match refused
	if _, ok := replaceBullet(in, "- gamma", "- x"); ok {
		t.Fatal("stale replace should fail")
	}
	// delete
	out, ok = deleteBullet(in, "- alpha")
	if !ok || strings.Contains(out, "- alpha") {
		t.Fatalf("delete failed: %q", out)
	}
	// add to drafts (dedupe)
	out, ok = addBulletToSection(in, "drafts", "- gamma")
	if !ok || !strings.Contains(out, "- alpha\n- beta\n- gamma") {
		t.Fatalf("add failed: %q", out)
	}
	if _, ok := addBulletToSection(in, "drafts", "- alpha"); ok {
		t.Fatal("dup add should fail")
	}
}
