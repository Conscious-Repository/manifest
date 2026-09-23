package approvals

import (
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"manifest/aion"
)

// The gate is bounded, not blanket (2026-09-23): a fresh candidate applies,
// a candidate whose source note changed is held with the reason, a moved-on
// context record does not hold an append, an owner edit rides Confirm, and
// replay/artifact/portal/contract snapshots stay held.
func TestExtractionGateAppliesFreshAndHoldsStale(t *testing.T) {
	for _, tc := range []struct{ scenario, want string }{
		{"fresh", ""},
		{"edited", ""},
		{"context-moved", ""},
		{"source", "source changed"},
		{"context-missing", "context missing"},
		{"replay", "invalid snapshot"},
		{"artifact", "cannot be revalidated"},
		{"portal", "cannot be revalidated"},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			s, vault, data := aionTestStore(t)
			source := "---\ncategories: [aion]\n---\nOwner said capture this task.\n"
			if err := os.WriteFile(filepath.Join(vault, "source.md"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			hashes := map[string]string{"source.md": EvidenceHash(source)}
			for _, name := range []string{"backlog.md", "heuristics.md", "people.md"} {
				raw, err := os.ReadFile(filepath.Join(vault, "system", "aion", name))
				if err != nil {
					t.Fatal(err)
				}
				hashes["system/aion/"+name] = EvidenceHash(string(raw))
			}
			p := aionProposal(aion.ProposalPayload{Kind: "task", Title: "Capture this task", Status: "open", Sources: []string{"source.md"}, Captured: "2026-09-16", Quote: "capture this task"})
			p.ID = "abcdef123456"
			p.ExtractionSnapshot = EncodeExtractionSnapshot(hashes, p)
			switch tc.scenario {
			case "edited":
				p.Body += "\nowner reviewed and edited"
			case "context-moved":
				if err := os.WriteFile(filepath.Join(vault, "system", "aion", "backlog.md"), []byte("# Backlog\n\n- [ ] something else landed meanwhile [kind:: task]\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "source":
				if err := os.WriteFile(filepath.Join(vault, "source.md"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "context-missing":
				if err := os.Remove(filepath.Join(vault, "system", "aion", "heuristics.md")); err != nil {
					t.Fatal(err)
				}
			case "replay":
				b, _ := base64.RawURLEncoding.DecodeString(p.ExtractionSnapshot)
				var snap ExtractionSnapshot
				json.Unmarshal(b, &snap)
				snap.Replay = true
				b, _ = json.Marshal(snap)
				p.ExtractionSnapshot = base64.RawURLEncoding.EncodeToString(b)
			case "artifact":
				hashes["sha256:"+EvidenceHash("email")] = EvidenceHash("email")
				p.ExtractionSnapshot = EncodeExtractionSnapshot(hashes, p)
			case "portal":
				hashes["portal-records"] = EvidenceHash("summary")
				p.ExtractionSnapshot = EncodeExtractionSnapshot(hashes, p)
			}
			if _, err := s.ProposeOnce(p); err != nil {
				t.Fatal(err)
			}
			if hold := s.ExtractionHold(p); (tc.want == "") != (hold == "") || !strings.Contains(hold, tc.want) {
				t.Fatalf("hold %q, want %q", hold, tc.want)
			}
			before, auditBefore := extractionTree(t, vault), extractionTree(t, data)
			err := s.Confirm(p.ID)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("fresh candidate refused: %v", err)
				}
				raw, _ := os.ReadFile(filepath.Join(vault, AionBacklogPath))
				if !strings.Contains(string(raw), "Capture this task") || len(s.List("approved")) != 1 || len(s.List("pending")) != 0 {
					t.Fatalf("apply did not land: approved=%d pending=%d\n%s", len(s.List("approved")), len(s.List("pending")), raw)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "replay=false") {
				t.Fatalf("hold: %v", err)
			}
			// a refusal changes nothing, and survives reopening the store
			for attempt := 0; attempt < 2; attempt++ {
				if !reflect.DeepEqual(before, extractionTree(t, vault)) || !reflect.DeepEqual(auditBefore, extractionTree(t, data)) {
					t.Fatal("refusal changed vault or audit")
				}
				if len(s.List("pending")) != 1 || len(s.List("approved")) != 0 {
					t.Fatal("refusal settled proposal")
				}
				s = NewStore(filepath.Dir(s.dir)).WithVaultRoot(vault).WithVaultWriter(s.vw).WithAionCapability("aion-approved")
				if err := s.Confirm(p.ID); err == nil {
					t.Fatal("held proposal applied after reopen")
				}
			}
		})
	}
}

// The multi-file contract lane stays held even when every dependency matches.
func TestExtractionContractLaneStaysHeld(t *testing.T) {
	vault := t.TempDir()
	s := NewStore(t.TempDir()).WithVaultRoot(vault)
	if err := os.WriteFile(filepath.Join(vault, "source.md"), []byte("email"), 0600); err != nil {
		t.Fatal(err)
	}
	p := Proposal{ID: "abcdef123456", Type: TypeReContract, Ritual: "ooda-email", Action: "re: contract — Bid", ApplyPath: "system/realestate/contracts/bid.md", Body: "evidence"}
	p.ExtractionSnapshot = EncodeExtractionSnapshot(map[string]string{"source.md": EvidenceHash("email")}, p)
	if hold := s.ExtractionHold(p); !strings.Contains(hold, "not yet transactional") {
		t.Fatalf("contract hold: %q", hold)
	}
	if _, err := s.ProposeOnce(p); err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "replay=false") {
		t.Fatal(err)
	}
	if len(s.List("pending")) != 1 || len(s.List("approved")) != 0 {
		t.Fatal("held contract settled")
	}
}

func extractionTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if d.IsDir() {
			out[rel+"/"] = "directory"
			return nil
		}
		b, err := os.ReadFile(name)
		if err == nil {
			out[rel] = EvidenceHash(string(b))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestExtractionSnapshotCannotDispatchOperation(t *testing.T) {
	s := NewStore(t.TempDir())
	called := false
	s.WithOperationDecision(func(string, string) error { called = true; return nil })
	p := Proposal{ID: "abcdef123456", Type: TypeManifestOperation, Action: "Review", ApplyPath: "operation-id", Body: "evidence"}
	p.ExtractionSnapshot = EncodeExtractionSnapshot(map[string]string{"source.md": EvidenceHash("source")}, p)
	if _, err := s.ProposeOnce(p); err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "unsupported proposal type") {
		t.Fatal(err)
	}
	if called || len(s.List("approved")) != 0 || len(s.List("pending")) != 1 {
		t.Fatal("snapshot bypassed gate through operation dispatcher")
	}
}

func TestExtractionDependencyDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, hash, want string
	}{
		{"source.md", "not-a-hash", "invalid dependency hash"},
		{"../source.md", EvidenceHash("source"), "invalid dependency path"},
		{"/source.md", EvidenceHash("source"), "invalid dependency path"},
		{"dir/../source.md", EvidenceHash("source"), "invalid dependency path"},
		{"dir\\source.md", EvidenceHash("source"), "invalid dependency path"},
		{"sha256:" + EvidenceHash("email"), EvidenceHash("email"), "cannot be revalidated"},
		{"portal-records", EvidenceHash("summary"), "cannot be revalidated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStore(t.TempDir()).WithVaultRoot(t.TempDir())
			for _, typ := range []string{TypeAionBacklog, TypeAionResolve, TypeAionHeuristic, TypeReBacklog, TypeReResolve, TypeReContract} {
				p := Proposal{Type: typ, Body: "evidence"}
				p.ExtractionSnapshot = EncodeExtractionSnapshot(map[string]string{tc.name: tc.hash}, p)
				if err := s.checkExtractionSnapshot(p); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("%s: %v", typ, err)
				}
			}
		})
	}
}
func TestExtractionReferences(t *testing.T) {
	records := map[string]string{
		"system/realestate/contractors/builder.md": "---\ncategories: [contractor]\n---\n",
		"system/realestate/properties/home.md":     "---\ncategories: [property]\n---\n## rocks\n- [ ] Renovation\n",
	}
	p := ReContractPayload{Kind: "bid", Contractor: "builder", Name: "Bid", Doc: "sha256:exact", Total: 10, Allocations: []ReContractAllocation{{Property: "home", Node: "renovation", Amount: 10}}}
	if err := ValidateExtractionContractReferences(p, records); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"missing-property", "ambiguous-property", "invalid-node", "invalid-contractor", "create"} {
		t.Run(bad, func(t *testing.T) {
			q := p
			q.Allocations = append([]ReContractAllocation(nil), p.Allocations...)
			r := map[string]string{}
			for k, v := range records {
				r[k] = v
			}
			switch bad {
			case "missing-property":
				q.Allocations[0].Property = "absent"
			case "ambiguous-property":
				r["elsewhere/home.md"] = r["system/realestate/properties/home.md"]
			case "invalid-node":
				q.Allocations[0].Node = "invented"
			case "invalid-contractor":
				q.Contractor = "../builder"
			case "create":
				q.ContractorCreate = "Guess"
			}
			if ValidateExtractionContractReferences(q, r) == nil {
				t.Fatal("accepted invalid reference")
			}
		})
	}
}

func TestExtractionPublicationDoesNotReuseDifferentSnapshot(t *testing.T) {
	s := NewStore(t.TempDir())
	p := Proposal{ID: "abcdef123456", Action: "Review", Body: "evidence"}
	if _, err := s.ProposeOnce(p); err != nil {
		t.Fatal(err)
	}
	p.ExtractionSnapshot = EncodeExtractionSnapshot(map[string]string{"source.md": EvidenceHash("source")}, p)
	if _, err := s.ProposeOnce(p); err == nil {
		t.Fatal("reused historical unguarded proposal")
	}
	if s.List("pending")[0].ExtractionSnapshot != "" {
		t.Fatal("rewrote historical proposal")
	}
}
