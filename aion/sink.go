package aion

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"manifest/record"
	"manifest/vaultindex"
)

// ExtractSink is the watcher-side half of the extraction pipeline (spec §3):
// it receives touched vault paths from the ONE kernel watcher (via the
// vaultindex reindex callback — no second subscription), filters to
// knowledge-zone notes whose frontmatter categories include "aion", diffs
// them against a content-hash cursor, and spools a targeted ritual for the
// aion-extractor spirit. The engine does the thinking (§7); a missed spool
// is recoverable — queued paths persist and a ticker retries.
type ExtractSink struct {
	vaultRoot     string
	systemRoot    string
	extrinsicRoot string
	cursorPath    string
	sp            Spooler

	mu sync.Mutex
	c  cursor
}

// Spooler is what the sink needs from the spirits store; *spirits.Store
// satisfies it.
type Spooler interface {
	SpoolRunNow(spirit, ritual, request, skill string) error
	EngineAlive() (bool, time.Time)
}

// The targeted ritual the sink spools (the engine-side spirit files carry
// the extraction contract).
const (
	ExtractorSpirit = "aion-extractor"
	ExtractorRitual = "extract"
)

// maxBatchNotes caps how many notes one spool names: the ritual has a step
// ceiling (system reads + one read per note + the approval writes), so a
// bulk change extracts across several small runs instead of one doomed
// giant one.
const maxBatchNotes = 4

// maxSpoolRequest mirrors the spirits store's request cap; batches are cut
// to stay under it (the remainder stays queued for the next flush).
const maxSpoolRequest = 3800

type cursor struct {
	// Notes maps vault-relative path → the sha1 of the content last spooled.
	Notes map[string]noteMark `json:"notes"`
	// Queued are paths awaiting a successful spool (engine down / busy).
	Queued []string `json:"queued"`
}

type noteMark struct {
	Hash      string `json:"hash"`
	SpooledAt string `json:"spooledAt"`
}

func NewExtractSink(vaultRoot, systemRoot, extrinsicRoot, dataDir string, sp Spooler) *ExtractSink {
	s := &ExtractSink{
		vaultRoot:     vaultRoot,
		systemRoot:    systemRoot,
		extrinsicRoot: extrinsicRoot,
		cursorPath:    filepath.Join(dataDir, "aion", "cursor.json"),
		sp:            sp,
	}
	s.c = cursor{Notes: map[string]noteMark{}}
	if b, err := os.ReadFile(s.cursorPath); err == nil {
		_ = json.Unmarshal(b, &s.c)
		if s.c.Notes == nil {
			s.c.Notes = map[string]noteMark{}
		}
	} else {
		// FRESH cursor: baseline the existing corpus — every current aion
		// note is presumed already processed (the historic backlog was
		// imported wholesale), so only FUTURE creations/edits trigger
		// extraction. Without this, the first watcher sweep over the
		// historic corpus queues everything.
		s.baseline()
	}
	return s
}

// baseline records the content hash of every existing aion note without
// queueing anything, and persists the cursor so the walk happens once.
func (s *ExtractSink) baseline() {
	s.walkAionNotes(func(rel, hash string) {
		s.c.Notes[rel] = noteMark{Hash: hash, SpooledAt: "baseline"}
	})
	s.saveLocked() // construction-time: no concurrent access yet
}

// catchUp queues aion notes created or edited WHILE THE APP WAS DOWN (the
// watcher only sees live events): any note whose hash differs from the
// cursor is due for extraction. Runs once at Start, after the initial
// baseline exists; batching bounds the run size as usual.
func (s *ExtractSink) catchUp() {
	s.mu.Lock()
	changed := false
	s.walkAionNotes(func(rel, hash string) {
		if s.c.Notes[rel].Hash == hash || contains(s.c.Queued, rel) {
			return
		}
		s.c.Queued = append(s.c.Queued, rel)
		changed = true
	})
	if changed {
		s.saveLocked()
	}
	s.mu.Unlock()
}

