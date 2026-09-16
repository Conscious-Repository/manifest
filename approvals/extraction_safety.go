package approvals

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"manifest/mdfm"
	"manifest/realestate"
)

// ExtractionSnapshot binds the reviewed proposal to exact source/context bytes.
// Replay is deliberately false; a stale proposal requires reconciliation.
type ExtractionSnapshot struct {
	Version  int               `json:"version"`
	Replay   bool              `json:"replay"`
	Files    map[string]string `json:"files"`
	Proposal string            `json:"proposal"`
}

func extractionProposalHash(p Proposal) string {
	return EvidenceHash(p.Type + "\n" + p.Ritual + "\n" + p.ApplyPath + "\n" + strings.TrimSpace(p.Body))
}
func EncodeExtractionSnapshot(files map[string]string, p Proposal) string {
	b, _ := json.Marshal(ExtractionSnapshot{Version: 1, Files: files, Proposal: extractionProposalHash(p)})
	return base64.RawURLEncoding.EncodeToString(b)
}
func (s *Store) checkExtractionSnapshot(p Proposal) error {
	if p.ExtractionSnapshot == "" {
		return nil
	}
	blocked := func(reason string) error {
		status := "journal unavailable (not configured)"
		if s.extractionDataDir != "" {
			receipt, err := s.journalExtractionRefusal(p, reason)
			if err != nil {
				status = "journal unavailable: " + err.Error()
			} else {
				status = receipt
			}
		}
		return fmt.Errorf("extraction uncertain/stale: %s; %s; %s; pending, replay=false", reason, ExtractionCommitUnavailable, status)
	}
	switch p.Type {
	case TypeAionBacklog, TypeAionResolve, TypeAionHeuristic, TypeReBacklog, TypeReResolve, TypeReContract:
	default:
		return blocked("snapshot on unsupported proposal type")
	}
	b, err := base64.RawURLEncoding.DecodeString(p.ExtractionSnapshot)
	var snap ExtractionSnapshot
	if err != nil || json.Unmarshal(b, &snap) != nil || snap.Version != 1 || snap.Replay || len(snap.Files) == 0 || snap.Proposal != extractionProposalHash(p) {
		return blocked("invalid or edited snapshot")
	}
	// This is diagnostic validation only. V1 cannot express absence predicates,
	// a category namespace revision, or artifact store identity. Never interpret
	// matching declared files as evidence that the complete read set is stable.
	for name, hash := range snap.Files {
		if len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
			return blocked("invalid dependency hash")
		}
		if strings.HasPrefix(name, "sha256:") || name == "portal-records" {
			return blocked("artifact or portal dependency cannot be revalidated by this snapshot version")
		}
		if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") {
			return blocked("invalid dependency path")
		}
	}
	root, err := os.OpenRoot(s.vaultRoot)
	if err != nil {
		return blocked("vault unavailable")
	}
	defer root.Close()
	for name, hash := range snap.Files {
		raw, err := root.ReadFile(name)
		if err != nil || EvidenceHash(string(raw)) != hash {
			return blocked("source or context changed")
		}
	}
	// The existing applies perform independent reads/writes, and contract apply
	// may mutate several files. A preflight is not an atomic compare-and-swap.
	// Until the writer can commit the complete read/write set, refuse even a
	// matching snapshot. In particular, never turn this check into a retry.
	return blocked("atomic dependency CAS and artifact revalidation not implemented (complete dependency manifest and write/audit/decision transaction unavailable; external editors do not honor application locks)")
}

// ValidateExtractionContractReferences accepts only exact canonical slugs and
// node IDs. Creating contractors/tree nodes remains blocked in this phase.
func ValidateExtractionContractReferences(p ReContractPayload, records map[string]string) error {
	fail := func() error { return fmt.Errorf("uncertain domain references; pending reconciliation") }
	if p.Validate() != nil || p.ContractorCreate != "" || len(p.NewMilestones) > 0 || len(p.Tasks) > 0 {
		return fail()
	}
	canonical := func(kind, slug string) (string, bool) {
		if slug == "" || path.Base(slug) != slug || slug == "." || slug == ".." {
			return "", false
		}
		folder := kind + "s"
		if kind == "property" {
			folder = "properties"
		}
		expected := "system/realestate/" + folder + "/" + slug + ".md"
		raw, ok := records[expected]
		if !ok {
			return "", false
		}
		fm, _ := mdfm.Split(raw)
		if !slices.Contains(mdfm.List(fm["categories"]), kind) {
			return "", false
		}
		count := 0
		for name, body := range records {
			f, _ := mdfm.Split(body)
			if strings.EqualFold(strings.TrimSuffix(path.Base(name), ".md"), slug) && slices.Contains(mdfm.List(f["categories"]), kind) {
				count++
			}
		}
		return raw, count == 1
	}
	if _, ok := canonical("contractor", p.Contractor); !ok {
		return fail()
	}
	for _, a := range p.Allocations {
		raw, ok := canonical("property", a.Property)
		if !ok {
			return fail()
		}
		_, body := mdfm.Split(raw)
		_, lines := rockSection(body)
		stages := realestate.ParseWork(lines)
		count := 0
		var walk func([]*realestate.WorkNode)
		walk = func(nodes []*realestate.WorkNode) {
			for _, n := range nodes {
				if n.ID == a.Node {
					count++
				}
				walk(n.Children)
			}
		}
		for _, stage := range stages {
			if stage.ID == a.Node {
				count++
			}
			walk(stage.Tasks)
		}
		if count != 1 {
			return fail()
		}
	}
	return nil
}
