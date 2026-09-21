package server

import (
	"os"
	"strings"
	"testing"
	"time"

	"manifest/aion"
)

// TestAionCorpusLiveVaultGuard is the operator's pre-deploy check, skipped
// unless MANIFEST_AION_LIVE_VAULT names the real vault root: it renders the
// transcript sub-pack from the REAL log/ through the REAL tier map, in memory
// only, and asserts that no held note's filename or title appears in any
// rendered byte. It logs the coverage it would publish and the unmapped
// names (those are for the owner, not the pack). Nothing is written.
func TestAionCorpusLiveVaultGuard(t *testing.T) {
	vault := os.Getenv("MANIFEST_AION_LIVE_VAULT")
	if vault == "" {
		t.Skip("set MANIFEST_AION_LIVE_VAULT=/path/to/vault to run against the live log/")
	}
	tm, err := aion.LoadTierMap()
	if err != nil {
		t.Fatal(err)
	}
	raws, err := readAionTranscriptDir(vault + "/log")
	if err != nil {
		t.Fatal(err)
	}
	corpus := aion.BuildTranscriptCorpus(tm, raws, nil)
	files := aion.RenderTranscriptPack(aion.TranscriptPackInput{
		Revision: "live-check", At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Corpus: corpus,
	})
	// the guard key is the dated FILENAME (with and without .md): bare titles
	// recur across syncs ("rj sync" is dated many times, one of them held)
	var heldKeys []string
	for name, e := range tm {
		if e.Tier == aion.TierHeld {
			heldKeys = append(heldKeys, strings.ToLower(strings.TrimSuffix(name, ".md")))
		}
	}
	for _, f := range files {
		text := strings.ToLower(f.Path + "\n" + f.Body)
		for _, key := range heldKeys {
			if strings.Contains(text, key) {
				t.Fatalf("%s references held note %q", f.Path, key)
			}
		}
	}
	first, last := corpus.Span()
	t.Logf("would publish %d notes (%d open to the portal) · span %s → %s · held %d · unmapped %d · files %d",
		len(corpus.Notes), len(corpus.PortalArtifacts()), first, last, corpus.Held, corpus.Unmapped, len(files))
	if len(corpus.UnmappedNames) > 0 {
		t.Logf("UNMAPPED (excluded until tiered): %s", strings.Join(corpus.UnmappedNames, "; "))
	}
}
