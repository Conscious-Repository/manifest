package approvals

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"manifest/aion"
	"manifest/artifacts"
	"manifest/realestate"
	"manifest/record"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

func TestExtractionBoundaryReceipt(t *testing.T) {
	// a refusal receipt needs a refusal: the bounded gate (2026-09-23) holds a
	// candidate whose source note changed, and journals that
	s, p, vault, data := holdFixture(t)
	before, decisions := extractionTree(t, vault), extractionTree(t, s.dir)
	if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), ExtractionCommitUnavailable) {
		t.Fatal(err)
	}
	file, r := journalRecord(t, data)
	want := CheckExtractionCommitBoundary()
	if r.Feasibility == nil || !reflect.DeepEqual(*r.Feasibility, want) || want.State != "commitUnavailable" || want.Replay || len(want.Blockers) != 7 {
		t.Fatalf("assessment: %+v", r.Feasibility)
	}
	// A historical terminal receipt remains byte-identical on restart and retry.
	r.Feasibility = nil
	if err := saveExtractionTransaction(filepath.Dir(file), r); err != nil {
		t.Fatal(err)
	}
	original := extractionTree(t, data)
	if err := s.RecoverExtractionJournal(); err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err == nil {
		t.Fatal("confirmed")
	}
	if !reflect.DeepEqual(original, extractionTree(t, data)) || !reflect.DeepEqual(before, extractionTree(t, vault)) || !reflect.DeepEqual(decisions, extractionTree(t, s.dir)) {
		t.Fatal("rewrote historical evidence or owner state")
	}
	s.WithExtractionJournal("")
	if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), ExtractionCommitUnavailable) {
		t.Fatal(err)
	}
}

// A real legacy apply can land even when settlement fails. The same failure
// fixture with a snapshot must never reach the writer or change decision history.
func TestExtractionBoundarySettlement(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "snapshot"}[snapshot], func(t *testing.T) {
			s, vault, data := aionTestStore(t)
			s.WithExtractionJournal(data)
			p := aionProposal(aion.ProposalPayload{Kind: "task", Title: "Settlement witness", Status: "open"})
			if snapshot {
				raw, err := os.ReadFile(filepath.Join(vault, AionBacklogPath))
				if err != nil {
					t.Fatal(err)
				}
				p.ExtractionSnapshot = EncodeExtractionSnapshot(map[string]string{AionBacklogPath: EvidenceHash(string(raw))}, p)
			}
			p, err := s.Propose(p)
			if err != nil {
				t.Fatal(err)
			}
			// A directory at the decision filename makes settlement fail deterministically.
			if err := os.Mkdir(filepath.Join(s.dir, "approved", p.ID+".md"), 0700); err != nil {
				t.Fatal(err)
			}
			before, decisions := extractionTree(t, vault), extractionTree(t, s.dir)
			if err := s.Confirm(p.ID); err == nil {
				t.Fatal("expected failure")
			}
			// under the bounded gate (2026-09-23) a fresh snapshot candidate applies
			// like a legacy one: the line lands, then settlement fails the same way
			if changed := !reflect.DeepEqual(before, extractionTree(t, vault)); !changed {
				t.Fatalf("vault changed=%v snapshot=%v", changed, snapshot)
			}
			if !reflect.DeepEqual(decisions, extractionTree(t, s.dir)) {
				t.Fatal("decision history changed")
			}
			raw, err := os.ReadFile(filepath.Join(vault, AionBacklogPath))
			if err != nil || !strings.Contains(string(raw), "Settlement witness") {
				t.Fatalf("apply did not land: %s %v", raw, err)
			}
		})
	}
}

func TestExtractionBoundaryFileStorePartialPublication(t *testing.T) {
	vault := t.TempDir()
	w := vaultwriter.New(vault).WithAudit(t.TempDir()).Grant(vaultwriter.Capability{Name: "re-files", Zone: record.ZoneSystem, Pattern: "system/realestate/files/**", Actor: vaultwriter.ActorUserAction})
	injected := errors.New("index publication interrupted")
	fs := realestate.NewFileStore(vault, "system/realestate", func(rel string, b []byte) error {
		if filepath.Base(rel) == "files.json" {
			return injected
		}
		return w.WriteCap("re-files", rel, b)
	})
	if _, err := fs.Save([]byte("document"), "bid.pdf", "application/pdf", time.Unix(0, 0)); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	hash := EvidenceHash("document")
	b, err := os.ReadFile(filepath.Join(vault, "system/realestate/files", hash+".pdf"))
	if err != nil || string(b) != "document" {
		t.Fatalf("blob: %s %v", b, err)
	}
	if _, _, ok := fs.Lookup("sha256:" + hash); ok {
		t.Fatal("unpublished index resolved blob")
	}
	if _, err := os.Stat(filepath.Join(vault, "system/realestate/files/files.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestExtractionBoundaryIndependentProjections(t *testing.T) {
	s, p, vault, data := journalFixture(t)
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err = ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	pool, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg, err := artifacts.NewRegistry(pool)
	if err != nil {
		t.Fatal(err)
	}
	first, err := reg.Put(artifacts.Put{Ref: "source.md", Content: []byte("reviewed")})
	if err != nil {
		t.Fatal(err)
	}
	// Both stores can drift without changing the V1 snapshot's declared source.
	if err = os.WriteFile(filepath.Join(vault, "new-category.md"), []byte("---\ncategories: [aion]\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = ix.DB().QueryRow("SELECT count(*) FROM notes WHERE path='new-category.md'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("index should lag: %d %v", count, err)
	}
	if err = ix.ReindexPaths([]string{"new-category.md"}); err != nil {
		t.Fatal(err)
	}
	if err = ix.DB().QueryRow("SELECT count(*) FROM notes WHERE path='new-category.md'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("index failed to advance: %d %v", count, err)
	}
	next, err := reg.Put(artifacts.Put{ID: first.Artifact.ID, ExpectedHead: first.Artifact.Head, Content: []byte("changed")})
	if err != nil || next.Artifact.Head == first.Artifact.Head {
		t.Fatalf("artifact did not advance: %v", err)
	}
	// The index and the artifact registry drifted, but the declared source did
	// not: under the bounded gate (2026-09-23) that candidate applies — those
	// stores are projections of the vault, never the evidence a backlog append
	// depends on — and neither store is touched by the apply.
	if err = s.Confirm(p.ID); err != nil {
		t.Fatalf("fresh candidate held by unrelated store drift: %v", err)
	}
	if len(s.List("approved")) != 1 {
		t.Fatal("apply did not settle")
	}
	if files, _ := filepath.Glob(filepath.Join(data, "extraction-transactions", "*.json")); len(files) != 0 {
		t.Fatal("an apply must not leave a refusal receipt")
	}
	head, ok := reg.Get(first.Artifact.ID)
	if !ok || head.Head != next.Artifact.Head {
		t.Fatal("apply rewrote artifact")
	}
}
