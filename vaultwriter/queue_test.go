package vaultwriter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendBulletToQueue_BytePreserving(t *testing.T) {
	in := "# queue\n- alpha\n- beta\n\n# posted\n- old\n"
	out, changed := appendBulletToQueue(in, "- gamma")
	if !changed {
		t.Fatal("expected changed")
	}
	want := "# queue\n- alpha\n- beta\n- gamma\n\n# posted\n- old\n"
	if out != want {
		t.Fatalf("append mismatch:\n got %q\nwant %q", out, want)
	}
	// the rest of the file (everything but the new bullet) is byte-identical
	if strings.Replace(out, "- gamma\n", "", 1) != in {
		t.Fatalf("append changed bytes outside the new bullet")
	}
}

func TestAppendBulletToQueue_Dedupe(t *testing.T) {
	in := "# queue\n- alpha\n\n# posted\n- beta\n"
	// identical bullet already in queue → refuse
	if _, changed := appendBulletToQueue(in, "- alpha"); changed {
		t.Fatal("duplicate in queue should not be re-added")
	}
	// identical bullet already in posted → refuse (never re-queue a posted line)
	if _, changed := appendBulletToQueue(in, "- beta"); changed {
		t.Fatal("duplicate in posted should not be re-added")
	}
	// indentation/leading-dash normalization still dedupes
	if _, changed := appendBulletToQueue(in, "alpha"); changed {
		t.Fatal("normalized duplicate should not be re-added")
	}
}

func TestAppendBulletToQueue_CreatesHeading(t *testing.T) {
	out, changed := appendBulletToQueue("some notes\n", "- x")
	if !changed {
		t.Fatal("expected changed")
	}
	if out != "some notes\n\n# queue\n\n- x\n" {
		t.Fatalf("create-heading mismatch: %q", out)
	}
}

func TestAppendBulletToQueue_HandEditedThreaded(t *testing.T) {
	// a realistic, hand-edited queue with a nested thread + stray blank lines
	in := "# queue\n" +
		"- the best fundraising trick is not needing the money\n" +
		"- thread\n" +
		"\t- **1/** a good heuristic is compression\n" +
		"\t- **2/** schmidhuber's core insight\n" +
		"\n" +
		"# posted\n" +
		"- gating a working product behind a sales demo\n"
	out, changed := appendBulletToQueue(in, "- an agent cannot choose a future its action set does not contain.")
	if !changed {
		t.Fatal("expected changed")
	}
	// the new bullet lands at the end of the queue section (after the thread block)
	if !strings.Contains(out, "schmidhuber's core insight\n- an agent cannot choose") {
		t.Fatalf("bullet not appended at end of queue:\n%s", out)
	}
	// posted section + thread block untouched
	if !strings.Contains(out, "\n# posted\n- gating a working product behind a sales demo\n") {
		t.Fatalf("posted section changed:\n%s", out)
	}
	if strings.Replace(out, "- an agent cannot choose a future its action set does not contain.\n", "", 1) != in {
		t.Fatal("hand-edited content changed outside the new bullet")
	}
}

func TestMoveBulletToPosted(t *testing.T) {
	in := "# queue\n- alpha\n- beta\n\n# posted\n- old\n"
	out, moved := moveBulletToPosted(in, "- beta")
	if !moved {
		t.Fatal("expected moved")
	}
	want := "# queue\n- alpha\n\n# posted\n- beta\n- old\n"
	if out != want {
		t.Fatalf("move mismatch:\n got %q\nwant %q", out, want)
	}
	// a bullet not in the queue is a no-op
	if _, moved := moveBulletToPosted(in, "- nonexistent"); moved {
		t.Fatal("moving a missing bullet should report not-moved")
	}
}

func TestWriterQueueOps_Guarded(t *testing.T) {
	vault := t.TempDir()
	os.WriteFile(filepath.Join(vault, "x posts.md"), []byte("# queue\n- alpha\n\n# posted\n"), 0o644)
	w := New(vault)

	// append via the guarded method
	if err := w.AppendQueueBullet("x posts.md", "- beta"); err != nil {
		t.Fatalf("AppendQueueBullet: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(vault, "x posts.md"))
	if !strings.Contains(string(b), "- alpha\n- beta\n") {
		t.Fatalf("append not persisted: %s", b)
	}
	// duplicate refused
	if err := w.AppendQueueBullet("x posts.md", "- beta"); err == nil {
		t.Fatal("expected duplicate refusal")
	}
	// mark posted
	if err := w.MoveBulletToPosted("x posts.md", "- beta"); err != nil {
		t.Fatalf("MoveBulletToPosted: %v", err)
	}
	b, _ = os.ReadFile(filepath.Join(vault, "x posts.md"))
	if !strings.Contains(string(b), "# posted\n- beta\n") {
		t.Fatalf("move not persisted: %s", b)
	}

	// engine-owned path refused by the guard
	if err := w.AppendQueueBullet("system/excalibur/x posts.md", "- x"); err == nil {
		t.Fatal("engine-owned path should be refused")
	}
}
