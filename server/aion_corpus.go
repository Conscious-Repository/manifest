package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"manifest/aion"
	"manifest/threads"
)

// The transcript channel of kairos's standing context pack (aion-context
// transcripts, 2026-09-21) — the aion-category notes under the vault's log/,
// tier-gated by aion/tier-map.json, exported beside the contract pack as
//
//	<packDir>/transcripts/…   open + internal notes verbatim, INDEX.md
//	<packDir>/digests/…       topic rollups, email-<date>.md, README.md (stamp)
//
// and the OPEN subset published to portal.aion.bio's ARTIFACTS through the
// team dir's existing content-addressed files/ tree (threads.Store.SaveBlob —
// the same blobs comment attachments use, served by the same signed-in
// /api/team/file/{hash} route). Mail digests project the personal-email
// worker's synced threads; anything sensitive lands in an owner-only hold
// file under dataDir, never under /shared.
//
// Same discipline as aion_pack.go: pure render, deterministic bytes, atomic
// files, stamp written LAST, skip-when-unchanged by revision. The vault is
// read, never written. Held notes are never read past the tier gate — their
// bytes are opened only to hash the revision and count them.

// aionCorpusSnapshot is one composed read of the transcript channel.
type aionCorpusSnapshot struct {
	Revision string
	At       time.Time
	Corpus   aion.TranscriptCorpus
	Email    []aion.EmailDay
}

// aionCorpusRevision reads the stamp on an existing transcript sub-pack.
func aionCorpusRevision(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(aion.TranscriptStampFile)))
	if err != nil {
		return ""
	}
	if m := aionPackRevRe.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// readAionTranscriptDir reads every top-level .md file of the transcript
// directory (never recursive — log/ is flat, and a subfolder is not a
// transcript). Sorted by name so the revision hash is stable.
func readAionTranscriptDir(dir string) ([]aion.RawNote, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []aion.RawNote
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, aion.RawNote{Name: e.Name(), Body: b})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// aionCorpusRevisionOf fingerprints everything the render depends on: the
// tier map, every note's name + bytes (held ones too — a held note appearing
// or vanishing changes the counts), and the projected mail days.
func aionCorpusRevisionOf(tm aion.TierMap, raws []aion.RawNote, days []aion.EmailDay) string {
	h := sha256.New()
	for _, name := range tm.Names() {
		e := tm[name]
		fmt.Fprintf(h, "tier|%s|%s\n", name, e.Tier)
	}
	for _, r := range raws {
		sum := sha256.Sum256(r.Body)
		fmt.Fprintf(h, "note|%s|%x\n", r.Name, sum)
	}
	if b, err := json.Marshal(days); err == nil {
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// syncAionCorpus writes the rendered sub-pack when the stamp is behind.
// Every file lands tmp+rename, group-readable (kairos reads via teamshare).
// Then transcripts/ and digests/ are PRUNED of anything not rendered — this
// is what retracts a note the owner moved to held — and only then the stamp
// is written. Reports whether it wrote.
func syncAionCorpus(dir string, files []aion.PackFile, rev string) (bool, error) {
	if dir == "" || len(files) == 0 {
		return false, nil
	}
	if rev != "" && aionCorpusRevision(dir) == rev {
		return false, nil
	}
	stamp := files[len(files)-1]
	if stamp.Path != aion.TranscriptStampFile {
		return false, fmt.Errorf("aion corpus: renderer must end with %s, got %s", aion.TranscriptStampFile, stamp.Path)
	}
	keep := map[string]bool{}
	write := func(f aion.PackFile) error {
		path := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o775); err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(f.Body), 0o664); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
	for _, f := range files[:len(files)-1] {
		if err := write(f); err != nil {
			return false, err
		}
		keep[f.Path] = true
	}
	keep[stamp.Path] = true
	for _, sub := range []string{"transcripts", "digests"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			rel := sub + "/" + e.Name()
			if !keep[rel] {
				if err := os.Remove(filepath.Join(dir, sub, e.Name())); err != nil && !os.IsNotExist(err) {
					return false, err
				}
			}
		}
	}
	if err := write(stamp); err != nil {
		return false, err
	}
	return true, nil
}

// ---- portal artifacts -----------------------------------------------------

// aionArtifactManifestFile is the publisher's own ledger inside the team dir:
// which hashes it put into files/ and at which corpus revision. It is what
// makes a retraction exact (a note moved off `open` has its blob removed) and
// what the ARTIFACTS list reads. It is an index of published refs, not a
// store — the bytes live in the existing files/ tree only.
const aionArtifactManifestFile = "transcripts.json"

type aionArtifactManifest struct {
	Revision  string                    `json:"revision"`
	Artifacts []aion.TranscriptArtifact `json:"artifacts"`
}

