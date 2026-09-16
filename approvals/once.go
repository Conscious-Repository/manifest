package approvals

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProposeOnce preserves a prior decision for a caller-supplied stable identity.
func (s *Store) ProposeOnce(p Proposal) (Proposal, error) {
	if p.ID == "" || len(p.ID) > 64 || strings.Trim(p.ID, "0123456789abcdef") != "" {
		return Proposal{}, fmt.Errorf("invalid stable proposal identity")
	}
	s.decisionMu.Lock()
	defer s.decisionMu.Unlock()
	for _, status := range statuses {
		if _, e := os.ReadDir(filepath.Join(s.dir, status)); e != nil {
			return Proposal{}, e
		}
		path := filepath.Join(s.dir, status, p.ID+".md")
		if _, e := os.Stat(path); e == nil {
			old, e := s.parse(path)
			old.Status = status
			return old, e
		} else if !os.IsNotExist(e) {
			return Proposal{}, e
		}
	}
	return s.Propose(p)
}
