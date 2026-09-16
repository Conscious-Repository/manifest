package personalemail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"manifest/approvals"
	"manifest/connectorhandoff"
)

// OwnershipSnapshot checks the deployed worker's durable activation against the
// same history, config, receipt and account binding used by Poll. It is read-only;
// enabled describes configuration, not worker liveness or semantic parity.
func OwnershipSnapshot(dataDir, root string) (f connectorhandoff.RecordFence, enabled bool, err error) {
	f, err = connectorhandoff.FenceSnapshot(root, "email")
	if err != nil {
		return connectorhandoff.RecordFence{}, false, fmt.Errorf("email fence unreadable")
	}
	if dataDir == "" {
		return f, false, fmt.Errorf("email successor data directory unavailable")
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "personal-email-worker.json"))
	if err != nil {
		return f, false, fmt.Errorf("email worker configuration unavailable")
	}
	var c Config
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || c.DataDir != dataDir || c.LegacyRoot != root {
		return f, false, fmt.Errorf("email worker configuration does not match projection paths")
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return f, false, fmt.Errorf("invalid email worker configuration")
	}
	s := New(c, nil, nil)
	if err := s.verifyOwnership(f); err != nil {
		return f, false, err
	}
	return f, c.Enabled, nil
}

func (s *Service) verifyOwnership(f connectorhandoff.RecordFence) error {
	binding, err := s.validate()
	if err != nil {
		return fmt.Errorf("email account binding unavailable or contradictory")
	}
	st, err := s.read()
	if err != nil {
		return fmt.Errorf("email activation unavailable or invalid")
	}
	return s.matchOwnership(f, st, binding)
}

func (s *Service) matchOwnership(f connectorhandoff.RecordFence, st State, binding string) error {
	if f.Owner != "manifest" || f.Revision != st.Revision || f.Evidence != st.PlanHash || st.BindingHash != approvals.EvidenceHash(binding+"\x00"+s.Config.Account) {
		return fmt.Errorf("email ownership/activation mismatch")
	}
	return nil
}
