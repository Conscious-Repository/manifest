package spirits

import (
	"errors"
	"os"
	"path/filepath"

	"manifest/connectorhandoff"
	"manifest/domainextract"
	"manifest/personalemail"
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
	if s.emailDuty(r.Spirit, r.Ritual) {
		s.projectEmailOwnership(r)
		return
	}
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
	if r.Harness == "excalibur" && r.Spirit == "extractor" && (r.Ritual == "aion" || r.Ritual == "real-estate" || r.Ritual == "ooda-email") {
		r.MigrationState, r.MigrationDetail = domainextract.Readiness(s.migrationDataDir, s.root, r.Ritual, r.ConfiguredOwner != "")
		r.LegacyActionable = r.LegacyActionable && s.connectorDispatchGuard(r.Spirit, r.Ritual) == nil
		if f, err := connectorhandoff.DutyFenceSnapshot(s.root, r.Spirit+"/"+r.Ritual); err == nil {
			r.ConfiguredOwner = f.Owner
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

// Email has a dedicated activation receipt; connector-handoff/email.json is
// not written by its worker and cannot establish successor ownership.
func (s *Store) projectEmailOwnership(r *RitualRow) {
	r.ConfiguredOwner = connectorhandoff.Legacy
	r.LegacyActionable = false
	r.MigrationState = "blocked"
	r.MigrationDetail = "Email ownership uncertain: matching dispatch fence and worker activation required."
	f, enabled, err := personalemail.OwnershipSnapshot(s.migrationDataDir, s.root)
	r.FenceProtected = f.Revision > 0 && f.Owner != connectorhandoff.Legacy
	if err != nil {
		r.MigrationDetail += " " + err.Error() + "."
		return
	}
	// A surviving incompatible migration label is an explicit contradiction.
	record, err := connectorhandoff.Read(s.migrationDataDir, "email")
	if err != nil && !errors.Is(err, os.ErrNotExist) || err == nil && record.Owner != "manifest" {
		r.MigrationDetail += " Connector handoff label contradicts activation or is unreadable."
		return
	}
	r.ConfiguredOwner = "manifest"
	r.SuccessorEnabled = enabled
	r.MigrationState = "fence-protected"
	if enabled {
		r.MigrationState = "successor-enabled"
	}
	r.MigrationDetail = "Hash-bound personal-email activation matches dispatch fence; legacy dispatch fenced."
	if !r.LegacyEnabled {
		r.MigrationDetail += " Legacy schedule disabled."
	} else {
		r.MigrationDetail += " Legacy schedule still enabled; fence protects dispatch."
	}
	r.MigrationDetail += " Worker enablement is configuration, not liveness, semantic parity or final decommission."
}
