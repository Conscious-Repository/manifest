package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
	"manifest/teamportal"
	"manifest/threads"
	"manifest/vaultindex"
)

// Synthetic transcript fixtures with INVENTED names — the fixture tier map
// below is injected, so no real note name (held or otherwise) is needed.
const (
	cOpen     = "2026-04-02 ultrasound bench memo.md"
	cInternal = "2026-06-11 lab ops sync.md"
	cHeld     = "2026-08-09 quokka equity sync.md"
	cUnmapped = "2026-09-20 wallaby sync.md"
	cMailOK   = "2026-09-10 lab supplies quote 1a04e57870972dba.md"
	cMailHeld = "2026-09-10 visa question 2b15f68981083ecb.md"
)

func corpusTierMap() aion.TierMap {
	return aion.TierMap{
		cOpen:     {Tier: aion.TierOpen, Reason: "research"},
		cInternal: {Tier: aion.TierInternal, Reason: "ops"},
		cHeld:     {Tier: aion.TierHeld, Reason: "compensation"},
		cMailOK:   {Tier: aion.TierInternal, Reason: "ops mail"},
	}
}

func writeCorpusNote(t *testing.T, dir, name, cats, people, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ncategories:\n" + cats + "---\n" + people + "\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// corpusFixture: a live projection (liveFixture) whose vault also carries a
// log/ of synthetic transcripts, pointed at a temp pack dir.
func corpusFixture(t *testing.T) (*Server, *AionLive, string, string) {
	t.Helper()
	srv, live, _, _ := liveFixture(t)
	vault := filepath.Dir(filepath.Dir(srv.aion.Path(""))) // <vault>/system/aion → <vault>
	if !filepath.IsAbs(vault) {
		t.Fatalf("fixture vault %q is not absolute", vault)
	}
	logDir := filepath.Join(vault, "log")
	writeCorpusNote(t, logDir, cOpen, "  - aion\n  - research\n", "[[maria lopez]]", "Transducer conversion efficiency depends on the crystal.")
	writeCorpusNote(t, logDir, cInternal, "  - aion\n  - sync\n", "[[maria lopez]]", "The incubator rig moves to bay two.")
	writeCorpusNote(t, logDir, cHeld, "  - aion\n  - sync\n", "[[quokka person]]", "QUOKKACANARY vestibule percentages agreed.")
	writeCorpusNote(t, logDir, cUnmapped, "  - aion\n  - sync\n", "[[wallaby person]]", "WALLABYCANARY untiered.")
	writeCorpusNote(t, logDir, "2026-09-01 ooda call.md", "  - ooda\n", "[[x]]", "not aion")
	packDir := filepath.Join(t.TempDir(), "aion-context")
	live.tierMap = corpusTierMap()
	live.UseTranscripts(logDir)
	live.UseAPack(packDir)
	return srv, live, packDir, logDir
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(b)
}

// The sub-pack lands beside the contract pack, tier-gated, stamped LAST, and
// a second sync with nothing changed is a no-op.
func TestAionCorpusSyncWritesGatedFilesAndSkipsWhenUnchanged(t *testing.T) {
	_, live, packDir, _ := corpusFixture(t)
	live.SyncCorpus()

	if _, err := os.Stat(filepath.Join(packDir, "transcripts", cOpen)); err != nil {
		t.Fatalf("open note missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(packDir, "transcripts", cInternal)); err != nil {
		t.Fatalf("internal note missing: %v", err)
	}
	for _, absent := range []string{cHeld, cUnmapped, "2026-09-01 ooda call.md"} {
		if _, err := os.Stat(filepath.Join(packDir, "transcripts", absent)); err == nil {
			t.Fatalf("%s must not be written", absent)
		}
	}
	index := mustRead(t, filepath.Join(packDir, "transcripts", "INDEX.md"))
	if !strings.Contains(index, "held notes excluded: 1") || !strings.Contains(index, "unmapped notes excluded: 1") {
		t.Fatalf("INDEX coverage wrong:\n%s", index)
	}
	rev := aionCorpusRevision(packDir)
	if rev == "" || !strings.Contains(index, "revision: "+rev) {
		t.Fatalf("stamp %q not shared by INDEX", rev)
	}
	for _, name := range []string{"fundraising.md", "hiring.md", "research.md", "strategy.md", "README.md"} {
		if _, err := os.Stat(filepath.Join(packDir, "digests", name)); err != nil {
			t.Fatalf("digest %s missing", name)
		}
	}
	// the contract pack's README now points at the sub-dirs
	if readme := mustRead(t, filepath.Join(packDir, "README.md")); !strings.Contains(readme, "transcripts/INDEX.md") {
		t.Fatal("pack README does not name the transcript corpus")
	}
	// nothing in the pack names held or unmapped material
	_ = filepath.Walk(packDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		text := strings.ToLower(mustRead(t, path) + path)
		for _, leak := range []string{"quokka", "wallaby", "canary"} {
			if strings.Contains(text, leak) {
				t.Errorf("%s leaks %q", path, leak)
			}
		}
		return nil
	})

	// skip-when-unchanged
	sentinel := filepath.Join(packDir, "transcripts", "INDEX.md")
	if err := os.WriteFile(sentinel, []byte("SENTINEL"), 0o664); err != nil {
		t.Fatal(err)
	}
	live.SyncCorpus()
	if mustRead(t, sentinel) != "SENTINEL" {
		t.Fatal("an unchanged corpus rewrote the pack")
	}
	if live.corpusRev != rev {
		t.Fatalf("revision moved without a change: %s → %s", rev, live.corpusRev)
	}
}

// Moving a note to held RETRACTS it: the transcript file disappears, the
// counts move, and the stamp moves.
func TestAionCorpusRetractsANoteMovedToHeld(t *testing.T) {
	_, live, packDir, _ := corpusFixture(t)
	live.SyncCorpus()
	before := aionCorpusRevision(packDir)
	live.tierMap[cOpen] = aion.TierEntry{Tier: aion.TierHeld, Reason: "reclassified"}
	live.SyncCorpus()
	if _, err := os.Stat(filepath.Join(packDir, "transcripts", cOpen)); err == nil {
		t.Fatal("a note moved to held is still in transcripts/")
	}
	index := mustRead(t, filepath.Join(packDir, "transcripts", "INDEX.md"))
	if strings.Contains(index, cOpen) || !strings.Contains(index, "held notes excluded: 2") {
		t.Fatalf("INDEX still lists the retracted note or miscounts:\n%s", index)
	}
	if after := aionCorpusRevision(packDir); after == before || after == "" {
		t.Fatalf("stamp did not move on retraction: %s → %s", before, after)
	}
}

// Portal ARTIFACTS: the open note lands in the team dir's files/ tree under
// its sha256, the team state lists it (and only it), and a tier change
// removes the blob again.
func TestAionArtifactsPublishOpenOnlyAndRetract(t *testing.T) {
	srv, live, _, logDir := corpusFixture(t)
	teamDir := t.TempDir()
	team, err := teamportal.New(teamDir)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := threads.New(teamDir)
	if err != nil {
		t.Fatal(err)
	}
	private, _ := threads.New(t.TempDir())
	srv.UseThreads(private, nil, team, blobs, "owner@aion.bio")

	live.SyncCorpus()
	openBytes := mustRead(t, filepath.Join(logDir, cOpen))
	sum := sha256.Sum256([]byte(openBytes))
	openHash := hex.EncodeToString(sum[:])
	path := blobs.BlobPath(openHash)
	if path == "" {
		t.Fatal("open note not published to files/")
	}
	if mustRead(t, path) != openBytes {
		t.Fatal("published blob is not the note's own text")
	}
	if srv.AionFileBlob(openHash) != path {
		t.Fatal("the portal's blob route would not resolve the artifact")
	}
	// internal + held never reach files/
	for _, name := range []string{cInternal, cHeld} {
		s := sha256.Sum256([]byte(mustRead(t, filepath.Join(logDir, name))))
		if blobs.BlobPath(hex.EncodeToString(s[:])) != "" {
			t.Fatalf("%s was published", name)
		}
	}
	state := live.TeamState()
	if len(state.Artifacts) != 1 || state.Artifacts[0].Hash != openHash || state.Artifacts[0].Tier != aion.TierOpen ||
		state.Artifacts[0].Date != "2026-04-02" || state.Artifacts[0].Title != "ultrasound bench memo" {
		t.Fatalf("team state artifacts = %+v", state.Artifacts)
	}
	b, _ := json.Marshal(live.TeamStateJSON())
	if !strings.Contains(string(b), `"artifacts":[{"hash":"`+openHash+`"`) {
		t.Fatalf("team state JSON lacks the artifact: %s", b)
	}
	if strings.Contains(string(b), "QUOKKA") || strings.Contains(string(b), cInternal) {
		t.Fatal("team state carries non-open material")
	}
	// the manifest is the publisher's ledger, next to items.ext.json
	if _, err := os.Stat(filepath.Join(teamDir, aionArtifactManifestFile)); err != nil {
		t.Fatal("manifest missing")
	}

	// retraction: open → internal removes the blob and the listing
	live.tierMap[cOpen] = aion.TierEntry{Tier: aion.TierInternal, Reason: "reclassified"}
	live.SyncCorpus()
	if blobs.BlobPath(openHash) != "" {
		t.Fatal("blob survived reclassification")
	}
	if got := live.TeamState().Artifacts; len(got) != 0 {
		t.Fatalf("artifacts after retraction = %+v", got)
	}
}

// The email digest projects the index's synced threads: kairos's file in the
// pack carries the shareable thread and the held COUNT; the held subject is
// only in the owner's 0600 file under dataDir — outside the pack.
func TestAionEmailDigestRoutesToPackAndOwnerHold(t *testing.T) {
	srv, live, packDir, logDir := corpusFixture(t)
	vault := filepath.Dir(logDir)
	if err := os.WriteFile(filepath.Join(vault, "maria lopez.md"), []byte("---\ncategories: [people]\nemail: maria@example.com\n---\nfriend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mail := func(name, thread, body string) {
		content := "---\ncategories:\n  - sync\n  - aion\ngmail-thread-id: " + thread + "\n---\n[[maria lopez]]\n\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(logDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mail(cMailOK, "1a04e57870972dba", "MAILCANARY-OK the quote for the transducers is attached")
	mail(cMailHeld, "2b15f68981083ecb", "MAILCANARY-HELD my h1b transfer timing")
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	srv.UseIndex(ix)
	// an extractor job for the shareable thread (the worker's extract:true
	// output shape — state, ritual, source document, candidate action lines)
	jobs := filepath.Join(srv.aionDataDir, "domain-extraction")
	if err := os.MkdirAll(jobs, 0o700); err != nil {
		t.Fatal(err)
	}
	job := map[string]any{
		"state": "completed",
		"input": map[string]any{"ritual": "aion", "documents": []map[string]string{{"name": "log/" + cMailOK, "text": "MAILCANARY-OK …"}}},
		"candidates": []map[string]string{
			{"action": "aion: task — Approve the transducer order", "body": "Source: log/" + cMailOK + "\n\n```aion\nquote: MAILCANARY-OK\n```"},
			{"action": "aion: decision — Buy the Olympus V303 first", "body": "Source: log/" + cMailOK + "\n\n…"},
		},
	}
	jb, _ := json.Marshal(job)
	if err := os.WriteFile(filepath.Join(jobs, "job1.json"), jb, 0o600); err != nil {
		t.Fatal(err)
	}

	live.SyncCorpus()
	digest := mustRead(t, filepath.Join(packDir, "digests", "email-2026-09-10.md"))
	for _, want := range []string{"Coverage: 1 threads · 1 items held for owner review", "- lab supplies quote", "with maria lopez",
		"- Buy the Olympus V303 first — from: lab supplies quote", "- [task] Approve the transducer order — from: lab supplies quote"} {
		if !strings.Contains(digest, want) {
			t.Fatalf("digest lacks %q:\n%s", want, digest)
		}
	}
	for _, leak := range []string{"CANARY", "visa", "h1b", "1a04e57870972dba", "2b15f68981083ecb"} {
		if strings.Contains(digest, leak) {
			t.Fatalf("digest leaks %q:\n%s", leak, digest)
		}
	}
	hold := filepath.Join(srv.aionDataDir, "aion", "email-hold", "email-2026-09-10.md")
	fi, err := os.Stat(hold)
	if err != nil {
		t.Fatalf("hold file missing: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("hold file mode = %o, want 600", fi.Mode().Perm())
	}
	if rel, _ := filepath.Rel(packDir, hold); !strings.HasPrefix(rel, "..") {
		t.Fatal("hold file is inside the pack")
	}
	held := mustRead(t, hold)
	if !strings.Contains(held, "visa question") || !strings.Contains(held, "pattern: immigration") || strings.Contains(held, "CANARY") {
		t.Fatalf("hold file wrong:\n%s", held)
	}
	// the email notes are aion-category log/ notes too: the tiered one is in
	// the corpus, the unmapped one is excluded and counted
	index := mustRead(t, filepath.Join(packDir, "transcripts", "INDEX.md"))
	if !strings.Contains(index, cMailOK) || strings.Contains(index, cMailHeld) {
		t.Fatalf("INDEX mis-gates the mail notes:\n%s", index)
	}
}

// The five-method PortalLive seam is untouched: artifacts ride TeamStateJSON.
func TestAionArtifactsRideTheExistingTeamStateRoute(t *testing.T) {
	_, live, _, _ := corpusFixture(t)
	if got := live.Artifacts(); got == nil || len(got) != 0 {
		t.Fatalf("artifacts before any publish = %#v, want an empty list", got)
	}
	var _ PortalLive = live
}
