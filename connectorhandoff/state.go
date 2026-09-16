// Package connectorhandoff owns portable, credential-free migration evidence.
// It does not call connectors, decide approvals, or write the vault.
package connectorhandoff

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Phase string

const (
	NotReady        Phase = "not-ready"
	CheckpointReady Phase = "checkpoint-ready"
	Paused          Phase = "paused-awaiting-reconciliation"
	Enabled         Phase = "successor-enabled"
	Verified        Phase = "verified"
	Blocked         Phase = "blocked"
)

// Record is separate from a connector cursor: disabling config cannot surrender
// ownership. Rollback requires reconciliation, never implicit legacy fallback.
type Record struct {
	Version        int    `json:"version"`
	Source         string `json:"source"`
	Phase          Phase  `json:"phase"`
	Owner          string `json:"owner"`
	RollbackOwner  string `json:"rollbackOwner"`
	CheckpointHash string `json:"checkpointHash"`
	ApprovalHash   string `json:"approvalHash"`
	// Evidence contains hashes of operator-reviewed receipts, not flag values.
	Evidence map[string]string `json:"evidence,omitempty"`
}

func validSource(s string) bool { return s == "email" || s == "granola" || s == "pocket" }
func ValidHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (r Record) Validate() error {
	if r.Version != 1 || !validSource(r.Source) || r.RollbackOwner != "excalibur" {
		return fmt.Errorf("invalid connector migration record")
	}
	switch r.Phase {
	case NotReady, CheckpointReady, Paused, Blocked:
		if r.Owner != "excalibur" {
			return fmt.Errorf("invalid checkpoint owner")
		}
	case Enabled, Verified:
		if r.Owner != "manifest" {
			return fmt.Errorf("invalid successor owner")
		}
		for _, k := range []string{"dispatch-exclusion", "account-binding", "source-reconciliation"} {
			if !ValidHash(r.Evidence[k]) {
				return fmt.Errorf("missing handoff evidence: %s", k)
			}
		}
		if r.Phase == Verified {
			for _, k := range []string{"successor-run", "restart-no-change", "approval-vaultwriter"} {
				if !ValidHash(r.Evidence[k]) {
					return fmt.Errorf("missing verification evidence: %s", k)
				}
			}
		}
	default:
		return fmt.Errorf("invalid migration phase")
	}
	if r.Phase != NotReady && r.Phase != Blocked && (!ValidHash(r.CheckpointHash) || !ValidHash(r.ApprovalHash)) {
		return fmt.Errorf("missing checkpoint hashes")
	}
	return nil
}
func Read(dataDir, source string) (Record, error) {
	if !validSource(source) {
		return Record{}, fmt.Errorf("invalid connector source")
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "connector-handoff", source+".json"))
	if err != nil {
		return Record{}, err
	}
	var r Record
	if uniqueJSON(b) != nil || json.Unmarshal(b, &r) != nil || r.Source != source {
		return Record{}, fmt.Errorf("invalid migration record")
	}
	return r, r.Validate()
}

// LegacyAllowed fails closed on corrupt evidence; absent evidence retains the
// still-live legacy duty. Pausing is explicit and survives successor flag changes.
func LegacyAllowed(dataDir, source string) error {
	r, err := Read(dataDir, source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("connector migration evidence unreadable; legacy dispatch refused")
	}
	if r.Phase == Paused || r.Phase == Enabled || r.Phase == Verified || r.Phase == Blocked {
		return fmt.Errorf("connector migration %s; legacy dispatch refused", r.Phase)
	}
	return nil
}
