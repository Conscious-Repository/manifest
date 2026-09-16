package approvals

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"manifest/mdfm"
)

const PersonalEmailRitual = "personal-email-sync"

// ProposeEmail publishes through the canonical inbox, never applies a note.
// The worker persists a no-replay intent before calling this seam. Existing
// historical claims remain intact, including conflicting quarantined creates.
func (s *Store) ProposeEmail(p Proposal) (Proposal, error) {
	s.decisionMu.Lock()
	defer s.decisionMu.Unlock()
	release, err := s.decisionFence()
	if err != nil {
		return Proposal{}, err
	}
	defer release()
	if p.ID == "" || len(p.ID) > 64 || strings.Trim(p.ID, "0123456789abcdef") != "" || p.GmailThreadID == "" || p.Ritual != PersonalEmailRitual {
		return Proposal{}, fmt.Errorf("email proposal identity required")
	}
	switch p.Type {
	case TypeCreateVaultNote:
		fm, _ := mdfm.Split(p.Proposed)
		if !CreateVaultNotePathAllowed(p.ApplyPath) || fm["gmail-thread-id"] != p.GmailThreadID {
			return Proposal{}, fmt.Errorf("invalid email create")
		}
	case TypeAppendVaultNote:
		if !AppendVaultNotePathAllowed(p.ApplyPath) || strings.HasPrefix(strings.TrimSpace(p.Proposed), "---") {
			return Proposal{}, fmt.Errorf("invalid email append")
		}
	default:
		return Proposal{}, fmt.Errorf("invalid email proposal type")
	}
	inv, err := s.connectorInventoryMode("gmail-thread", false, true)
	if err != nil {
		return Proposal{}, err
	}
	for _, old := range inv.Items {
		if old.ID == p.ID || (p.Type == TypeCreateVaultNote && (old.SourceID == p.GmailThreadID || strings.EqualFold(old.Path, p.ApplyPath))) {
			return Proposal{}, fmt.Errorf("existing email claim; reconcile without replay")
		}
	}
	fence := "````"
	for strings.Contains(p.Proposed, fence) {
		fence += "`"
	}
	p.Body = strings.TrimSpace(p.Body) + "\n\n" + fence + "proposed\n" + p.Proposed + "\n" + fence
	result, err := s.propose(p)
	if err != nil {
		return result, err
	}
	d, err := os.Open(filepath.Join(s.dir, "pending"))
	if err != nil {
		return result, err
	}
	defer d.Close()
	return result, d.Sync()
}

// claimPersonalEmailEffect is a permanent no-replay intent in the canonical
// approval path. It is synced before any vault mutation, and retained after
// both success and failure. It contains only identity/evidence hashes. A crash
// may require manual reconciliation, but cannot cause an automatic replay.
func (s *Store) claimPersonalEmailEffect(p Proposal, lane bool) error {
	if p.Ritual != PersonalEmailRitual {
		return nil
	}
	if !lane {
		return fmt.Errorf("personal email requires the canonical vaultwriter capability")
	}
	if p.ID == "" || len(p.ID) > 64 || strings.Trim(p.ID, "0123456789abcdef") != "" {
		return fmt.Errorf("invalid email effect identity")
	}
	dir := filepath.Join(s.dir, "email-effects")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	parent, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	err = parent.Sync()
	parent.Close()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, p.ID+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("email effect claimed or unavailable; reconcile without replay: %w", err)
	}
	b, _ := json.Marshal(map[string]any{"proposalId": p.ID, "threadId": p.GmailThreadID, "evidenceHash": EvidenceHash(serialize(p)), "replay": false})
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// OpenEmailStore opens an existing canonical inbox without creating folders or
// configuring a vault writer. Used by the standalone proposal-only worker.
func OpenEmailStore(artifacts string) (*Store, error) {
	dir := filepath.Join(artifacts, "approvals")
	for _, status := range statuses {
		info, err := os.Stat(filepath.Join(dir, status))
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("canonical approval directory required")
		}
	}
	return &Store{dir: dir, root: filepath.Dir(artifacts)}, nil
}