func readAionArtifactManifest(dir string) aionArtifactManifest {
	var m aionArtifactManifest
	if b, err := os.ReadFile(filepath.Join(dir, aionArtifactManifestFile)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	if m.Artifacts == nil {
		m.Artifacts = []aion.TranscriptArtifact{}
	}
	return m
}

// publishAionArtifacts puts every OPEN note into the team dir's files/ tree
// (dedup by hash — SaveBlob is idempotent) and removes any blob a previous
// publish wrote that is no longer open. Returns the manifest now on disk and
// whether anything changed. The corpus's own tier gate is re-applied through
// PortalArtifacts, so an internal note cannot reach here.
func publishAionArtifacts(blobs *threads.Store, rev string, corpus aion.TranscriptCorpus) (aionArtifactManifest, bool, error) {
	if blobs == nil {
		return aionArtifactManifest{}, false, nil
	}
	dir := blobs.Dir()
	old := readAionArtifactManifest(dir)
	if rev != "" && old.Revision == rev {
		return old, false, nil
	}
	wantArts := corpus.PortalArtifacts()
	want := map[string]bool{}
	bodies := map[string]aion.TranscriptNote{}
	for _, n := range corpus.Notes {
		bodies[n.Hash] = n
	}
	for _, a := range wantArts {
		want[a.Hash] = true
		if blobs.BlobPath(a.Hash) != "" {
			continue
		}
		n := bodies[a.Hash]
		ref, err := blobs.SaveBlob(bytes.NewReader(n.Body), a.Name, a.Mime)
		if err != nil {
			return old, false, fmt.Errorf("publish %s: %w", a.Name, err)
		}
		if ref.Hash != a.Hash {
			return old, false, fmt.Errorf("publish %s: blob hash %s != note hash %s", a.Name, ref.Hash, a.Hash)
		}
	}
	for _, a := range old.Artifacts {
		if want[a.Hash] {
			continue
		}
		if p := blobs.BlobPath(a.Hash); p != "" {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return old, false, fmt.Errorf("retract %s: %w", a.Name, err)
			}
		}
	}
	if wantArts == nil {
		wantArts = []aion.TranscriptArtifact{}
	}
	m := aionArtifactManifest{Revision: rev, Artifacts: wantArts}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return old, false, err
	}
	path := filepath.Join(dir, aionArtifactManifestFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return old, false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return old, false, err
	}
	return m, true, nil
}

// ---- owner-only mail hold ---------------------------------------------------

// writeAionEmailHold writes one owner-only file per day that held anything,
// 0600 under a 0700 dir — by construction outside the /shared pack. Nothing
// is pruned here: the hold is the owner's record.
func writeAionEmailHold(holdDir string, days []aion.EmailDay, rev string, at time.Time) error {
	if holdDir == "" {
		return nil
	}
	for _, day := range days {
		if len(day.Held) == 0 {
			continue
		}
		if err := os.MkdirAll(holdDir, 0o700); err != nil {
			return err
		}
		body := []byte(aion.RenderEmailHold(day, rev, at))
		path := filepath.Join(holdDir, "email-"+day.Date+".md")
		if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, body) {
			continue
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, body, 0o600); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
	}
	return nil
}

// ---- AionLive glue ----------------------------------------------------------

// UseTranscripts points the transcript channel at the vault's log directory
// (absolute). "" disables the corpus; the contract pack is unaffected.
func (l *AionLive) UseTranscripts(absDir string) {
	l.mu.Lock()
	l.transcriptDir = absDir
	l.mu.Unlock()
}

// aionBlobs is the team dir's blob store, the portal's files/ tree (nil until
// main wires UseThreads, which happens after the pack is enabled — so the
// first PackLoop tick publishes what UseAPack could not).
func (l *AionLive) aionBlobs() *threads.Store {
	if l.s == nil || l.s.threads == nil {
		return nil
	}
	return l.s.threads.aionFS
}

// Artifacts is the published OPEN-note list the portal's team state carries.
// Read from the manifest the publisher left in the team dir, memoized per
// revision; empty (never nil) when nothing is published yet.
func (l *AionLive) Artifacts() []aion.TranscriptArtifact {
	blobs := l.aionBlobs()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.artifacts == nil && blobs != nil {
		m := readAionArtifactManifest(blobs.Dir())
		l.artifacts, l.artifactsRev = m.Artifacts, m.Revision
	}
	out := make([]aion.TranscriptArtifact, 0, len(l.artifacts))
	return append(out, l.artifacts...)
}

