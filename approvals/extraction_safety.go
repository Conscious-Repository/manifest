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

// ExtractionHold says why a snapshot-bearing proposal cannot be applied right
// now ("" = it can). Pure: no journal, no writes — the approval card shows it
// instead of a blanket banner, and Confirm journals it when it refuses.
//
// The rule (2026-09-23, replacing the 2026-09-16 blanket hold that kept even
// a fresh, matching proposal pending forever): a source document must still
// carry the exact bytes the candidate was extracted from — that is what the
// quote and the owner's review were made against; a system record the
// extraction read for context (backlog, heuristics, people) must still exist
// but may have moved on, because a backlog append does not depend on the
// backlog's bytes and a resolve refuses on its own when its title is gone.
// Single-file appends and resolves apply under that rule. The multi-file
// contract lane stays held, and artifact/portal dependencies this snapshot
// version cannot revalidate stay held. An owner edit through the card no
// longer invalidates the evidence: the edit is the review.
func (s *Store) ExtractionHold(p Proposal) string {
	if p.ExtractionSnapshot == "" {
		return ""
	}
	switch p.Type {
	case TypeAionBacklog, TypeAionResolve, TypeAionHeuristic, TypeReBacklog, TypeReResolve, TypeReContract:
	default:
		return "snapshot on unsupported proposal type"
	}
	b, err := base64.RawURLEncoding.DecodeString(p.ExtractionSnapshot)
	var snap ExtractionSnapshot
	if err != nil || json.Unmarshal(b, &snap) != nil || snap.Version != 1 || snap.Replay || len(snap.Files) == 0 {
		return "invalid snapshot"
	}
	for name, hash := range snap.Files {
		if len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
			return "invalid dependency hash"
		}
		if strings.HasPrefix(name, "sha256:") || name == "portal-records" {
			return "artifact or portal dependency cannot be revalidated by this snapshot version"
		}
		if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") {
			return "invalid dependency path"
		}
	}
	root, err := os.OpenRoot(s.vaultRoot)
	if err != nil {
		return "vault unavailable"
	}
	defer root.Close()
	for name, hash := range snap.Files {
		raw, err := root.ReadFile(name)
		if err != nil {
			return "source or context missing: " + name
		}
		if strings.HasPrefix(name, "system/") {
			continue // context may move on; the apply lane judges the live record
		}
		if EvidenceHash(string(raw)) != hash {
			return "source changed since extraction: " + name + " — reject and re-run the extraction"
		}
	}
	if p.Type == TypeReContract {
		return "contract intake writes several records and is not yet transactional — held; reject or leave pending"
	}
	return ""
}

func (s *Store) checkExtractionSnapshot(p Proposal) error {
	if p.ExtractionSnapshot == "" {
		return nil
	}
	reason := s.ExtractionHold(p)
	if reason == "" {
		return nil
	}
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
