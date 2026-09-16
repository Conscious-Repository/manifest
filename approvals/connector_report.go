package approvals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"manifest/mdfm"
)

// EvidenceHash hashes even operator-controlled IDs and paths: neither is safe
// to print verbatim. Full SHA-256 avoids ambiguous truncated references.
func EvidenceHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

type ReconciliationIssue struct {
	Class        string             `json:"class"`
	Source       string             `json:"source"`
	IdentityHash string             `json:"identityHash,omitempty"`
	Count        int                `json:"count"`
	Proposals    []ProposalEvidence `json:"proposals,omitempty"`
}
type ProposalEvidence struct {
	IDHash           string `json:"idHash"`
	Status           string `json:"status"`
	ArtifactPathHash string `json:"artifactPathHash"`
	ApplyPathHash    string `json:"applyPathHash"`
	ContentHash      string `json:"contentHash"`
}
type InventoryReport struct {
	Hash   string                `json:"hash"`
	Files  int                   `json:"files"`
	Issues []ReconciliationIssue `json:"issues"`
}

// InspectConnectorInventory collects evidence without selecting a winning
// proposal, constructing a Store, or changing the strict activation inventory.
// Its hash uses the same ordered rows as ReadConnectorInventory on valid input.
func InspectConnectorInventory(artifacts string) InventoryReport {
	r := InventoryReport{Issues: []ReconciliationIssue{}}
	h := sha256.New()
	type member struct {
		p   ProposalEvidence
		typ string
	}
	groups := map[string][]member{}
	ids := map[string][]ProposalEvidence{}
	add := func(class, source, id string, ps []ProposalEvidence) {
		hash := ""
		if id != "" {
			hash = EvidenceHash(id)
		}
		n := len(ps)
		if n == 0 {
			n = 1
		}
		r.Issues = append(r.Issues, ReconciliationIssue{class, source, hash, n, ps})
	}
	for _, status := range statuses {
		entries, err := os.ReadDir(filepath.Join(artifacts, "approvals", status))
		if err != nil {
			add("inventory-unavailable", "unknown", "", nil)
			continue
		}
		for _, ent := range entries {
			if !strings.HasSuffix(ent.Name(), ".md") {
				continue
			}
			rel := filepath.Join(status, ent.Name())
			p := ProposalEvidence{Status: status, ArtifactPathHash: EvidenceHash(rel)}
			if !ent.Type().IsRegular() {
				add("inventory-unavailable", "unknown", "", []ProposalEvidence{p})
				continue
			}
			b, err := os.ReadFile(filepath.Join(artifacts, "approvals", rel))
			if err != nil {
				add("inventory-unavailable", "unknown", "", []ProposalEvidence{p})
				continue
			}
			r.Files++
			p.ContentHash = EvidenceHash(string(b))
			row, _ := json.Marshal([]string{status, ent.Name(), p.ContentHash})
			h.Write(row)
			fm, body := mdfm.Split(string(b))
			proposed, _ := mdfm.ExtractFencedBlock(body, "proposed")
			pf, _ := mdfm.Split(proposed)
			p.IDHash = EvidenceHash(fm["id"])
			p.ApplyPathHash = EvidenceHash(strings.TrimSpace(fm["apply-path"]))
			ids[fm["id"]] = append(ids[fm["id"]], p)
			if fm["id"] == "" || fm["id"]+".md" != ent.Name() {
				add("invalid-proposal-identity", "unknown", "", []ProposalEvidence{p})
			}
			found := false
			for _, source := range []string{"granola", "pocket", "gmail-thread"} {
				values := []string{pf[source+"-id"], pf[source+"_id"], fm[source+"-id"], fm[source+"_id"]}
				sid := ""
				conflict := false
				for _, v := range values {
					if v != "" {
						if sid != "" && sid != v {
							conflict = true
						}
						sid = v
					}
				}
				if sid == "" {
					continue
				}
				found = true
				if conflict || malformedIdentity(sid) || repeatedAlias(proposed, source) || repeatedAlias(string(b), source) {
					add("malformed-conflicting-aliases", source, sid, []ProposalEvidence{p})
				}
				typ := fm["type"]
				if typ != TypeCreateVaultNote && !(source == "gmail-thread" && typ == TypeAppendVaultNote) {
					add("invalid-connector-type", source, sid, []ProposalEvidence{p})
				}
				groups[source+"\x00"+sid] = append(groups[source+"\x00"+sid], member{p, typ})
			}
			if !found {
				source := "unknown"
				switch fm["ritual"] {
				case "granola-sync":
					source = "granola"
				case "pocket-sync":
					source = "pocket"
				case "email-sync":
					source = "gmail-thread"
				}
				// Unattributed creates also require review: ritual metadata may be absent.
				if source != "unknown" || fm["type"] == TypeCreateVaultNote || fm["type"] == TypeAppendVaultNote {
					add("missing-source-identity", source, "", []ProposalEvidence{p})
				}
			}
		}
	}
	for id, ps := range ids {
		if len(ps) > 1 {
			add("invalid-proposal-identity", "unknown", id, ps)
		}
	}
	for key, ms := range groups {
		parts := strings.SplitN(key, "\x00", 2)
		creates, appends, rejected := 0, 0, false
		ps := []ProposalEvidence{}
		for _, m := range ms {
			ps = append(ps, m.p)
			if m.typ == TypeCreateVaultNote {
				creates++
			}
			if m.typ == TypeAppendVaultNote {
				appends++
			}
			rejected = rejected || m.p.Status == "rejected"
		}
		if creates > 1 {
			add("duplicate-create-identity", parts[0], parts[1], ps)
		}
		if creates > 0 && appends > 0 {
			add("create-append-lineage", parts[0], parts[1], ps)
		}
		if len(ms) > 1 && rejected {
			add("rejected-duplicates", parts[0], parts[1], ps)
		}
	}
	r.Hash = hex.EncodeToString(h.Sum(nil))
	sort.Slice(r.Issues, func(i, j int) bool {
		a, _ := json.Marshal(r.Issues[i])
		b, _ := json.Marshal(r.Issues[j])
		return string(a) < string(b)
	})
	return r
}
func malformedIdentity(s string) bool {
	return len(s) >= 512 || strings.ContainsAny(s, " \t\r\n/\\`:\"'[]{}")
}
func repeatedAlias(raw, source string) bool {
	// Inspect only frontmatter; Split intentionally has last-key-wins semantics.
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, _, ok := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		if ok && (key == source+"-id" || key == source+"_id") {
			if seen[key] {
				return true
			}
			seen[key] = true
		}
	}
	return false
}
