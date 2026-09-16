package transcriptsync

import (
	"fmt"
	"path/filepath"
	"reflect"

	"manifest/approvals"
)

// ReconcileCheckpoint reconstructs persisted outcomes from the canonical inbox
// and a frozen source-ID index. It performs no writes and preserves legacy IDs.
// The returned state is staged evidence, NOT permission to poll: historical
// source coverage, account binding and dispatch exclusion still need proof.
func (s *Service) ReconcileCheckpoint(source, legacyRoot string) (State, string, error) {
	st, err := s.Import(source, legacyRoot, "", false)
	if err != nil {
		return st, "", err
	}
	inv, owner, err := approvals.ReadReconciledConnectorInventory(filepath.Join(legacyRoot, "artifacts"), source, filepath.Dir(s.dir))
	if err != nil {
		return st, "", err
	}
	if s.idx == nil || s.idx.db == nil {
		return st, inv.Hash, fmt.Errorf("frozen source index required")
	}
	column := "granola_id"
	if source == "pocket" {
		column = "pocket_id"
	}
	rows, err := s.idx.db.Query("SELECT " + column + ", path FROM notes WHERE " + column + " IS NOT NULL AND " + column + " != ''")
	if err != nil {
		return st, inv.Hash, fmt.Errorf("source index unavailable")
	}
	notes := map[string]string{}
	for rows.Next() {
		var id, path string
		if err = rows.Scan(&id, &path); err != nil {
			break
		}
		if !validID(id) || path == "" || notes[id] != "" {
			err = fmt.Errorf("duplicate or invalid vault source identity [REDACTED]")
			break
		}
		notes[id] = path
	}
	scanErr := rows.Err()
	rows.Close()
	if err != nil {
		return st, inv.Hash, err
	}
	if scanErr != nil {
		return st, inv.Hash, fmt.Errorf("source index unreadable")
	}
	for _, p := range inv.Items {
		if p.Source != source {
			continue
		}
		if !validID(p.SourceID) {
			return st, inv.Hash, fmt.Errorf("invalid approval source identity")
		}
		hasNote := notes[p.SourceID] != ""
		if owner != nil && p.SourceID == owner.SourceID {
			if hasNote {
				return st, inv.Hash, fmt.Errorf("reconciled uncertain source now has a note; new owner review required")
			}
			st.Items[p.SourceID] = Outcome{ProposalID: p.ID, Disposition: owner.Disposition, Replay: false}
			continue
		}
		if (p.Status == "approved") != hasNote {
			return st, inv.Hash, fmt.Errorf("uncertain approval/vault outcome; owner reconciliation required [REDACTED]")
		}
		st.Items[p.SourceID] = Outcome{ProposalID: p.ID, Disposition: p.Status}
	}
	for id := range notes {
		if _, ok := st.Items[id]; !ok {
			st.Items[id] = Outcome{Disposition: "existing-note"}
		}
	}
	again, latestOwner, err := approvals.ReadReconciledConnectorInventory(filepath.Join(legacyRoot, "artifacts"), source, filepath.Dir(s.dir))
	if err != nil || again.Hash != inv.Hash || !reflect.DeepEqual(owner, latestOwner) {
		return State{}, "", fmt.Errorf("canonical approvals changed during checkpoint")
	}
	latest, err := s.Import(source, legacyRoot, "", false)
	if err != nil || latest.ImportedFrom != st.ImportedFrom {
		return State{}, "", fmt.Errorf("legacy checkpoint changed during reconciliation")
	}
	return st, inv.Hash, nil
}
