package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testConfig(root string) Config {
	return Config{
		Root:        root,
		NewDailyDir: "intrinsic",
		GoalsName:   "goals.md",
		SkipDirs:    []string{".git", ".obsidian", ".trash", "attachments"},
		SystemRoot:  "system",
	}
}

// buildVault creates a fixture shaped like the real vault: a root daily, a
// subfolder daily, a date-PREFIXED note that must not classify as daily, a
// goals file, content inside skipped directories, and SYSTEM-ZONE files that
// must never classify (a date-named file under system/excalibur/ is not a
// daily; a goals-named file under system/ is not the goals master).
func buildVault(t *testing.T) string {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "2026-06-29.md"), "#journal\nhello\n")
	write(t, filepath.Join(dir, "intrinsic", "2026-06-26.md"), "#journal\nfoo\n")
	write(t, filepath.Join(dir, "intrinsic", "2026-01-09 meeting.md"), "notes\n")
	write(t, filepath.Join(dir, "goals.md"), "# Goals\n")
	write(t, filepath.Join(dir, ".obsidian", "workspace.json"), "{}\n")
	write(t, filepath.Join(dir, "attachments", "note.md"), "should be skipped\n")
	// system zone: date-named + goals-named files that must NOT classify
	write(t, filepath.Join(dir, "system", "excalibur", "spirits", "x", "memories", "window", "2026-07-07.md"), "engine memory\n")
	write(t, filepath.Join(dir, "system", "crm", "fundraising", "goals.md"), "not the goals master\n")
	return dir
}

func TestClassifyAndScan(t *testing.T) {
	dir := buildVault(t)
	snap, err := NewScanner(testConfig(dir)).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Daily) != 2 {
		t.Fatalf("expected 2 dailies, got %d: %v", len(snap.Daily), snap.Daily)
	}
	if _, ok := snap.Daily["2026-06-29"]; !ok {
		t.Fatal("root daily missing")
	}
	if _, ok := snap.Daily["2026-06-26"]; !ok {
		t.Fatal("subdir daily missing")
	}
	if _, ok := snap.Daily["2026-01-09"]; ok {
		t.Fatal("date-prefixed meeting note must NOT classify as daily")
	}
	// zone short-circuit: daily/goals detection applies ONLY in the knowledge zone
	if _, ok := snap.Daily["2026-07-07"]; ok {
		t.Fatal("a date-named file under system/excalibur/ must NOT classify as daily")
	}
	if snap.GoalsPath != filepath.Join(dir, "goals.md") {
		t.Fatalf("goals master must be the knowledge-zone goals.md, got %q", snap.GoalsPath)
	}
	for _, p := range snap.Daily {
		if filepath.Base(filepath.Dir(p)) == "attachments" {
			t.Fatal("attachments/ should have been skipped")
		}
	}
}

func TestResolveAnywhere(t *testing.T) {
	dir := buildVault(t)
	ix, err := NewIndex(testConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := ix.DailyNote("2026-06-29"); p != filepath.Join(dir, "2026-06-29.md") {
		t.Fatalf("root resolve: %s", p)
	}
	if p, _ := ix.DailyNote("2026-06-26"); p != filepath.Join(dir, "intrinsic", "2026-06-26.md") {
		t.Fatalf("subdir resolve: %s", p)
	}
	want := filepath.Join(dir, "intrinsic", "2025-01-01.md")
	if p, _ := ix.DailyNote("2025-01-01"); p != want {
		t.Fatalf("would-create path: got %s want %s", p, want)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("DailyNote must not create a file for a missing date")
	}
	if _, ok := ix.Lookup("2025-01-01"); ok {
		t.Fatal("Lookup should miss for an absent date")
	}
}

func TestFrontmatterType(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	write(t, a, "---\ntype: agent\ntags: [x]\n---\nbody\n")
	if got := frontmatterType(a); got != "agent" {
		t.Fatalf("type: %q", got)
	}
	b := filepath.Join(dir, "b.md")
	write(t, b, "#journal\nno frontmatter\n")
	if got := frontmatterType(b); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	c := filepath.Join(dir, "c.md")
	write(t, c, "---\nfoo: bar\n---\n")
	if got := frontmatterType(c); got != "" {
		t.Fatalf("expected empty type, got %q", got)
	}
}

func TestScanIdempotent(t *testing.T) {
	dir := buildVault(t)
	s := NewScanner(testConfig(dir))
	a, _ := s.Scan()
	b, _ := s.Scan()
	if len(a.Daily) != len(b.Daily) || a.GoalsPath != b.GoalsPath {
		t.Fatal("scan not idempotent")
	}
	for k, v := range a.Daily {
		if b.Daily[k] != v {
			t.Fatalf("daily mismatch for %s", k)
		}
	}
}

func TestWatcherUpdatesIndex(t *testing.T) {
	dir := buildVault(t)
	ix, err := NewIndex(testConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWatcher(ix, testConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "2026-06-29.md")
	dst := filepath.Join(dir, "intrinsic", "2026-06-29.md")
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p, ok := ix.Lookup("2026-06-29"); ok && p == dst {
			return // index reflected the move
		}
		time.Sleep(50 * time.Millisecond)
	}
	p, ok := ix.Lookup("2026-06-29")
	t.Fatalf("index did not update after move: got %q ok=%v want %q", p, ok, dst)
}
