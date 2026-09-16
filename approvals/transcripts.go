package approvals

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"manifest/mdfm"
)

// ProposeTranscript reconciles source identity across all decisions while holding
// the same lock as Confirm/Reject. Legacy IDs, edited titles and decisions survive
// migration. An unreadable inbox is an error, never an empty inventory.
func (s *Store) ProposeTranscript(source, id string, p Proposal) (Proposal, bool, error) {
	if (source != "granola" && source != "pocket") || strings.TrimSpace(id) == "" {
		return Proposal{}, false, fmt.Errorf("invalid transcript source")
	}
	if p.Type != TypeCreateVaultNote || !CreateVaultNotePathAllowed(p.ApplyPath) {
		return Proposal{}, false, fmt.Errorf("invalid transcript proposal")
	}
	fm, _ := mdfm.Split(p.Proposed)
	if fm[source+"-id"] != id {
		return Proposal{}, false, fmt.Errorf("transcript identity mismatch")
	}
	s.decisionMu.Lock()
	defer s.decisionMu.Unlock()
	inv, err := s.connectorInventory()
	if err != nil {
		return Proposal{}, false, err
	}
	var match *ConnectorApproval
	for _, old := range inv.Items {
		if old.Source == source && old.SourceID == id {
			copy := old
			match = &copy
			continue
		}
		if strings.EqualFold(old.Path, p.ApplyPath) {
			return Proposal{}, false, fmt.Errorf("transcript filename conflicts with an existing proposal")
		}
	}
	if match != nil {
		old, err := s.parse(filepath.Join(s.dir, match.Status, match.ID+".md"))
		old.Status = match.Status
		return old, false, err
	}
	// A fence longer than any backtick run prevents transcript text from ending it.
	fence := "````"
	for strings.Contains(p.Proposed, fence) {
		fence += "`"
	}
	p.Body = strings.TrimSpace(p.Body) + "\n\n" + fence + "proposed\n" + p.Proposed + "\n" + fence
	result, err := s.Propose(p)
	if err == nil {
		d, e := os.Open(filepath.Join(s.dir, "pending"))
		if e != nil {
			return Proposal{}, false, e
		}
		err = d.Sync()
		d.Close()
	}
	return result, err == nil, err
}
