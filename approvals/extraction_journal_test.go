package approvals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"manifest/aion"
)

func journalFixture(t *testing.T) (*Store, Proposal, string, string) {
	t.Helper()
	s, vault, data := aionTestStore(t)
	s.WithExtractionJournal(data)
	if err := os.WriteFile(filepath.Join(vault, "source.md"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	p := aionProposal(aion.ProposalPayload{Kind: "task", Title: "Reviewed task", Status: "open", Sources: []string{"source.md"}, Captured: "2026-09-16", Quote: "source"})
	p.ID = "abcdef123456"
	p.ExtractionSnapshot = EncodeExtractionSnapshot(map[string]string{"source.md": EvidenceHash("source")}, p)
	if _, err := s.ProposeOnce(p); err != nil {
		t.Fatal(err)
	}
	return s, p, vault, data
}

// holdFixture is journalFixture with the source note changed after extraction:
// the one condition that holds a single-file candidate under the bounded gate
// (2026-09-23), so the journal's refusal paths still have a refusal to record.
func holdFixture(t *testing.T) (*Store, Proposal, string, string) {
	t.Helper()
	s, p, vault, data := journalFixture(t)
	if err := os.WriteFile(filepath.Join(vault, "source.md"), []byte("source changed after extraction"), 0600); err != nil {
		t.Fatal(err)
	}
	return s, p, vault, data
}
func journalRecord(t *testing.T, data string) (string, ExtractionTransaction) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(data, "extraction-transactions", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("records: %v %v", files, err)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var r ExtractionTransaction
	if err = json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return files[0], r
}
func TestExtractionJournalRefusalAndDuplicate(t *testing.T) {
	s, p, vault, data := holdFixture(t)
	before := extractionTree(t, vault)
	pending := extractionTree(t, s.dir)
	for n := 0; n < 3; n++ {
		if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "state=aborted") {
			t.Fatal(err)
		}
		file, r := journalRecord(t, data)
		if r.Version != 1 || r.Replay || r.State != "aborted" || len(r.Transitions) != 2 || r.Transitions[0].State != "prepare" || r.DependenciesComplete || r.WritesComplete || r.Writes[0].After.Known || len(r.Missing) < 7 || EvidenceHash(string(r.ApprovalBytes)) != r.ApprovalDigest {
			t.Fatalf("bad receipt: %+v", r)
		}
		fi, err := os.Stat(file)
		if err != nil || fi.Mode().Perm() != 0600 {
			t.Fatalf("permissions: %v %v", fi, err)
		}
		if !reflect.DeepEqual(before, extractionTree(t, vault)) || !reflect.DeepEqual(pending, extractionTree(t, s.dir)) {
			t.Fatal("refusal mutated vault or approval")
		}
		original := extractionTree(t, data)
		if err := s.RecoverExtractionJournal(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, extractionTree(t, data)) {
			t.Fatal("duplicate recovery changed terminal receipt")
		}
		s = NewStore(filepath.Dir(s.dir)).WithVaultRoot(vault).WithVaultWriter(s.vw).WithAionCapability("aion-approved").WithExtractionJournal(data)
	}
}

func TestExtractionJournalInterruptedRecovery(t *testing.T) {
	for _, scenario := range []string{"prepare", "committing", "partial-write", "all-after-audit-missing", "approval-settlement-crash", "committed", "replay"} {
		t.Run(scenario, func(t *testing.T) {
			s, p, vault, data := holdFixture(t)
			if err := s.Confirm(p.ID); err == nil {
				t.Fatal("confirmed")
			}
			file, r := journalRecord(t, data)
			r.State = "prepare"
			r.Transitions = r.Transitions[:1]
			if scenario != "prepare" {
				journalTransition(&r, "committing", "simulated interrupted future writer")
			}
			if scenario == "committed" {
				journalTransition(&r, "committed", "unverified foreign completion")
			}
			if scenario == "replay" {
				r.Replay = true
			}
			if scenario == "partial-write" || scenario == "all-after-audit-missing" || scenario == "approval-settlement-crash" {
				// Simulate a landed partial write followed by an owner edit; recovery must
				// preserve the owner bytes regardless of before/after claims in the record.
				if err := os.WriteFile(filepath.Join(vault, "source.md"), []byte("intervening owner edit"), 0600); err != nil {
					t.Fatal(err)
				}
				r.Writes = []ExtractionWrite{{Path: "source.md", Capability: "aion-approved", Before: ExtractionImage{Known: true, Hash: EvidenceHash("source"), Bytes: []byte("source")}, After: ExtractionImage{Known: true, Hash: EvidenceHash("intervening owner edit"), Bytes: []byte("intervening owner edit")}}}
			}
			if scenario == "approval-settlement-crash" {
				b, err := os.ReadFile(filepath.Join(s.dir, "pending", p.ID+".md"))
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(s.dir, "approved", p.ID+".md"), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			b, _ := json.Marshal(r)
			if err := os.WriteFile(file, b, 0600); err != nil {
				t.Fatal(err)
			}
			before, decisions := extractionTree(t, vault), extractionTree(t, s.dir)
			s = NewStore(filepath.Dir(s.dir)).WithVaultRoot(vault).WithExtractionJournal(data)
			if err := s.RecoverExtractionJournal(); err != nil {
				t.Fatal(err)
			}
			_, got := journalRecord(t, data)
			if got.State != "uncertain" || got.Replay {
				t.Fatalf("recovery: %+v", got)
			}
			if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "state=uncertain") {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, extractionTree(t, vault)) || !reflect.DeepEqual(decisions, extractionTree(t, s.dir)) {
				t.Fatal("recovery changed vault or settled approval")
			}
		})
	}
}

func TestExtractionJournalFailureStaysHeld(t *testing.T) {
	for _, scenario := range []string{"unavailable", "inside-vault", "symlink", "corrupt", "audit-failure", "dependency-missing", "expected-absent-added", "category-identity", "index-identity", "artifact-store-identity"} {
		t.Run(scenario, func(t *testing.T) {
			// journal robustness is exercised under a real hold (a changed source);
			// the identity scenarios add unrelated files, which the bounded gate
			// accepts — the candidate applies and no receipt is needed
			identity := scenario == "expected-absent-added" || scenario == "category-identity" || scenario == "index-identity" || scenario == "artifact-store-identity"
			var s *Store
			var p Proposal
			var vault, data string
			if identity {
				s, p, vault, data = journalFixture(t)
			} else {
				s, p, vault, data = holdFixture(t)
			}
			switch scenario {
			case "unavailable":
				s.WithExtractionJournal(filepath.Join(data, "missing"))
			case "inside-vault":
				s.WithExtractionJournal(vault)
			case "symlink":
				if err := os.Symlink(vault, filepath.Join(data, "extraction-transactions")); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				s.Confirm(p.ID)
				file, _ := journalRecord(t, data)
				if err := os.WriteFile(file, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := s.RecoverExtractionJournal(); err == nil {
					t.Fatal("accepted corruption")
				}
			case "audit-failure":
				if err := os.Mkdir(filepath.Join(data, "write-audit.log"), 0700); err != nil {
					t.Fatal(err)
				}
			case "dependency-missing":
				if err := os.Remove(filepath.Join(vault, "source.md")); err != nil {
					t.Fatal(err)
				}
			default:
				// V1 cannot bind these predicates at all. Adding/replacing identity
				// evidence cannot turn matching declared source hashes into authorization.
				if err := os.WriteFile(filepath.Join(vault, scenario+".md"), []byte("new identity"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, decisions := extractionTree(t, vault), extractionTree(t, s.dir)
			err := s.Confirm(p.ID)
			if identity {
				if err != nil || len(s.List("approved")) != 1 {
					t.Fatalf("an unrelated added file must not hold a fresh candidate: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "replay=false") {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, extractionTree(t, vault)) || !reflect.DeepEqual(decisions, extractionTree(t, s.dir)) {
				t.Fatal("failed receipt escaped hold")
			}
		})
	}
}
