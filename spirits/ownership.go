package spirits

import (
	"errors"
	"os"
	"path/filepath"

	"manifest/connectorhandoff"
)

// Ownership describes routing intent and the legacy file independently. Neither
// an enablement flag nor a disabled legacy schedule proves a completed handoff.
func (s *Store) projectOwnership(r *RitualRow) {
	r.Harness = s.harnessName
	if r.Harness == "" {
		r.Harness = filepath.Base(filepath.Clean(s.root))
	}
	r.ConfiguredOwner = s.dutyOwners[r.Spirit+"/"+r.Ritual]
	r.LegacyActionable = r.Valid && !r.Retired && r.ConfiguredOwner == ""
	if source := s.connectorSource(r.Spirit, r.Ritual); source != "" {
		record, err := connectorhandoff.Read(s.migrationDataDir, source)
		if err == nil {
			r.MigrationState = string(record.Phase)
			r.ConfiguredOwner = record.Owner
			r.LegacyActionable = r.LegacyActionable && s.connectorDispatchGuard(r.Spirit, r.Ritual) == nil
			r.MigrationDetail = "Persisted connector handoff evidence; rollback owner: " + record.RollbackOwner
			if r.LegacyEnabled && !r.LegacyActionable {
				r.MigrationDetail += "; legacy schedule still enabled: dispatch exclusion unresolved"
			}
			return
		}
		if !errors.Is(err, os.ErrNotExist) {
			r.MigrationState, r.MigrationDetail, r.LegacyActionable = "blocked", "Connector handoff evidence unreadable; legacy dispatch refused.", false
			return
		}
		r.MigrationState = string(connectorhandoff.NotReady)
		r.MigrationDetail = "Legacy connector remains owner; reconciled checkpoint and enforceable dispatch exclusion required."
		if r.ConfiguredOwner != "" {
			r.MigrationState, r.MigrationDetail = "blocked", "Successor flag configured without handoff evidence; polling remains blocked."
		} else {
			r.ConfiguredOwner = "excalibur"
		}
		return
	}
	if r.Retired {
		r.MigrationState, r.MigrationDetail = "retired", r.RetirementReason
		if r.LegacyEnabled {
			r.MigrationState = "retirement-conflict"
			r.MigrationDetail += "; legacy file still enabled: engine schedule must be reconciled"
		}
		return
	}
	if r.ConfiguredOwner != "" {
		r.MigrationState = "handoff-unverified"
		r.MigrationDetail = "Configured for " + r.ConfiguredOwner + "; legacy manual dispatch blocked. State transfer and successor execution require evidence."
		if r.LegacyEnabled {
			r.MigrationState = "ownership-conflict"
			r.MigrationDetail += " Legacy file remains enabled; possible dual dispatch."
		}
		return
	}
	r.ConfiguredOwner = r.Harness
	if r.Harness != "excalibur" {
		return
	}
	r.MigrationState = "legacy-retiring"
	switch r.Spirit + "/" + r.Ritual {
	case "ea-coordinator/email-sync":
		r.MigrationDetail = "Legacy owner; personal email successor and per-thread state transfer are not implemented."
	case "ea-coordinator/granola-sync", "ea-coordinator/pocket-sync":
		r.MigrationDetail = "Legacy owner; Manifest transcript successor is not enabled. Pause, state/approval reconciliation and handoff evidence remain required."
	case "extractor/aion", "extractor/real-estate", "extractor/ooda-email":
		r.MigrationDetail = "Legacy owner; replacement is not enabled. Verified execution and extraction quality comparison remain required; existing model and authority retained."
	default:
		r.MigrationDetail = "Legacy owner; no successor ownership established."
	}
}
