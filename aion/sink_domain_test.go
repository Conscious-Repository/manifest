package aion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// domainSpooler captures spirit + request per spool (the shared fakeSpooler
// keeps only request strings).
type domainSpooler struct {
	alive   bool
	spooled []spooledReq
}

type spooledReq struct{ spirit, ritual, request string }

func (f *domainSpooler) SpoolRunNow(spirit, ritual, request, skill string) error {
	f.spooled = append(f.spooled, spooledReq{spirit, ritual, request})
	return nil
}

func (f *domainSpooler) EngineAlive() (bool, time.Time) { return f.alive, time.Now() }

var reTestDomain = DomainSpec{
	Name:       "realestate",
	Categories: []string{"real-estate", "ooda", "ooda-group"},
	Spirit:     "extractor",
	Ritual:     "real-estate",
	Request:    "extract real-estate items from these vault notes:",
}

func writeNote(t *testing.T, vault, rel, categories string) {
	t.Helper()
	full := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("---\ncategories: ["+categories+"]\n---\n\nnote body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The RE-spec sink ignores aion-only notes and routes RE categories to the
// extractor real-estate ritual; a dual-tagged note reaches BOTH sinks over one vault.
func TestSinkDomainRouting(t *testing.T) {
	vault := t.TempDir()
	dataDir := t.TempDir()
	aionSp := &domainSpooler{alive: true}
	reSp := &domainSpooler{alive: true}
	aionSink := NewExtractSink(ExtractorDomain, vault, "system", "extrinsic", dataDir, aionSp)
	reSink := NewExtractSink(reTestDomain, vault, "system", "extrinsic", dataDir, reSp)

	writeNote(t, vault, "log/aion-only.md", "aion")
	writeNote(t, vault, "log/re-only.md", "real-estate")
	writeNote(t, vault, "log/ooda-note.md", "ooda")
	writeNote(t, vault, "log/dual.md", "aion, ooda-group")

	paths := []string{"log/aion-only.md", "log/re-only.md", "log/ooda-note.md", "log/dual.md"}
	aionSink.Notify(paths)
	reSink.Notify(paths)

	if len(aionSp.spooled) != 1 {
		t.Fatalf("aion spooled %d requests, want 1", len(aionSp.spooled))
	}
	if got := aionSp.spooled[0]; got.spirit != "extractor" ||
		!contains2(got.request, "log/aion-only.md") || !contains2(got.request, "log/dual.md") ||
		contains2(got.request, "log/re-only.md") {
		t.Fatalf("aion routing wrong: %+v", got)
	}
	if len(reSp.spooled) != 1 {
		t.Fatalf("re spooled %d requests, want 1", len(reSp.spooled))
	}
	if got := reSp.spooled[0]; got.spirit != "extractor" ||
		!contains2(got.request, "log/re-only.md") || !contains2(got.request, "log/ooda-note.md") ||
		!contains2(got.request, "log/dual.md") || contains2(got.request, "log/aion-only.md") {
		t.Fatalf("re routing wrong: %+v", got)
	}
	// per-domain cursors — separate files
	if _, err := os.Stat(filepath.Join(dataDir, "aion", "cursor.json")); err != nil {
		t.Fatal("aion cursor missing")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "realestate", "cursor.json")); err != nil {
		t.Fatal("re cursor missing")
	}
}

// The anti-flood pin: a FRESH RE cursor over a historic ooda-tagged corpus
// baselines everything — zero queued, zero spooled.
func TestReSinkBaselinesHistoricOodaCorpus(t *testing.T) {
	vault := t.TempDir()
	dataDir := t.TempDir()
	for _, rel := range []string{"log/old-1.md", "log/old-2.md", "log/old-3.md"} {
		writeNote(t, vault, rel, "ooda")
	}
	sp := &domainSpooler{alive: true}
	sink := NewExtractSink(reTestDomain, vault, "system", "extrinsic", dataDir, sp)
	if n := sink.QueuedCount(); n != 0 {
		t.Fatalf("fresh cursor queued %d historic notes — flood!", n)
	}
	sink.Flush()
	if len(sp.spooled) != 0 {
		t.Fatalf("historic corpus spooled %d requests, want 0", len(sp.spooled))
	}
	// an EDIT after baseline does queue
	writeNote(t, vault, "log/old-1.md", "ooda") // rewrite changes nothing (same bytes)?
	// change the content for a real hash delta
	full := filepath.Join(vault, "log", "old-1.md")
	if err := os.WriteFile(full, []byte("---\ncategories: [ooda]\n---\n\nedited body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink.Notify([]string{"log/old-1.md"})
	if len(sp.spooled) != 1 || !contains2(sp.spooled[0].request, "log/old-1.md") {
		t.Fatalf("post-baseline edit did not spool: %+v", sp.spooled)
	}
}

func contains2(s, sub string) bool { return strings.Contains(s, sub) }