// corpusSnapshot composes the transcript channel: the log/ notes through the
// tier gate, the index's people for the INDEX rows, and the mail projection.
func (l *AionLive) corpusSnapshot() (*aionCorpusSnapshot, error) {
	l.mu.Lock()
	dir, prevRev, prevAt := l.transcriptDir, l.corpusRev, l.corpusAt
	l.mu.Unlock()
	if dir == "" {
		return nil, nil
	}
	tm := l.tierMap // tests inject a fixture map; production loads the data file
	if tm == nil {
		var err error
		if tm, err = aion.LoadTierMap(); err != nil {
			return nil, err
		}
	}
	raws, err := readAionTranscriptDir(dir)
	if err != nil {
		return nil, err
	}
	var isPerson func(string) (string, bool)
	var email []aion.EmailDay
	if l.s != nil && l.s.index != nil {
		if people, err := l.s.index.PeopleNotes(); err == nil {
			byKey := map[string]string{}
			for _, p := range people {
				byKey[strings.ToLower(p.Key)] = p.Display
			}
			isPerson = func(key string) (string, bool) {
				d, ok := byKey[key]
				return d, ok
			}
		}
		threads, err := aionEmailThreads(l.s.index, l.s.index.VaultRoot(), filepath.Join(l.s.aionDataDir, "domain-extraction"), tm)
		if err != nil {
			log.Printf("aion corpus: mail projection skipped: %v", err)
		} else {
			email = aion.ProjectEmailDays(threads)
		}
	}
	corpus := aion.BuildTranscriptCorpus(tm, raws, isPerson)
	rev := aionCorpusRevisionOf(tm, raws, email)
	// the stamp's timestamp is the moment THIS revision was first composed
	// (aion_pack.go uses lastGoodAt the same way): it moves only when the
	// revision does, so an unchanged corpus re-renders byte-identically.
	at := prevAt
	if at.IsZero() || rev != prevRev {
		at = time.Now().UTC()
	}
	return &aionCorpusSnapshot{Revision: rev, At: at, Corpus: corpus, Email: email}, nil
}

// SyncCorpus runs the transcript channel end to end: pack sub-dirs, portal
// artifacts, owner hold files. Best-effort like syncPackLocked — one log line
// per failing revision, never blocking a read path. Safe to call from any
// goroutine; it takes l.mu only briefly.
func (l *AionLive) SyncCorpus() {
	l.corpusMu.Lock() // one sync at a time: the loop and main's kick may overlap
	defer l.corpusMu.Unlock()
	l.mu.Lock()
	packDir := l.packDir
	l.mu.Unlock()
	snap, err := l.corpusSnapshot()
	if err != nil || snap == nil {
		if err != nil && l.corpusErrRev != "read" {
			l.corpusErrRev = "read"
			log.Printf("aion corpus: %v", err)
		}
		return
	}
	l.mu.Lock()
	prevRev := l.corpusRev
	l.mu.Unlock()
	if names := snap.Corpus.UnmappedNames; len(names) > 0 && prevRev != snap.Revision {
		// names go to the owner's log only — never into the pack
		log.Printf("aion corpus: %d aion note(s) in %s have no tier and are excluded everywhere until tiered: %s",
			len(names), "the tier map", strings.Join(names, "; "))
	}
	l.mu.Lock()
	l.corpusRev, l.corpusAt = snap.Revision, snap.At
	l.mu.Unlock()

	if packDir != "" {
		files := aion.RenderTranscriptPack(aion.TranscriptPackInput{
			Revision: snap.Revision, At: snap.At, Corpus: snap.Corpus, Email: snap.Email,
		})
		wrote, err := syncAionCorpus(packDir, files, snap.Revision)
		if err != nil {
			if l.corpusErrRev != snap.Revision {
				l.corpusErrRev = snap.Revision
				log.Printf("aion corpus: %v", err)
			}
		} else if wrote {
			log.Printf("aion corpus: %s/{transcripts,digests} @ %s (%d notes · %d held · %d unmapped · %d mail days)",
				packDir, snap.Revision, len(snap.Corpus.Notes), snap.Corpus.Held, snap.Corpus.Unmapped, len(snap.Email))
		}
		if l.s != nil && l.s.aionDataDir != "" {
			if err := writeAionEmailHold(filepath.Join(l.s.aionDataDir, "aion", "email-hold"), snap.Email, snap.Revision, snap.At); err != nil {
				log.Printf("aion corpus: email hold: %v", err)
			}
		}
	}

	if blobs := l.aionBlobs(); blobs != nil {
		m, changed, err := publishAionArtifacts(blobs, snap.Revision, snap.Corpus)
		if err != nil {
			log.Printf("aion artifacts: %v", err)
			return
		}
		l.mu.Lock()
		l.artifacts, l.artifactsRev = m.Artifacts, m.Revision
		l.mu.Unlock()
		if changed {
			log.Printf("aion artifacts: %d open transcript(s) published to %s/files @ %s", len(m.Artifacts), blobs.Dir(), m.Revision)
		}
	}
}