// walkAionNotes visits every knowledge-zone aion note with its content hash.
func (s *ExtractSink) walkAionNotes(fn func(rel, hash string)) {
	skip := map[string]bool{".git": true, ".obsidian": true, ".trash": true, "attachments": true}
	_ = filepath.WalkDir(s.vaultRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != s.vaultRoot && (skip[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(s.vaultRoot, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !s.isAionNote(rel) {
			return nil
		}
		if h, ok := s.hashNote(rel); ok {
			fn(rel, h)
		}
		return nil
	})
}

// Start runs the retry loop: an early first flush (a restart with work
// already queued shouldn't wait five minutes), then the steady ticker for
// queued paths (engine down / spirit busy at Notify time).
func (s *ExtractSink) Start(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Second):
			s.catchUp() // notes created/edited while the app was down
			s.Flush()
		}
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.Flush()
			}
		}
	}()
}

// Notify feeds touched vault-relative paths (the reindex callback's slice).
// Non-aion paths are cheap to reject; changed aion notes enqueue + flush.
func (s *ExtractSink) Notify(paths []string) {
	changed := false
	s.mu.Lock()
	for _, rel := range paths {
		rel = filepath.ToSlash(rel)
		if !s.isAionNote(rel) {
			continue
		}
		h, ok := s.hashNote(rel)
		if !ok {
			continue // unreadable/deleted — nothing to extract
		}
		if s.c.Notes[rel].Hash == h {
			continue // unchanged since last spool
		}
		if !contains(s.c.Queued, rel) {
			s.c.Queued = append(s.c.Queued, rel)
			changed = true
		}
	}
	if changed {
		s.saveLocked()
	}
	s.mu.Unlock()
	if changed {
		s.Flush()
	}
}

// Flush spools queued paths when the engine is alive. A busy spirit
// (double-spool guard) or dead engine leaves the queue intact.
func (s *ExtractSink) Flush() {
	if s.sp == nil {
		return
	}
	if alive, _ := s.sp.EngineAlive(); !alive {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.c.Queued) == 0 {
		return
	}
	// batch under the note cap + request cap; the remainder stays queued
	var batch []string
	req := "extract aion items from these vault notes:"
	for _, rel := range s.c.Queued {
		if len(batch) >= maxBatchNotes {
			break
		}
		line := "\n- " + rel
		if len(req)+len(line) > maxSpoolRequest {
			break
		}
		req += line
		batch = append(batch, rel)
	}
	if len(batch) == 0 {
		return
	}
	if err := s.sp.SpoolRunNow(ExtractorSpirit, ExtractorRitual, req, ""); err != nil {
		return // ErrAlreadyActive / spool failure — retry on the next tick
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rest := s.c.Queued[len(batch):]
	for _, rel := range batch {
		if h, ok := s.hashNote(rel); ok {
			s.c.Notes[rel] = noteMark{Hash: h, SpooledAt: now}
		}
	}
	s.c.Queued = append([]string{}, rest...)
	s.saveLocked()
}

// QueuedCount reports the current retry backlog (surfaced in settings).
func (s *ExtractSink) QueuedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.c.Queued)
}

// isAionNote is the trigger filter (spec §3): a markdown note in the
// KNOWLEDGE zone (this alone excludes system/** — and so system/excalibur —
// and extrinsic/**) whose frontmatter categories include "aion" in either
// YAML style (vaultindex.ParseNote handles inline and block lists).
func (s *ExtractSink) isAionNote(rel string) bool {
	if !strings.HasSuffix(rel, ".md") {
		return false
	}
	if record.ZoneOf(rel, s.systemRoot, s.extrinsicRoot) != record.ZoneKnowledge {
		return false
	}
	b, err := os.ReadFile(filepath.Join(s.vaultRoot, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	note := vaultindex.ParseNote(rel, b, 0, nil)
	for _, c := range note.Categories {
		if strings.EqualFold(c, "aion") {
			return true
		}
	}
	return false
}

func (s *ExtractSink) hashNote(rel string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(s.vaultRoot, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:]), true
}

func (s *ExtractSink) saveLocked() {
	if err := os.MkdirAll(filepath.Dir(s.cursorPath), 0o755); err != nil {
		return
	}
	b, _ := json.MarshalIndent(s.c, "", "  ")
	_ = os.WriteFile(s.cursorPath, b, 0o644)
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
