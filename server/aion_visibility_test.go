package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
	"manifest/approvals"
	"manifest/spirits"
)

// plantCreateNote drops a pending create-vault-note as the connector casts
// file it: the proposed note (frontmatter + body) inside a proposed fence.
func plantCreateNote(t *testing.T, root, id, applyPath, proposed string) {
	t.Helper()
	p := filepath.Join(root, "artifacts", "approvals", "pending", id+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nid: " + id + "\ntype: create-vault-note\naction: Create vault note: " + applyPath +
		"\nagent: ea-coordinator\ncreated: 2026-09-21T08:00:00Z\napply-path: " + applyPath +
		"\n---\n\nNew transcript.\n\n````proposed\n" + proposed + "\n````\n"
	if err := os.WriteFile(p, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}

func visibilityServer(t *testing.T, tm aion.TierMap) (*Server, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "excalibur")
	s := &Server{}
	s.UseHarnesses([]Harness{{Name: "excalibur", Spirits: spirits.NewStore(root), Approvals: approvals.NewStore(filepath.Join(root, "artifacts"))}})
	s.aionLive = &AionLive{s: s, tierMap: tm}
	return s, root
}

// The approvals card proposes a visibility tier for a synced transcript
// note: the data file's tier when the note is already tiered, `held`
// otherwise (unknown never defaults to more exposure); a note that is not a
// connector transcript gets no suggestion at all.
func TestApprovalVisibilitySuggestion(t *testing.T) {
	tm := aion.TierMap{"2026-09-20 rj sync.md": {Tier: aion.TierInternal, Reason: "team sync"}}
	s, root := visibilityServer(t, tm)
	plantCreateNote(t, root, "g1", "2026-09-21 New Granola Sync.md",
		"---\ncategories:\n  - aion\ngranola-id: gr_1\n---\n[[jane doe]]\n\n## Transcript\n\nhi\n")
	plantCreateNote(t, root, "p1", "2026-09-20 RJ Sync.md",
		"---\ncategories: [aion]\npocket-id: pk_1\n---\n\n## Transcript\n\nhi\n")
	plantCreateNote(t, root, "e1", "2026-09-19 - 2026-09-21 hi from bedrock.md",
		"---\ncategories: [personal]\ngmail-thread-id: th_1\n---\n\n## 2026-09-19 — them\n\nhi\n")
	plantCreateNote(t, root, "n1", "2026-09-21 hand-filed note.md",
		"---\ncategories: [aion]\n---\n\n## Notes\n\nhi\n")

	rows := map[string]approvalRow{}
	for _, r := range s.approvalRows(nil) {
		rows[r.ID] = r
	}
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	g := rows["g1"].VisibilitySuggestion
	if g == nil || g.Suggested != aion.TierHeld || g.Known || g.Source != "granola" || g.Note != "2026-09-21 new granola sync.md" {
		t.Fatalf("granola suggestion = %+v, want held/unknown/granola", g)
	}
	p := rows["p1"].VisibilitySuggestion
	if p == nil || p.Suggested != aion.TierInternal || !p.Known || p.Source != "pocket" || p.Note != "2026-09-20 rj sync.md" {
		t.Fatalf("pocket suggestion = %+v, want internal/known/pocket", p)
	}
	// an email thread note is a transcript too: the suggestion rides along
	// (the card shows it only once the owner tags the note aion)
	e := rows["e1"].VisibilitySuggestion
	if e == nil || e.Suggested != aion.TierHeld || e.Known || e.Source != "email" {
		t.Fatalf("email suggestion = %+v, want held/unknown/email", e)
	}
	if rows["n1"].VisibilitySuggestion != nil {
		t.Fatalf("a hand-filed note must carry no suggestion: %+v", rows["n1"].VisibilitySuggestion)
	}
}

// The accepted tier is recorded in the tier map (the one tier store) under
// the filename that was written, and nothing else moves: a non-aion note
// records nothing, a tier outside the vocabulary is refused, and an
// unconfigured checkout is an error rather than a silent skip.
func TestApprovalVisibilityRecorded(t *testing.T) {
	s, _ := visibilityServer(t, nil)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "aion"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed, err := aion.MarshalTierMap(aion.TierMap{"2026-01-19 aion team sync.md": {Tier: aion.TierOpen, Reason: "team sync", Bytes: 1}})
	if err != nil {
		t.Fatal(err)
	}
	mapPath := filepath.Join(repo, "aion", "tier-map.json")
	if err := os.WriteFile(mapPath, seed, 0o644); err != nil {
		t.Fatal(err)
	}
	s.terminal = &termCfg{codingRepo: repo}

	proposed := "---\ncategories:\n  - aion\ngranola-id: gr_1\n---\n\n## Transcript\n\nhi\n"
	approved := approvals.Proposal{ID: "g1", Type: approvals.TypeCreateVaultNote,
		ApplyPath: "2026-09-21 Retitled Sync.md", Proposed: proposed}
	if err := s.aionRecordVisibility(approved, "open"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(mapPath)
	tm, err := aion.ParseTierMap(raw)
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := tm["2026-09-21 retitled sync.md"]; !ok || e.Tier != aion.TierOpen || !strings.Contains(e.Reason, "approvals inbox (granola)") || e.Bytes != len(proposed) {
		t.Fatalf("recorded entry = %+v (%v)", e, ok)
	}
	if len(tm) != 2 {
		t.Fatalf("entries = %d, want 2 (the seed kept)", len(tm))
	}
	// the export predicate now sees it
	if got, ok := tm.SourceTier("log/2026-09-21 retitled sync"); !ok || got != aion.TierOpen {
		t.Fatalf("SourceTier = %q %v", got, ok)
	}

	// a note without the aion category is not the tier map's business
	before, _ := os.ReadFile(mapPath)
	plain := approved
	plain.Proposed = "---\ncategories: [personal]\ngranola-id: gr_2\n---\n\n## Transcript\n\nhi\n"
	plain.ApplyPath = "2026-09-21 personal chat.md"
	if err := s.aionRecordVisibility(plain, "held"); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(mapPath); string(after) != string(before) {
		t.Fatal("a non-aion note changed the tier map")
	}
	// the vocabulary is closed
	if err := s.aionRecordVisibility(approved, "public"); err == nil {
		t.Fatal("accepted a tier outside open|internal|held")
	}
	// no checkout → an error the owner sees, not a silent skip
	s.terminal = nil
	if err := s.aionRecordVisibility(approved, "held"); err == nil || !strings.Contains(err.Error(), "boardRepo") {
		t.Fatalf("unconfigured checkout: err = %v", err)
	}
}
