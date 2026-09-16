package approvals

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
