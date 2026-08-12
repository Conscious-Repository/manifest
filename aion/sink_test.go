package aion

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeSpooler struct {
	alive    bool
	err      error
	requests []string
}

func (f *fakeSpooler) SpoolRunNow(spirit, ritual, request, skill string) error {
	if f.err != nil {
		return f.err
	}
	f.requests = append(f.requests, request)
	return nil
}

func (f *fakeSpooler) EngineAlive() (bool, time.Time) { return f.alive, time.Now() }

func sinkFixture(t *testing.T) (*ExtractSink, *fakeSpooler, string) {
	t.Helper()
	vault := t.TempDir()
	dataDir := t.TempDir()
	sp := &fakeSpooler{alive: true}
	// the sink is constructed over the EMPTY vault (baseline = nothing);
	// the notes below are post-baseline creations, the triggering case
	sink := NewExtractSink(ExtractorDomain, vault, "system", "extrinsic", dataDir, sp)
	write := func(rel, content string) {
		abs := filepath.Join(vault, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// BOTH yaml category styles (spec §8 category-detection test)
	write("log/2026-08-03 aion sync.md", "---\ncategories: [aion, projects]\n---\n\ntranscript body\n")
	write("aion master plan.md", "---\ncategories:\n  - aion\n---\n\nthe plan\n")
	// exclusions: wrong category, system zone, excalibur, non-md
	write("log/2026-08-02 house sync.md", "---\ncategories: [house]\n---\n\nx\n")
	write("system/aion/backlog.md", "---\ncategories: [aion]\n---\n\n## Tasks\n")
	write("system/excalibur/spirits/aion-extractor/cornerstone.md", "---\ncategories: [aion]\n---\nx\n")
	write("notes.txt", "categories: [aion]")
	return sink, sp, vault
}

// TestSinkBaselinesExistingCorpus: a FRESH cursor over a vault that already
// carries aion notes marks them seen WITHOUT spooling — only later edits
// trigger (the historic corpus was imported wholesale, not re-extracted).
func TestSinkBaselinesExistingCorpus(t *testing.T) {
	vault := t.TempDir()
	dataDir := t.TempDir()
	sp := &fakeSpooler{alive: true}
	abs := filepath.Join(vault, "log", "2026-07-20 aion team sync.md")
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	_ = os.WriteFile(abs, []byte("---\ncategories: [aion]\n---\nhistoric transcript\n"), 0o644)

	sink := NewExtractSink(ExtractorDomain, vault, "system", "extrinsic", dataDir, sp)
	// the watcher sweeping the historic corpus is a no-op
	sink.Notify([]string{"log/2026-07-20 aion team sync.md"})
	if len(sp.requests) != 0 || sink.QueuedCount() != 0 {
		t.Fatalf("baselined note spooled: %d requests, %d queued", len(sp.requests), sink.QueuedCount())
	}
	// an EDIT after the baseline triggers as usual
	_ = os.WriteFile(abs, []byte("---\ncategories: [aion]\n---\nhistoric transcript\n\nnew decision\n"), 0o644)
	sink.Notify([]string{"log/2026-07-20 aion team sync.md"})
	if len(sp.requests) != 1 {
		t.Fatalf("post-baseline edit did not spool: %d", len(sp.requests))
	}
}

// TestSinkBatchNoteCap: a bulk change spools at most maxBatchNotes per run.
func TestSinkBatchNoteCap(t *testing.T) {
	s, sp, vault := sinkFixture(t)
	var rels []string
	for i := 0; i < 10; i++ {
		rel := "log/2026-08-08 bulk " + string(rune('a'+i)) + ".md"
		abs := filepath.Join(vault, filepath.FromSlash(rel))
		_ = os.WriteFile(abs, []byte("---\ncategories: [aion]\n---\nnote\n"), 0o644)
		rels = append(rels, rel)
	}
	s.Notify(rels)
	if n := strings.Count(sp.requests[0], "\n- "); n != maxBatchNotes {
		t.Fatalf("first batch %d notes, want %d", n, maxBatchNotes)
	}
	for i := 0; i < 5 && s.QueuedCount() > 0; i++ {
		s.Flush()
	}
	total := 0
	for _, r := range sp.requests {
		total += strings.Count(r, "\n- ")
	}
	if total != 10 || s.QueuedCount() != 0 {
		t.Fatalf("drained %d of 10, %d queued", total, s.QueuedCount())
	}
}

func TestSinkCategoryDetectionBothStylesAndExclusions(t *testing.T) {
	s, sp, _ := sinkFixture(t)
	s.Notify([]string{
		"log/2026-08-03 aion sync.md",
		"aion master plan.md",
		"log/2026-08-02 house sync.md",
		"system/aion/backlog.md",
		"system/excalibur/spirits/aion-extractor/cornerstone.md",
		"notes.txt",
	})
	if len(sp.requests) != 1 {
		t.Fatalf("requests: %d", len(sp.requests))
	}
	req := sp.requests[0]
	if !strings.Contains(req, "- log/2026-08-03 aion sync.md") || !strings.Contains(req, "- aion master plan.md") {
		t.Fatalf("aion notes missing from request:\n%s", req)
	}
	for _, banned := range []string{"house sync", "system/aion", "excalibur", "notes.txt"} {
		if strings.Contains(req, banned) {
			t.Fatalf("excluded path %q spooled:\n%s", banned, req)
		}
	}
}

func TestSinkCursorSkipsUnchanged(t *testing.T) {
	s, sp, vault := sinkFixture(t)
	s.Notify([]string{"log/2026-08-03 aion sync.md"})
	s.Notify([]string{"log/2026-08-03 aion sync.md"}) // unchanged — no respool
	if len(sp.requests) != 1 {
		t.Fatalf("unchanged note respooled: %d", len(sp.requests))
	}
	// edit the note → re-proposes
	abs := filepath.Join(vault, "log", "2026-08-03 aion sync.md")
	b, _ := os.ReadFile(abs)
	_ = os.WriteFile(abs, append(b, []byte("\nmore\n")...), 0o644)
	s.Notify([]string{"log/2026-08-03 aion sync.md"})
	if len(sp.requests) != 2 {
		t.Fatalf("changed note not respooled: %d", len(sp.requests))
	}
}

func TestSinkEngineDownQueuesAndRetries(t *testing.T) {
	s, sp, _ := sinkFixture(t)
	sp.alive = false
	s.Notify([]string{"log/2026-08-03 aion sync.md"})
	if len(sp.requests) != 0 {
		t.Fatal("spooled while engine down")
	}
	if s.QueuedCount() != 1 {
		t.Fatalf("queued: %d", s.QueuedCount())
	}
	sp.alive = true
	s.Flush()
	if len(sp.requests) != 1 || s.QueuedCount() != 0 {
		t.Fatalf("retry failed: %d requests, %d queued", len(sp.requests), s.QueuedCount())
	}
}

func TestSinkBusySpiritStaysQueued(t *testing.T) {
	s, sp, _ := sinkFixture(t)
	sp.err = errors.New("already queued or running")
	s.Notify([]string{"log/2026-08-03 aion sync.md"})
	if s.QueuedCount() != 1 {
		t.Fatalf("queued: %d", s.QueuedCount())
	}
	sp.err = nil
	s.Flush()
	if len(sp.requests) != 1 || s.QueuedCount() != 0 {
		t.Fatal("busy retry failed")
	}
}

func TestSinkCursorPersists(t *testing.T) {
	s, sp, vault := sinkFixture(t)
	dataDir := filepath.Dir(filepath.Dir(s.cursorPath))
	s.Notify([]string{"log/2026-08-03 aion sync.md"})
	// a fresh sink over the same dataDir sees the mark — no respool
	s2 := NewExtractSink(ExtractorDomain, vault, "system", "extrinsic", dataDir, sp)
	s2.Notify([]string{"log/2026-08-03 aion sync.md"})
	if len(sp.requests) != 1 {
		t.Fatalf("cursor not persisted: %d requests", len(sp.requests))
	}
}

func TestSinkBatchRespectsRequestCap(t *testing.T) {
	vault := t.TempDir()
	dataDir := t.TempDir()
	sp := &fakeSpooler{alive: true}
	// sink first (empty-vault baseline), notes after — the triggering case
	s := NewExtractSink(ExtractorDomain, vault, "system", "extrinsic", dataDir, sp)
	var rels []string
	for i := 0; i < 100; i++ {
		rel := "log/2026-08-03 aion note about a very long meeting series volume " +
			strings.Repeat("x", 20) + " " + strings.Repeat("y", i%7) + " " + string(rune('a'+i%26)) + itoa(i) + ".md"
		abs := filepath.Join(vault, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		_ = os.WriteFile(abs, []byte("---\ncategories: [aion]\n---\nnote "+itoa(i)+"\n"), 0o644)
		rels = append(rels, rel)
	}
	s.Notify(rels)
	if len(sp.requests) == 0 {
		t.Fatal("nothing spooled")
	}
	for _, r := range sp.requests {
		if len(r) > maxSpoolRequest {
			t.Fatalf("request over cap: %d", len(r))
		}
	}
	// the remainder stays queued and drains on later flushes (4 per run)
	for i := 0; i < 40 && s.QueuedCount() > 0; i++ {
		s.Flush()
	}
	if s.QueuedCount() != 0 {
		t.Fatalf("queue never drained: %d", s.QueuedCount())
	}
	total := 0
	for _, r := range sp.requests {
		total += strings.Count(r, "\n- ")
	}
	if total != 100 {
		t.Fatalf("spooled %d of 100", total)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
