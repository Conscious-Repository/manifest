package spirits

import (
	"errors"
	"os"

	"manifest/connectorhandoff"
	"manifest/transcriptsync"
)

func (s *Store) WithTranscriptSync(svc *transcriptsync.Service) *Store {
	s.transcriptSync = svc
	return s
}

func (s *Store) projectTranscriptOwnership(r *RitualRow, source string) {
	f, fenceErr := connectorhandoff.DutyFenceSnapshot(s.root, r.Spirit+"/"+r.Ritual)
	r.FenceProtected = fenceErr == nil && f.Revision > 0 && f.Owner != connectorhandoff.Legacy
	record, err := connectorhandoff.Read(s.migrationDataDir, source)
	if fenceErr == nil && !r.FenceProtected && errors.Is(err, os.ErrNotExist) && r.ConfiguredOwner == "" {
		r.ConfiguredOwner = connectorhandoff.Legacy
		r.MigrationState = string(connectorhandoff.NotReady)
		r.MigrationDetail = "Legacy connector remains owner; reconciled checkpoint and enforceable dispatch exclusion required."
		return
	}
	r.LegacyActionable = false
	r.MigrationState = "blocked"
	r.MigrationDetail = "Transcript ownership uncertain: matching duty fence, handoff and successor continuity state required."
	if err == nil {
		r.ConfiguredOwner = record.Owner
	}
	if fenceErr == nil && f.Revision > 0 {
		r.ConfiguredOwner = f.Owner
	}
	if fenceErr != nil || err != nil {
		return
	}
	// Pre-cutover phases retain their meaning only while the fence agrees.
	if record.Owner == connectorhandoff.Legacy && !r.FenceProtected {
		r.MigrationState = string(record.Phase)
		r.LegacyActionable = r.Valid && !r.Retired && s.dutyOwners[r.Spirit+"/"+r.Ritual] == "" && s.connectorDispatchGuard(r.Spirit, r.Ritual) == nil
		return
	}
	st, enabled, err := s.transcriptSync.OwnershipSnapshot(s.migrationDataDir, s.root, source, f, record)
	if err != nil {
		r.MigrationDetail += " " + err.Error() + "."
		return
	}
	r.SuccessorEnabled = enabled
	r.SuccessorHealth = "unknown"
	r.SuccessorLastAttempt = st.LastAttempt
	r.SuccessorLastSuccess = st.LastSuccess
	r.MigrationState = "fence-protected"
	if st.Error != "" {
		r.SuccessorHealth = "error"
		if enabled {
			r.MigrationState = "blocked"
		}
	} else if !st.LastSuccess.IsZero() && !st.LastSuccess.Before(st.LastAttempt) {
		r.SuccessorHealth = "last-success"
		if enabled {
			r.MigrationState = "successor-enabled"
		}
	}
	r.MigrationDetail = "Validated transcript handoff, duty fence and successor checkpoint/account binding; legacy dispatch fenced."
	if r.LegacyEnabled {
		r.MigrationDetail += " Legacy schedule still enabled; fence protects dispatch."
	} else {
		r.MigrationDetail += " Legacy schedule disabled."
	}
	r.MigrationDetail += " Successor enablement is configuration; health describes recorded attempts, not current liveness, semantic parity or final decommission."
}
