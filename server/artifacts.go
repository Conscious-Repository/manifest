package server

// ONE artifact resolver (owner ask 2026-08-12): "when I delegate work I want to
// click and VIEW the result" — from the FEED card, from the delegation-done
// card, and from the TODOS board. Every surface asks the same two questions
// here, so they can never point at different files:
//
//   1. which file IS the result?  — the run's library brief (frontmatter
//      `run:`), or the newest brief carrying the delegation's [todo:: id]
//      token, or the item's own `artifacts/library/…` reference;
//   2. how does the client open it? — artifactRefSplit: the two-media rule
//      (§8). A brief inside the vault (legacy in-vault harness) opens in the
//      note view; a brief in the harness tree opens through the spirits read
//      API. Never both, never guessed at the call site.

import (
	"os"
	"path/filepath"
	"strings"

	"manifest/spirits"
)

// libraryFn is a lazily-loaded harness library — the caller builds it once per
// harness and hands it down, so a page of feed cards costs at most one library
// read (and none at all when every card's link already resolves).
type libraryFn func() []spirits.LibraryDoc

// harnessLibrary returns a memoized loader for one harness's briefs.
func harnessLibrary(h Harness) libraryFn {
	var docs []spirits.LibraryDoc
	loaded := false
	return func() []spirits.LibraryDoc {
		if !loaded {
			loaded = true
			if h.Spirits != nil {
				docs = h.Spirits.Library()
			}
		}
		return docs
	}
}

// artifactRefSplit resolves a harness-relative ref to (vaultRel, harnessRef).
// vaultRel is non-empty only under the legacy in-vault layout (the note view);
// harnessRef opens through /api/spirits/file. Both empty when the file is gone.
func (s *Server) artifactRefSplit(h Harness, ref string) (string, string) {
	if ref == "" || h.Spirits == nil {
		return "", ""
	}
	abs := filepath.Join(h.Spirits.Root(), filepath.FromSlash(ref))
	if _, err := os.Stat(abs); err != nil {
		return "", "" // referenced file missing — nothing to open
	}
	if s.index != nil {
		if rel, err := filepath.Rel(s.index.VaultRoot(), abs); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel), "" // legacy: harness still inside the vault
		}
	}
	return "", filepath.ToSlash(filepath.FromSlash(ref))
}

// libraryDocForRun is the brief a run wrote: the report's `run:` id matched
// against the briefs' own `run:` field (the engine stamps both). The whole doc,
// not just the ref — a caller that lost the delegation token (a truncated
// `request:` in an old report) can recover it from the brief's own text.
func libraryDocForRun(h Harness, reportID string, lib libraryFn) (spirits.LibraryDoc, bool) {
	if h.Spirits == nil || reportID == "" {
		return spirits.LibraryDoc{}, false
	}
	runID := reportID
	if sum, _, ok := h.Spirits.Run(reportID); ok && sum.Run != "" {
		runID = sum.Run
	}
	return libraryDocForRunID(runID, reportID, lib)
}

// libraryDocForRunID is libraryDocForRun for a caller that already read the
// report (and so already knows its `run:` id) — no second read of the report.
func libraryDocForRunID(runID, reportID string, lib libraryFn) (spirits.LibraryDoc, bool) {
	if runID == "" && reportID == "" {
		return spirits.LibraryDoc{}, false
	}
	for _, d := range lib() {
		if d.Run != "" && (d.Run == runID || d.Run == reportID) {
			return d, true
		}
	}
	return spirits.LibraryDoc{}, false
}

// libraryRefForToken is the newest brief carrying a delegation's [todo:: id]
// token — the fallback for a card that names no library file at all (the
// engine put the reference nowhere the link/body regex can see it).
func libraryRefForToken(todoID string, lib libraryFn) string {
	todoID = strings.TrimSpace(todoID)
	if todoID == "" {
		return ""
	}
	for _, d := range lib() { // newest first
		for _, m := range todoTokenRe.FindAllStringSubmatch(d.Title+"\n"+d.Body, -1) {
			if strings.TrimSpace(m[1]) == todoID {
				return d.Ref
			}
		}
	}
	return ""
}
