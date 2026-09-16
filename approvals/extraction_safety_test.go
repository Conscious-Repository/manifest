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

func TestExtractionConfirmFailsClosed(t *testing.T) {
	for _, drift := range []string{"none", "source", "context", "proposal", "replay"} {
		t.Run(drift, func(t *testing.T) {
			vault := t.TempDir()
			s := NewStore(t.TempDir())
			s.vaultRoot = vault
			files := map[string]string{"source.md": "original category and source", "context.md": "original context"}
			hashes := map[string]string{}
			for name, raw := range files {
				if err := os.WriteFile(filepath.Join(vault, name), []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
				hashes[name] = EvidenceHash(raw)
			}
			p := Proposal{ID: "abcdef123456abcdef123456", Action: "Review extraction", Agent: "extractor", Ritual: "aion", Type: TypeAionBacklog, ApplyPath: AionBacklogPath, Body: "exact provenance"}
			p.ExtractionSnapshot = EncodeExtractionSnapshot(hashes, p)
			if drift == "source" || drift == "context" {
				os.WriteFile(filepath.Join(vault, drift+".md"), []byte("changed"), 0600)
			}
			if drift == "proposal" {
				p.Body += " edited"
			}
			if drift == "replay" {
				b, _ := base64.RawURLEncoding.DecodeString(p.ExtractionSnapshot)
				var snap ExtractionSnapshot
				json.Unmarshal(b, &snap)
				snap.Replay = true
				b, _ = json.Marshal(snap)
				p.ExtractionSnapshot = base64.RawURLEncoding.EncodeToString(b)
			}
			if _, err := s.ProposeOnce(p); err != nil {
				t.Fatal(err)
			}
			if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "replay=false") {
				t.Fatal(err)
			}
			pending := s.List("pending")
			if len(pending) != 1 || pending[0].ExtractionSnapshot != p.ExtractionSnapshot {
				t.Fatal(pending)
			}
			if len(s.List("approved")) != 0 {
				t.Fatal("approved blocked proposal")
			}
			if _, err := os.Stat(filepath.Join(vault, AionBacklogPath)); !os.IsNotExist(err) {
				t.Fatal("vault mutated")
			}
		})
	}
}

// Pin the bounded hold with a fully configured writer: absence of a writer
// must not be what prevents a mutation. Snapshot the entire tree, including
// sidecars and audit files, rather than checking only the expected target.
func TestExtractionHoldWithWriterAndRestart(t *testing.T) {
	for _, scenario := range []string{"unchanged", "source", "context", "category", "files-index", "missing-property", "missing-contract", "added-ambiguous-property", "added-contract", "audit-unavailable", "artifact", "portal", "stale-approval"} {
		t.Run(scenario, func(t *testing.T) {
			s, vault, data := aionTestStore(t)
			files := map[string]string{
				"source.md":                            "---\ncategories: [aion]\n---\nOwner said capture this task.\n",
				"context.md":                           "context",
				"system/realestate/properties/home.md": "---\ncategories: [property]\n---\n",
				"system/realestate/contracts/bid.md":   "---\ncategories: [contract]\n---\n",
				"system/realestate/files/files.json":   "{}\n",
			}
			write := func(name, raw string) {
				t.Helper()
				full := filepath.Join(vault, name)
				if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
			}
			hashes := map[string]string{}
			for name, raw := range files {
				write(name, raw)
				hashes[name] = EvidenceHash(raw)
			}
			p := aionProposal(aion.ProposalPayload{Kind: "task", Title: "Capture this task", Status: "open", Sources: []string{"source.md"}, Captured: "2026-09-16", Quote: "capture this task"})
			p.ID = "abcdef123456"
			switch scenario {
			case "source", "context":
				write(scenario+".md", "changed")
			case "category":
				write("source.md", strings.ReplaceAll(files["source.md"], "[aion]", "[real-estate]"))
			case "files-index":
				write("system/realestate/files/files.json", "{\"changed\":true}\n")
			case "missing-property", "missing-contract":
				name := "system/realestate/properties/home.md"
				if scenario == "missing-contract" {
					name = "system/realestate/contracts/bid.md"
				}
				if err := os.Remove(filepath.Join(vault, name)); err != nil {
					t.Fatal(err)
				}
			case "added-ambiguous-property":
				write("elsewhere/home.md", files["system/realestate/properties/home.md"])
			case "added-contract":
				write("system/realestate/contracts/bid-2.md", files["system/realestate/contracts/bid.md"])
			case "audit-unavailable":
				if err := os.Mkdir(filepath.Join(data, "write-audit.log"), 0700); err != nil {
					t.Fatal(err)
				}
			case "artifact":
				hashes["sha256:"+EvidenceHash("email")] = EvidenceHash("email")
			case "portal":
				hashes["portal-records"] = EvidenceHash("portal summary")
			}
			p.ExtractionSnapshot = EncodeExtractionSnapshot(hashes, p)
			if scenario == "stale-approval" {
				p.Body += "\nchanged after review"
			}
			if _, err := s.ProposeOnce(p); err != nil {
				t.Fatal(err)
			}
			before, auditBefore := extractionTree(t, vault), extractionTree(t, data)
			for attempt := 0; attempt < 3; attempt++ {
				if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "replay=false") {
					t.Fatalf("confirm: %v", err)
				}
				if !reflect.DeepEqual(before, extractionTree(t, vault)) || !reflect.DeepEqual(auditBefore, extractionTree(t, data)) {
					t.Fatal("refusal changed vault or audit")
				}
				if len(s.List("pending")) != 1 || len(s.List("approved")) != 0 {
					t.Fatal("refusal settled proposal")
				}
				if _, err := s.ProposeOnce(p); err != nil {
					t.Fatal(err)
				}
				// Simulate reopening after interruption without refreshing evidence.
				s = NewStore(filepath.Dir(s.dir)).WithVaultRoot(vault).WithVaultWriter(s.vw).WithAionCapability("aion-approved")
			}
		})
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
