package transcriptsync

import (
	"fmt"

	"manifest/approvals"
	"manifest/connectorhandoff"
)

// OwnershipSnapshot validates local continuity using the configured service.
// Unlike enterSuccessor it creates no fence lock or operational files and does
// not contact upstreams. Recorded outcomes are not proof of current liveness.
func (s *Service) OwnershipSnapshot(dataDir, root, source string, f connectorhandoff.RecordFence, r connectorhandoff.Record) (State, bool, error) {
	if s == nil || s.handoffDataDir != dataDir || s.cfg.LegacyRoot != root {
		return State{}, false, fmt.Errorf("matching guarded transcript service required")
	}
	c, err := s.config(source)
	if err != nil {
		return State{}, false, err
	}
	st, err := s.Status(source)
	if err != nil {
		return State{}, false, err
	}
	if r.Validate() != nil || r.Source != source || (r.Phase != connectorhandoff.Enabled && r.Phase != connectorhandoff.Verified) || f.Duty != "ea-coordinator/"+source+"-sync" || f.Revision == 0 || f.Owner != "manifest" || f.Evidence != r.Evidence["dispatch-exclusion"] || r.Evidence["account-binding"] != approvals.EvidenceHash(c.Account) || st.ImportedFrom != r.CheckpointHash {
		return State{}, false, fmt.Errorf("successor ownership, account or checkpoint mismatch")
	}
	return st, c.Enabled, nil
}
